package cliproxy

import (
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

func TestKiloNativeManagerRecoveryJointBridge(t *testing.T) {
	const managerTest = "TestKiloManagerRecoveryJointRuntime"
	goBin, managerDir := os.Getenv("KILO_JOINT_GO_BIN"), os.Getenv("KILO_JOINT_MANAGER_DIR")
	if goBin == "" || managerDir == "" {
		t.Skip("recovery joint runtime not configured; integration is not verified")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var responses atomic.Int32
	f, id := m6Fixture(t, func(w http.ResponseWriter, r *http.Request) {
		if responses.Add(1) == 1 {
			m6Failure(m6QuotaFailure, 200)(w, r)
		} else {
			m6Failure(`{"error":{"code":"account_restricted"}}`, 403)(w, r)
		}
	}, false)
	m6Request(t, f, false)
	restrictedID := m2Create(t, f, ` ,"name":"restricted-other"`)
	m4oDirect(t, f, restrictedID)
	status, _ := f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","input":"second"}`)
	if status != 403 {
		t.Fatalf("second synthetic status=%d", status)
	}
	f.assertReleased(t, 2)
	before := f.manager.KiloRestriction(id)
	other := f.manager.KiloRestriction(restrictedID)
	if before.Reason != "quota_exhausted" || other.Reason != "account_restricted" {
		t.Fatalf("fixture reasons=%+v %+v", before, other)
	}
	var phases atomic.Int32
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && r.URL.Path == "/fixture" {
			doc := ojson.NewDocString("{}")
			_ = doc.Set("account_id", id)
			_ = doc.Set("restricted_account_id", restrictedID)
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
		v := f.manager.KiloRestriction(id)
		sibling := f.manager.KiloRestriction(restrictedID)
		a, _ := f.manager.GetByID(id)
		b, _ := f.manager.GetByID(restrictedID)
		valid := a != nil && b != nil && !a.Disabled && !b.Disabled && sibling.Reason == "account_restricted" && sibling.Generation == other.Generation && sibling.Persisted && f.posts.Load() == 2 && f.usageDirect.Load() == 0
		switch phase {
		case "before":
			valid = valid && step == 1 && v.Reason == "quota_exhausted" && v.Generation == before.Generation && v.Persisted
		case "restored":
			raw, err := os.ReadFile(filepath.Join(f.cfg.AuthDir, id))
			valid = valid && step == 2 && v.Reason == "none" && v.Generation > before.Generation && v.Persisted && err == nil && ojson.GetBytes(raw, "kilo_auto_restriction.reason").String() == "none"
		default:
			valid = false
		}
		if !valid {
			t.Errorf("joint recovery phase=%s step=%d", phase, step)
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
	t.Logf("Manager recovery joint result:\n%s", output)
	if err != nil {
		t.Fatalf("Manager recovery joint failed: %v", err)
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
			t.Fatal("joint recovery skipped")
		}
	}
	if !ran || !passed || phases.Load() != 2 || f.posts.Load() != 2 {
		t.Fatal("joint recovery missing evidence")
	}
	m2Reload(t, f)
	if f.manager.KiloRestriction(id).Reason != "none" || f.manager.KiloRestriction(restrictedID).Reason != "account_restricted" {
		t.Fatal("joint restored state not durable")
	}
	t.Log("real Manager/Core PASS: same-value enabled=true durably restores only selected auto restriction; sibling restricted retained; creation model2, list/restore/reload model0 usage0")
}
