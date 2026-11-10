package helps

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/basispoints"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

var bpsAttachmentIDs basispoints.AttachmentCache
var bpsImageHosts = struct {
	sync.Mutex
	byRoot map[string]*bpsImageHost
}{byRoot: make(map[string]*bpsImageHost)}

type bpsImageHost struct {
	sync.Mutex
	relay *basispoints.ImageRelay
}
type bpsImageError struct {
	status  int
	message string
}

func (e bpsImageError) Error() string         { return e.message }
func (e bpsImageError) StatusCode() int       { return e.status }
func (e bpsImageError) IsRequestScoped() bool { return true }

// BPSContainsImages只检查协议中的图片部件，不遍历工具参数或文本。
func BPSContainsImages(raw []byte) bool {
	root := ojson.ParseBytes(raw)
	for _, key := range []string{"input", "messages"} {
		for _, item := range root.Get(key).Array() {
			for _, field := range []string{"content", "output"} {
				for _, part := range item.Get(field).Array() {
					switch part.Get("type").String() {
					case "input_image", "image_url", "image":
						return true
					}
				}
			}
		}
	}
	return false
}

func bpsRelayHost(cfg *config.Config) (*bpsImageHost, error) {
	if err := cfg.ValidateBPSProtocol(); err != nil {
		return nil, err
	}
	root, err := filepath.Abs(filepath.Join(config.BPSStateDirectory(cfg), "image-relay"))
	if err != nil {
		return nil, err
	}
	bpsImageHosts.Lock()
	defer bpsImageHosts.Unlock()
	if host := bpsImageHosts.byRoot[root]; host != nil {
		return host, nil
	}
	relay, err := basispoints.NewImageRelay(cfg.BPSProtocol.ImageRelayBaseURL, root)
	if err != nil {
		return nil, err
	}
	host := &bpsImageHost{relay: relay}
	bpsImageHosts.byRoot[root] = host
	return host, nil
}

// ServeBPSImage按随机能力链接读取临时图片；关图不使在途请求的链接失效。
func ServeBPSImage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	bpsImageHosts.Lock()
	var relay *basispoints.ImageRelay
	for _, host := range bpsImageHosts.byRoot {
		if host.relay.HasLiveCapability(r.URL.Path) {
			relay = host.relay
			break
		}
	}
	bpsImageHosts.Unlock()
	if relay == nil {
		http.NotFound(w, r)
		return
	}
	relay.ServeHTTP(w, r)
}

// PrepareBPSImages完成图片处理后进入同一供体协议，调用者随后执行最终payload规则。
func PrepareBPSImages(ctx context.Context, cfg *config.Config, auth *coreauth.Auth, raw []byte, scope string) ([]byte, *basispoints.Bridge, func(), error) {
	if cfg == nil {
		cfg = &config.Config{}
	}
	release := func() {}
	fail := func(err error) ([]byte, *basispoints.Bridge, func(), error) { return nil, nil, release, err }
	if !cfg.IsBPSProtocolEnabled() {
		return fail(bpsImageError{400, "BPS协议已关闭"})
	}
	if err := cfg.ValidateBPSProtocol(); err != nil {
		return fail(bpsImageError{400, err.Error()})
	}
	if !cfg.IsBPSImagesEnabled() && BPSContainsImages(raw) {
		return fail(bpsImageError{400, "BPS图片输入已关闭，当前、历史和工具结果图片均被拒绝"})
	}
	if !cfg.IsBPSImagesEnabled() || !BPSContainsImages(raw) {
		wire, bridge, err := basispoints.Prepare(raw, scope, nil)
		return wire, bridge, release, err
	}
	count := 0
	for _, item := range ojson.GetBytes(raw, "input").Array() {
		for _, field := range []string{"content", "output"} {
			for _, part := range item.Get(field).Array() {
				if part.Get("type").String() == "input_image" {
					count++
				}
			}
		}
	}
	if count > 20 {
		return fail(bpsImageError{400, "BPS每请求最多支持20张图片"})
	}
	plan, err := basispoints.PrepareNativeImages(raw)
	if err != nil {
		return fail(bpsImageError{400, err.Error()})
	}
	// 整个请求先通过协议校验，再产生附件上传或中转文件副作用。
	if _, _, err = plan.PrepareWithCatalog(scope, nil, nil); err != nil {
		return fail(err)
	}
	if cfg.BPSProtocol.Transport() == "relay" {
		host, err := bpsRelayHost(cfg)
		if err != nil {
			return fail(err)
		}
		host.Lock()
		err = host.relay.SetPublicOrigin(cfg.BPSProtocol.ImageRelayBaseURL)
		var wire []byte
		if err == nil {
			wire, err = host.relay.Rewrite(raw, scope)
		}
		host.Unlock()
		if err != nil {
			return fail(err)
		}
		wire, bridge, err := basispoints.Prepare(wire, scope, nil)
		return wire, bridge, release, err
	}
	if plan.HasImages() {
		if auth == nil {
			return fail(bpsImageError{401, "BPS凭证缺失"})
		}
		token, _ := auth.Metadata["access_token"].(string)
		account, _ := auth.Metadata["account_id"].(string)
		cacheScope := ""
		if scope != "" {
			cacheScope = scope + "\x00" + auth.ID + "\x00" + account + "\x00" + token
		}
		client := NewProxyAwareHTTPClient(ctx, cfg, auth, 0)
		_, err = plan.Upload(ctx, &bpsAttachmentIDs, cacheScope, func(uploadCtx context.Context, img basispoints.InlineAttachment) (string, error) {
			return uploadBPSImage(uploadCtx, client, token, account, img)
		})
		if errors.Is(err, basispoints.ErrAttachmentBusy) {
			err = bpsImageError{503, "BPS附件上传繁忙，请稍后重试"}
		}
		if err != nil {
			return fail(err)
		}
	}
	wire, bridge, err := plan.PrepareWithCatalog(scope, nil, nil)
	return wire, bridge, release, err
}

func uploadBPSImage(ctx context.Context, client *http.Client, token, account string, img basispoints.InlineAttachment) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	body, contentType, size, err := img.Multipart()
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, basispoints.AttachmentsURL, body)
	if err != nil {
		return "", err
	}
	if err = ApplyBPSHeaders(req, strings.TrimSpace(token), strings.TrimSpace(account)); err != nil {
		return "", bpsImageError{401, "BPS附件认证信息不完整"}
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.ContentLength = size
	// 附件重定向不携带凭证，也不自动重试其他账号。
	transport := *client
	transport.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := transport.Do(req)
	if err != nil {
		return "", bpsImageError{502, "BPS附件上传失败，请求未重放"}
	}
	defer func() { _ = res.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(res.Body, (64<<10)+1))
	if err != nil {
		return "", bpsImageError{502, "BPS附件回执读取失败"}
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", bpsImageError{502, fmt.Sprintf("BPS附件上传返回HTTP%d，请求未重放", res.StatusCode)}
	}
	if len(raw) > 64<<10 || !ojson.ValidBytes(raw) {
		return "", bpsImageError{502, "BPS附件回执无效"}
	}
	id := ojson.GetBytes(raw, "openai_file_id").String()
	if !basispoints.ValidAttachmentID(id) {
		return "", bpsImageError{502, "BPS附件回执缺少有效附件ID"}
	}
	return id, nil
}
