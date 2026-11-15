package cliproxy

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
)

func TestKiloNativeManagerQuotaJointBridge(t *testing.T) {
	const managerTest = "TestKiloManagerQuotaJointRuntime"
	goBin, managerDir := os.Getenv("KILO_JOINT_GO_BIN"), os.Getenv("KILO_JOINT_MANAGER_DIR")
	if goBin == "" || managerDir == "" {
		t.Skip("quota joint runtime not configured; integration is not verified")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	id := m2Create(t, f, "")
	m4oDirect(t, f, id)
	m5Codex(t, f, "codex-a.json", "outer-a", "synthetic-outer-old", true, true)
	m5Codex(t, f, "codex-b.json", "outer-b", "synthetic-outer-b", false, false)
	var outer, siwc, usages, phases atomic.Int32
	var importedFailed atomic.Bool
	f.oauthHandler = func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Error("joint OAuth method")
		}
		_ = r.ParseForm()
		if r.Form.Get("grant_type") != "refresh_token" {
			t.Error("joint OAuth grant")
		}
		if r.URL.Path == "/oauth/token" {
			outer.Add(1)
			if r.Form.Get("refresh_token") != "synthetic-refresh-codex-a.json" {
				t.Error("joint outer grant")
			}
			doc := ojson.NewDocString(`{"access_token":"synthetic-outer-current","refresh_token":"synthetic-refresh-current","expires_in":604800}`)
			_ = doc.Set("id_token", m5JWT("outer-a"))
			_, _ = w.Write(doc.Bytes())
		} else if r.URL.Path == "/api/accounts/oauth/token" {
			siwc.Add(1)
			if r.Form.Get("resource") != "https://api.openai.com/v1" || r.Form.Get("client_id") != "synthetic-issued-client" {
				t.Error("joint SIWC form")
			}
			_, _ = io.WriteString(w, `{"access_token":"synthetic-siwc-current","refresh_token":"synthetic-siwc-refresh-current","expires_in":604800}`)
		} else {
			t.Error("joint OAuth path")
			w.WriteHeader(500)
		}
	}
	f.usageHandler = func(w http.ResponseWriter, r *http.Request) {
		count := usages.Add(1)
		if r.Method != "GET" || r.URL.Path != "/backend-api/wham/usage" {
			t.Error("joint usage route")
		}
		wantToken, wantAccount := "synthetic-outer-current", "outer-a"
		if count > 1 {
			wantToken, wantAccount = "synthetic-imported-joint", "imported-account"
		}
		if r.Header.Get("Authorization") != "Bearer "+wantToken || r.Header.Get("ChatGPT-Account-Id") != wantAccount {
			t.Error("joint usage wrong material")
		}
		if importedFailed.Load() {
			w.WriteHeader(401)
			_, _ = io.WriteString(w, "private-upstream-secret")
			return
		}
		_, _ = io.WriteString(w, m5UsageBody)
	}
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && r.URL.Path == "/fixture" {
			doc := ojson.NewDocString(`{"credential_id":"codex-a.json","chatgpt_account_id":"outer-a","imported_access_token":"synthetic-imported-joint","imported_account_id":"imported-account"}`)
			_ = doc.Set("account_id", id)
			_, _ = w.Write(doc.Bytes())
			return
		}
		if r.Method != "POST" || r.URL.Path != "/verify" {
			w.WriteHeader(404)
			return
		}
		body, _ := io.ReadAll(r.Body)
		phase := ojson.GetBytes(body, "phase").String()
		step := phases.Add(1)
		v, err := f.manager.KiloQuota(id)
		if err != nil {
			t.Error(err)
			w.WriteHeader(500)
			return
		}
		valid := outer.Load() == 1 && siwc.Load() == 1 && f.posts.Load() == 0
		switch phase {
		case "reference":
			valid = valid && step == 1 && usages.Load() == 1 && v.BindingRevision == 1 && v.Query.Status == "success" && v.Source.CredentialID == "codex-a.json"
		case "imported":
			valid = valid && step == 2 && usages.Load() == 2 && v.BindingRevision == 2 && v.Query.Status == "success"
			importedFailed.Store(true)
		case "failed":
			valid = valid && step == 3 && usages.Load() == 3 && v.Source.Status == "needs_reimport" && v.Query.ObservedAt != nil && v.Stale
		case "unlinked":
			_, exists := f.manager.GetByID("codex-a.json")
			valid = valid && step == 4 && usages.Load() == 3 && v.BindingRevision == 3 && v.Source.Kind == "none" && exists
		default:
			valid = false
		}
		if !valid {
			t.Errorf("joint phase mismatch %s", phase)
			w.WriteHeader(500)
			return
		}
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer control.Close()
	command := exec.CommandContext(ctx, goBin, "test", "-json", "-count=1", "-run", "^"+managerTest+"$", "./internal/http/router")
	command.Dir = managerDir
	command.Env = append(kiloAccountsJointEnvironment(f.server.URL, control.URL), "KILO_JOINT_MANAGEMENT_KEY="+kiloManagementKey)
	output, err := command.CombinedOutput()
	t.Logf("Manager quota joint result:\n%s", output)
	if err != nil {
		t.Fatalf("Manager quota joint failed: %v", err)
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
			t.Fatal("joint skipped")
		}
	}
	if !ran || !passed || phases.Load() != 4 || outer.Load() != 1 || siwc.Load() != 1 || usages.Load() != 3 || f.usageDirect.Load() != 3 || f.posts.Load() != 0 {
		t.Fatal("joint final counts mismatch")
	}
	raw, err := os.ReadFile(filepath.Join(f.cfg.AuthDir, id))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"synthetic-imported", "synthetic-outer", "synthetic-refresh", "synthetic-siwc"} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Fatal("unlinked Kilo retained material")
		}
	}
	m2Reload(t, f)
	view, err := f.manager.KiloQuota(id)
	if err != nil || view.BindingRevision != 3 || view.Source.Kind != "none" {
		t.Fatal("unlinked revision lost on reload")
	}
	t.Log("real Manager/Core Router PASS: original outerOAuth1/SIWCOAuth1, usageGET3/direct3/CONNECT0; imported refresh0; usage401 keeps prior observed_at/values; unlink revision3 keeps Codex; reload preserved; model0")
}
