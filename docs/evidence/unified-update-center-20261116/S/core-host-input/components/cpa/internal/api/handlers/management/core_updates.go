package management

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/gin-gonic/gin"
)

// forwardCoreUpdate addresses only this CPA's registered host socket.
func (h *Handler) forwardCoreUpdate(c *gin.Context, action string, fields map[string]string) {
	socket, site := os.Getenv("MX_UPDATE_SOCKET"), os.Getenv("MX_UPDATE_SITE_ID")
	if !filepath.IsAbs(socket) || site == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "update_site_unregistered"})
		return
	}
	if _, err := h.updateStore(); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "update_site_unregistered"})
		return
	}
	doc := ojson.NewDocString("{}")
	for key, value := range fields {
		if err := doc.Set(key, value); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_update_request"})
			return
		}
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 40 * time.Second}
	request, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, "http://registered-site/v1/"+action, bytes.NewReader(doc.Bytes()))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_update_request"})
		return
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-MX-Site-ID", site)
	response, err := client.Do(request)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "update_host_unavailable", "message": "Registered host task service is unavailable; existing operations remain queryable"})
		return
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil || !ojson.ValidBytes(data) {
		c.JSON(http.StatusBadGateway, gin.H{"error": "invalid_update_host_response"})
		return
	}
	c.Data(response.StatusCode, "application/json", data)
}

func (h *Handler) GetCoreUpdateDiscovery(c *gin.Context) { h.forwardCoreUpdate(c, "discover", nil) }

func (h *Handler) PreviewCoreUpdate(c *gin.Context) {
	data, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 4096))
	if err != nil || !ojson.ValidBytes(data) || ojson.GetBytes(data, "release_id").String() == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "fixed_release_required"})
		return
	}
	h.forwardCoreUpdate(c, "preview", map[string]string{"release_id": ojson.GetBytes(data, "release_id").String()})
}

func (h *Handler) InstallCoreUpdate(c *gin.Context) {
	data, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 4096))
	if err != nil || !ojson.ValidBytes(data) || !ojson.GetBytes(data, "confirm").IsBool() || !ojson.GetBytes(data, "confirm").Bool() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "confirmation_required"})
		return
	}
	h.forwardCoreUpdate(c, "install", map[string]string{"candidate_id": c.Param("candidate")})
}
