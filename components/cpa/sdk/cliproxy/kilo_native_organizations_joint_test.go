package cliproxy

import (
	"context"
	"fmt"
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
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func TestKiloNativeManagerOrganizationsJointBridge(t *testing.T) {
	const managerTest = "TestKiloManagerOrganizationsJointRuntime"
	goBin, managerDir := os.Getenv("KILO_JOINT_GO_BIN"), os.Getenv("KILO_JOINT_MANAGER_DIR")
	if goBin == "" || managerDir == "" {
		t.Skip("organization joint runtime not configured; integration is not verified")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, goBin, "test", "-list", "^"+managerTest+"$", "./internal/http/router")
	command.Dir, command.Env = managerDir, kiloAccountsJointEnvironment("", "")
	listed, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("Manager target discovery %v\n%s", err, listed)
	}
	found := false
	for _, line := range strings.Split(string(listed), "\n") {
		found = found || strings.TrimSpace(line) == managerTest
	}
	if !found {
		t.Fatal("Manager organization joint target absent")
	}
	seen := make(chan kiloSessionSeen, 10)
	f := newKiloNativeFixture(t, kiloSessionUpstream(t, seen), true)
	id := m2Create(t, f, "")
	m4oDirect(t, f, id)
	a, _ := f.manager.GetByID(id)
	machine, initialRevision := coreauth.KiloMachineID(a), coreauth.KiloIdentity(a).Revision
	var profiles, denied, phases atomic.Int32
	base := m4oProfile(t, &profiles)
	f.profileHandler = func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer synthetic-denied-key" {
			profiles.Add(1)
			denied.Add(1)
			if r.Method != "GET" || r.URL.Path != "/api/profile" || r.Header.Get("Accept") != "application/json" {
				t.Error("candidate profile protocol mismatch")
			}
			_, _ = io.WriteString(w, `{"organizations":[],"hasPersonalAccount":false}`)
			return
		}
		base(w, r)
	}
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "GET" && r.URL.Path == "/fixture" {
			doc := ojson.NewDocString(`{"replacement_key":"synthetic-denied-key"}`)
			_ = doc.Set("account_id", id)
			_, _ = w.Write(doc.Bytes())
			return
		}
		if r.Method != "POST" || r.URL.Path != "/verify" {
			w.WriteHeader(404)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		phase := ojson.GetBytes(raw, "phase").String()
		expected, number := "", int32(0)
		switch phase {
		case "first":
			expected, number = "org_first", 1
		case "second":
			expected, number = "org_second", 2
		case "personal":
			expected, number = "", 3
		default:
			w.WriteHeader(400)
			return
		}
		if phases.Add(1) != number {
			t.Error("joint phase ordering changed")
			w.WriteHeader(500)
			return
		}
		account, _ := f.manager.GetByID(id)
		if account == nil || coreauth.KiloIdentity(account).Organization != expected || coreauth.KiloMachineID(account) != machine || coreauth.KiloIdentity(account).Revision != initialRevision+uint64(number) {
			t.Error("committed org/revision/machine differs")
			w.WriteHeader(500)
			return
		}
		if phase == "second" {
			for _, body := range []string{`{"model":"gpt-6.1-sol","previous_response_id":"resp-chain-1","input":"stale response"}`, `{"model":"gpt-6.1-sol","input":[{"type":"function_call_output","call_id":"call-resp-chain-1","output":"stale tool"}]}`} {
				status, _ := f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", body)
				if status != 409 {
					t.Errorf("old binding status=%d", status)
				}
			}
			status, _ := kiloSessionRequest(t, f, "/v1/responses", "synthetic-kilo-key", "joint-org-session", `{"model":"gpt-6.1-sol","input":"stale session"}`)
			if status != 409 || f.posts.Load() != 1 {
				t.Error("old organization binding sent upstream")
			}
		}
		f.expectedOrganization.Store(expected)
		session := ""
		if phase == "first" {
			session = "joint-org-session"
		}
		status, data := kiloSessionRequest(t, f, "/v1/responses", "synthetic-kilo-key", session, fmt.Sprintf(`{"model":"gpt-6.1-sol","input":"joint %s"}`, phase))
		if status != 200 {
			t.Errorf("new identity inference=%d %s", status, data)
			w.WriteHeader(500)
			return
		}
		if observation := <-seen; observation.Account != machine {
			t.Error("inference header UUID changed")
		}
		f.assertReleased(t, int(number))
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer control.Close()
	command = exec.CommandContext(ctx, goBin, "test", "-json", "-count=1", "-run", "^"+managerTest+"$", "./internal/http/router")
	command.Dir = managerDir
	command.Env = append(kiloAccountsJointEnvironment(f.server.URL, control.URL), "KILO_JOINT_MANAGEMENT_KEY="+kiloManagementKey)
	output, err := command.CombinedOutput()
	t.Logf("Manager organization joint result:\n%s", output)
	if err != nil {
		t.Fatalf("Manager organization joint failed: %v", err)
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
			t.Fatal("Manager joint skipped")
		}
	}
	if !ran || !passed || phases.Load() != 3 || profiles.Load() != 5 || denied.Load() != 1 || f.posts.Load() != 3 || f.deviceDirect.Load() != 5 || f.deviceConnects.Load() != 0 {
		t.Fatalf("joint counts ran=%v pass=%v phases=%d profile=%d denied=%d model=%d direct=%d CONNECT=%d", ran, passed, phases.Load(), profiles.Load(), denied.Load(), f.posts.Load(), f.deviceDirect.Load(), f.deviceConnects.Load())
	}
	raw, err := os.ReadFile(filepath.Join(f.cfg.AuthDir, id))
	if err != nil || ojson.GetBytes(raw, "organization_id").String() != "" || ojson.GetBytes(raw, "api_key").String() != kiloInitialKey || ojson.GetBytes(raw, "machine_id").String() != machine {
		t.Fatal("final durable credential mismatch")
	}
	m2Reload(t, f)
	a, _ = f.manager.GetByID(id)
	if a == nil || coreauth.KiloMachineID(a) != machine || coreauth.KiloIdentity(a).Organization != "" {
		t.Fatal("organization reload differs")
	}
	t.Log("real Manager/Core Router PASS profileGET5/direct5/CONNECT0; denied candidate key1; explicit synthetic modelPOST3; old response/tool/session409 addPOST0; UUID stable; final personal space; reload1")
}
