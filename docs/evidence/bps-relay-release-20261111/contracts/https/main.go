package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/basispoints"
)

func main() {
	root, err := os.MkdirTemp("", "bps-https-state-")
	if err != nil { panic("临时目录失败") }
	defer os.RemoveAll(root)
	relay, err := basispoints.NewImageRelay("https://relay.example.com:8443", root)
	if err != nil { panic("创建图片宿主失败") }
	defer relay.Close()
	var pixels bytes.Buffer
	if err = png.Encode(&pixels, image.NewRGBA(image.Rect(0, 0, 2, 3))); err != nil { panic("生成图片失败") }
	raw, _ := json.Marshal(map[string]any{"model": "gpt-6-astra", "input": []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_image", "image_url": "data:image/png;base64," + base64.StdEncoding.EncodeToString(pixels.Bytes()), "detail": "high"}}}}})
	out, err := relay.Rewrite(raw, "https-local-fixture")
	if err != nil { panic("创建随机图片能力失败") }
	var payload struct { Input []struct { Content []struct { URL string `json:"image_url"` } `json:"content"` } `json:"input"` }
	if json.Unmarshal(out, &payload) != nil || len(payload.Input) != 1 || len(payload.Input[0].Content) != 1 { panic("图片回执无效") }
	capability, err := url.Parse(payload.Input[0].Content[0].URL)
	if err != nil { panic("能力路径无效") }
	manifest, _ := json.Marshal(map[string]string{"path": capability.Path, "png_base64": base64.StdEncoding.EncodeToString(pixels.Bytes())})
	if os.WriteFile("/exchange/ready.json", manifest, 0600) != nil { panic("写入测试回执失败") }
	if os.WriteFile(filepath.Join(root, "private-state.txt"), []byte("SYNTHETIC_PRIVATE_STATE"), 0600) != nil { panic("写入测试状态失败") }
	var mu sync.Mutex
	counts := map[string]int{}
	server := &http.Server{Addr: ":8317", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		kind := "outside"
		if r.URL.Path == capability.Path { kind = "valid" } else if r.URL.Path == basispoints.ImageRelayPath { kind = "directory" } else if strings.HasPrefix(r.URL.Path, basispoints.ImageRelayPath) { kind = "unknown" }
		mu.Lock(); counts[r.Method+"_"+kind]++; mu.Unlock()
		relay.ServeHTTP(w, r)
	})}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	go func() { if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed { panic("测试监听失败") } }()
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdown)
	mu.Lock(); counters, _ := json.Marshal(counts); mu.Unlock()
	if os.WriteFile("/exchange/counts.json", counters, 0600) != nil { panic("写入请求计数失败") }
}
