package cliproxy

import (
	"context"
	"fmt"
	"github.com/MoeForge/moego/ojson"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestKiloNativeManagerUX2CapacityJointBridge(t *testing.T) {
	const managerTest = "TestKiloManagerUX2CapacityJointRuntime"
	goBin, managerDir := os.Getenv("KILO_JOINT_GO_BIN"), os.Getenv("KILO_JOINT_MANAGER_DIR")
	if goBin == "" || managerDir == "" {
		t.Skip("UX2 joint runtime not configured")
	}
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	var gateMu sync.Mutex
	gate := make(chan struct{})
	close(gate)
	holding := false
	count := 0
	done := make(chan ux2Reply, 100)
	entered := make(chan struct{}, 100)
	var posts, active, peak atomic.Int32
	ids := ux2NativeIPs(t, f, 6, func(_ int, w http.ResponseWriter, r *http.Request) {
		n := posts.Add(1)
		value := active.Add(1)
		for old := peak.Load(); value > old && !peak.CompareAndSwap(old, value); old = peak.Load() {
		}
		gateMu.Lock()
		wait := gate
		gateMu.Unlock()
		entered <- struct{}{}
		select {
		case <-wait:
		case <-r.Context().Done():
			active.Add(-1)
			return
		}
		active.Add(-1)
		w.Header().Set("Content-Type", "text/event-stream")
		doc := ojson.NewDocString(kiloTerminal)
		_ = doc.Set("response.id", fmt.Sprintf("ux2-joint-%d", n))
		_, _ = fmt.Fprintf(w, "data: %s\n\n", doc.String())
	})
	id := m2Create(t, f, `,"name":"UX2 local capacity"`)
	ux2NativePolicy(t, f, id, 5)
	ips := []string{}
	for i := 1; i <= 6; i++ {
		ips = append(ips, fmt.Sprintf("203.0.113.%d", i))
	}
	var controlMu sync.Mutex
	release := func() error {
		if !holding {
			return nil
		}
		gateMu.Lock()
		close(gate)
		gateMu.Unlock()
		holding = false
		for i := 0; i < count; i++ {
			select {
			case result := <-done:
				if result.err != nil || result.status != 200 {
					return fmt.Errorf("inference status%d: %v", result.status, result.err)
				}
			case <-time.After(10 * time.Second):
				return fmt.Errorf("release did not finish")
			}
		}
		count = 0
		f.manager.SyncKiloAutoEgress(time.Now())
		return nil
	}
	defer func() { controlMu.Lock(); defer controlMu.Unlock(); _ = release() }()
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		controlMu.Lock()
		defer controlMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		doc := ojson.NewDocString("{}")
		switch r.Method + " " + r.URL.Path {
		case "GET /fixture":
			_ = doc.Set("account_id", id)
			_ = doc.Set("proxy_ids", ids)
			_ = doc.Set("specified_ips", ips)
		case "GET /state":
			view, err := f.executor.KiloProxyState()
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			_ = doc.Set("pool", view)
			_ = doc.Set("concurrency", f.manager.CredentialConcurrencySnapshot())
			_ = doc.Set("posts", posts.Load())
			_ = doc.Set("active", active.Load())
			_ = doc.Set("peak", peak.Load())
		case "POST /hold":
			if holding {
				http.Error(w, "already holding", 409)
				return
			}
			body, _ := io.ReadAll(r.Body)
			count = int(ojson.GetBytes(body, "count").Int())
			if count < 1 || count > 100 {
				http.Error(w, "count", 400)
				return
			}
			gateMu.Lock()
			gate = make(chan struct{})
			gateMu.Unlock()
			holding = true
			for i := 0; i < count; i++ {
				go func(n int) {
					done <- ux2Request(context.Background(), f, fmt.Sprintf("joint-%d-%d", posts.Load(), n), `{"model":"gpt-6.1-sol","input":"joint capacity"}`)
				}(i)
			}
			for i := 0; i < count; i++ {
				select {
				case <-entered:
				case <-time.After(15 * time.Second):
					http.Error(w, "upstream plateau missing", 500)
					return
				}
			}
			_ = doc.Set("held", count)
		case "POST /release":
			if err := release(); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			_ = doc.Set("active", active.Load())
		case "POST /sync":
			f.manager.SyncKiloAutoEgress(time.Now())
			_ = doc.Set("ok", true)
		default:
			w.WriteHeader(404)
			return
		}
		_, _ = w.Write(doc.Bytes())
	}))
	defer control.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, goBin, "test", "-json", "-count=1", "-run", "^"+managerTest+"$", "./internal/http/router")
	command.Dir = managerDir
	command.Env = append(kiloAccountsJointEnvironment(f.server.URL, control.URL), "KILO_JOINT_MANAGEMENT_KEY="+kiloManagementKey)
	output, err := command.CombinedOutput()
	t.Logf("Manager UX2 joint:\n%s", output)
	if err != nil {
		t.Fatalf("Manager UX2 joint: %v", err)
	}
	ran, passed := false, false
	for _, line := range strings.Split(string(output), "\n") {
		if ojson.Get(line, "Test").String() != managerTest {
			continue
		}
		switch ojson.Get(line, "Action").String() {
		case "run":
			ran = true
		case "pass":
			passed = true
		case "skip":
			t.Fatal("UX2 joint skipped")
		}
	}
	if !ran || !passed || posts.Load() == 0 || active.Load() != 0 {
		t.Fatalf("joint evidence run=%v pass=%v posts=%d active=%d", ran, passed, posts.Load(), active.Load())
	}
	t.Logf("real Manager/Core routers with dedicated-key CONNECT/TLS inference: POST%d peak%d active0", posts.Load(), peak.Load())
}
