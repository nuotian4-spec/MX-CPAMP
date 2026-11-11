package helps

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MoeForge/moego/base64"
	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/basispoints"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

type bpsImageTransport func(*http.Request) (*http.Response, error)

func (f bpsImageTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type bpsImageCancelBody struct {
	ctx    context.Context
	closed chan struct{}
}

func (body *bpsImageCancelBody) Read([]byte) (int, error) {
	<-body.ctx.Done()
	return 0, body.ctx.Err()
}
func (body *bpsImageCancelBody) Close() error { close(body.closed); return nil }

func TestBPSImageUploadContextDeadline(t *testing.T) {
	for _, shortParent := range []bool{false, true} {
		t.Run(map[bool]string{false: "60秒子截止", true: "保留较短父截止"}[shortParent], func(t *testing.T) {
			parent := context.Background()
			cancel := func() {}
			if shortParent {
				parent, cancel = context.WithTimeout(parent, 5*time.Second)
			}
			defer cancel()
			start := time.Now()
			var uploadedContext context.Context
			ctx := context.WithValue(parent, "cliproxy.roundtripper", bpsImageTransport(func(r *http.Request) (*http.Response, error) {
				uploadedContext = r.Context()
				deadline, present := uploadedContext.Deadline()
				if !present {
					t.Fatal("附件上传没有截止时间")
				}
				if parentDeadline, ok := parent.Deadline(); ok {
					if !deadline.Equal(parentDeadline) {
						t.Fatal("未保留更短父截止时间")
					}
				} else if deadline.Before(start.Add(60*time.Second)) || deadline.After(time.Now().Add(60*time.Second)) {
					t.Fatal("上传时限不是60秒", deadline)
				}
				if r.Header.Get("User-Agent") != "Mozilla/5.0" {
					t.Fatal("附件User-Agent未沿用供体")
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"openai_file_id":"file-deadline"}`))}, nil
			}))
			wire, _, _, err := PrepareBPSImages(ctx, &config.Config{}, bpsTestAuth(), bpsImageBody(t, bpsImagePart(bpsTestImage(t, 20))), "")
			if err != nil || len(wire) == 0 {
				t.Fatal(err)
			}
			if uploadedContext == nil || uploadedContext.Err() != context.Canceled {
				t.Fatal("上传完成后未释放子context")
			}
		})
	}
}

func TestBPSImageUploadParentCancellation(t *testing.T) {
	for _, stage := range []string{"headers", "body"} {
		t.Run(stage, func(t *testing.T) {
			bpsAttachmentIDs = basispoints.AttachmentCache{}
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()
			started := make(chan struct{})
			closed := make(chan struct{})
			var calls atomic.Int32
			transport := bpsImageTransport(func(r *http.Request) (*http.Response, error) {
				if calls.Add(1) > 1 {
					return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"openai_file_id":"file-recovered"}`))}, nil
				}
				close(started)
				if stage == "headers" {
					<-r.Context().Done()
					return nil, r.Context().Err()
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: &bpsImageCancelBody{ctx: r.Context(), closed: closed}}, nil
			})
			ctx := context.WithValue(parent, "cliproxy.roundtripper", transport)
			raw := bpsImageBody(t, bpsImagePart(bpsTestImage(t, 21)))
			results := make(chan error, 2)
			upload := func(requestContext context.Context) {
				_, _, _, err := PrepareBPSImages(requestContext, &config.Config{}, bpsTestAuth(), raw, "cancel-scope")
				results <- err
			}
			go upload(ctx)
			select {
			case <-started:
			case <-time.After(2 * time.Second):
				t.Fatal("附件上传未开始")
			}
			go upload(ctx)
			cancel()
			for range 2 {
				select {
				case err := <-results:
					if err == nil {
						t.Fatal("父取消后仍成功")
					}
				case <-time.After(2 * time.Second):
					t.Fatal("父取消后上传或同作用域等待未释放")
				}
			}
			if stage == "body" {
				select {
				case <-closed:
				default:
					t.Fatal("响应体未关闭")
				}
			}
			fresh := context.WithValue(context.Background(), "cliproxy.roundtripper", transport)
			go upload(fresh)
			select {
			case err := <-results:
				if err != nil {
					t.Fatal("同作用域取消后未恢复上传", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("取消后同作用域flight仍占用")
			}
			if calls.Load() != 2 {
				t.Fatal("取消或重试改变上传次数", calls.Load())
			}
		})
	}
}
func bpsTestImage(t *testing.T, red uint8) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 3))
	img.Set(0, 0, color.RGBA{R: red, A: 255})
	var data bytes.Buffer
	if err := png.Encode(&data, img); err != nil {
		t.Fatal(err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(data.Bytes())
}
func bpsImageBody(t *testing.T, images ...map[string]any) []byte {
	t.Helper()
	doc := ojson.NewDocString(`{"model":"gpt-6-luna","input":[{"role":"user","content":[]}]}`)
	if err := doc.Set("input.0.content", images); err != nil {
		t.Fatal(err)
	}
	return doc.Bytes()
}
func bpsTestAuth() *coreauth.Auth {
	return &coreauth.Auth{ID: "credential-a", Provider: "bps", Metadata: map[string]any{"access_token": "TOKEN_A", "account_id": "ACCOUNT_A"}}
}
func bpsImagePart(reference string) map[string]any {
	return map[string]any{"type": "input_image", "image_url": reference}
}

func TestBPSImageNativeUploadAndCredentialScopedCache(t *testing.T) {
	bpsAttachmentIDs = basispoints.AttachmentCache{}
	calls := 0
	ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", bpsImageTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.String() != basispoints.AttachmentsURL || r.Header.Get("Authorization") == "" || r.Header.Get("chatgpt-account-id") == "" {
			t.Fatal("附件目标或认证错误")
		}
		reader, err := r.MultipartReader()
		if err != nil {
			t.Fatal(err)
		}
		part, err := reader.NextPart()
		if err != nil {
			t.Fatal(err)
		}
		if part.FormName() != "file" || part.FileName() != "image.png" {
			t.Fatal("附件multipart语义错误")
		}
		raw, _ := io.ReadAll(part)
		if len(raw) == 0 {
			t.Fatal("附件内容为空")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"openai_file_id":"file-uploaded"}`))}, nil
	}))
	raw := bpsImageBody(t, bpsImagePart(bpsTestImage(t, 1)))
	auth := bpsTestAuth()
	for _, step := range []struct{ scope, id, account, token string }{{"caller/thread", "a", "1", "1"}, {"caller/thread", "a", "1", "1"}, {"caller/thread", "b", "1", "1"}, {"caller/thread", "a", "2", "1"}, {"caller/thread", "a", "1", "2"}, {"other/thread", "a", "1", "1"}, {"", "a", "1", "1"}, {"", "a", "1", "1"}} {
		auth.ID = step.id
		auth.Metadata["account_id"] = step.account
		auth.Metadata["access_token"] = step.token
		wire, bridge, release, err := PrepareBPSImages(ctx, &config.Config{}, auth, raw, step.scope)
		release()
		if err != nil || bridge == nil {
			t.Fatal(err)
		}
		if strings.Contains(string(wire), "data:image") || !strings.Contains(string(wire), "file-uploaded") {
			t.Fatal("原生附件未替换图片", string(wire))
		}
	}
	if calls != 7 {
		t.Fatal("附件缓存未按凭证/账号/token/会话隔离", calls)
	}
}

func TestBPSImageNativeToolScreenshotStaysInline(t *testing.T) {
	data := bpsTestImage(t, 2)
	doc := ojson.NewDocString(`{"model":"gpt-6-luna","tools":[{"type":"function","name":"view_image"}],"input":[{"type":"function_call","call_id":"call_image","name":"view_image","arguments":"{}"},{"type":"function_call_output","call_id":"call_image","output":[{"type":"input_text","text":"before"},{"type":"input_image"},{"type":"input_text","text":"after"}]}]}`)
	_ = doc.Set("input.1.output.1.image_url", data)
	ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", bpsImageTransport(func(*http.Request) (*http.Response, error) {
		t.Fatal("内联工具截图触发上传")
		return nil, nil
	}))
	wire, _, release, err := PrepareBPSImages(ctx, &config.Config{}, bpsTestAuth(), doc.Bytes(), "caller/thread")
	release()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range ojson.GetBytes(wire, "input").Array() {
		if item.Get("type").String() == "function_call_output" {
			parts := item.Get("output").Array()
			found = len(parts) == 3 && item.Get("call_id").String() == "call_image" && parts[0].Get("text").String() == "before" && parts[1].Get("image_url").String() == data && parts[2].Get("text").String() == "after"
		}
	}
	if !found {
		t.Fatal("工具截图关联或顺序丢失")
	}
}

func TestBPSImageUploadRejectsNonDonorReceipt(t *testing.T) {
	for _, response := range []string{`{"id":"file-other"}`, `{"file_id":"file-other"}`, `{"openai_file_id":"invalid"}`, strings.Repeat(" ", 64<<10) + `{"openai_file_id":"file-other"}`} {
		ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", bpsImageTransport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(response))}, nil
		}))
		wire, _, _, err := PrepareBPSImages(ctx, &config.Config{}, bpsTestAuth(), bpsImageBody(t, bpsImagePart(bpsTestImage(t, 9))), "")
		if err == nil || len(wire) != 0 {
			t.Fatal("接受错误的附件回执")
		}
	}
}

func TestBPSImageRejectsAllShapesWhenDisabled(t *testing.T) {
	off := false
	cfg := &config.Config{BPSProtocol: config.BPSProtocolConfig{ImagesEnabled: &off}}
	for _, reference := range []map[string]any{bpsImagePart(bpsTestImage(t, 1)), bpsImagePart("https://images.example.com/a.png"), {"type": "input_image", "file_id": "file-existing"}} {
		raw := bpsImageBody(t, reference)
		before := bytes.Clone(raw)
		if _, _, _, err := PrepareBPSImages(context.Background(), cfg, nil, raw, ""); err == nil {
			t.Fatal("关图接受图片")
		}
		if !bytes.Equal(raw, before) {
			t.Fatal("拒绝请求改变原始历史")
		}
	}
	tool := []byte(`{"input":[{"type":"function_call_output","output":[{"type":"input_image","image_url":"https://example.com/a.png"}]}]}`)
	if _, _, _, err := PrepareBPSImages(context.Background(), cfg, nil, tool, ""); err == nil {
		t.Fatal("关图接受工具截图")
	}
	if BPSContainsImages([]byte(`{"input":[{"type":"function_call","arguments":"image_url"}],"tools":[{"description":"input_image"}]}`)) {
		t.Fatal("把工具参数当作图片")
	}
}

func TestBPSImageBatchValidationAndUploadFailure(t *testing.T) {
	calls := 0
	ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", bpsImageTransport(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 500, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"secret":"CANARY"}`))}, nil
	}))
	good := bpsImagePart(bpsTestImage(t, 1))
	bad := bpsImagePart("data:image/png;base64,invalid")
	if _, _, _, err := PrepareBPSImages(ctx, &config.Config{}, bpsTestAuth(), bpsImageBody(t, good, bad), ""); err == nil || calls != 0 {
		t.Fatal("未在上传前校验所有图片", err, calls)
	}
	parts := make([]map[string]any, 21)
	for i := range parts {
		parts[i] = bpsImagePart("https://images.example.com/a.png")
	}
	if _, _, _, err := PrepareBPSImages(ctx, &config.Config{}, bpsTestAuth(), bpsImageBody(t, parts...), ""); err == nil || calls != 0 {
		t.Fatal("超额图片进入上传")
	}
	wire, _, _, err := PrepareBPSImages(ctx, &config.Config{}, bpsTestAuth(), bpsImageBody(t, good), "")
	if err == nil || len(wire) != 0 || calls != 1 || strings.Contains(err.Error(), "CANARY") {
		t.Fatal("上传失败未原子停止或泄漏回执", err, calls)
	}
}

func TestBPSImageSecondUploadFailureStopsBatch(t *testing.T) {
	calls := 0
	ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", bpsImageTransport(func(*http.Request) (*http.Response, error) {
		calls++
		status, body := 200, `{"openai_file_id":"file-first"}`
		if calls == 2 {
			status, body = 500, `{}`
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	}))
	raw := bpsImageBody(t, bpsImagePart(bpsTestImage(t, 11)), bpsImagePart(bpsTestImage(t, 12)))
	wire, bridge, _, err := PrepareBPSImages(ctx, &config.Config{}, bpsTestAuth(), raw, "")
	if err == nil || wire != nil || bridge != nil || calls != 2 {
		t.Fatal("多图部分上传失败后仍进入模型请求", err, calls)
	}
}

func TestBPSImageRelayCapabilityAndSwitchSnapshot(t *testing.T) {
	cfg := &config.Config{AuthDir: filepath.Join(t.TempDir(), "auths"), BPSProtocol: config.BPSProtocolConfig{ImageTransport: "relay", ImageRelayBaseURL: "https://images.example.com"}}
	wire, _, release, err := PrepareBPSImages(context.Background(), cfg, bpsTestAuth(), bpsImageBody(t, bpsImagePart(bpsTestImage(t, 3))), "caller/thread")
	release()
	if err != nil {
		t.Fatal(err)
	}
	imageURL := ""
	for _, item := range ojson.GetBytes(wire, "input").Array() {
		for _, part := range item.Get("content").Array() {
			if part.Get("type").String() == "input_image" {
				imageURL = part.Get("image_url").String()
			}
		}
	}
	parsed, err := url.Parse(imageURL)
	if err != nil || !strings.HasPrefix(parsed.Path, basispoints.ImageRelayPath) {
		t.Fatal(imageURL, err)
	}
	host, err := bpsRelayHost(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = host.relay.Close()
		bpsImageHosts.Lock()
		for key, value := range bpsImageHosts.byRoot {
			if value == host {
				delete(bpsImageHosts.byRoot, key)
			}
		}
		bpsImageHosts.Unlock()
	})
	off := false
	next := cfg.CloneForRuntime()
	next.BPSProtocol.ImagesEnabled = &off
	if _, _, _, err := PrepareBPSImages(context.Background(), next, nil, bpsImageBody(t, bpsImagePart(imageURL)), ""); err == nil {
		t.Fatal("新请求未应用关图")
	}
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		w := httptest.NewRecorder()
		ServeBPSImage(w, httptest.NewRequest(method, parsed.Path, nil))
		if w.Code != 200 || w.Header().Get("Content-Type") != "image/png" {
			t.Fatal(method, w.Code)
		}
		if method == http.MethodHead && w.Body.Len() != 0 {
			t.Fatal("HEAD返回正文")
		}
	}
	for _, path := range []string{basispoints.ImageRelayPath + strings.Repeat("x", 43), basispoints.ImageRelayPath + "../private"} {
		w := httptest.NewRecorder()
		ServeBPSImage(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != 404 {
			t.Fatal("无能力链接读取图片")
		}
	}
	w := httptest.NewRecorder()
	ServeBPSImage(w, httptest.NewRequest(http.MethodPost, parsed.Path, nil))
	if w.Code != 405 {
		t.Fatal("存在公开上传入口")
	}
}
