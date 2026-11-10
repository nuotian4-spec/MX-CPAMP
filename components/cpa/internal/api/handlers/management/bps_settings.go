package management

import (
	"io"
	"net/http"
	"strings"

	"github.com/MoeForge/moego/ojson"
	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
)

type bpsSettingsInput struct {
	Enabled           *bool  `json:"enabled"`
	ImagesEnabled     *bool  `json:"images_enabled"`
	ImageTransport    string `json:"image_transport"`
	ImageRelayBaseURL string `json:"image_relay_base_url"`
}

func bpsSettingsResponse(cfg *config.Config) gin.H {
	return gin.H{"enabled": cfg.IsBPSProtocolEnabled(), "images_enabled": cfg.IsBPSImagesEnabled(), "image_transport": cfg.BPSProtocol.Transport(), "image_relay_base_url": cfg.BPSProtocol.ImageRelayBaseURL,
		"limits": gin.H{"ttl_seconds": 1800, "max_image_bytes": 20 << 20, "max_request_images": 20, "max_request_bytes": 32 << 20, "max_pixels": 64 * 1024 * 1024, "max_storage_bytes": 1 << 30, "max_storage_entries": 512}}
}

func (h *Handler) GetBPSSettings(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, bpsSettingsResponse(h.configSnapshot()))
}

func (h *Handler) PutBPSSettings(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	raw, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 4096))
	if err != nil {
		c.JSON(400, gin.H{"error": "BPS设置输入无效"})
		return
	}
	root := ojson.ParseBytes(raw)
	if !ojson.ValidBytes(raw) || !root.IsObject() || !root.Get("enabled").IsBool() || !root.Get("images_enabled").IsBool() || root.Get("image_transport").Type != ojson.String || root.Get("image_relay_base_url").Type != ojson.String {
		c.JSON(400, gin.H{"error": "BPS设置需要完整开关与传输模式"})
		return
	}
	for key := range root.Map() {
		if key != "enabled" && key != "images_enabled" && key != "image_transport" && key != "image_relay_base_url" {
			c.JSON(400, gin.H{"error": "BPS设置包含未知字段"})
			return
		}
	}
	enabled, imagesEnabled := root.Get("enabled").Bool(), root.Get("images_enabled").Bool()
	input := bpsSettingsInput{Enabled: &enabled, ImagesEnabled: &imagesEnabled, ImageTransport: root.Get("image_transport").String(), ImageRelayBaseURL: root.Get("image_relay_base_url").String()}
	h.mu.Lock()
	previous := h.cfg
	next := h.cfg.CloneForRuntime()
	next.BPSProtocol.Enabled = input.Enabled
	next.BPSProtocol.ImagesEnabled = input.ImagesEnabled
	next.BPSProtocol.ImageTransport = input.ImageTransport
	next.BPSProtocol.ImageRelayBaseURL = strings.TrimRight(strings.TrimSpace(input.ImageRelayBaseURL), "/")
	if err := next.ValidateBPSProtocol(); err != nil {
		h.mu.Unlock()
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	h.cfg = next
	snapshot, saved := h.saveConfigAndSnapshotLocked(c)
	if !saved {
		h.cfg = previous
		h.mu.Unlock()
		return
	}
	h.mu.Unlock()
	h.reloadConfigAfterManagementSave(c.Request.Context(), snapshot)
	c.JSON(http.StatusOK, bpsSettingsResponse(next))
}
