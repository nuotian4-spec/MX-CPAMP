package cliproxy

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/home"
	native "github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor"
)

// The loopback peer speaks RESP to the real Home client, including its subscriber handshake.
func m5HomePeer(t *testing.T, refresh func(string) string) (*home.Client, config.HomeConfig) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	connections := []net.Conn{}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			connections = append(connections, conn)
			mu.Unlock()
			go func() {
				defer conn.Close()
				r := bufio.NewReader(conn)
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					if !strings.HasPrefix(line, "*") {
						return
					}
					count, err := strconv.Atoi(strings.TrimSpace(line[1:]))
					if err != nil {
						return
					}
					args := make([]string, count)
					for i := range args {
						line, err = r.ReadString('\n')
						if err != nil || !strings.HasPrefix(line, "$") {
							return
						}
						size, err := strconv.Atoi(strings.TrimSpace(line[1:]))
						if err != nil || size < 0 {
							return
						}
						data := make([]byte, size+2)
						if _, err = io.ReadFull(r, data); err != nil {
							return
						}
						args[i] = string(data[:size])
					}
					reply := "+OK\r\n"
					switch strings.ToUpper(args[0]) {
					case "HELLO":
						reply = "%6\r\n$6\r\nserver\r\n$5\r\nredis\r\n$5\r\nproto\r\n:3\r\n$2\r\nid\r\n:1\r\n$4\r\nmode\r\n$10\r\nstandalone\r\n$4\r\nrole\r\n$6\r\nmaster\r\n$7\r\nmodules\r\n*0\r\n"
					case "PING":
						reply = "+PONG\r\n"
					case "GET":
						body := "{}"
						if len(args) == 2 && ojson.Get(args[1], "type").String() == "refresh" {
							body = refresh(args[1])
						}
						reply = fmt.Sprintf("$%d\r\n%s\r\n", len(body), body)
					case "SUBSCRIBE":
						reply = "*3\r\n$9\r\nsubscribe\r\n$6\r\nconfig\r\n:1\r\n"
					}
					if _, err = io.WriteString(conn, reply); err != nil {
						return
					}
				}
			}()
		}
	}()
	host, portText, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(portText)
	cfg := config.HomeConfig{Enabled: true, Host: host, Port: port, DisableClusterDiscovery: true}
	client := home.New(cfg)
	old := home.Current()
	home.SetCurrent(client)
	ctx, cancel := context.WithCancel(context.Background())
	ready, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		_ = client.RunConfigSubscriberLifetime(ctx, func([]byte) error { return nil }, func() { close(ready) })
	}()
	t.Cleanup(func() {
		cancel()
		client.Close()
		_ = listener.Close()
		mu.Lock()
		for _, conn := range connections {
			_ = conn.Close()
		}
		mu.Unlock()
		<-done
		home.SetCurrent(old)
	})
	select {
	case <-ready:
	case <-done:
		t.Fatal("Home subscriber exited before ready")
	case <-time.After(5 * time.Second):
		t.Fatal("Home subscriber did not become ready")
	}
	return client, cfg
}

func TestKiloQuotaNativeHomeRESPAndNoLocalFallback(t *testing.T) {
	for _, kind := range []string{"auth", "envelope", "no_outer", "unavailable", "rejected"} {
		t.Run(kind, func(t *testing.T) {
			f := newKiloNativeFixture(t, kiloSuccessStream, true)
			id := m2Create(t, f, "")
			m4oDirect(t, f, id)
			a := m5Codex(t, f, "codex-a.json", "outer-a", "synthetic-outer-old", true, false)
			var refreshes, usages atomic.Int32
			client, cfg := m5HomePeer(t, func(key string) string {
				refreshes.Add(1)
				if ojson.Get(key, "auth_index").String() != a.Index || ojson.Get(key, "access_token_sha256").String() == "" {
					t.Error("Home GET refresh identity/hash absent")
				}
				if kind == "rejected" {
					return `{"error":{"type":"error","message":"private-upstream-secret"}}`
				}
				result := a.Clone()
				result.Metadata["access_token"] = "synthetic-outer-home"
				result.Metadata["expired"] = time.Now().Add(7 * 24 * time.Hour).Format(time.RFC3339)
				if kind == "no_outer" {
					result.Metadata["access_token"] = ""
					result.Metadata["siwc"] = map[string]any{"access_token": "synthetic-siwc-home"}
				}
				doc := ojson.NewDocString(`{}`)
				if kind == "envelope" {
					_ = doc.Set("auth", result)
					_ = doc.Set("auth_index", a.Index)
				} else {
					_ = doc.Set("id", result.ID)
					_ = doc.Set("provider", "codex")
					_ = doc.Set("metadata", result.Metadata)
				}
				return doc.String()
			})
			f.cfg.Home = cfg
			f.manager.SetConfig(f.cfg)
			f.manager.RegisterExecutor(native.NewCodexExecutor(f.cfg))
			if kind == "unavailable" {
				client.Close()
			}
			f.usageHandler = func(w http.ResponseWriter, r *http.Request) {
				usages.Add(1)
				if r.Header.Get("Authorization") != "Bearer synthetic-outer-home" || r.Header.Get("ChatGPT-Account-Id") != "outer-a" {
					t.Error("Home outer not selected")
				}
				_, _ = io.WriteString(w, m5UsageBody)
			}
			m5Bind(t, f, id, 0, `{"kind":"cpa_codex","credential_id":"codex-a.json","chatgpt_account_id":"outer-a"}`)
			view := m5Query(t, f, id, 1)
			if kind == "auth" || kind == "envelope" {
				m5Assert(t, view, "success", "")
				if refreshes.Load() != 1 || usages.Load() != 1 {
					t.Fatal("Home counts")
				}
			} else {
				if ojson.GetBytes(view, "query.status").String() != "failed" || usages.Load() != 0 {
					t.Fatalf("Home failure fell back: %s", view)
				}
			}
			if f.oauthConnects.Load() != 0 || f.posts.Load() != 0 {
				t.Fatal("Home delegated to local OAuth or model")
			}
			t.Logf("Home %s real RESP GET refresh%d, usageGET%d, localOAuth0/model0; remote service behavior not asserted", kind, refreshes.Load(), usages.Load())
		})
	}
}
