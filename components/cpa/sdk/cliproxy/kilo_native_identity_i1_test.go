package cliproxy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func i1Credential(t *testing.T, f *kiloNativeFixture, id string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(f.cfg.AuthDir, id))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func TestI1LegacyIdentityAndInvalidFields(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy-first-profile", true: "invalid-save-and-selection"}[invalid], func(t *testing.T) {
			seen := make(chan string, 4)
			f := newKiloNativeFixture(t, m4MachineStream(seen), true)
			id := m2Create(t, f, "")
			path := filepath.Join(f.cfg.AuthDir, id)
			doc := ojson.NewDoc(i1Credential(t, f, id))
			machine := "e53925b5-bbf4-4c6c-9cca-67c0598854bf"
			if invalid {
				machine = "not-a-machine"
			}
			_ = doc.Set("machine_id", machine)
			_ = doc.Delete("device_profiles")
			if err := os.WriteFile(path, doc.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			m2Reload(t, f)
			if invalid {
				m2Request(t, f, "PATCH", kiloAccountsURL+"/"+id, `{"name":"invalid-identity"}`, 409)
				key := kiloInitialKey
				if _, saveErr := f.manager.SaveKiloDeviceAccount(context.Background(), id, coreauth.KiloAccountChanges{APIKey: &key}, ""); !errors.Is(saveErr, coreauth.ErrKiloMachineIdentity) {
					t.Fatalf("device save accepted invalid identity: %v", saveErr)
				}
				status, _ := f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","input":"invalid"}`)
				if status != 503 || f.posts.Load() != 0 {
					t.Fatalf("status=%d posts=%d", status, f.posts.Load())
				}
				if !bytes.Equal(i1Credential(t, f, id), doc.Bytes()) {
					t.Fatal("invalid identity replaced")
				}
				i1Released(t, f, 0)
				return
			}
			for i := 0; i < 2; i++ {
				status, body := f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","input":"legacy"}`)
				if status != 200 {
					t.Fatalf("status=%d %s", status, body)
				}
				if <-seen != machine {
					t.Fatal("legacy machine changed")
				}
				m2Reload(t, f)
			}
			raw := i1Credential(t, f, id)
			if ojson.GetBytes(raw, "device_profiles.0.id").String() != machine || ojson.GetBytes(raw, "device_profiles.0.machine_id").String() != machine {
				t.Fatal("first profile not durable")
			}
		})
	}
}
func TestI1ConcurrentInitializationAndWriteFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			seen := make(chan string, 4)
			f := newKiloNativeFixture(t, m4MachineStream(seen), true)
			id := m2Create(t, f, "")
			doc := ojson.NewDoc(i1Credential(t, f, id))
			_ = doc.Delete("machine_id")
			_ = doc.Delete("device_profiles")
			if err := os.WriteFile(filepath.Join(f.cfg.AuthDir, id), doc.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			m2Reload(t, f)
			counter := &m4CountStore{Store: m2FileStore(f)}
			f.manager.SetStore(counter)
			if fail {
				f.manager.SetStore(m2FailStore{Store: counter, save: true})
			}
			done := make(chan int, 2)
			for i := 0; i < 2; i++ {
				go func() {
					status, _ := f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","input":"initialize"}`)
					done <- status
				}()
			}
			want := 200
			if fail {
				want = 503
			}
			for i := 0; i < 2; i++ {
				if status := <-done; status != want {
					t.Fatalf("status=%d want=%d", status, want)
				}
			}
			if fail {
				if f.posts.Load() != 0 || counter.saves.Load() != 0 {
					t.Fatal("failed identity persistence reached upstream")
				}
			} else {
				first, second := <-seen, <-seen
				if first == "" || first != second || counter.backfills.Load() != 1 {
					t.Fatalf("initialization headers differ or saves=%d", counter.backfills.Load())
				}
			}
			results := 2
			if fail {
				results = 0
			}
			i1Released(t, f, results)
		})
	}
}
func TestI1LegacyBindingReloadAndTools(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			seen := make(chan kiloSessionSeen, 4)
			f := newKiloNativeFixture(t, kiloSessionUpstream(t, seen), true)
			id := m2Create(t, f, "")
			status, body := kiloSessionRequest(t, f, "/v1/responses", "synthetic-kilo-key", "i1-old-session", fmt.Sprintf(`{"model":"gpt-6.1-sol","input":"first","stream":%t}`, stream))
			if status != 200 {
				t.Fatalf("first=%d %s", status, body)
			}
			first := <-seen
			files, err := filepath.Glob(filepath.Join(config.KiloStateDirectory(f.cfg), "bindings", "*.json"))
			if err != nil || len(files) != 1 {
				t.Fatalf("bindings=%v err=%v", files, err)
			}
			raw, _ := os.ReadFile(files[0])
			doc := ojson.NewDoc(raw)
			for i := range doc.Get("bindings").Array() {
				_ = doc.Delete(fmt.Sprintf("bindings.%d.device_profile_id", i))
			}
			if err = os.WriteFile(files[0], doc.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			m2Reload(t, f)
			status, body = kiloSessionRequest(t, f, "/v1/responses", "synthetic-kilo-key", "i1-old-session", fmt.Sprintf(`{"model":"gpt-6.1-sol","previous_response_id":"resp-chain-1","input":[{"type":"function_call_output","call_id":"call-resp-chain-1","output":"ok"}],"stream":%t}`, stream))
			if status != 200 {
				t.Fatalf("continuation=%d %s", status, body)
			}
			second := <-seen
			if first != second {
				t.Fatal("reload or legacy binding switched device/session")
			}
			raw, _ = os.ReadFile(files[0])
			machine := ojson.GetBytes(i1Credential(t, f, id), "machine_id").String()
			if ojson.GetBytes(raw, "bindings.0.device_profile_id").String() != machine {
				t.Fatal("legacy binding did not persist first profile")
			}
			i1Released(t, f, 2)
		})
	}
}

type i1Observed struct {
	IP                       int
	Machine, Remote, Session string
}

func i1IPPolicy(t *testing.T, f *kiloNativeFixture, id string, specified bool, want int) []byte {
	t.Helper()
	state := m2Request(t, f, "GET", kiloProxiesURL, "", 200)
	mode, ips := "auto", "[]"
	if specified {
		mode = "specified"
		ips = `["203.0.113.1","203.0.113.2"]`
	}
	return m2Request(t, f, "PUT", kiloAccountsURL+"/"+id+"/egress", fmt.Sprintf(`{"expected_revision":%d,"mode":"auto","exclusive_proxy_ids":[],"primary_proxy_id":"","capacity_mode":"ip","allocation_mode":%q,"max_ips":2,"specified_ips":%s}`, ojson.GetBytes(state, "revision").Uint(), mode, ips), want)
}
func TestI1TwoIPsContinuationReclaimAndIsolation(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	seen := make(chan i1Observed, 12)
	release := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	var posts atomic.Int32
	i1NativeIPs(t, f, 2, func(ip int, w http.ResponseWriter, r *http.Request) {
		n := posts.Add(1)
		seen <- i1Observed{ip, r.Header.Get("X-KILOCODE-MACHINEID"), r.RemoteAddr, r.Header.Get("X-Session-Id")}
		if n <= 2 {
			<-release
		}
		doc := ojson.NewDocString(kiloTerminal)
		_ = doc.Set("response.id", fmt.Sprintf("i1-response-%d", n))
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "data: %s\n\n", doc.String())
	})
	state := m2Request(t, f, "GET", kiloProxiesURL, "", 200)
	m2Request(t, f, "PATCH", "/v8/management/kilo/proxy-pool", fmt.Sprintf(`{"expected_revision":%d,"ip_concurrency_limit":1}`, ojson.GetBytes(state, "revision").Uint()), 200)
	id := m2Create(t, f, "")
	i1IPPolicy(t, f, id, false, 200)
	done := make(chan ux2Reply, 2)
	go func() {
		done <- ux2Request(context.Background(), f, "i1-first", `{"model":"gpt-6.1-sol","input":"first"}`)
	}()
	first := i1Observe(t, seen)
	go func() {
		done <- ux2Request(context.Background(), f, "i1-second", `{"model":"gpt-6.1-sol","input":"second","stream":true}`)
	}()
	second := i1Observe(t, seen)
	if first.IP == second.IP || first.Machine == second.Machine || first.Machine == "" || second.Machine == "" {
		t.Fatalf("IP profiles=%+v %+v", first, second)
	}
	once.Do(func() { close(release) })
	for i := 0; i < 2; i++ {
		result := <-done
		if result.err != nil || result.status != 200 {
			t.Fatalf("request=%+v", result)
		}
	}
	raw := i1Credential(t, f, id)
	if len(ojson.GetBytes(raw, "device_profiles").Array()) != 2 {
		t.Fatal("two IPs did not produce two durable profiles")
	}
	for _, item := range []struct {
		session  string
		expected i1Observed
	}{{"i1-first", first}, {"i1-second", second}} {
		result := ux2Request(context.Background(), f, item.session, `{"model":"gpt-6.1-sol","input":"continue"}`)
		if result.status != 200 || result.err != nil {
			t.Fatalf("continuation=%+v", result)
		}
		got := <-seen
		if got.Machine != item.expected.Machine || got.IP != item.expected.IP || got.Session != item.expected.Session {
			t.Fatalf("continuation changed=%+v want=%+v", got, item.expected)
		}
		if got.Remote != item.expected.Remote {
			t.Fatal("same profile did not reuse its connection")
		}
	}
	i1Released(t, f, 4)
	m2Request(t, f, "PATCH", kiloAccountsURL+"/"+id, `{"enabled":false}`, 200)
	f.manager.SyncKiloAutoEgress(time.Now())
	f.manager.SyncKiloAutoEgress(time.Now())
	after := i1Credential(t, f, id)
	if !bytes.Equal([]byte(ojson.GetBytes(raw, "device_profiles").Raw), []byte(ojson.GetBytes(after, "device_profiles").Raw)) {
		t.Fatal("reclaim deleted identity profiles")
	}
	other := m2Create(t, f, "")
	i1IPPolicy(t, f, other, true, 200)
	result := ux2Request(context.Background(), f, "i1-other", `{"model":"gpt-6.1-sol","input":"other"}`)
	if result.status != 200 {
		t.Fatalf("other=%+v", result)
	}
	got := <-seen
	if got.Machine == first.Machine || got.Machine == second.Machine {
		t.Fatal("IP transfer carried old account identity")
	}
	m2Request(t, f, "PATCH", kiloAccountsURL+"/"+other, `{"enabled":false}`, 200)
	f.manager.SyncKiloAutoEgress(time.Now())
	f.manager.SyncKiloAutoEgress(time.Now())
	m2Request(t, f, "PATCH", kiloAccountsURL+"/"+id, `{"enabled":true}`, 200)
	i1IPPolicy(t, f, id, true, 200)
	result = ux2Request(context.Background(), f, "i1-first", `{"model":"gpt-6.1-sol","input":"restored"}`)
	if result.status != 200 {
		t.Fatalf("restore=%+v", result)
	}
	got = <-seen
	if got.Machine != first.Machine {
		t.Fatal("restored account changed its retained profile")
	}
	m2Reload(t, f)
	result = ux2Request(context.Background(), f, "i1-second", `{"model":"gpt-6.1-sol","input":"restart"}`)
	if result.status != 200 {
		t.Fatalf("restart=%+v", result)
	}
	got = <-seen
	if got.Machine != second.Machine {
		t.Fatal("restart changed second profile")
	}
	i1Released(t, f, 7)
}
func TestI1ManualGroupPersistenceFailureIsAtomic(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	var posts atomic.Int32
	i1NativeIPs(t, f, 2, func(_ int, w http.ResponseWriter, r *http.Request) { posts.Add(1); kiloSuccessStream(w, r) })
	id := m2Create(t, f, "")
	before := m2Request(t, f, "GET", kiloProxiesURL, "", 200)
	f.manager.SetStore(m2FailStore{Store: m2FileStore(f), save: true})
	i1IPPolicy(t, f, id, true, 503)
	after := m2Request(t, f, "GET", kiloProxiesURL, "", 200)
	if ojson.GetBytes(after, "revision").Uint() != ojson.GetBytes(before, "revision").Uint() || posts.Load() != 0 || len(ojson.GetBytes(i1Credential(t, f, id), "device_profiles").Array()) != 1 {
		t.Fatal("failed group save published profiles or capacity")
	}
	i1Released(t, f, 0)
	f.manager.SetStore(m2FileStore(f))
	i1IPPolicy(t, f, id, true, 200)
	if len(ojson.GetBytes(i1Credential(t, f, id), "device_profiles").Array()) != 2 {
		t.Fatal("manual group did not save all identities")
	}
	result := ux2Request(context.Background(), f, "i1-after-failure", `{"model":"gpt-6.1-sol","input":"available"}`)
	if result.status != 200 || posts.Load() != 1 {
		t.Fatalf("post failure=%+v posts=%d", result, posts.Load())
	}
	i1Released(t, f, 1)
}

func i1Observe(t *testing.T, seen <-chan i1Observed) i1Observed {
	t.Helper()
	select {
	case value := <-seen:
		return value
	case <-time.After(10 * time.Second):
		t.Fatal("identity upstream did not receive request")
		return i1Observed{}
	}
}
func i1Released(t *testing.T, f *kiloNativeFixture, results int) {
	t.Helper()
	ux2Wait(t, "identity request retained a lease or result count differs", func() bool {
		if len(f.hook.snapshot()) != results {
			return false
		}
		for _, p := range f.executor.KiloProxySnapshot().Proxies {
			if p.Active != 0 {
				return false
			}
		}
		for _, a := range f.manager.CredentialConcurrencySnapshot()["credentials"].([]coreauth.CredentialConcurrencyEntry) {
			if a.Active != 0 {
				return false
			}
		}
		return true
	})
}
