package routeros

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"
)

type retentionRoundTripper func(*http.Request) (*http.Response, error)

func (f retentionRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestPruneInventoryUsesMaintenanceClientAndParentDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	wantDeadline, _ := ctx.Deadline()
	ordinaryCalls, maintenanceCalls := 0, 0
	client := &Client{baseURL: "https://router.example:59443", username: "fixture", password: "fixture"}
	client.http = &http.Client{Transport: retentionRoundTripper(func(*http.Request) (*http.Response, error) {
		ordinaryCalls++
		return nil, errors.New("short inventory client must not be used")
	})}
	client.actionHTTP = &http.Client{Transport: retentionRoundTripper(func(request *http.Request) (*http.Response, error) {
		maintenanceCalls++
		if deadline, ok := request.Context().Deadline(); !ok || !deadline.Equal(wantDeadline) {
			t.Error("inventory lost the caller's deadline")
		}
		if request.Method != http.MethodGet || request.URL.Path != "/rest/file" || request.URL.Query().Get(".proplist") != ".id,name" {
			t.Error("inventory must request only identifiers and names")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`[]`)), Request: request}, nil
	})}
	if _, err := client.PruneManagedFiles(ctx, "usb1/xroute"); err != nil {
		t.Fatal(err)
	}
	if ordinaryCalls != 0 || maintenanceCalls != 1 {
		t.Fatalf("unexpected client calls: ordinary=%d maintenance=%d", ordinaryCalls, maintenanceCalls)
	}
	cancel()
	client.actionHTTP.Transport = retentionRoundTripper(func(request *http.Request) (*http.Response, error) {
		return nil, request.Context().Err()
	})
	if _, err := client.PruneManagedFiles(ctx, "usb1/xroute"); !errors.Is(err, context.Canceled) {
		t.Fatalf("inventory ignored parent cancellation: %v", err)
	}
}

func TestPruneManagedFilesRetainsThreeBackupGenerationsAndRemovesInstallArtifacts(t *testing.T) {
	rows := `[
		{".id":"*1","name":"SB-GATEWAY-20260920T010000Z-aaaaaaaaaaaa.backup"},
		{".id":"*2","name":"SB-GATEWAY-20260920T010000Z-aaaaaaaaaaaa-export.rsc"},
		{".id":"*3","name":"SB-GATEWAY-20260920T020000Z-bbbbbbbbbbbb.backup"},
		{".id":"*4","name":"SB-GATEWAY-20260920T020000Z-bbbbbbbbbbbb-export.rsc"},
		{".id":"*5","name":"SB-GATEWAY-20260920T030000Z-cccccccccccc.backup"},
		{".id":"*6","name":"SB-GATEWAY-20260920T040000Z-dddddddddddd.backup"},
		{".id":"*7","name":"SB-GATEWAY-20260920T050000Z-eeeeeeeeeeee.backup"},
		{".id":"*8","name":"usb1/sb-gateway/sb-gateway-1.6.4-linux-arm64.tar"},
		{".id":"*9","name":"usb1/sb-gateway/sb-gateway-1.6.4-linux-arm64.tar.sha256"},
		{".id":"*10","name":"usb1/sb-gateway/install.rsc"},
		{".id":"*11","name":"usb1/sb-gateway/variables.rsc"},
		{".id":"*12","name":"usb1/sb-gateway/config/generated/xray.json"},
		{".id":"*13","name":"usb1/sb-gateway/data/lifecycle-uploads/sb-gateway-1.7.0-linux-arm64.tar"},
		{".id":"*14","name":"usb1/sb-gateway/routeros-ca.crt"},
		{".id":"*15","name":"autosupout.rif"},
		{".id":"*16","name":"foreign.backup"},
		{".id":"*17","name":"usb1/sb-gateway/container-startup.rsc"},
		{".id":"*18","name":"usb1/sb-gateway/xroute-1.7.0-rc.1-linux-arm64.tar"},
		{".id":"*19","name":"usb1/sb-gateway/xroute-1.7.0-rc.1-linux-arm64.tar.sha256"},
		{".id":"*20","name":"usb1/foreign/xroute-1.7.0-rc.1-linux-arm64.tar"},
		{".id":"*21","name":"usb1/sb-gateway/data/lifecycle-uploads/xroute-1.7.0-rc.1-linux-arm64.tar"},
		{".id":"*22","name":"usb1/sb-gateway/xroute-latest-linux-arm64.tar"}
	]`
	deleted := []string{}
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		if request.Method == http.MethodGet && request.URL.Path == "/rest/file" {
			_, _ = response.Write([]byte(rows))
			return
		}
		if request.Method == http.MethodDelete {
			deleted = append(deleted, request.URL.Path)
			_, _ = response.Write([]byte(`{}`))
			return
		}
		http.Error(response, "unexpected request", http.StatusBadRequest)
	}))
	defer server.Close()
	client := newTestClient(t, server)
	defer client.CloseIdleConnections()

	result, err := client.PruneManagedFiles(context.Background(), "usb1/sb-gateway")
	if err != nil {
		t.Fatal(err)
	}
	if result.Backups != 4 || result.Install != 7 {
		t.Fatalf("prune result = %#v", result)
	}
	sort.Strings(deleted)
	want := []string{"/rest/file/*1", "/rest/file/*10", "/rest/file/*11", "/rest/file/*17", "/rest/file/*18", "/rest/file/*19", "/rest/file/*2", "/rest/file/*3", "/rest/file/*4", "/rest/file/*8", "/rest/file/*9"}
	sort.Strings(want)
	if len(deleted) != len(want) {
		t.Fatalf("deleted = %#v", deleted)
	}
	for index := range want {
		if deleted[index] != want[index] {
			t.Fatalf("deleted = %#v", deleted)
		}
	}
}

func TestPruneManagedFilesRejectsUnsafeStorageRootBeforeNetwork(t *testing.T) {
	client := &Client{}
	for _, root := range []string{"usb1", "../usb1/sb-gateway", "usb1/../flash", "flash/xroute", "pub", "xroute/./data", ""} {
		if _, err := client.PruneManagedFiles(context.Background(), root); err == nil {
			t.Fatalf("unsafe storage root accepted: %q", root)
		}
	}
}

func TestPruneManagedFilesSupportsConfirmedCHRSystemDirectory(t *testing.T) {
	for _, architecture := range []string{"x86", "x86_64"} {
		t.Run(architecture, func(t *testing.T) {
			resourceRead := false
			deleted := []string{}
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/rest/system/resource":
					resourceRead = true
					_, _ = io.WriteString(w, `{"board-name":"CHR","architecture-name":"`+architecture+`"}`)
				case r.Method == http.MethodGet && r.URL.Path == "/rest/file":
					if !resourceRead {
						t.Error("system files inventoried before confirming CHR")
					}
					_, _ = io.WriteString(w, `[
						{".id":"*1","name":"xroute/install.rsc"},
						{".id":"*2","name":"xroute/state/keep.json"},
						{".id":"*3","name":"other/install.rsc"},
						{".id":"*4","name":"xroute-other/install.rsc"},
						{".id":"*5","name":"xroute/xroute-1.7.2-linux-amd64.tar"},
						{".id":"*6","name":"xroute/users.txt"},
						{".id":"*7","name":"xroute/setup.rsc"}
					]`)
				case r.Method == http.MethodDelete:
					deleted = append(deleted, r.URL.Path)
					_, _ = io.WriteString(w, `{}`)
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					http.Error(w, "unexpected", http.StatusBadRequest)
				}
			}))
			defer server.Close()
			client := newTestClient(t, server)
			defer client.CloseIdleConnections()
			got, err := client.PruneManagedFiles(t.Context(), "xroute")
			if err != nil {
				t.Fatal(err)
			}
			sort.Strings(deleted)
			if got.Install != 3 || got.Backups != 0 || strings.Join(deleted, ",") != "/rest/file/*1,/rest/file/*5,/rest/file/*7" {
				t.Fatalf("unexpected cleanup: result=%+v deleted=%v", got, deleted)
			}
		})
	}
}

func TestPruneSystemDirectoryRequiresLiveCHRProofBeforeFileAccess(t *testing.T) {
	for _, resource := range []string{
		`{"board-name":"RB5009","architecture-name":"arm64"}`,
		`{"board-name":"CHR","architecture-name":"arm64"}`,
		`{"board-name":"RouterBOARD","architecture-name":"x86_64"}`,
		`{"board-name":"CHR"}`,
		`[]`,
	} {
		t.Run(resource, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/rest/system/resource" {
					t.Errorf("file access without valid platform proof: %s %s", r.Method, r.URL.Path)
					http.Error(w, "forbidden", http.StatusForbidden)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, resource)
			}))
			defer server.Close()
			client := newTestClient(t, server)
			defer client.CloseIdleConnections()
			if _, err := client.PruneManagedFiles(t.Context(), "xroute"); err == nil {
				t.Fatal("unconfirmed system disk accepted")
			}
		})
	}
}
