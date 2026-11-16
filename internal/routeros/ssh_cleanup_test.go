package routeros

import (
	"context"
	"errors"
	"io"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Ati054/xroute/internal/managedidentity"
	"golang.org/x/crypto/ssh"
)

func TestManagedImportCleanupInventoriesOnceAndValidatesBeforeRemoval(t *testing.T) {
	names := []string{managedidentity.Literal("XROUTE-apply-0123456789ab.rsc"), managedidentity.Literal("XROUTE-rollback-abcdef012345.rsc")}
	command, receipt, err := managedImportCleanupCommand(append(names, names[0]))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(command, "/file find") != 1 || strings.Count(command, "/file remove") != 1 {
		t.Fatalf("cleanup must inventory and remove once: %s", command)
	}
	remove := strings.Index(command, "/file remove")
	for _, check := range []string{`name="` + names[0] + `"`, `name="` + names[1] + `"`, `$count0 > 1`, `$count1 > 1`} {
		if at := strings.Index(command, check); at < 0 || at >= remove {
			t.Fatalf("missing validation before removal: %q", check)
		}
	}
	if strings.Index(command, `:put "`+receipt+`"`) <= remove {
		t.Fatal("completion receipt precedes removal")
	}
	plain, same, _ := managedImportCleanupCommand(names)
	if plain != command || same != receipt {
		t.Fatal("duplicate names changed cleanup or its receipt")
	}
	if command, receipt, err := managedImportCleanupCommand(nil); err != nil || command != "" || receipt != "" {
		t.Fatal("empty cleanup should make no SSH request")
	}
	for _, unsafe := range []string{"variables.rsc", "../" + names[0], names[0] + `"; /system/reboot`} {
		if command, _, err := managedImportCleanupCommand(append(names, unsafe)); err == nil || command != "" {
			t.Fatalf("accepted an unsafe cleanup target %q", unsafe)
		}
	}
}

func TestImportCleanupRequiresReceiptAndHonorsCancellation(t *testing.T) {
	names := []string{managedidentity.Literal("XROUTE-apply-0123456789ab.rsc")}
	command, receipt, err := managedImportCleanupCommand(names)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"success", "silent", "error", "trailing", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			config := &ssh.ServerConfig{PasswordCallback: func(ssh.ConnMetadata, []byte) (*ssh.Permissions, error) { return nil, nil }}
			config.AddHostKey(testSSHSigner(t))
			entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
			defer func() {
				close(release)
				_ = listener.Close()
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Error("cleanup fixture server did not finish")
				}
			}()
			go func() {
				defer close(done)
				raw, acceptErr := listener.Accept()
				if acceptErr != nil {
					return
				}
				defer raw.Close()
				connection, channels, requests, handshakeErr := ssh.NewServerConn(raw, config)
				if handshakeErr != nil {
					t.Error(handshakeErr)
					return
				}
				defer connection.Close()
				go ssh.DiscardRequests(requests)
				for incoming := range channels {
					channel, requests, acceptErr := incoming.Accept()
					if acceptErr != nil {
						return
					}
					for request := range requests {
						if request.Type != "exec" {
							_ = request.Reply(false, nil)
							continue
						}
						var payload struct{ Command string }
						if err := ssh.Unmarshal(request.Payload, &payload); err != nil || payload.Command != command {
							t.Error("unexpected cleanup command")
						}
						_ = request.Reply(true, nil)
						close(entered)
						switch mode {
						case "success":
							_, _ = io.WriteString(channel, receipt+"\n")
						case "error":
							_, _ = io.WriteString(channel, "ERROR:"+strings.TrimPrefix(receipt, "OK:")+"\n")
						case "trailing":
							_, _ = io.WriteString(channel, receipt+"\nuntrusted output\n")
						case "cancel":
							<-release
							return
						}
						_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
						_ = channel.Close()
						return
					}
				}
			}()
			_, portText, _ := net.SplitHostPort(listener.Addr().String())
			port, _ := strconv.Atoi(portText)
			transport, err := NewSSHTransport(SSHOptions{Host: "127.0.0.1", Port: port, Username: "fixture", Password: "fixture", KnownHostsPath: filepath.Join(t.TempDir(), "known_hosts"), Timeout: 10 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			result := make(chan error, 1)
			go func() { result <- transport.RemoveManagedImports(ctx, names...) }()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("cleanup session did not start")
			}
			if mode == "cancel" {
				cancel()
			}
			select {
			case err := <-result:
				if mode == "success" && err != nil || mode != "success" && err == nil {
					t.Fatalf("mode %s returned %v", mode, err)
				}
				if mode == "cancel" && !errors.Is(err, context.Canceled) {
					t.Fatalf("cancellation was lost: %v", err)
				}
				if err != nil && strings.Contains(err.Error(), "untrusted output") {
					t.Fatal("cleanup exposed remote output")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("cleanup did not finish after cancellation or server exit")
			}
		})
	}
}
