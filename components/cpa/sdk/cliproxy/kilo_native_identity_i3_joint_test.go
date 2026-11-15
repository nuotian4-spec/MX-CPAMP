package cliproxy

import (
	"bytes"
	"context"
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
)

// This bridge runs the real Core and Manager routers without model or probe I/O.
func TestKiloNativeManagerClientIdentityI3JointBridge(t *testing.T) {
	const managerTest = "TestKiloManagerClientIdentityI3JointRuntime"
	goBin, managerDir := os.Getenv("KILO_JOINT_GO_BIN"), os.Getenv("KILO_JOINT_MANAGER_DIR")
	if goBin == "" || managerDir == "" {
		t.Skip("I3 joint runtime not configured; integration is not verified")
	}
	f, store := i3IdentityFixture(t)
	before := i3ReadSnapshot(t, f)
	stateBefore := i3StateBytes(t, f)
	viewBefore, err := f.executor.KiloProxyState()
	if err != nil {
		t.Fatal(err)
	}
	var reads, phases atomic.Int32
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/scenario" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			reads.Add(1)
			doc := ojson.NewDocString("{}")
			if err := doc.Set("accounts", []string{"i3-persisted.json", "i3-legacy.json", "i3-pending.json", "i3-invalid.json", "i3-stale.json", "i3-unbound.json"}); err != nil {
				t.Error(err)
				w.WriteHeader(500)
				return
			}
			_, _ = w.Write(doc.Bytes())
			return
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil || r.Method != http.MethodPost || ojson.GetBytes(raw, "mode").String() != "identity-i3" {
			w.WriteHeader(400)
			return
		}
		phases.Add(1)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer control.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, goBin, "test", "-json", "-count=1", "-run", "^"+managerTest+"$", "./internal/http/router")
	command.Dir = managerDir
	command.Env = append(kiloAccountsJointEnvironment(f.server.URL, control.URL), "KILO_JOINT_MANAGEMENT_KEY="+kiloManagementKey)
	output, err := command.CombinedOutput()
	t.Logf("Manager I3 joint result:\n%s", output)
	if err != nil {
		t.Fatalf("Manager I3 joint: %v", err)
	}
	ran, passed := false, false
	for _, line := range strings.Split(string(output), "\n") {
		if ojson.Get(line, "Test").String() != managerTest {
			continue
		}
		switch ojson.Get(line, "Action").String() {
		case "run":
			ran = true
		case "pass":
			passed = true
		case "skip":
			t.Fatal("I3 Manager joint skipped")
		}
	}
	if !ran || !passed || reads.Load() != 1 || phases.Load() != 1 {
		t.Fatalf("I3 joint evidence: ran=%v passed=%v reads=%d phases=%d", ran, passed, reads.Load(), phases.Load())
	}
	viewAfter, err := f.executor.KiloProxyState()
	if err != nil || !reflect.DeepEqual(viewBefore, viewAfter) || !bytes.Equal(stateBefore, i3StateBytes(t, f)) || !reflect.DeepEqual(before, i3ReadSnapshot(t, f)) {
		t.Fatal("I3 joint changed identity, egress relations or capacity")
	}
	if store.saves.Load() != 0 || f.posts.Load() != 0 || f.probes.Load() != 0 || f.connects.Load() != 0 || len(f.hook.snapshot()) != 0 {
		t.Fatalf("I3 joint side effects: saves=%d model=%d probe=%d connect=%d results=%d", store.saves.Load(), f.posts.Load(), f.probes.Load(), f.connects.Load(), len(f.hook.snapshot()))
	}
	t.Log("I3 dual Router read-only identity PASS: 6 accounts, same-IP aliases, direct/legacy/idle/skew, unchanged identity/state/capacity; Save/model/probe/connect/results=0")
}
