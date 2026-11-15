package cliproxy

import (
	"context"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
)

func TestUX2QuotaSnapshotSurvivesCompletedInference(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	id := m2Create(t, f, "")
	m5HealthyQueryRoute(t, f)
	m5Codex(t, f, "ux2-quota-source.json", "ux2-outer", "synthetic-outer", false, false)
	m5Bind(t, f, id, 0, `{"kind":"cpa_codex","credential_id":"ux2-quota-source.json","chatgpt_account_id":"ux2-outer"}`)
	f.usageHandler = func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, m5UsageBody) }
	before := m5Query(t, f, id, 1)
	m5Assert(t, before, "success", "")
	authBefore, _ := f.manager.GetByID(id)
	result := ux2Request(context.Background(), f, "ux2-quota-preserve", `{"model":"gpt-6.1-sol","input":"完成推理后保留额度观测"}`)
	if result.err != nil || result.status != 200 {
		t.Fatalf("正式推理状态 %d: %s %v", result.status, result.body, result.err)
	}
	ux2Wait(t, "正式推理结果尚未记录", func() bool { return len(f.hook.snapshot()) == 1 })
	authAfter, _ := f.manager.GetByID(id)
	if authAfter.Generation <= authBefore.Generation {
		t.Fatal("正式结果未推进通用Generation")
	}
	after := m2Request(t, f, "GET", kiloAccountsURL+"/"+id+"/quota", "", 200)
	m5Assert(t, after, "success", "")
	for _, path := range []string{"windows", "query.observed_at", "query.attempted_at", "binding_revision"} {
		if ojson.GetBytes(before, path).Raw != ojson.GetBytes(after, path).Raw {
			t.Fatalf("推理改变了额度字段 %s", path)
		}
	}
	if ojson.GetBytes(after, "stale").Bool() || f.posts.Load() != 1 || f.usageConnects.Load() != 1 {
		t.Fatalf("额度快照或调用计数异常：%s", after)
	}
	t.Log("正式专用Key推理成功、Generation增长后，窗口和观测时间原值保留；模型POST1、额度GET1")
}

func TestUX2QuotaQueryAllowsInferenceCompletion(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	id := m2Create(t, f, "")
	m5HealthyQueryRoute(t, f)
	m5Codex(t, f, "ux2-quota-source.json", "ux2-outer", "synthetic-outer", false, false)
	m5Bind(t, f, id, 0, `{"kind":"cpa_codex","credential_id":"ux2-quota-source.json","chatgpt_account_id":"ux2-outer"}`)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	f.usageHandler = func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		_, _ = io.WriteString(w, m5UsageBody)
	}
	type reply struct {
		status int
		body   []byte
	}
	done := make(chan reply, 1)
	go func() {
		status, body := f.request(t, "POST", kiloAccountsURL+"/"+id+"/quota/query", kiloManagementKey, `{"expected_binding_revision":1}`)
		done <- reply{status, body}
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("额度请求未进入真实TLS上游")
	}
	result := ux2Request(context.Background(), f, "ux2-quota-concurrent", `{"model":"gpt-6.1-sol","input":"额度查询期间完成正式推理"}`)
	if result.err != nil || result.status != 200 {
		t.Fatalf("正式推理状态 %d: %s %v", result.status, result.body, result.err)
	}
	ux2Wait(t, "正式推理结果尚未记录", func() bool { return len(f.hook.snapshot()) == 1 })
	once.Do(func() { close(release) })
	select {
	case got := <-done:
		if got.status != 200 {
			t.Fatalf("额度查询被普通推理错误拒绝：%d %s", got.status, got.body)
		}
		m5Assert(t, got.body, "success", "")
	case <-time.After(5 * time.Second):
		t.Fatal("额度查询未收口")
	}
	view := m2Request(t, f, "GET", kiloAccountsURL+"/"+id+"/quota", "", 200)
	if ojson.GetBytes(view, "query.observed_at").Type == ojson.Null || len(ojson.GetBytes(view, "windows").Array()) == 0 {
		t.Fatalf("完成的额度观测未发布：%s", view)
	}
	if f.posts.Load() != 1 || f.usageConnects.Load() != 1 {
		t.Fatal("出现额外模型或额度请求")
	}
	t.Log("真实额度GET在途穿插正式推理成功后仍HTTP200发布窗口；模型POST1、额度GET1")
}
