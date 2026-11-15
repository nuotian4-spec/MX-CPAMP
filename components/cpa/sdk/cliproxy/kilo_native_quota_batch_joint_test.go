package cliproxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
)

func TestKiloNativeManagerQuotaBatchJointBridge(t *testing.T) {
	const managerTest = "TestKiloManagerQuotaBatchWebJointRuntime"
	goBin, managerDir := os.Getenv("KILO_JOINT_GO_BIN"), os.Getenv("KILO_JOINT_MANAGER_DIR")
	if goBin == "" || managerDir == "" || os.Getenv("KILO_JOINT_NODE_BIN") == "" || os.Getenv("KILO_JOINT_WEB_DIR") == "" {
		t.Skip("batch joint runtime not configured; integration is not verified")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	m5Codex(t, f, "codex-batch.json", "outer-batch", "synthetic-outer-batch", false, false)
	ids := make([]string, 0, 4)
	for range 4 {
		id := m2Create(t, f, "")
		m4oDirect(t, f, id)
		m5Bind(t, f, id, 0, `{"kind":"cpa_codex","credential_id":"codex-batch.json","chatgpt_account_id":"outer-batch"}`)
		ids = append(ids, id)
	}
	var usages, active, maximum, verifications atomic.Int32
	var barrierReached atomic.Bool
	ready := make(chan struct{})
	f.usageHandler = func(w http.ResponseWriter, r *http.Request) {
		count := usages.Add(1)
		current := active.Add(1)
		defer active.Add(-1)
		for {
			previous := maximum.Load()
			if current <= previous || maximum.CompareAndSwap(previous, current) {
				break
			}
		}
		if r.Method != "GET" || r.URL.Path != "/backend-api/wham/usage" || r.Header.Get("Authorization") != "Bearer synthetic-outer-batch" || r.Header.Get("ChatGPT-Account-Id") != "outer-batch" {
			t.Error("batch usage source or route mismatch")
		}
		if count <= 3 {
			if count == 3 {
				barrierReached.Store(true)
				close(ready)
			}
			select {
			case <-ready:
			case <-r.Context().Done():
				t.Error("batch did not reach the three-request barrier")
				return
			}
		} else if count != 4 || !barrierReached.Load() {
			t.Error("batch fourth request crossed barrier out of sequence")
		}
		_, _ = io.WriteString(w, m5UsageBody)
	}
	verify := func() bool {
		valid := usages.Load() == 4 && maximum.Load() == 3 && active.Load() == 0 && barrierReached.Load() && f.posts.Load() == 0 && f.usageDirect.Load() == 4 && f.usageConnects.Load() == 0 && f.oauthConnects.Load() == 0
		for _, id := range ids {
			v, err := f.manager.KiloQuota(id)
			valid = valid && err == nil && v.BindingRevision == 1 && v.Source.Kind == "cpa_codex" && v.Source.CredentialID == "codex-batch.json" && v.Query.Status == "success" && v.Query.ObservedAt != nil && !v.Stale && len(v.Windows) == 3 && v.Windows[0].RemainingPercent != nil && *v.Windows[0].RemainingPercent == 75
		}
		return valid
	}
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && r.URL.Path == "/fixture" {
			doc := ojson.NewDocString(`{"credential_id":"codex-batch.json"}`)
			_ = doc.Set("account_ids", ids)
			_, _ = w.Write(doc.Bytes())
			return
		}
		if r.Method == "POST" && r.URL.Path == "/verify" {
			verifications.Add(1)
			if !verify() {
				t.Error("batch bridge verification failed")
				w.WriteHeader(500)
				return
			}
			_, _ = io.WriteString(w, `{"ok":true,"usage_requests":4,"max_inflight":3,"model_requests":0}`)
			return
		}
		w.WriteHeader(404)
	}))
	defer control.Close()
	command := exec.CommandContext(ctx, goBin, "test", "-json", "-count=1", "-run", "^"+managerTest+"$", "./internal/http/router")
	command.Dir = managerDir
	command.Env = append(kiloAccountsJointEnvironment(f.server.URL, control.URL), "KILO_JOINT_MANAGEMENT_KEY="+kiloManagementKey)
	output, err := command.CombinedOutput()
	t.Logf("Manager/Web batch joint result:\n%s", output)
	if err != nil {
		t.Fatalf("Manager/Web batch joint failed: %v", err)
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
			t.Fatal("batch joint skipped")
		}
	}
	if !ran || !passed || verifications.Load() != 1 || !verify() {
		t.Fatalf("batch joint final evidence mismatch: usage=%d max=%d active=%d verify=%d direct=%d", usages.Load(), maximum.Load(), active.Load(), verifications.Load(), f.usageDirect.Load())
	}
	t.Log("real Web/Manager/Core batch PASS: four bound Kilo accounts, actual usage GET4/direct4, max in-flight3 reached by deterministic barrier, first window75% each, no OAuth/model POST")
}
