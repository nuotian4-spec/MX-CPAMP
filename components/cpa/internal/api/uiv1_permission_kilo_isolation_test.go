package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/MoeForge/moego/ojson"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func TestUIV1PermissionReleasePreservesKiloRestriction(t *testing.T) {
	server, manager, _, store := newBPSAccessServer(t)
	auth, err := manager.Register(coreauth.WithSkipPersist(context.Background()), &coreauth.Auth{
		ID: "uiv1-kilo-restricted", Provider: "kilo", Status: coreauth.StatusActive,
		Metadata: map[string]any{"kilo_auto_restriction": map[string]any{"reason": "account_restricted", "generation": uint64(7)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	before, _ := manager.GetByID(auth.ID)
	body := ojson.NewDocString(`{"provider":"kilo","scopes":[{"scope":"credential"}]}`)
	_ = body.Set("auth_id", auth.ID)
	_ = body.Set("expected_registration_epoch", strconv.FormatUint(auth.RegistrationEpoch, 10))
	_ = body.Set("expected_credential_version", strconv.FormatUint(auth.CredentialVersion, 10))
	_ = body.Set("expected_generation", strconv.FormatUint(auth.Generation, 10))
	call := func(authorized bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/v8/management/credentials/permission-suspensions/release", strings.NewReader(body.String()))
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("Content-Type", "application/json")
		if authorized {
			req.Header.Set("Authorization", "Bearer test-password")
		}
		response := httptest.NewRecorder()
		server.engine.ServeHTTP(response, req)
		return response
	}
	if response := call(false); response.Code != http.StatusUnauthorized {
		t.Fatalf("未认证请求未被管理鉴权拦截: %d", response.Code)
	}
	server.managementRoutesEnabled.Store(false)
	if response := call(true); response.Code != http.StatusNotFound {
		t.Fatalf("关闭管理入口后仍可释放: %d", response.Code)
	}
	server.managementRoutesEnabled.Store(true)
	if response := call(true); response.Code != http.StatusBadRequest {
		t.Fatalf("Kilo凭据进入BPS释放路径: %d %s", response.Code, response.Body.String())
	}
	_ = body.Set("provider", "bps")
	if response := call(true); response.Code != http.StatusNotFound {
		t.Fatalf("伪报BPS来源改变Kilo凭据: %d %s", response.Code, response.Body.String())
	}
	after, _ := manager.GetByID(auth.ID)
	if !reflect.DeepEqual(before, after) || store.writes.Load() != 0 {
		t.Fatal("BPS释放路径改变Kilo限制或持久凭据")
	}
	t.Log("正式v8路由401/关闭404；Kilo释放400、伪报BPS404；Kilo凭据快照与限制逐项保持，凭据写入0")
}
