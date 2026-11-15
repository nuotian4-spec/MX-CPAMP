package cliproxy

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/api"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilo"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/access"
	sdkauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/auth"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"gopkg.in/yaml.v3"
)

// TestKiloNativeBrowserHost exposes only test routers and synthetic upstreams.
// A separate Manager test process serves the final production Web artifact.
func TestKiloNativeBrowserHost(t *testing.T) {
	goBin, managerDir := os.Getenv("KILO_JOINT_GO_BIN"), os.Getenv("KILO_JOINT_MANAGER_DIR")
	if os.Getenv("KILO_BROWSER_RECEIPT") == "" || os.Getenv("KILO_BROWSER_PANEL") == "" || goBin == "" || managerDir == "" {
		t.Skip("browser host is started explicitly with a receipt and final panel")
	}
	var mode atomic.Value
	mode.Store("completed")
	var sequence, usageCalls, profileCalls, deviceCalls, canceled, running atomic.Int32
	var usageStatus atomic.Int32
	usageStatus.Store(200)
	var approved atomic.Bool
	release := make(chan struct{}, 16)
	var f *kiloNativeFixture
	f = newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
		<-f.requests
		running.Add(1)
		defer running.Add(-1)
		n := sequence.Add(1)
		current := mode.Load().(string)
		if current == "slow" {
			select {
			case <-release:
			case <-r.Context().Done():
				canceled.Add(1)
				return
			}
		}
		if current == "failed" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, `{"error":{"code":"synthetic_failure","message":"synthetic-private-upstream"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if current == "quota_exhausted" {
			_, _ = io.WriteString(w, "data: "+m6QuotaFailure+"\n\ndata: "+kiloTerminal+"\n\n")
			return
		}
		if r.URL.Path == "/api/v1/chat/completions" {
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"合成 Auto Free 输出 <b>纯文本</b>\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
			return
		}
		doc := ojson.NewDocString(kiloTerminal)
		_ = doc.Set("response.id", fmt.Sprintf("m10-browser-%d", n))
		_ = doc.Set("response.output.0.content.0.text", "合成 Normal 输出 <script>纯文本</script>；本轮未访问真实模型。")
		if current == "incomplete" {
			_ = doc.Set("type", "response.incomplete")
			_ = doc.Set("response.status", "incomplete")
			_ = doc.Set("response.incomplete_details.reason", "max_output_tokens")
		}
		_, _ = fmt.Fprintf(w, "data: %s\n\n", doc.String())
	}, true)
	f.allowAutoFree, f.allowDirect = true, true
	activeID := m2Create(t, f, `,"name":"UX1 已查询额度 · 共享出口"`)
	disabledID := m2Create(t, f, `,"name":"UX1 已关联未查询 · 人工停用","enabled":false`)
	restrictedID := m2Create(t, f, `,"name":"UX1 来源失效 · 专属出口"`)
	m5Codex(t, f, "m10-codex-a.json", "m10-outer-a", "synthetic-outer-a", false, false)
	m5Codex(t, f, "m10-codex-b.json", "m10-outer-b", "synthetic-outer-b", false, false)
	f.usageHandler = func(w http.ResponseWriter, r *http.Request) {
		usageCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if status := int(usageStatus.Load()); status != 200 {
			w.WriteHeader(status)
			_, _ = io.WriteString(w, `{"error":"synthetic-private-usage"}`)
			return
		}
		doc := ojson.NewDocString(m5UsageBody)
		if mode.Load().(string) == "quota_exhausted" {
			_ = doc.Set("rate_limit.primary_window.used_percent", 100)
			_ = doc.Set("rate_limit.primary_window.reset_after_seconds", 7200)
		}
		_, _ = w.Write(doc.Bytes())
	}
	f.profileHandler = func(w http.ResponseWriter, r *http.Request) {
		profileCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"organizations":[{"id":"m10-org-a","name":"合成组织A"},{"id":"m10-org-b","name":"合成组织B"}],"hasPersonalAccount":true}`)
	}
	f.deviceHandler = func(w http.ResponseWriter, r *http.Request) {
		deviceCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && r.URL.Path == "/api/device-auth/codes" {
			_, _ = io.WriteString(w, `{"code":"m10-private-device","user_code":"M10-LOCAL","verificationUrl":"https://app.kilo.ai/device","expiresIn":600}`)
		} else if approved.Load() {
			_, _ = io.WriteString(w, `{"token":"synthetic-kilo-token","userEmail":"m10@example.invalid"}`)
		} else {
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, `{"status":"pending"}`)
		}
	}
	samples, occupancy := seedUX1BrowserSamples(t, f, activeID, disabledID, restrictedID)
	defer occupancy(false)
	gate := &kiloBrowserResponseGate{requests: map[string]map[string]int{"a": {}, "b": {}}}
	startKiloBrowserServer(t, f, gate, "a")
	defer f.server.Close()
	peer := newKiloBrowserPeer(t, f.cfg)
	startKiloBrowserServer(t, peer, gate, "b")
	defer peer.server.Close()
	peerID := m2Create(t, peer, `,"name":"Core B 独立账号"`)
	gate.requests["b"] = map[string]int{}
	defer gate.release()
	proxyFaults := map[string]*kiloBrowserFileFault{
		"a": {path: filepath.Join(config.KiloStateDirectory(f.cfg), kilo.StateFileName)},
		"b": {path: filepath.Join(config.KiloStateDirectory(peer.cfg), kilo.StateFileName)},
	}
	defer func() {
		for _, fault := range proxyFaults {
			if err := fault.set(false); err != nil {
				t.Error(err)
			}
		}
	}()
	stop := make(chan struct{})
	var stopOnce sync.Once
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		switch r.Method + " " + r.URL.Path {
		case "GET /state":
			doc := ojson.NewDocString(`{}`)
			_ = doc.Set("account_ids", map[string]string{"active": activeID, "disabled": disabledID, "restricted": restrictedID})
			_ = doc.Set("ux1_samples", samples)
			_ = doc.Set("model_posts", f.posts.Load())
			_ = doc.Set("model_running", running.Load())
			_ = doc.Set("model_canceled", canceled.Load())
			_ = doc.Set("usage_calls", usageCalls.Load())
			_ = doc.Set("profile_calls", profileCalls.Load())
			_ = doc.Set("device_calls", deviceCalls.Load())
			_ = doc.Set("results", len(f.hook.snapshot()))
			_ = doc.Set("concurrency", f.manager.CredentialConcurrencySnapshot())
			_ = doc.Set("model_mode", mode.Load())
			_ = doc.Set("cores", map[string]any{
				"a": map[string]any{"account_ids": []string{activeID, disabledID, restrictedID}, "results": len(f.hook.snapshot()), "concurrency": f.manager.CredentialConcurrencySnapshot()},
				"b": map[string]any{"account_ids": []string{peerID}, "results": len(peer.hook.snapshot()), "concurrency": peer.manager.CredentialConcurrencySnapshot()},
			})
			_ = doc.Set("delay", gate.snapshot())
			_ = doc.Set("proxy_storage_failure", map[string]bool{"a": proxyFaults["a"].isEnabled(), "b": proxyFaults["b"].isEnabled()})
			_, _ = w.Write(doc.Bytes())
		case "POST /gate":
			body, _ := io.ReadAll(r.Body)
			core, method, path := ojson.GetBytes(body, "core").String(), ojson.GetBytes(body, "method").String(), ojson.GetBytes(body, "path").String()
			if (core != "a" && core != "b") || (method != "GET" && method != "PATCH") || !strings.HasPrefix(path, "/v8/management/kilo/") || strings.ContainsAny(path, "?#") {
				w.WriteHeader(400)
				return
			}
			if !gate.arm(core, method, path) {
				w.WriteHeader(409)
				return
			}
			_, _ = io.WriteString(w, `{"armed":true}`)
		case "POST /gate/release":
			gate.release()
			_, _ = io.WriteString(w, `{"released":true}`)
		case "POST /options":
			body, _ := io.ReadAll(r.Body)
			if v := ojson.GetBytes(body, "occupancy"); v.Exists() {
				occupancy(v.Bool())
			}
			core := ojson.GetBytes(body, "core").String()
			if core == "" {
				core = "a"
			}
			target := f
			if core == "b" {
				target = peer
			} else if core != "a" {
				w.WriteHeader(400)
				return
			}
			if value := ojson.GetBytes(body, "model_mode"); value.Exists() {
				switch value.String() {
				case "completed", "incomplete", "failed", "slow", "quota_exhausted":
					mode.Store(value.String())
				default:
					w.WriteHeader(400)
					return
				}
			}
			if value := ojson.GetBytes(body, "device_approved"); value.Exists() {
				approved.Store(value.Bool())
			}
			if value := ojson.GetBytes(body, "usage_status"); value.Exists() {
				usageStatus.Store(int32(value.Int()))
			}
			if value := ojson.GetBytes(body, "organization_id"); value.Exists() {
				f.expectedOrganization.Store(value.String())
			}
			if value := ojson.GetBytes(body, "storage_failure"); value.Exists() {
				if value.Bool() {
					target.manager.SetStore(m2FailStore{Store: m2FileStore(target), save: true})
				} else {
					target.manager.SetStore(m2FileStore(target))
				}
			}
			if value := ojson.GetBytes(body, "proxy_storage_failure"); value.Exists() {
				if err := proxyFaults[core].set(value.Bool()); err != nil {
					w.WriteHeader(409)
					_, _ = io.WriteString(w, `{"error":"synthetic state file fault could not be changed"}`)
					return
				}
			}
			_, _ = io.WriteString(w, `{"ok":true}`)
		case "POST /release":
			release <- struct{}{}
			_, _ = io.WriteString(w, `{"ok":true}`)
		case "POST /advance":
			id := r.URL.Query().Get("id")
			if id == "" {
				id = activeID
			}
			view, err := f.manager.KiloProbes(id)
			if err != nil {
				w.WriteHeader(404)
				return
			}
			var due *time.Time
			for _, next := range []*time.Time{view.Health.NextRunAt, view.ModelProbe.NextRunAt} {
				if next != nil && (due == nil || next.Before(*due)) {
					due = next
				}
			}
			if due == nil {
				w.WriteHeader(409)
				return
			}
			offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
			f.manager.ProcessKiloProbes(t.Context(), due.Add(time.Duration(offset)*time.Second))
			_, _ = io.WriteString(w, `{"scheduled":true}`)
		case "POST /recover":
			id := r.URL.Query().Get("id")
			if id == "" {
				id = restrictedID
			}
			view := f.manager.KiloRestriction(id)
			if view.RecoverAt == nil {
				w.WriteHeader(409)
				return
			}
			offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
			f.manager.ProcessKiloRecovery(t.Context(), view.RecoverAt.Add(time.Duration(offset)*time.Second))
			doc := ojson.NewDocString(`{}`)
			_ = doc.Set("restriction", f.manager.KiloRestriction(id))
			_, _ = w.Write(doc.Bytes())
		case "POST /stop":
			gate.release()
			stopOnce.Do(func() { close(stop) })
			_, _ = io.WriteString(w, `{"stopping":true}`)
		case "GET /wait-stop":
			select {
			case <-stop:
				_, _ = io.WriteString(w, `{"stopped":true}`)
			case <-r.Context().Done():
			}
		default:
			w.WriteHeader(404)
		}
	}))
	defer control.Close()
	command := exec.Command(goBin, "test", "-v", "-count=1", "-timeout=2h", "-run", "^TestKiloManagerBrowserHost$", "./internal/http/router")
	command.Dir = managerDir
	command.Env = append(os.Environ(), "KILO_JOINT_CPA_URL="+f.server.URL, "KILO_BROWSER_CPA_ALIAS="+peer.server.URL, "KILO_JOINT_CONTROL_URL="+control.URL, "KILO_JOINT_MANAGEMENT_KEY="+kiloManagementKey)
	output, err := command.CombinedOutput()
	t.Logf("Manager browser host:\n%s", output)
	if err != nil {
		t.Fatalf("Manager browser host exited: %v", err)
	}
	occupancy(false)
	f.manager.StopKiloProbes()
	peer.manager.StopKiloProbes()
	assertUX1BrowserReleased(t, f, len(f.hook.snapshot()))
	assertUX1BrowserReleased(t, peer, len(peer.hook.snapshot()))
	t.Logf("browser host stopped; model=%d usage=%d profile=%d device=%d canceled=%d", f.posts.Load(), usageCalls.Load(), profileCalls.Load(), deviceCalls.Load(), canceled.Load())
}

type kiloBrowserFileFault struct {
	mu      sync.Mutex
	path    string
	enabled bool
}

func (f *kiloBrowserFileFault) isEnabled() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.enabled
}

// The original file is retained byte-for-byte while a directory forces real writes to fail.
func (f *kiloBrowserFileFault) set(enabled bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.enabled == enabled {
		return nil
	}
	if enabled {
		if err := os.Rename(f.path, f.path+".saved"); err != nil {
			return err
		}
		if err := os.Mkdir(f.path, 0700); err != nil {
			_ = os.Rename(f.path+".saved", f.path)
			return err
		}
	} else {
		if err := os.Remove(f.path); err != nil {
			return err
		}
		if err := os.Rename(f.path+".saved", f.path); err != nil {
			return err
		}
	}
	f.enabled = enabled
	return nil
}

// Peers share only the synthetic network; managers, auth stores and Kilo state are independent.
func newKiloBrowserPeer(t *testing.T, source *config.Config) *kiloNativeFixture {
	t.Helper()
	raw, err := yaml.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	if err := yaml.Unmarshal(raw, cfg); err != nil {
		t.Fatal(err)
	}
	cfg.AuthDir, cfg.Kilo.StateDir = t.TempDir(), t.TempDir()
	f := &kiloNativeFixture{cfg: cfg, hook: &kiloNativeHook{}, configPath: filepath.Join(t.TempDir(), "config.yaml")}
	f.manager = coreauth.NewManager(nil, nil, f.hook)
	f.manager.SetConfig(cfg)
	store := sdkauth.NewFileTokenStore()
	store.SetBaseDir(cfg.AuthDir)
	f.manager.SetStore(store)
	return f
}

func startKiloBrowserServer(t *testing.T, f *kiloNativeFixture, gate *kiloBrowserResponseGate, core string) {
	t.Helper()
	if f.server != nil {
		f.server.Close()
	}
	f.server = httptest.NewUnstartedServer(nil)
	_, port, err := net.SplitHostPort(f.server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	f.cfg.Port, err = strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	f.cfg.RedisUsageQueueRetentionSeconds = 60
	f.cfg.RemoteManagement.SecretKey = kiloManagementKey
	f.cfg.CredentialInFlight = config.DefaultCredentialInFlightConfig()
	raw, err := yaml.Marshal(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	raw, _, err = config.NormalizeConfigLayout(raw, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = config.ParseConfigBytes(raw); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(f.configPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	service := &Service{cfg: f.cfg, coreManager: f.manager}
	var server *api.Server
	server = api.NewServer(f.cfg, f.manager, access.NewManager(), f.configPath,
		api.WithLocalManagementPassword(kiloManagementKey),
		api.WithPostAuthPersistHook(service.runtimeAuthSyncHook()),
		api.WithConfigReloadHook(func(ctx context.Context, cfg *config.Config) {
			f.manager.SetConfig(cfg)
			if !server.UpdateClientsContext(ctx, cfg) {
				t.Error("browser configuration reload rejected")
			}
		}),
	)
	f.server.Config.Handler = gate.wrap(core, server.Handler())
	f.server.Start()
	f.server.Client().Transport = &http.Transport{Proxy: nil}
}

type kiloBrowserDelay struct {
	core, method, path           string
	entered, completed, released bool
	status                       int
	done                         chan struct{}
}

type kiloBrowserResponseGate struct {
	mu       sync.Mutex
	active   *kiloBrowserDelay
	requests map[string]map[string]int
}

func (g *kiloBrowserResponseGate) arm(core, method, path string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.active != nil && !g.active.released {
		return false
	}
	g.active = &kiloBrowserDelay{core: core, method: method, path: path, done: make(chan struct{})}
	return true
}

func (g *kiloBrowserResponseGate) release() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.active != nil && !g.active.released {
		g.active.released = true
		close(g.active.done)
	}
}

func (g *kiloBrowserResponseGate) snapshot() map[string]any {
	g.mu.Lock()
	defer g.mu.Unlock()
	counts := make(map[string]map[string]int, len(g.requests))
	for core, paths := range g.requests {
		counts[core] = make(map[string]int, len(paths))
		for path, count := range paths {
			counts[core][path] = count
		}
	}
	state := map[string]any{"requests": counts}
	if delay := g.active; delay != nil {
		state["gate"] = map[string]any{"core": delay.core, "method": delay.method, "path": delay.path, "entered": delay.entered, "completed": delay.completed, "released": delay.released, "status": delay.status}
	}
	return state
}

func (g *kiloBrowserResponseGate) wrap(core string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		g.requests[core][r.Method+" "+r.URL.Path]++
		delay := g.active
		if delay == nil || delay.released || delay.entered || delay.core != core || delay.method != r.Method || delay.path != r.URL.Path {
			delay = nil
		} else {
			delay.entered = true
		}
		g.mu.Unlock()
		if delay == nil {
			next.ServeHTTP(w, r)
			return
		}
		response := httptest.NewRecorder()
		next.ServeHTTP(response, r)
		g.mu.Lock()
		delay.completed, delay.status = true, response.Code
		g.mu.Unlock()
		// Client cancellation never changes the already completed operation.
		// Stop and explicit release both close this gate, including after cancellation.
		<-delay.done
		for key, values := range response.Header() {
			w.Header()[key] = values
		}
		w.WriteHeader(response.Code)
		_, _ = w.Write(response.Body.Bytes())
	})
}

func TestKiloBrowserResponseGate(t *testing.T) {
	gate := &kiloBrowserResponseGate{requests: map[string]map[string]int{"a": {}, "b": {}}}
	defer gate.release()
	path := "/v8/management/kilo/accounts/target"
	if !gate.arm("a", "PATCH", path) || gate.arm("b", "GET", path) {
		t.Fatal("only one gate may be active")
	}
	for _, other := range []struct{ core, method, path string }{{"b", "PATCH", path}, {"a", "GET", path}, {"a", "PATCH", path + "/other"}} {
		response := httptest.NewRecorder()
		gate.wrap(other.core, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(204)
		})).ServeHTTP(response, httptest.NewRequest(other.method, other.path, nil))
		if response.Code != 204 {
			t.Fatal("nonmatching request was changed")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var saved atomic.Bool
	response := httptest.NewRecorder()
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		gate.wrap("a", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			saved.Store(true)
			w.Header().Set("X-Test-Result", "saved")
			w.WriteHeader(202)
			_, _ = io.WriteString(w, "saved body")
		})).ServeHTTP(response, httptest.NewRequest("PATCH", path, nil).WithContext(ctx))
	}()
	deadline := time.Now().Add(time.Second)
	for {
		state := gate.snapshot()["gate"].(map[string]any)
		if state["completed"] == true {
			if !saved.Load() || state["status"] != 202 || state["released"] != false {
				t.Fatal("gate did not retain completed operation")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("handler did not finish")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case <-finished:
		t.Fatal("response escaped before release")
	default:
	}
	gate.release()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("release did not unblock response")
	}
	if response.Code != 202 || response.Header().Get("X-Test-Result") != "saved" || response.Body.String() != "saved body" || !saved.Load() {
		t.Fatal("completed response or write was changed")
	}
	gate.release()
	if !gate.arm("b", "GET", path) {
		t.Fatal("released gate cannot be reused")
	}
}
