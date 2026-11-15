package cliproxy

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func TestKiloNativeManagerProbesJointBridge(t *testing.T) {
	const managerTest = "TestKiloManagerProbesWebJointRuntime"
	const normal = "gpt-6.1-sol"
	goBin, managerDir := os.Getenv("KILO_JOINT_GO_BIN"), os.Getenv("KILO_JOINT_MANAGER_DIR")
	if goBin == "" || managerDir == "" || os.Getenv("KILO_JOINT_NODE_BIN") == "" || os.Getenv("KILO_JOINT_WEB_DIR") == "" {
		t.Skip("automatic probe joint runtime not configured; integration is not verified")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/chat/completions" {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"health observed\"},\"finish_reason\":\"stop\"}]}\n\n")
			return
		}
		if r.URL.Path != "/api/v1/responses" {
			t.Error("unexpected automatic model route")
		}
		kiloSuccessStream(w, r)
	}, true)
	f.allowAutoFree = true
	id := m2Create(t, f, `,"name":"M8BM configured target"`)
	blockedID := m2Create(t, f, `,"enabled":false,"name":"M8BM disabled sidecar"`)
	m8bConfig(t, f, blockedID, 0, true, true, 300)
	blocked, _ := f.manager.GetByID(blockedID)
	// This sidecar starts with a stored restriction; it never sends a model call.
	blocked.Metadata["kilo_auto_restriction"] = map[string]any{"reason": "quota_exhausted", "generation": f.manager.KiloRestriction(blockedID).Generation + 1, "identity_revision": coreauth.KiloIdentity(blocked).Revision, "source_revision": uint64(0), "recover_at": ""}
	if _, err := f.manager.Update(t.Context(), blocked); err != nil {
		t.Fatal(err)
	}
	restriction := f.manager.KiloRestriction(blockedID)
	if restriction.Reason != "quota_exhausted" {
		t.Fatal("stored sidecar restriction not retained")
	}
	var mu sync.Mutex
	phase := 0
	var due time.Time
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method == "GET" && r.URL.Path == "/fixture" {
			doc := ojson.NewDocString("{}")
			_ = doc.Set("account_id", id)
			_ = doc.Set("normal_model", normal)
			_, _ = w.Write(doc.Bytes())
			return
		}
		if r.Method != "POST" || (r.URL.Path != "/advance" && r.URL.Path != "/verify") {
			w.WriteHeader(404)
			return
		}
		body, _ := io.ReadAll(r.Body)
		step := ojson.GetBytes(body, "phase").String()
		view, _ := f.manager.KiloProbes(id)
		valid := false
		switch step {
		case "defaults":
			valid = phase == 0 && !view.Health.Enabled && !view.ModelProbe.Enabled && view.Revision == 0
			<-f.manager.ProcessKiloProbes(ctx, time.Now())
			valid = valid && f.posts.Load() == 0
		case "before_due":
			valid = phase == 1 && view.Revision == 1 && view.Health.Enabled && view.ModelProbe.Enabled && view.ModelProbe.Model == normal && view.ModelProbe.IntervalSeconds == 300 && view.Health.NextRunAt != nil && view.ModelProbe.NextRunAt != nil
			if valid {
				due = *view.Health.NextRunAt
				valid = view.ModelProbe.NextRunAt.Equal(due)
				<-f.manager.ProcessKiloProbes(ctx, due.Add(-time.Second))
				valid = valid && f.posts.Load() == 0
			}
		case "first_due":
			if phase == 2 {
				<-f.manager.ProcessKiloProbes(ctx, due)
				view, _ = f.manager.KiloProbes(id)
				valid = f.posts.Load() == 1 && view.Health.ModelCalls == 1 && view.Health.Status == "completed" && view.ModelProbe.ModelCalls == 0 && view.ModelProbe.Status == "skipped"
			}
		case "second_due":
			if phase == 3 {
				<-f.manager.ProcessKiloProbes(ctx, due.Add(300*time.Second))
				view, _ = f.manager.KiloProbes(id)
				valid = f.posts.Load() == 2 && view.Health.ModelCalls == 1 && view.ModelProbe.ModelCalls == 1 && view.ModelProbe.Status == "completed"
			}
		case "":
			valid = r.URL.Path == "/verify" && phase == 4 && ojson.GetBytes(body, "account_id").String() == id && ojson.GetBytes(body, "normal_model").String() == normal && view.Revision == 2 && !view.Health.Enabled && !view.ModelProbe.Enabled && view.Health.NextRunAt == nil && view.ModelProbe.NextRunAt == nil
			<-f.manager.ProcessKiloProbes(ctx, due.Add(time.Hour))
			view, _ = f.manager.KiloProbes(id)
			other, _ := f.manager.KiloProbes(blockedID)
			a, _ := f.manager.GetByID(id)
			b, _ := f.manager.GetByID(blockedID)
			valid = valid && f.posts.Load() == 2 && view.ManualModelCalls == 0 && view.Health.ModelCalls == 1 && view.ModelProbe.ModelCalls == 1 &&
				other.Health.ModelCalls == 0 && other.ModelProbe.ModelCalls == 0 && other.Health.Status == "skipped" && other.ModelProbe.Status == "skipped" &&
				a != nil && !a.Disabled && !a.InferenceVerified && b != nil && b.Disabled && reflect.DeepEqual(restriction, f.manager.KiloRestriction(blockedID))
			results := f.hook.snapshot()
			valid = valid && len(results) == 2
			for _, result := range results {
				valid = valid && result.AuthID == id && result.Provider == "kilo" && result.Success
			}
			snapshot := f.manager.CredentialConcurrencySnapshot()
			valid = valid && snapshot["pool_waiting"] == 0
			for _, entry := range snapshot["credentials"].([]coreauth.CredentialConcurrencyEntry) {
				valid = valid && entry.Active == 0 && entry.PinnedWaiting == 0
			}
			for _, proxy := range f.executor.KiloProxySnapshot().Proxies {
				valid = valid && proxy.Active == 0 && proxy.Owner == ""
			}
			if len(f.requests) != 2 {
				valid = false
			} else {
				health, model := <-f.requests, <-f.requests
				valid = valid && ojson.GetBytes(health, "model").String() == "kilo-auto/free" && ojson.GetBytes(health, "messages.0.content").String() == "hi" && ojson.GetBytes(model, "model").String() == "openai/"+normal && ojson.GetBytes(model, "input").String() == "hi"
			}
		}
		if !valid {
			t.Errorf("automatic joint phase=%d/%s posts=%d view=%+v", phase, step, f.posts.Load(), view)
			w.WriteHeader(500)
			return
		}
		phase++
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer control.Close()
	command := exec.CommandContext(ctx, goBin, "test", "-json", "-count=1", "-run", "^"+managerTest+"$", "./internal/http/router")
	command.Dir = managerDir
	command.Env = append(kiloAccountsJointEnvironment(f.server.URL, control.URL), "KILO_JOINT_MANAGEMENT_KEY="+kiloManagementKey)
	output, err := command.CombinedOutput()
	t.Logf("Manager automatic probe joint result:\n%s", output)
	if err != nil {
		t.Fatalf("Manager automatic probe joint failed: %v", err)
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
			t.Fatal("automatic joint skipped")
		}
	}
	mu.Lock()
	finalPhase := phase
	mu.Unlock()
	if !ran || !passed || finalPhase != 5 {
		t.Fatal("automatic joint missing real evidence")
	}
	f.assertReleased(t, 2)
	t.Log("real account page/Manager/Core probes configuration and observations PASS; defaults0/299s0/300shealth1/600smodel1; normal and AutoFree hi; saved off adds0; disabled sidecar generates0 and retains stored restriction; two successful results; account/proxy/waiters0")
}
