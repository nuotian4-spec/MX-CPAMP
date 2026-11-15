package middleware

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/logging"
)

func TestKiloPayloadLoggingSuppression(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "deferred", true: "eager"}[enabled], func(t *testing.T) {
			dir := t.TempDir()
			logger := logging.NewFileRequestLogger(enabled, dir, "", 10)
			body := strings.Repeat("private-body-marker", 100000)
			router := gin.New()
			router.Use(RequestLoggingMiddleware(logger))
			router.POST("/v1/responses", func(c *gin.Context) {
				SuppressAuthenticatedRequestBody(c)
				CaptureAuthenticatedRequestBody(c)
				got, err := io.ReadAll(c.Request.Body)
				if err != nil || string(got) != body {
					t.Fatalf("body changed: %v", err)
				}
				c.Set("REQUEST_BODY_OVERRIDE", []byte(body))
				c.Set("API_REQUEST", []byte(body))
				c.Set(logging.DeferredAPIRequestContextKey, []logging.DeferredAPIRequest{func() []byte {
					t.Error("suppressed deferred closure executed")
					return []byte(body)
				}})
				err = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
					if err != nil {
						return err
					}
					if !d.IsDir() {
						t.Errorf("temporary body file exists before response: %s", path)
					}
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
				c.JSON(413, gin.H{"error": "kilo_request_too_large"})
			})
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest("POST", "/v1/responses", strings.NewReader(body)))
			if w.Code != 413 {
				t.Fatalf("HTTP=%d", w.Code)
			}
			err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return err
				}
				data, err := os.ReadFile(path)
				if bytes.Contains(data, []byte("private-body-marker")) {
					t.Fatalf("body leaked: %s", path)
				}
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestKiloPayloadNonKiloLoggingSourcesRemain(t *testing.T) {
	for _, path := range []string{"/v1/responses", "/v1/completions", "/v1/audio/transcriptions"} {
		t.Run(path, func(t *testing.T) {
			logger := logging.NewFileRequestLogger(true, t.TempDir(), "", 0)
			router := gin.New()
			router.Use(RequestLoggingMiddleware(logger))
			router.POST(path, func(c *gin.Context) {
				for _, key := range []string{logging.APIRequestSourceContextKey, logging.APIResponseSourceContextKey} {
					source := extractFileBodySource(c, key)
					if source == nil {
						t.Fatalf("non-Kilo source missing: %s", key)
					}
					file, err := source.CreatePart("body")
					if err != nil {
						t.Fatal(err)
					}
					_, _ = file.WriteString("non-kilo-body")
					_ = file.Close()
				}
				c.Status(http.StatusOK)
			})
			req := httptest.NewRequest("POST", path, strings.NewReader("multipart-body"))
			req.Header.Set("Content-Type", "multipart/form-data; boundary=test")
			router.ServeHTTP(httptest.NewRecorder(), req)
		})
	}
}
