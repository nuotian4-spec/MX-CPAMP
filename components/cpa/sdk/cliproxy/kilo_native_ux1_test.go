package cliproxy

import (
	"context"
	"errors"
	"fmt"
	"github.com/MoeForge/moego/base64"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilo"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/watcher"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/watcher/synthesizer"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func ux1TwoProxies(t *testing.T, f *kiloNativeFixture) (*atomic.Int32, string) {
	t.Helper()
	second, _ := url.Parse(f.cfg.Kilo.Proxies[0].URL)
	second.User = url.UserPassword("second", "synthetic")
	doc := ojson.NewDocString("{}")
	_ = doc.Set("expected_revision", 0)
	_ = doc.Set("name", "second")
	_ = doc.Set("url", second.String())
	created := m4Request(t, f, "POST", kiloProxiesURL, doc.String(), 201)
	secondID := ojson.GetBytes(created, "proxies.1.id").String()
	selected := &atomic.Int32{}
	expected := "Basic " + base64.StdEncoding.EncodeToString([]byte("second:synthetic"))
	f.deviceProxyAuth = func(value string) {
		if value == expected {
			selected.Add(1)
		} else {
			t.Errorf("device selected wrong proxy identity")
		}
	}
	m4Request(t, f, "POST", kiloProxiesURL+"/local-proxy/recheck", `{"expected_revision":1}`, 200)
	m4Request(t, f, "POST", kiloProxiesURL+"/"+secondID+"/recheck", `{"expected_revision":1}`, 200)
	return selected, secondID
}
func ux1Start(t *testing.T, f *kiloNativeFixture, proxyID string) string {
	t.Helper()
	state := m4Request(t, f, "GET", kiloProxiesURL, "", 200)
	rev := ojson.GetBytes(state, "revision").Uint()
	body := fmt.Sprintf(`{"name":"ux1 device","authorization_egress":{"mode":"pool","proxy_id":%q,"expected_revision":%d},"inference_egress":{"mode":"pool","candidate_proxy_ids":["local-proxy",%q],"exclusive_proxy_ids":[],"primary_proxy_id":%q,"expected_revision":%d}}`, proxyID, rev, proxyID, proxyID, rev)
	data := m4Request(t, f, "POST", kiloDeviceURL, body, 200)
	if ojson.GetBytes(data, "authorization_egress.proxy_id").String() != proxyID {
		t.Fatal("pin missing")
	}
	return ojson.GetBytes(data, "id").String()
}
func ux1NoActive(t *testing.T, f *kiloNativeFixture) {
	t.Helper()
	for _, p := range f.executor.KiloProxySnapshot().Proxies {
		if p.Active != 0 || p.Owner != "" {
			t.Fatalf("lease retained %+v", p)
		}
	}
}
func TestUX1NativePinnedProxyPollsAndPolicySave(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	selected, proxyID := ux1TwoProxies(t, f)
	var calls, polls atomic.Int32
	f.deviceHandler = func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && polls.Add(1) == 1 {
			calls.Add(1)
			w.WriteHeader(202)
			return
		}
		m4DeviceUpstream(t, &calls)(w, r)
	}
	id := ux1Start(t, f, proxyID)
	ux1NoActive(t, f)
	beforeProbes := f.probes.Load()
	first := m4Request(t, f, "POST", kiloDeviceURL+"/"+id+"/poll", "", 200)
	if ojson.GetBytes(first, "status").String() != "pending" {
		t.Fatal(string(first))
	}
	ux1NoActive(t, f)
	state := m4Request(t, f, "GET", kiloProxiesURL, "", 200)
	rev := ojson.GetBytes(state, "revision").Uint()
	m4Request(t, f, "DELETE", kiloProxiesURL+"/"+proxyID, fmt.Sprintf(`{"expected_revision":%d}`, rev), 409)
	m4Request(t, f, "PATCH", kiloProxiesURL+"/"+proxyID, fmt.Sprintf(`{"expected_revision":%d,"url":"http://127.0.0.1:1"}`, rev), 409)
	time.Sleep(3100 * time.Millisecond)
	second := m4Request(t, f, "POST", kiloDeviceURL+"/"+id+"/poll", "", 200)
	if ojson.GetBytes(second, "status").String() != "approved" {
		t.Fatal(string(second))
	}
	ux1NoActive(t, f)
	saved := m4Request(t, f, "POST", kiloDeviceURL+"/"+id+"/save", "", 201)
	accountID := ojson.GetBytes(saved, "id").String()
	t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(accountID) })
	if selected.Load() != 3 || f.deviceConnects.Load() != 3 || f.deviceDirect.Load() != 0 || f.posts.Load() != 0 || f.probes.Load() != beforeProbes {
		t.Fatalf("counts selected=%d connect=%d direct=%d model=%d probes=%d", selected.Load(), f.deviceConnects.Load(), f.deviceDirect.Load(), f.posts.Load(), f.probes.Load())
	}
	persisted, _, err := (kilo.ProxyStateStore{Dir: config.KiloStateDirectory(f.cfg)}).Load(kilo.ProxyState{})
	if err != nil || len(persisted.Accounts[accountID].CandidateProxyIDs) != 2 || persisted.Accounts[accountID].PrimaryProxyID != proxyID || len(persisted.PendingDevices) != 0 {
		t.Fatalf("policy %+v %v", persisted, err)
	}
	for _, p := range persisted.Proxies {
		if p.AccountID != "" {
			t.Fatal("shared selection became exclusive")
		}
	}
	m2Reload(t, f)
	got, _ := f.manager.GetByID(accountID)
	if got == nil {
		t.Fatal("reload missing account")
	}
	t.Log("formal Router/Executor/CONNECT: selected second proxy for Start+2 Poll; waiting active=0; no extra IP/model/direct calls; persisted candidate policy reload PASS")
}
func TestUX1NativeDisableTerminatesPin(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	selected, proxyID := ux1TwoProxies(t, f)
	var calls atomic.Int32
	f.deviceHandler = m4DeviceUpstream(t, &calls)
	id := ux1Start(t, f, proxyID)
	state := m4Request(t, f, "GET", kiloProxiesURL, "", 200)
	rev := ojson.GetBytes(state, "revision").Uint()
	m4Request(t, f, "PATCH", kiloProxiesURL+"/"+proxyID, fmt.Sprintf(`{"expected_revision":%d,"enabled":false}`, rev), 200)
	poll := m4Request(t, f, "POST", kiloDeviceURL+"/"+id+"/poll", "", 200)
	if ojson.GetBytes(poll, "status").String() != "cancelled" || selected.Load() != 1 || f.deviceDirect.Load() != 0 {
		t.Fatal("disabled pin silently continued")
	}
	ux1NoActive(t, f)
}

type ux1DeviceStore struct {
	coreauth.Store
	before                func(*coreauth.Auth)
	after                 func(*coreauth.Auth)
	failBefore, failAfter bool
}

func (s ux1DeviceStore) Save(ctx context.Context, a *coreauth.Auth) (string, error) {
	if s.before != nil {
		s.before(a)
	}
	if s.failBefore {
		return "", errors.New("synthetic save failure")
	}
	path, err := s.Store.Save(ctx, a)
	if err != nil {
		return path, err
	}
	if s.after != nil {
		s.after(a)
	}
	if s.failAfter {
		return path, errors.New("synthetic postwrite failure")
	}
	return path, nil
}
func TestUX1NativeDeviceWriteFailureAndRecovery(t *testing.T) {
	for _, kind := range []string{"before", "compensation", "after", "interrupt_before", "interrupt_after", "malformed"} {
		t.Run(kind, func(t *testing.T) {
			f := newKiloNativeFixture(t, kiloSuccessStream, true)
			f.allowDirect = true
			var calls atomic.Int32
			f.deviceHandler = m4DeviceUpstream(t, &calls)
			id := m4Start(t, f, "direct")
			m4Request(t, f, "POST", kiloDeviceURL+"/"+id+"/poll", "", 200)
			policyPath := filepath.Join(config.KiloStateDirectory(f.cfg), kilo.StateFileName)
			var reserved string
			var pendingBytes []byte
			store := ux1DeviceStore{Store: m2FileStore(f), failBefore: kind != "after" && kind != "interrupt_after", failAfter: kind == "after" || kind == "interrupt_after"}
			store.before = func(a *coreauth.Auth) {
				reserved = a.ID
				state, _, err := (kilo.ProxyStateStore{Dir: config.KiloStateDirectory(f.cfg)}).Load(kilo.ProxyState{})
				if err != nil || state.Accounts[a.ID].Mode != "pool" || len(state.PendingDevices) != 1 {
					t.Errorf("credential write preceded policy %v %+v", err, state)
				}
				auths, err := synthesizer.NewFileSynthesizer().Synthesize(&synthesizer.SynthesisContext{Config: f.cfg, AuthDir: f.cfg.AuthDir, Now: time.Now()})
				if err != nil || len(auths) != 0 {
					t.Error("watcher saw credential before write")
				}
				pendingBytes, _ = os.ReadFile(policyPath)
				if kind == "compensation" || kind == "interrupt_before" || kind == "malformed" {
					if err := os.Rename(policyPath, policyPath+".pending"); err != nil {
						t.Error(err)
					}
					if err := os.Mkdir(policyPath, 0700); err != nil {
						t.Error(err)
					}
				}
			}
			store.after = func(a *coreauth.Auth) {
				auths, err := synthesizer.NewFileSynthesizer().Synthesize(&synthesizer.SynthesisContext{Config: f.cfg, AuthDir: f.cfg.AuthDir, Now: time.Now()})
				if err != nil || len(auths) != 1 || auths[0].ID != a.ID {
					t.Error("watcher did not see complete credential")
				}
				state, _, err := (kilo.ProxyStateStore{Dir: config.KiloStateDirectory(f.cfg)}).Load(kilo.ProxyState{})
				if err != nil || state.Accounts[a.ID].Mode != "pool" {
					t.Error("watcher credential has no durable policy")
				}
			}
			f.manager.SetStore(store)
			m4Request(t, f, "POST", kiloDeviceURL+"/"+id+"/save", "", 503)
			if kind == "compensation" || kind == "interrupt_before" || kind == "malformed" {
				if err := os.Remove(policyPath); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(policyPath+".pending", policyPath); err != nil {
					t.Fatal(err)
				}
			}
			approved := m4Request(t, f, "POST", kiloDeviceURL+"/"+id+"/poll", "", 200)
			if ojson.GetBytes(approved, "status").String() != "approved" {
				t.Fatal("save failure consumed approval")
			}
			f.manager.SetStore(m2FileStore(f))
			if kind == "interrupt_before" || kind == "interrupt_after" || kind == "malformed" {
				if kind == "malformed" {
					if err := os.WriteFile(filepath.Join(f.cfg.AuthDir, reserved), []byte("{"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				m2Reload(t, f)
				m4Request(t, f, "GET", kiloProxiesURL, "", 200)
				state, _, err := (kilo.ProxyStateStore{Dir: config.KiloStateDirectory(f.cfg)}).Load(kilo.ProxyState{})
				if err != nil {
					t.Fatal(err)
				}
				if kind == "interrupt_before" && (len(state.PendingDevices) != 0 || len(state.Accounts) != 0) {
					t.Fatal("orphan policy not reclaimed")
				}
				if kind == "interrupt_after" && (len(state.PendingDevices) != 0 || state.Accounts[reserved].Mode != "pool" || len(f.manager.List()) != 1) {
					t.Fatal("published account lost policy")
				}
				if kind == "malformed" && (len(state.PendingDevices) != 1 || len(pendingBytes) == 0) {
					t.Fatal("malformed existing file misclassified as absent")
				}
			} else {
				saved := m4Request(t, f, "POST", kiloDeviceURL+"/"+id+"/save", "", 201)
				if ojson.GetBytes(saved, "id").String() != reserved || len(f.manager.List()) != 1 {
					t.Fatal("retry identity changed")
				}
				state, _, err := (kilo.ProxyStateStore{Dir: config.KiloStateDirectory(f.cfg)}).Load(kilo.ProxyState{})
				if err != nil || len(state.PendingDevices) != 0 || state.Accounts[reserved].Mode != "pool" {
					t.Fatal("retry policy not finalized")
				}
			}
			t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(reserved) })
			if calls.Load() != 2 || f.posts.Load() != 0 {
				t.Fatal("save/reload replayed authorization or model")
			}
		})
	}
}

func TestUX1NativeWatcherSeesPolicyBeforeCredential(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	f.allowDirect = true
	var calls atomic.Int32
	f.deviceHandler = m4DeviceUpstream(t, &calls)
	id := m4Start(t, f, "direct")
	m4Request(t, f, "POST", kiloDeviceURL+"/"+id+"/poll", "", 200)
	if err := os.WriteFile(f.configPath, []byte(fmt.Sprintf("auth-dir: %q\n", f.cfg.AuthDir)), 0600); err != nil {
		t.Fatal(err)
	}
	w, err := watcher.NewWatcher(f.configPath, f.cfg.AuthDir, func(*config.Config) {})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Stop()
	w.SetConfig(f.cfg)
	updates := make(chan watcher.AuthUpdate, 16)
	w.SetAuthUpdateQueue(updates)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err = w.Start(ctx); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan *coreauth.Auth, 1), make(chan struct{})
	f.manager.SetStore(ux1DeviceStore{Store: m2FileStore(f), after: func(a *coreauth.Auth) { entered <- a.Clone(); <-release }})
	done := make(chan []byte, 1)
	go func() {
		status, data := f.request(t, "POST", kiloDeviceURL+"/"+id+"/save", kiloManagementKey, "")
		if status != 201 {
			t.Errorf("save status=%d", status)
		}
		done <- data
	}()
	var reserved *coreauth.Auth
	select {
	case reserved = <-entered:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("credential write not entered")
	}
	t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(reserved.ID) })
	select {
	case update := <-updates:
		state, _, loadErr := (kilo.ProxyStateStore{Dir: config.KiloStateDirectory(f.cfg)}).Load(kilo.ProxyState{})
		if update.Auth == nil || update.ID != reserved.ID || loadErr != nil || state.Accounts[reserved.ID].Mode != "pool" {
			t.Error("native watcher observed incomplete policy")
		}
	case <-time.After(5 * time.Second):
		t.Error("native fsnotify watcher did not observe credential")
	}
	if len(f.manager.List()) != 0 {
		t.Error("manager published before store returned")
	}
	close(release)
	<-done
	if len(f.manager.List()) != 1 {
		t.Fatal("manager did not publish completed save")
	}
	t.Log("real fsnotify watcher observed credential only after complete policy was durable; Manager publication followed Store.Save")
}
