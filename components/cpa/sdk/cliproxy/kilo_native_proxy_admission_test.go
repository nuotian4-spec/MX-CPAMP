package cliproxy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilo"
	native "github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor"
)

func TestKiloA2PreviewHasNoStateSideEffects(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	f.probeHandler = func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "203.0.113.10")
	}
	before := m2Request(t, f, "GET", kiloProxiesURL, "", 200)
	u, _ := url.Parse(f.cfg.Kilo.Proxies[0].URL)
	u.User = url.UserPassword("synthetic-proxy-user", "synthetic-proxy-password")
	for range 2 {
		data := m2Request(t, f, "POST", kiloProxiesURL+"/preview", fmt.Sprintf(`{"url":%q}`, u.String()), 200)
		if ojson.GetBytes(data, "status").String() != "ready" || ojson.GetBytes(data, "observed_exit_ip").String() != "203.0.113.10" {
			t.Fatalf("unexpected preview: %s", data)
		}
		for _, secret := range []string{u.String(), "synthetic-proxy-user", "synthetic-proxy-password"} {
			if bytes.Contains(data, []byte(secret)) {
				t.Fatalf("preview exposed connection secret: %s", data)
			}
		}
	}
	after := m2Request(t, f, "GET", kiloProxiesURL, "", 200)
	if !bytes.Equal(before, after) || f.probes.Load() != 2 || f.posts.Load() != 0 {
		t.Fatalf("preview changed runtime or skipped observation: %s", after)
	}
	if _, err := os.Stat(filepath.Join(config.KiloStateDirectory(f.cfg), kilo.StateFileName)); !os.IsNotExist(err) {
		t.Fatalf("preview created persistent state: %v", err)
	}
}

func TestKiloA2ColdPreviewDoesNotReconcilePendingDeviceSaves(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	f.probeHandler = func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "203.0.113.10")
	}
	state := kilo.ProxyState{
		Revision: 1, Limit: 2,
		Proxies:        []kilo.SavedProxy{{ID: "local-proxy", URL: f.cfg.Kilo.Proxies[0].URL}},
		Accounts:       map[string]kilo.AccountEgress{"synthetic-pending.json": {Mode: "direct"}},
		PendingDevices: map[string]kilo.PendingDeviceSave{"synthetic-pending.json": {Draft: kilo.InferenceEgress{Mode: "direct", ExclusiveProxyIDs: []string{}}, PreviousPrimary: map[string]string{}}},
	}
	if err := (kilo.ProxyStateStore{Dir: config.KiloStateDirectory(f.cfg)}).Save(state); err != nil {
		t.Fatal(err)
	}
	disk := m3bDisk(t, f)
	m2Request(t, f, "POST", kiloProxiesURL+"/preview", fmt.Sprintf(`{"url":%q}`, f.cfg.Kilo.Proxies[0].URL), 200)
	if !bytes.Equal(disk, m3bDisk(t, f)) {
		t.Fatal("cold preview reconciled persistent device reservations")
	}
	if _, ok := f.manager.Executor("kilo"); ok {
		t.Fatal("cold preview published runtime configuration")
	}
}

func TestKiloA2ConcurrentCreateUsesLockFreeObservation(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	entered, proceed := make(chan struct{}, 2), make(chan struct{})
	defer func() {
		select {
		case <-proceed:
		default:
			close(proceed)
		}
	}()
	f.probeHandler = func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		select {
		case <-proceed:
			_, _ = io.WriteString(w, "203.0.113.10")
		case <-r.Context().Done():
		}
	}
	m2Request(t, f, "GET", kiloProxiesURL, "", 200)
	body := fmt.Sprintf(`{"expected_revision":0,"url":%q}`, f.cfg.Kilo.Proxies[0].URL)
	type result struct {
		status int
		body   []byte
	}
	done := make(chan result, 2)
	for range 2 {
		go func() {
			status, data := f.request(t, "POST", kiloProxiesURL, kiloManagementKey, body)
			done <- result{status, data}
		}()
	}
	m3bAwait(t, entered)
	m3bAwait(t, entered)
	locks := make(chan struct{})
	go func() {
		unlock := f.manager.LockKiloConfigUpdate()
		f.executor.UpdateConfig(f.cfg.CloneForRuntime())
		_, err := f.executor.KiloProxyState()
		unlock()
		if err != nil {
			t.Error(err)
		}
		close(locks)
	}()
	m3bAwait(t, locks)
	close(proceed)
	success, conflict := 0, 0
	for range 2 {
		r := <-done
		switch r.status {
		case 201:
			success++
		case 409:
			conflict++
			if ojson.GetBytes(r.body, "error").String() != kilo.ErrStateConflict.Error() {
				t.Fatalf("version conflict incorrectly classified: %s", r.body)
			}
		default:
			t.Fatalf("concurrent create: %d %s", r.status, r.body)
		}
	}
	view := m2Request(t, f, "GET", kiloProxiesURL, "", 200)
	if success != 1 || conflict != 1 || ojson.GetBytes(view, "revision").Uint() != 1 || len(ojson.GetBytes(view, "proxies").Array()) != 2 {
		t.Fatalf("concurrent same-IP admission: %d/%d %s", success, conflict, view)
	}
	t.Log("both probes ran while Manager selection and Executor state locks remained available; one atomic commit")
}

func TestKiloA2FailedObservationsSavePendingWithoutRegistration(t *testing.T) {
	for _, reason := range []string{"probe_failed", "not_public_ip"} {
		t.Run(reason, func(t *testing.T) {
			f := newKiloNativeFixture(t, kiloSuccessStream, true)
			f.probeHandler = func(w http.ResponseWriter, _ *http.Request) {
				if reason == "probe_failed" {
					http.Error(w, "synthetic-password-and-upstream-detail", 502)
					return
				}
				_, _ = io.WriteString(w, "127.0.0.2")
			}
			raw := f.cfg.Kilo.Proxies[0].URL
			preview := m2Request(t, f, "POST", kiloProxiesURL+"/preview", fmt.Sprintf(`{"url":%q}`, raw), 200)
			if ojson.GetBytes(preview, "status").String() != "pending" || ojson.GetBytes(preview, "reason").String() != reason || ojson.GetBytes(preview, "observed_exit_ip").Type != ojson.Null {
				t.Fatalf("pending preview: %s", preview)
			}
			data := m2Request(t, f, "POST", kiloProxiesURL, fmt.Sprintf(`{"expected_revision":0,"url":%q}`, raw), 201)
			if ojson.GetBytes(data, "proxies.1.admission_status").String() != "pending" || ojson.GetBytes(data, "proxies.1.admission_reason").String() != reason || ojson.GetBytes(data, "proxies.1.healthy").Bool() || ojson.GetBytes(data, "proxies.1.admitted_ip").String() != "" {
				t.Fatalf("failure was admitted: %s", data)
			}
			state, _, err := (kilo.ProxyStateStore{Dir: config.KiloStateDirectory(f.cfg)}).Load(kilo.ProxyState{})
			if err != nil || len(state.IPHistory) != 0 || state.Revision != 1 || f.probes.Load() != 2 {
				t.Fatalf("pending registered IP: %+v %v", state, err)
			}
			if bytes.Contains(data, []byte("synthetic-password")) || bytes.Contains(preview, []byte("synthetic-password")) {
				t.Fatal("upstream error leaked")
			}
			restarted := native.NewKiloExecutor(f.cfg)
			view, err := restarted.KiloProxyState()
			if err != nil || view.Proxies[1].AdmissionStatus != "pending" || view.Proxies[1].Healthy {
				t.Fatalf("pending lost across runtime restart: %+v %v", view, err)
			}
		})
	}
}

func TestKiloA2PersistenceFailureDoesNotHalfAdmit(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	f.probeHandler = func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "203.0.113.10")
	}
	before := m2Request(t, f, "GET", kiloProxiesURL, "", 200)
	path := filepath.Join(config.KiloStateDirectory(f.cfg), kilo.StateFileName)
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"expected_revision":0,"url":%q}`, f.cfg.Kilo.Proxies[0].URL)
	m2Request(t, f, "POST", kiloProxiesURL, body, 503)
	after := m2Request(t, f, "GET", kiloProxiesURL, "", 200)
	if !bytes.Equal(before, after) || f.probes.Load() != 1 {
		t.Fatalf("failed durable write published admission: %s", after)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	data := m2Request(t, f, "POST", kiloProxiesURL, body, 201)
	if ojson.GetBytes(data, "revision").Uint() != 1 || ojson.GetBytes(data, "proxies.1.admission_status").String() != "admitted" || f.probes.Load() != 2 {
		t.Fatalf("explicit fresh request encountered false registration: %s", data)
	}
}

func TestKiloA2ConnectionFailureRemainsPendingAndKeepsHistory(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	var fail atomic.Bool
	f.probeHandler = func(w http.ResponseWriter, _ *http.Request) {
		if fail.Load() {
			http.Error(w, "synthetic probe failure", 502)
			return
		}
		_, _ = io.WriteString(w, "203.0.113.10")
	}
	raw := f.cfg.Kilo.Proxies[0].URL
	data := m2Request(t, f, "POST", kiloProxiesURL, fmt.Sprintf(`{"expected_revision":0,"url":%q}`, raw), 201)
	pid := ojson.GetBytes(data, "proxies.1.id").String()
	fail.Store(true)
	u, _ := url.Parse(raw)
	u.User = url.UserPassword("synthetic-edit-user", "synthetic-edit-password")
	data = m2Request(t, f, "PATCH", kiloProxiesURL+"/"+pid, fmt.Sprintf(`{"expected_revision":1,"url":%q}`, u.String()), 200)
	if ojson.GetBytes(data, "proxies.1.admission_status").String() != "pending" || ojson.GetBytes(data, "proxies.1.admission_reason").String() != "probe_failed" || ojson.GetBytes(data, "proxies.1.admitted_ip").String() != "203.0.113.10" || ojson.GetBytes(data, "proxies.1.healthy").Bool() {
		t.Fatalf("failed connection lost admission boundary: %s", data)
	}
	state, _, err := (kilo.ProxyStateStore{Dir: config.KiloStateDirectory(f.cfg)}).Load(kilo.ProxyState{})
	if err != nil || len(state.IPHistory) != 1 || len(state.IPHistory["203.0.113.10"].ProxyIDs) != 1 || state.IPHistory["203.0.113.10"].ProxyIDs[0] != pid {
		t.Fatalf("original IP history lost: %+v %v", state.IPHistory, err)
	}
	fail.Store(false)
	data = m2Request(t, f, "PATCH", kiloProxiesURL+"/"+pid, `{"expected_revision":2,"enabled":true}`, 200)
	if ojson.GetBytes(data, "proxies.1.admission_status").String() != "pending" || f.probes.Load() != 2 {
		t.Fatalf("manual enable re-admitted pending: %s", data)
	}
	disk := m3bDisk(t, f)
	dup := m2Request(t, f, "POST", kiloProxiesURL, fmt.Sprintf(`{"expected_revision":3,"url":%q}`, raw), 409)
	if ojson.GetBytes(dup, "error").String() != "proxy_ip_already_registered" || !bytes.Equal(disk, m3bDisk(t, f)) {
		t.Fatalf("pending original lost duplicate protection: %s", dup)
	}
}

func TestKiloA2ManagementAuthAndReadonlyFields(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	before := m2Request(t, f, "GET", kiloProxiesURL, "", 200)
	raw := f.cfg.Kilo.Proxies[0].URL
	for _, path := range []string{kiloProxiesURL, kiloProxiesURL + "/preview"} {
		status, _ := f.request(t, "POST", path, "synthetic-kilo-key", fmt.Sprintf(`{"expected_revision":0,"url":%q}`, raw))
		if status != 401 {
			t.Fatalf("inference key gained management access: %s %d", path, status)
		}
	}
	for _, field := range []string{"admission_status", "admission_reason", "admitted_ip", "admitted_at", "observed_exit_ip", "registered_proxy_id"} {
		m2Request(t, f, "POST", kiloProxiesURL, fmt.Sprintf(`{"expected_revision":0,"url":%q,%q:"forged"}`, raw, field), 400)
		m2Request(t, f, "POST", kiloProxiesURL+"/preview", fmt.Sprintf(`{"url":%q,%q:"forged"}`, raw, field), 400)
	}
	for _, invalid := range []string{"", "direct", "garbage", "file:///tmp/proxy"} {
		m2Request(t, f, "POST", kiloProxiesURL, fmt.Sprintf(`{"expected_revision":0,"url":%q}`, invalid), 400)
		m2Request(t, f, "POST", kiloProxiesURL+"/preview", fmt.Sprintf(`{"url":%q}`, invalid), 400)
	}
	after := m2Request(t, f, "GET", kiloProxiesURL, "", 200)
	if !bytes.Equal(before, after) || f.probes.Load() != 0 {
		t.Fatalf("rejected inputs changed state: %s", after)
	}
}

func TestKiloA2CancellationAndLateRevisionDoNotCommit(t *testing.T) {
	for _, action := range []string{"cancel", "revision", "runtime"} {
		t.Run(action, func(t *testing.T) {
			f := newKiloNativeFixture(t, kiloSuccessStream, true)
			entered, proceed, probeDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
			defer func() {
				select {
				case <-proceed:
				default:
					close(proceed)
				}
			}()
			f.probeHandler = func(w http.ResponseWriter, r *http.Request) {
				defer close(probeDone)
				close(entered)
				select {
				case <-proceed:
					_, _ = io.WriteString(w, "203.0.113.10")
				case <-r.Context().Done():
				}
			}
			before := m2Request(t, f, "GET", kiloProxiesURL, "", 200)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			req, _ := http.NewRequestWithContext(ctx, "POST", f.server.URL+kiloProxiesURL, strings.NewReader(fmt.Sprintf(`{"expected_revision":0,"url":%q}`, f.cfg.Kilo.Proxies[0].URL)))
			req.Header.Set("Authorization", "Bearer "+kiloManagementKey)
			done := make(chan int, 1)
			go func() {
				res, err := f.server.Client().Do(req)
				if err != nil {
					done <- 0
					return
				}
				defer res.Body.Close()
				_, _ = io.Copy(io.Discard, res.Body)
				done <- res.StatusCode
			}()
			m3bAwait(t, entered)
			want := 409
			switch action {
			case "cancel":
				cancel()
				want = 0
			case "revision":
				before = m2Request(t, f, "PATCH", kiloProxiesURL+"/local-proxy", `{"expected_revision":0,"name":"concurrent"}`, 200)
			case "runtime":
				cfg := f.cfg.CloneForRuntime()
				cfg.Kilo.ProxyLimit = 3
				f.executor.UpdateConfig(cfg)
				before = m2Request(t, f, "GET", kiloProxiesURL, "", 200)
			}
			if action != "cancel" {
				close(proceed)
			}
			if status := <-done; status != want {
				t.Fatalf("late request status=%d want=%d", status, want)
			}
			m3bAwait(t, probeDone)
			after := m2Request(t, f, "GET", kiloProxiesURL, "", 200)
			if !bytes.Equal(before, after) || f.probes.Load() != 1 {
				t.Fatalf("late observation committed: %s", after)
			}
		})
	}
}

func TestKiloA2ConnectionEditReobservesAndPreservesFixedIPGate(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	var ip atomic.Value
	ip.Store("203.0.113.10")
	f.probeHandler = func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, ip.Load().(string))
	}
	raw := f.cfg.Kilo.Proxies[0].URL
	data := m2Request(t, f, "POST", kiloProxiesURL, fmt.Sprintf(`{"expected_revision":0,"url":%q}`, raw), 201)
	pid := ojson.GetBytes(data, "proxies.1.id").String()
	u, _ := url.Parse(raw)
	u.User = url.UserPassword("synthetic-edit-user", "synthetic-edit-password")
	data = m2Request(t, f, "PATCH", kiloProxiesURL+"/"+pid, fmt.Sprintf(`{"expected_revision":1,"url":%q}`, u.String()), 200)
	if ojson.GetBytes(data, "proxies.1.admission_status").String() != "admitted" || f.probes.Load() != 2 {
		t.Fatalf("same identity/IP edit rejected: %s", data)
	}
	ip.Store("203.0.113.11")
	data = m2Request(t, f, "POST", kiloProxiesURL, fmt.Sprintf(`{"expected_revision":2,"url":%q}`, raw), 201)
	disk := m3bDisk(t, f)
	dup := m2Request(t, f, "PATCH", kiloProxiesURL+"/"+pid, fmt.Sprintf(`{"expected_revision":3,"url":%q}`, raw), 409)
	if ojson.GetBytes(dup, "error").String() != "proxy_ip_already_registered" || !bytes.Equal(disk, m3bDisk(t, f)) {
		t.Fatalf("edit bypassed duplicate registry: %s", dup)
	}
	ip.Store("203.0.113.10")
	data = m2Request(t, f, "PATCH", kiloProxiesURL+"/"+pid, `{"expected_revision":3,"fixed_exit_ip":"203.0.113.10"}`, 200)
	disk = m3bDisk(t, f)
	probes := f.probes.Load()
	errBody := m2Request(t, f, "PATCH", kiloProxiesURL+"/"+pid, fmt.Sprintf(`{"expected_revision":4,"url":%q}`, raw), 409)
	if ojson.GetBytes(errBody, "error").String() != kilo.ErrStateConflict.Error() || !bytes.Equal(disk, m3bDisk(t, f)) || f.probes.Load() != probes {
		t.Fatalf("FixedIP URL gate changed: %s", errBody)
	}
	if bytes.Contains(data, []byte("synthetic-edit-password")) || bytes.Contains(data, []byte(u.String())) {
		t.Fatal("connection edit exposed secret")
	}
	ip.Store("203.0.113.12")
	data = m2Request(t, f, "PATCH", kiloProxiesURL+"/"+pid, fmt.Sprintf(`{"expected_revision":4,"url":%q,"name":"renamed"}`, u.String()), 200)
	if f.probes.Load() != probes || ojson.GetBytes(data, "proxies.1.admitted_ip").String() != "203.0.113.10" {
		t.Fatalf("unchanged connection silently re-admitted FixedIP entry: %s", data)
	}
}

func TestKiloA2CreateAdmitsAtomicallyAndRejectsDuplicate(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	f.probeHandler = func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "203.0.113.10")
	}
	url := f.cfg.Kilo.Proxies[0].URL
	m2Request(t, f, "POST", kiloProxiesURL+"/preview", fmt.Sprintf(`{"url":%q}`, url), 200)
	data := m2Request(t, f, "POST", kiloProxiesURL, fmt.Sprintf(`{"expected_revision":0,"url":%q}`, url), 201)
	pid := ojson.GetBytes(data, "proxies.1.id").String()
	if pid == "" || ojson.GetBytes(data, "revision").Uint() != 1 || ojson.GetBytes(data, "proxies.1.admission_status").String() != "admitted" || ojson.GetBytes(data, "proxies.1.admitted_ip").String() != "203.0.113.10" {
		t.Fatalf("create did not atomically admit: %s", data)
	}
	disk := m3bDisk(t, f)
	dup := m2Request(t, f, "POST", kiloProxiesURL, fmt.Sprintf(`{"expected_revision":1,"url":%q}`, url), 409)
	if ojson.GetBytes(dup, "error").String() != "proxy_ip_already_registered" || ojson.GetBytes(dup, "proxy_id").String() != pid {
		t.Fatalf("duplicate not classified: %s", dup)
	}
	after := m2Request(t, f, "GET", kiloProxiesURL, "", 200)
	if !bytes.Equal(data, after) || !bytes.Equal(disk, m3bDisk(t, f)) || f.probes.Load() != 3 {
		t.Fatalf("duplicate created side effects or reused preview: %s", after)
	}
	preview := m2Request(t, f, "POST", kiloProxiesURL+"/preview", fmt.Sprintf(`{"url":%q}`, url), 200)
	if ojson.GetBytes(preview, "status").String() != "duplicate" || ojson.GetBytes(preview, "registered_proxy_id").String() != pid || !bytes.Equal(disk, m3bDisk(t, f)) {
		t.Fatalf("duplicate preview mismatch: %s", preview)
	}
}
