package cliproxy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/api"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/pluginhost"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/access"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/api/handlers"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
	"gopkg.in/yaml.v3"
)

// The BPS capability uses the same live host-record boundary as the M1 fixture.
// It is not an external plugin binary and never substitutes the Kilo executor.
type m9BPSCapability struct{ url string }

type m9BPSModels struct{}

type m9CodexTransport struct{ transport http.RoundTripper }

func (p m9CodexTransport) RoundTripperFor(a *coreauth.Auth) http.RoundTripper {
	if a != nil && a.Provider == "codex" {
		return p.transport
	}
	return nil
}

func (m9BPSModels) StaticModels(context.Context, pluginapi.StaticModelRequest) (pluginapi.ModelResponse, error) {
	return pluginapi.ModelResponse{Provider: "cpa-bps", Models: []pluginapi.ModelInfo{{ID: "m9-bps-model", Object: "model", DisplayName: "M9 BPS", UserDefined: true}}}, nil
}

func (m9BPSModels) ModelsForAuth(context.Context, pluginapi.AuthModelRequest) (pluginapi.ModelResponse, error) {
	return pluginapi.ModelResponse{Provider: "cpa-bps", Models: []pluginapi.ModelInfo{{ID: "m9-bps-model", Object: "model", DisplayName: "M9 BPS", UserDefined: true}}}, nil
}

func (*m9BPSCapability) Identifier() string { return "synthetic-provider-distinct-from-bps" }
func (e *m9BPSCapability) Execute(ctx context.Context, req pluginapi.ExecutorRequest) (pluginapi.ExecutorResponse, error) {
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, e.url, bytes.NewReader(req.Payload))
	if err != nil {
		return pluginapi.ExecutorResponse{}, err
	}
	if token, ok := req.AuthMetadata["access_token"].(string); ok && token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Transport: &http.Transport{Proxy: nil}}
	defer client.CloseIdleConnections()
	response, err := client.Do(r)
	if err != nil {
		return pluginapi.ExecutorResponse{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	return pluginapi.ExecutorResponse{Payload: body}, err
}
func (*m9BPSCapability) ExecuteStream(context.Context, pluginapi.ExecutorRequest) (pluginapi.ExecutorStreamResponse, error) {
	return pluginapi.ExecutorStreamResponse{}, fmt.Errorf("unexpected BPS stream in key fixture")
}
func (*m9BPSCapability) CountTokens(context.Context, pluginapi.ExecutorRequest) (pluginapi.ExecutorResponse, error) {
	return pluginapi.ExecutorResponse{}, fmt.Errorf("unexpected BPS count in key fixture")
}
func (*m9BPSCapability) HttpRequest(context.Context, pluginapi.ExecutorHTTPRequest) (pluginapi.ExecutorHTTPResponse, error) {
	return pluginapi.ExecutorHTTPResponse{}, fmt.Errorf("unexpected BPS HTTP request in key fixture")
}
func (*m9BPSCapability) RouteModel(_ context.Context, req pluginapi.ModelRouteRequest) (pluginapi.ModelRouteResponse, error) {
	if req.RequestedModel == "m9-bps-model" {
		return pluginapi.ModelRouteResponse{Handled: true, TargetKind: pluginapi.ModelRouteTargetProvider, Target: "cpa-bps"}, nil
	}
	return pluginapi.ModelRouteResponse{}, nil
}

func TestKiloNativeManagerChannelKeysJointBridge(t *testing.T) {
	const managerTest = "TestKiloManagerChannelKeysWebJointRuntime"
	goBin, managerDir := os.Getenv("KILO_JOINT_GO_BIN"), os.Getenv("KILO_JOINT_MANAGER_DIR")
	if goBin == "" || managerDir == "" || os.Getenv("KILO_JOINT_NODE_BIN") == "" || os.Getenv("KILO_JOINT_WEB_DIR") == "" {
		t.Skip("channel key joint runtime not configured; integration is not verified")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	f := newKiloNativeFixture(t, kiloSuccessStream)
	f.cfg.APIKeys = append(f.cfg.APIKeys, "m9-legacy")
	f.cfg.CredentialInFlight = config.DefaultCredentialInFlightConfig()
	var codexPosts, bpsPosts, puts, reads, reloads, verifications atomic.Int32
	f.usageHandler = func(w http.ResponseWriter, r *http.Request) {
		n := codexPosts.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/backend-api/codex/responses" || r.Header.Get("Authorization") != "Bearer m9-codex-token" {
			t.Errorf("Codex production request mismatch: %s %s", r.Method, r.URL.Path)
		}
		terminal := ojson.NewDocString(kiloTerminal)
		_ = terminal.Set("response.id", fmt.Sprintf("m9-codex-%d", n))
		_ = terminal.Set("response.model", "m9-codex-model")
		_ = terminal.Set("response.output.0.content.0.text", "codex-ok")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "event: response.completed\ndata: %s\n\n", terminal.String())
	}
	codex := m5Codex(t, f, "m9-codex.json", "m9-codex-account", "m9-codex-token", false, false)
	// The explicit test transport below owns the loopback CONNECT proxy and root.
	// An auth proxy would select the independent production uTLS trust store.
	codex.ProxyURL = ""
	if _, err := f.manager.Update(context.Background(), codex); err != nil {
		t.Fatal(err)
	}
	codexTransport := http.DefaultTransport.(*http.Transport).Clone()
	proxyURL, err := url.Parse(f.cfg.Kilo.Proxies[0].URL)
	if err != nil {
		t.Fatal(err)
	}
	codexTransport.Proxy = http.ProxyURL(proxyURL)
	f.manager.SetRoundTripperProvider(m9CodexTransport{transport: codexTransport})
	t.Cleanup(codexTransport.CloseIdleConnections)
	registry.GetGlobalRegistry().RegisterClient(codex.ID, "codex", []*registry.ModelInfo{{ID: "m9-codex-model", Object: "model", OwnedBy: "codex", Type: "codex", UserDefined: true}})
	t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(codex.ID) })
	bps := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := bpsPosts.Add(1)
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer m9-bps-token" {
			t.Error("BPS fixture request material mismatch")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil || ojson.GetBytes(body, "input.0.content.0.text").String() != "key channel check" || ojson.GetBytes(body, "messages").Exists() {
			t.Errorf("BPS Responses input bridge missing: %s error=%v", body, err)
		}
		w.Header().Set("Content-Type", "application/json")
		doc := ojson.NewDocString(`{"object":"response","status":"completed","model":"m9-bps-model","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"bps-ok"}]}],"usage":{"input_tokens":3,"output_tokens":1,"total_tokens":4}}`)
		_ = doc.Set("id", fmt.Sprintf("m10-bps-%d", n))
		_, _ = io.WriteString(w, doc.String())
	}))
	defer bps.Close()
	if _, err := f.manager.Register(context.Background(), &coreauth.Auth{ID: "m9-bps.json", Provider: "cpa-bps", Status: coreauth.StatusActive, Metadata: map[string]any{"access_token": "m9-bps-token"}}); err != nil {
		t.Fatal(err)
	}
	registry.GetGlobalRegistry().RegisterClient("m9-bps.json", "cpa-bps", []*registry.ModelInfo{{ID: "m9-bps-model", Object: "model", OwnedBy: "bps", Type: "bps", UserDefined: true}})
	t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient("m9-bps.json") })
	capability := &m9BPSCapability{url: bps.URL}
	host := pluginhost.NewForTest(nil)
	host.RegisterPluginForTest("cpa-bps", pluginapi.Plugin{Metadata: pluginapi.Metadata{Name: "BPS fixture", Version: "1.0.0", Author: "test", GitHubRepository: "https://github.com/router-for-me/CLIProxyAPI"}, Capabilities: pluginapi.Capabilities{Executor: capability, ModelRouter: capability, ModelProvider: m9BPSModels{}, ExecutorInputFormats: []string{"openai-response"}, ExecutorOutputFormats: []string{"openai-response"}}})
	host.RegisterModels(context.Background(), registry.GetGlobalRegistry())
	host.RegisterExecutors(f.manager, registry.GetGlobalRegistry())
	if channel, ok := host.PluginExecutorChannel("cpa-bps"); !ok || channel != "bps" {
		t.Fatal("BPS fixture did not acquire the actual host channel mapping")
	}
	raw, err := yaml.Marshal(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	raw, _, err = config.NormalizeConfigLayout(raw, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := config.ParseConfigBytes(raw); err != nil {
		t.Fatalf("synthetic initial configuration is invalid: %v", err)
	}
	if err = os.WriteFile(f.configPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	reloaded := make(chan struct{}, 8)
	var server *api.Server
	server = api.NewServer(f.cfg, f.manager, access.NewManager(), f.configPath,
		api.WithLocalManagementPassword(kiloManagementKey), api.WithPluginHost(host),
		api.WithRouterConfigurator(func(_ *gin.Engine, h *handlers.BaseAPIHandler, _ *config.Config) { h.SetModelRouterHost(host) }),
		api.WithConfigReloadHook(func(ctx context.Context, cfg *config.Config) {
			f.manager.SetConfig(cfg)
			if !server.UpdateClientsContext(ctx, cfg) {
				t.Error("saved configuration was not applied to the live router")
			}
			reloads.Add(1)
			reloaded <- struct{}{}
		}),
		api.WithMiddleware(func(c *gin.Context) {
			if c.Request.URL.Path == "/v8/management/config.yaml" {
				if c.Request.Method == http.MethodPut {
					puts.Add(1)
					if c.Request.Header.Get("If-Match") == "" {
						t.Error("key transaction omitted If-Match at Core")
					}
				} else if c.Request.Method == http.MethodGet {
					reads.Add(1)
				}
			}
		}),
	)
	f.server.Close()
	f.server = httptest.NewServer(server.Handler())
	f.server.Client().Transport = &http.Transport{Proxy: nil}
	t.Cleanup(func() { f.server.Client().CloseIdleConnections(); f.server.Close() })
	if evidenceDir := os.Getenv("KILO_JOINT_EVIDENCE_DIR"); evidenceDir != "" {
		if err := os.WriteFile(filepath.Join(evidenceDir, "fixture-initial.yaml"), raw, 0600); err != nil {
			t.Fatal(err)
		}
		req, _ := http.NewRequest(http.MethodGet, f.server.URL+"/v8/management/config.yaml", nil)
		req.Header.Set("Authorization", "Bearer "+kiloManagementKey)
		response, err := f.server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(evidenceDir, "fixture-core-get.yaml"), body, 0600); err != nil {
			t.Fatal(err)
		}
		var info bytes.Buffer
		_, _ = fmt.Fprintf(&info, "HTTP=%d\n", response.StatusCode)
		_ = response.Header.Write(&info)
		if err := os.WriteFile(filepath.Join(evidenceDir, "fixture-core-get-headers.txt"), info.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
	}
	verify := func() error {
		for reloads.Load() < 6 {
			select {
			case <-reloaded:
			case <-ctx.Done():
				return fmt.Errorf("configuration reload incomplete: %d", reloads.Load())
			}
		}
		if puts.Load() != 6 || reads.Load() < 7 || f.posts.Load() != 0 || codexPosts.Load() != 0 || bpsPosts.Load() != 0 {
			return fmt.Errorf("configuration-only phase mismatch: PUT=%d GET=%d Kilo=%d Codex=%d BPS=%d", puts.Load(), reads.Load(), f.posts.Load(), codexPosts.Load(), bpsPosts.Load())
		}
		modelFor := map[string]string{"kilo": "gpt-6.1-sol", "codex": "m9-codex-model", "bps": "m9-bps-model"}
		for _, caller := range []string{"kilo", "codex", "bps", "legacy"} {
			key := "m9-" + caller
			if caller == "kilo" {
				key = "m9-kilo-rotated"
			}
			for _, target := range []string{"kilo", "codex", "bps"} {
				doc := ojson.NewDocString(`{"messages":[{"role":"user","content":"key channel check"}],"stream":false}`)
				_ = doc.Set("model", modelFor[target])
				status, body := f.request(t, "POST", "/v1/chat/completions", key, doc.String())
				allowed := caller == target || caller == "legacy" && target != "kilo"
				want := http.StatusForbidden
				if allowed {
					want = http.StatusOK
				}
				if status != want || allowed && !bytes.Contains(body, []byte(target+"-ok")) {
					return fmt.Errorf("channel %s -> %s: HTTP=%d want=%d body=%s", caller, target, status, want, body)
				}
				t.Logf("saved key %s -> %s HTTP=%d", caller, target, status)
			}
		}
		for _, key := range []string{"m9-kilo", "m9-temporary"} {
			status, _ := f.request(t, "POST", "/v1/chat/completions", key, `{"model":"gpt-6.1-sol","messages":[{"role":"user","content":"revoked"}]}`)
			if status != http.StatusUnauthorized {
				return fmt.Errorf("removed key %s HTTP=%d", key, status)
			}
		}
		if status, _ := f.request(t, "POST", "/v1/chat/completions", kiloManagementKey, `{"model":"gpt-6.1-sol","messages":[{"role":"user","content":"management key"}]}`); status != http.StatusUnauthorized {
			return fmt.Errorf("management key reached inference: HTTP=%d", status)
		}
		for _, key := range []string{"m9-kilo-rotated", "m9-codex", "m9-bps", "m9-legacy", ""} {
			status, _ := f.request(t, "GET", "/v8/management/config.yaml", key, "")
			if status != http.StatusUnauthorized {
				return fmt.Errorf("inference key granted Core management: HTTP=%d", status)
			}
		}
		if f.posts.Load() != 1 || codexPosts.Load() != 2 || bpsPosts.Load() != 2 || puts.Load() != 6 {
			return fmt.Errorf("final counters Kilo=%d Codex=%d BPS=%d PUT=%d", f.posts.Load(), codexPosts.Load(), bpsPosts.Load(), puts.Load())
		}
		return nil
	}
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && r.URL.Path == "/fixture" {
			_, _ = io.WriteString(w, `{"legacy_key":"m9-legacy"}`)
			return
		}
		if r.Method == "POST" && r.URL.Path == "/verify" {
			verifications.Add(1)
			body, _ := io.ReadAll(r.Body)
			if ojson.GetBytes(body, "keys.kilo").String() != "m9-kilo-rotated" || ojson.GetBytes(body, "keys.codex").String() != "m9-codex" || ojson.GetBytes(body, "keys.bps").String() != "m9-bps" || ojson.GetBytes(body, "legacy").String() != "m9-legacy" {
				t.Error("Web verification did not name the persisted keys")
				w.WriteHeader(400)
				return
			}
			if err := verify(); err != nil {
				t.Error(err)
				w.WriteHeader(500)
				return
			}
			_, _ = io.WriteString(w, `{"ok":true,"config_puts":6,"kilo_posts":1,"codex_posts":2,"bps_posts":2}`)
			return
		}
		w.WriteHeader(404)
	}))
	defer control.Close()
	command := exec.CommandContext(ctx, goBin, "test", "-json", "-count=1", "-run", "^"+managerTest+"$", "./internal/http/router")
	command.Dir = managerDir
	command.Env = append(kiloAccountsJointEnvironment(f.server.URL, control.URL), "KILO_JOINT_MANAGEMENT_KEY="+kiloManagementKey)
	output, err := command.CombinedOutput()
	t.Logf("Manager/Web channel key result:\n%s", output)
	if err != nil {
		t.Fatalf("Manager/Web channel key joint failed: %v", err)
	}
	ran, passed := false, false
	for _, line := range strings.Split(string(output), "\n") {
		if !ojson.Valid(line) || ojson.Get(line, "Test").String() != managerTest {
			continue
		}
		switch ojson.Get(line, "Action").String() {
		case "run":
			ran = true
		case "pass":
			passed = true
		case "skip":
			t.Fatal("channel key joint skipped")
		}
	}
	if !ran || !passed || verifications.Load() != 1 || puts.Load() != 6 || f.posts.Load() != 1 || codexPosts.Load() != 2 || bpsPosts.Load() != 2 {
		t.Fatal("joint channel key evidence incomplete")
	}
	f.assertReleased(t, 5)
	counts := map[string]int{}
	for _, result := range f.hook.snapshot() {
		if !result.Success {
			t.Fatal("channel key verification recorded a failed model result")
		}
		counts[result.Provider]++
	}
	if counts["kilo"] != 1 || counts["codex"] != 2 || counts["cpa-bps"] != 2 {
		t.Fatalf("provider result counts mismatch: %v", counts)
	}
	snapshot := f.manager.CredentialConcurrencySnapshot()
	if snapshot["pool_waiting"] != 0 {
		t.Fatalf("pool waiters remain: %v", snapshot["pool_waiting"])
	}
	for _, entry := range snapshot["credentials"].([]coreauth.CredentialConcurrencyEntry) {
		if entry.PinnedWaiting != 0 {
			t.Fatal("pinned account waiters remain")
		}
	}
	t.Log("real ConfigPage/KeyEditor/API -> Manager Router -> Core v8 PASS: six guarded PUTs, live reload, Kilo POST1, Codex POST2, registered BPS host Responses-to-Chat bridge POST2, cross-channel POST0, legacy Codex/BPS retained and Kilo denied")
}
