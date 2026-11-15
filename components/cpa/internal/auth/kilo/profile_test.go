package kilo

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
)

func TestKiloProfileProjectionAndValidation(t *testing.T) {
	for _, tc := range []struct {
		name, body                string
		status, wantStatus, count int
		personal                  bool
	}{
		{"missing", `{}`, 200, 0, 0, true}, {"null", `{"organizations":null}`, 200, 0, 0, true},
		{"filtered", `{"organizations":[null,3,{"id":"bad id"},{"id":"org-1","name":false},{"id":"org_2","name":"中文"}],"hasPersonalAccount":false,"token":"hidden"}`, 200, 0, 2, false},
		{"wrong-list", `{"organizations":{}}`, 200, 502, 0, false}, {"malformed", `invalid-secret`, 200, 502, 0, false}, {"array", `[]`, 200, 502, 0, false},
		{"status", `{"token":"hidden"}`, 401, 502, 0, false}, {"oversize", strings.Repeat("x", ProfileMaxBytes+1), 200, 502, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			w.WriteHeader(tc.status)
			_, _ = io.WriteString(w, tc.body)
			client := &http.Client{Transport: deviceProtocolTransport{w.Result()}}
			choices, err := FetchProfile(context.Background(), client, "synthetic-profile-key")
			if tc.wantStatus != 0 {
				var problem *ProfileError
				if !errors.As(err, &problem) || problem.Status != tc.wantStatus || strings.Contains(err.Error(), "hidden") {
					t.Fatalf("error=%v", err)
				}
				return
			}
			if err != nil || len(choices.Organizations) != tc.count || choices.HasPersonalAccount != tc.personal {
				t.Fatalf("choices=%+v err=%v", choices, err)
			}
		})
	}
	doc := ojson.NewDocString(`{"organizations":[{"id":"org-long"}]}`)
	_ = doc.Set("organizations.0.name", strings.Repeat("中", 300))
	w := httptest.NewRecorder()
	_, _ = w.Write(doc.Bytes())
	choices, err := FetchProfile(context.Background(), &http.Client{Transport: deviceProtocolTransport{w.Result()}}, "key")
	if err != nil || len([]rune(choices.Organizations[0].Name)) != 256 {
		t.Fatal("organization label limit")
	}
	for _, id := range []string{"", "bad id", "bad.org", strings.Repeat("a", 129), "中文"} {
		if ValidOrganizationID(id) {
			t.Fatal("invalid organization accepted")
		}
	}
}

func TestKiloProfileBudgetAndParentCancellation(t *testing.T) {
	for _, caller := range []bool{false, true} {
		t.Run(map[bool]string{false: "budget", true: "parent"}[caller], func(t *testing.T) {
			entered, closed := make(chan struct{}), make(chan struct{})
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(200)
				w.(http.Flusher).Flush()
				close(entered)
				<-r.Context().Done()
				close(closed)
			}))
			defer server.Close()
			roots := x509.NewCertPool()
			roots.AddCert(server.Certificate())
			transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, ServerName: server.Certificate().DNSNames[0]}, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				if address != "api.kilo.ai:443" {
					return nil, errors.New("unexpected destination")
				}
				return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
			}}
			client := &http.Client{Transport: transport}
			defer client.CloseIdleConnections()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			budget := 100 * time.Millisecond
			if caller {
				budget = time.Minute
			}
			done := make(chan error, 1)
			go func() { _, err := fetchProfileBudget(ctx, client, "synthetic", budget); done <- err }()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("profile not entered")
			}
			if caller {
				cancel()
			}
			select {
			case err := <-done:
				if caller {
					if !errors.Is(err, context.Canceled) {
						t.Fatalf("parent=%v", err)
					}
				} else {
					var p *ProfileError
					if !errors.As(err, &p) || p.Status != 503 || ctx.Err() != nil {
						t.Fatalf("budget=%v", err)
					}
				}
			case <-time.After(5 * time.Second):
				t.Fatal("profile not canceled")
			}
			select {
			case <-closed:
			case <-time.After(5 * time.Second):
				t.Fatal("profile socket retained")
			}
		})
	}
}
