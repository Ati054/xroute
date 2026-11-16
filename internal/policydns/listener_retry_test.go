package policydns

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"os"
	"syscall"
	"testing"
	"time"
)

type resourceLimitedDNSListener struct {
	net.Listener
	remaining int
}

func (l *resourceLimitedDNSListener) Accept() (net.Conn, error) {
	if l.remaining > 0 {
		l.remaining--
		return nil, &net.OpError{Op: "accept", Net: "tcp", Err: os.NewSyscallError("accept", syscall.EMFILE)}
	}
	return l.Listener.Accept()
}

func TestDNSListenerRecoversAfterTemporaryDescriptorExhaustion(t *testing.T) {
	raw, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	r := sharedQueryRuntime(t, map[string]upstream{}, laneConfig{ID: "lane", Final: "reject"})
	r.timeout = 5 * time.Second
	s := &service{ctx: ctx, runtime: r, limit: make(chan struct{}, 2), pending: make(chan struct{}, 8), tcpLimit: make(chan struct{}, 2)}
	s.wait.Add(1)
	go s.serveTCP("lane", &resourceLimitedDNSListener{raw, 2})
	defer func() { cancel(); raw.Close(); s.wait.Wait() }()
	conn, err := net.DialTimeout("tcp4", raw.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	query := dnsQuery(19, "listener-recovery.example.test")
	query[2] = 2 << 3 // Local STATUS reply needs no upstream service.
	frame := binary.BigEndian.AppendUint16(nil, uint16(len(query)))
	if _, err := conn.Write(append(frame, query...)); err != nil {
		t.Fatal(err)
	}
	var response [14]byte
	if _, err := io.ReadFull(conn, response[:]); err != nil {
		t.Fatalf("temporary descriptor pressure disabled DNS/TCP: %v", err)
	}
	if binary.BigEndian.Uint16(response[:2]) != 12 || response[5]&15 != 4 {
		t.Fatalf("unexpected DNS reply: %x", response)
	}
}

func TestDNSAcceptRetryStopsOnShutdownAndPermanentFailure(t *testing.T) {
	temporary := &net.OpError{Op: "accept", Net: "tcp", Err: os.NewSyscallError("accept", syscall.EMFILE)}
	delay := time.Second
	if retryDNSAccept(t.Context(), net.ErrClosed, &delay) {
		t.Fatal("closed listener retried")
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan bool, 1)
	go func() { done <- retryDNSAccept(ctx, temporary, &delay) }()
	cancel()
	select {
	case retry := <-done:
		if retry {
			t.Fatal("shutdown retried accept")
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("shutdown waited for backoff")
	}
}
