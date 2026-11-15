package management

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"gopkg.in/yaml.v3"
)

func newCPABPSModelsHandler(t *testing.T, id string) *Handler {
	t.Helper()
	file := writeTestConfigFile(t)
	initial := "plugins:\n  enabled: true\n  dir: plugins\n  configs:\n    " + id + ":\n      enabled: true\n      models: [existing-model]\n      fast_mode: false\n"
	if errWrite := os.WriteFile(file, []byte(initial), 0600); errWrite != nil {
		t.Fatal(errWrite)
	}
	return &Handler{
		cfg: &config.Config{Plugins: config.PluginsConfig{
			Enabled: true, Dir: "plugins",
			Configs: map[string]config.PluginInstanceConfig{
				id: pluginConfigFromYAML(t, "enabled: true\nmodels: [existing-model]\nfast_mode: false\n"),
			},
		}},
		configFilePath: file,
	}
}

func callCPABPSModelsHandler(h *Handler, method, id, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Params = gin.Params{{Key: "id", Value: id}}
	c.Request = httptest.NewRequest(method, "/v0/management/plugins/"+id+"/config", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	if method == http.MethodPut {
		h.PutPluginConfig(c)
	} else {
		h.PatchPluginConfig(c)
	}
	return rec
}

func TestCPABPSModelsRejectsInvalidBeforeMutation(t *testing.T) {
	for _, method := range []string{http.MethodPut, http.MethodPatch} {
		for _, models := range []string{`[{"id":"gpt-6-luna","name":"gpt-6-luna"}]`, `[1]`, `["gpt-6-luna",1]`, `{"id":"gpt-6-luna"}`, `"gpt-6-luna"`} {
			t.Run(method+"/"+models, func(t *testing.T) {
				h := newCPABPSModelsHandler(t, "cpa-bps")
				beforeConfig := h.cfg.CloneForRuntime()
				before := marshalPluginRaw(t, h.cfg.Plugins.Configs["cpa-bps"])
				disk, errRead := os.ReadFile(h.configFilePath)
				if errRead != nil {
					t.Fatal(errRead)
				}
				var reloads atomic.Int32
				h.SetConfigReloadHook(func(context.Context, *config.Config) { reloads.Add(1) })
				rec := callCPABPSModelsHandler(h, method, "cpa-bps", `{"enabled":false,"models":`+models+`}`)
				if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"error":"invalid_config"`) || !strings.Contains(rec.Body.String(), "字符串数组") {
					t.Fatalf("响应应为中文格式提示与400 invalid_config：status=%d body=%s", rec.Code, rec.Body.String())
				}
				if got := marshalPluginRaw(t, h.cfg.Plugins.Configs["cpa-bps"]); got != before {
					t.Fatalf("拒绝请求改变了内存配置：%s", got)
				}
				if !reflect.DeepEqual(h.cfg, beforeConfig) {
					t.Fatal("拒绝请求改变了完整内存配置")
				}
				after, errRead := os.ReadFile(h.configFilePath)
				if errRead != nil || string(after) != string(disk) {
					t.Fatalf("拒绝请求改变了磁盘配置：%v", errRead)
				}
				h.mu.Lock()
				generation := h.reloadGeneration
				h.mu.Unlock()
				if generation != 0 || reloads.Load() != 0 {
					t.Fatalf("拒绝请求触发了重载：generation=%d hook=%d", generation, reloads.Load())
				}
			})
		}
	}
}

func TestCPABPSModelsPreservesAcceptedConfigSemantics(t *testing.T) {
	for _, tc := range []struct {
		name, method, id, body string
		models                 any
		present                bool
	}{
		{"put_strings", http.MethodPut, "cpa-bps", `{"enabled":true,"models":["gpt-6-luna"]}`, []any{"gpt-6-luna"}, true},
		{"patch_strings", http.MethodPatch, "cpa-bps", `{"models":["gpt-6-luna"]}`, []any{"gpt-6-luna"}, true},
		{"put_empty", http.MethodPut, "cpa-bps", `{"enabled":true,"models":[]}`, []any{}, true},
		{"patch_empty", http.MethodPatch, "cpa-bps", `{"models":[]}`, []any{}, true},
		{"put_missing", http.MethodPut, "cpa-bps", `{"enabled":true}`, nil, false},
		{"patch_missing", http.MethodPatch, "cpa-bps", `{"fast_mode":true}`, []any{"existing-model"}, true},
		{"patch_delete", http.MethodPatch, "cpa-bps", `{"models":null}`, nil, false},
		{"other_put_objects", http.MethodPut, "other-plugin", `{"enabled":true,"models":[{"id":"custom"}]}`, []any{map[string]any{"id": "custom"}}, true},
		{"other_patch_objects", http.MethodPatch, "other-plugin", `{"models":[{"id":"custom"}]}`, []any{map[string]any{"id": "custom"}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newCPABPSModelsHandler(t, tc.id)
			reloads, done := captureConfigReload(h)
			rec := callCPABPSModelsHandler(h, tc.method, tc.id, tc.body)
			if rec.Code != http.StatusOK {
				t.Fatalf("合法配置应正常保存：status=%d body=%s", rec.Code, rec.Body.String())
			}
			snapshot := waitForAsyncReload(t, reloads)
			waitForReloadDone(t, done)
			data, errRead := os.ReadFile(h.configFilePath)
			if errRead != nil {
				t.Fatal(errRead)
			}
			var persisted config.Config
			if errDecode := yaml.Unmarshal(data, &persisted); errDecode != nil {
				t.Fatal(errDecode)
			}
			for name, cfg := range map[string]*config.Config{"内存": h.cfg, "磁盘": &persisted, "重载": snapshot} {
				item := cfg.Plugins.Configs[tc.id]
				var fields map[string]any
				if errDecode := item.Raw.Decode(&fields); errDecode != nil {
					t.Fatal(errDecode)
				}
				models, present := fields["models"]
				if present != tc.present || !reflect.DeepEqual(models, tc.models) {
					t.Fatalf("%s models 不符：present=%v value=%#v", name, present, models)
				}
				if !cfg.Plugins.Enabled || cfg.Plugins.Dir != "plugins" {
					t.Fatalf("%s全局插件开关或目录发生变化", name)
				}
			}
		})
	}
}
