package cliproxy

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func assertUX1BrowserReleased(t *testing.T, f *kiloNativeFixture, wantResults int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		pool := f.executor.KiloProxySnapshot()
		accounts := f.manager.CredentialConcurrencySnapshot()
		ready := len(f.hook.snapshot()) == wantResults && accounts["pool_waiting"] == 0
		for _, p := range pool.Proxies {
			// Exclusive ownership persists after its final HTTP lease is released.
			ownerReleased := p.Owner == "" || (p.AccountID != "" && p.Owner == p.AccountID)
			ready = ready && p.Active == 0 && ownerReleased
		}
		for _, a := range accounts["credentials"].([]coreauth.CredentialConcurrencyEntry) {
			ready = ready && a.Active == 0 && a.PinnedWaiting == 0
		}
		if ready {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("browser leases/results not released: pool=%+v results=%+v posts=%d accounts=%+v", f.executor.KiloProxySnapshot(), f.hook.snapshot(), f.posts.Load(), f.manager.CredentialConcurrencySnapshot())
}

func seedUX1BrowserSamples(t *testing.T, f *kiloNativeFixture, activeID, disabledID, invalidID string) (map[string]any, func(bool)) {
	t.Helper()
	state := m4Request(t, f, "GET", kiloProxiesURL, "", 200)
	create := func(name string) string {
		doc := ojson.NewDocString("{}")
		_ = doc.Set("expected_revision", ojson.GetBytes(state, "revision").Uint())
		_ = doc.Set("name", name)
		_ = doc.Set("url", f.cfg.Kilo.Proxies[0].URL)
		state = m4Request(t, f, "POST", kiloProxiesURL, doc.String(), 201)
		items := ojson.GetBytes(state, "proxies").Array()
		return items[len(items)-1].Get("id").String()
	}
	shared := create("共享备用出口")
	private := create("专属出口")
	unknown := create("尚未观测出口")
	set := func(id string, candidates, exclusive []string, primary string) {
		doc := ojson.NewDocString("{}")
		_ = doc.Set("expected_revision", ojson.GetBytes(state, "revision").Uint())
		_ = doc.Set("mode", "pool")
		_ = doc.Set("candidate_proxy_ids", candidates)
		_ = doc.Set("exclusive_proxy_ids", exclusive)
		_ = doc.Set("primary_proxy_id", primary)
		state = m4Request(t, f, "PUT", kiloAccountsURL+"/"+id+"/egress", doc.String(), 200)
	}
	set(activeID, []string{"local-proxy", shared}, []string{}, "local-proxy")
	set(invalidID, []string{private}, []string{private}, private)
	for _, id := range []string{"local-proxy", shared, private} {
		state = m4Request(t, f, "POST", kiloProxiesURL+"/"+id+"/recheck", fmt.Sprintf(`{"expected_revision":%d}`, ojson.GetBytes(state, "revision").Uint()), 200)
	}
	for _, entry := range []struct{ id, label string }{{"m10-codex-a.json", "Codex 团队 A"}, {"m10-codex-b.json", "Codex 团队 B"}} {
		a, _ := f.manager.GetByID(entry.id)
		a.Label = entry.label
		a.Metadata["label"] = entry.label
		if _, err := f.manager.Update(context.Background(), a); err != nil {
			t.Fatal(err)
		}
	}
	m5Bind(t, f, activeID, 0, `{"kind":"cpa_codex","credential_id":"m10-codex-a.json","chatgpt_account_id":"m10-outer-a"}`)
	m5Query(t, f, activeID, 1)
	m5Bind(t, f, disabledID, 0, `{"kind":"cpa_codex","credential_id":"m10-codex-b.json","chatgpt_account_id":"m10-outer-b"}`)
	m5Codex(t, f, "ux1-missing-codex.json", "ux1-missing-outer", "synthetic-missing", false, false)
	m5Bind(t, f, invalidID, 0, `{"kind":"cpa_codex","credential_id":"ux1-missing-codex.json","chatgpt_account_id":"ux1-missing-outer"}`)
	f.manager.Remove(context.Background(), "ux1-missing-codex.json")
	for _, provider := range []string{"claude", "bps"} {
		id := "ux1-" + provider + ".json"
		a := &coreauth.Auth{ID: id, FileName: id, Provider: provider, Label: provider + " 合成并发样本", Status: coreauth.StatusActive, Metadata: map[string]any{"type": provider, "label": provider + " 合成并发样本", "access_token": "synthetic-" + provider, "api_key": "synthetic-" + provider}, Attributes: map[string]string{coreauth.AttributePath: filepath.Join(f.cfg.AuthDir, id), coreauth.AttributeSource: filepath.Join(f.cfg.AuthDir, id), coreauth.AttributeSourceBackend: coreauth.AuthSourceFile}}
		if _, err := f.manager.Register(coreauth.WithAuthCreationIntent(context.Background()), a); err != nil {
			t.Fatal(err)
		}
	}
	var mu sync.Mutex
	var releases []func()
	occupancy := func(enabled bool) {
		mu.Lock()
		defer mu.Unlock()
		if !enabled {
			for _, release := range releases {
				release()
			}
			releases = nil
			return
		}
		if len(releases) > 0 {
			return
		}
		for _, id := range []string{activeID, "m10-codex-a.json", "m10-codex-a.json", "m10-codex-b.json", "ux1-claude.json", "ux1-bps.json"} {
			release, err := f.manager.AcquireCredentialGeneration(context.Background(), id)
			if err != nil {
				t.Errorf("synthetic admission %s: %v", id, err)
				return
			}
			releases = append(releases, release)
		}
		a, _ := f.manager.GetByID(activeID)
		_, release, wait, err := f.executor.TryAcquireEgress(a)
		if err != nil || wait {
			t.Errorf("synthetic egress wait=%v err=%v", wait, err)
			return
		}
		releases = append(releases, release)
	}
	occupancy(true)
	return map[string]any{"queried": activeID, "unqueried": disabledID, "invalid_source": invalidID, "shared_proxy": "local-proxy", "extra_shared_proxy": shared, "exclusive_proxy": private, "unknown_proxy": unknown, "concurrency_fixture": "real AcquireCredentialGeneration and KiloExecutor leases; toggle POST /options occupancy false"}, occupancy
}
