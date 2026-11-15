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
	"sync/atomic"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func TestKiloNativeManagerManualTestJointBridge(t *testing.T) {
	const managerTest = "TestKiloManagerManualTestWebJointRuntime"
	const normal = "gpt-6.1-sol"
	goBin, managerDir := os.Getenv("KILO_JOINT_GO_BIN"), os.Getenv("KILO_JOINT_MANAGER_DIR")
	if goBin == "" || managerDir == "" || os.Getenv("KILO_JOINT_NODE_BIN") == "" || os.Getenv("KILO_JOINT_WEB_DIR") == "" {
		t.Skip("manual joint runtime not configured; integration is not verified")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var posts, normalPosts, autoPosts atomic.Int32
	f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
		n := posts.Add(1)
		if n == 1 {
			m6Failure(m6QuotaFailure, 200)(w, r)
			return
		}
		if r.URL.Path == "/api/v1/chat/completions" {
			autoPosts.Add(1)
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"M8M auto generated\"},\"finish_reason\":\"stop\"}]}\n\n")
			return
		}
		if r.URL.Path != "/api/v1/responses" {
			t.Error("unexpected diagnostic path")
		}
		normalPosts.Add(1)
		kiloSuccessStream(w, r)
	}, true)
	f.allowAutoFree = true
	id := m2Create(t, f, `,"enabled":false,"name":"M8M disabled restricted"`)
	seed := m8Manual(t, f, id, normal)
	if ojson.GetBytes(seed, "status").String() != "failed" {
		t.Fatal("synthetic restriction not registered")
	}
	<-f.requests
	f.assertReleased(t, 1)
	before, _ := f.manager.GetByID(id)
	restriction := f.manager.KiloRestriction(id)
	if !before.Disabled || restriction.Reason != "quota_exhausted" {
		t.Fatal("fixture state missing")
	}
	var verified atomic.Int32
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && r.URL.Path == "/fixture" {
			doc := ojson.NewDocString("{}")
			_ = doc.Set("account_id", id)
			_ = doc.Set("normal_model", normal)
			_, _ = w.Write(doc.Bytes())
			return
		}
		if r.Method != "POST" || r.URL.Path != "/verify" {
			w.WriteHeader(404)
			return
		}
		body, _ := io.ReadAll(r.Body)
		after, _ := f.manager.GetByID(id)
		results := f.hook.snapshot()
		valid := ojson.GetBytes(body, "account_id").String() == id && ojson.GetBytes(body, "normal_model").String() == normal && ojson.GetBytes(body, "tests").Int() == 2 &&
			f.posts.Load() == 3 && posts.Load() == 3 && normalPosts.Load() == 1 && autoPosts.Load() == 1 && len(results) == 3 &&
			after != nil && after.Disabled && after.Status == before.Status && after.InferenceVerified == before.InferenceVerified && reflect.DeepEqual(restriction, f.manager.KiloRestriction(id))
		if len(results) == 3 {
			for _, result := range results[1:] {
				valid = valid && result.AuthID == id && result.Provider == "kilo" && result.Success
			}
		}
		for _, proxy := range f.executor.KiloProxySnapshot().Proxies {
			valid = valid && proxy.Active == 0 && proxy.Owner == ""
		}
		snapshot := f.manager.CredentialConcurrencySnapshot()
		valid = valid && snapshot["pool_waiting"] == 0
		for _, entry := range snapshot["credentials"].([]coreauth.CredentialConcurrencyEntry) {
			valid = valid && entry.Active == 0 && entry.PinnedWaiting == 0
		}
		if len(f.requests) != 2 {
			valid = false
		} else {
			first, second := <-f.requests, <-f.requests
			valid = valid && ojson.GetBytes(first, "model").String() == "openai/"+normal && ojson.GetBytes(first, "input").String() == "M8M normal synthetic" &&
				ojson.GetBytes(second, "model").String() == "kilo-auto/free" && ojson.GetBytes(second, "messages.#").Int() == 1 && ojson.GetBytes(second, "messages.0.content").String() == "M8M auto synthetic"
		}
		if !valid {
			t.Errorf("manual joint evidence mismatch: posts=%d normal=%d auto=%d results=%+v", posts.Load(), normalPosts.Load(), autoPosts.Load(), results)
			w.WriteHeader(500)
			return
		}
		verified.Add(1)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer control.Close()
	command := exec.CommandContext(ctx, goBin, "test", "-json", "-count=1", "-run", "^"+managerTest+"$", "./internal/http/router")
	command.Dir = managerDir
	command.Env = append(kiloAccountsJointEnvironment(f.server.URL, control.URL), "KILO_JOINT_MANAGEMENT_KEY="+kiloManagementKey)
	output, err := command.CombinedOutput()
	t.Logf("Manager manual joint result:\n%s", output)
	if err != nil {
		t.Fatalf("Manager manual joint failed: %v", err)
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
			t.Fatal("manual joint skipped")
		}
	}
	if !ran || !passed || verified.Load() != 1 {
		t.Fatal("manual joint missing real evidence")
	}
	f.assertReleased(t, 3)
	t.Log("real account page/Manager Router/AdminAuth/connection_ref/Core Router/manual admission/Executor/Transport/MarkResult PASS; setup restriction POST1 excluded; Normal1+AutoFree1 completed; Disabled/restriction/verification retained; account/proxy/waiters0")
}
