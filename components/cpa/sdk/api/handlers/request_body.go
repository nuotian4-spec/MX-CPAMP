package handlers

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"

	"github.com/MoeForge/moego/json"
	"github.com/gin-gonic/gin"
	"github.com/klauspost/compress/zstd"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/channelscope"
	log "github.com/sirupsen/logrus"
)

// MaxRequestBodyBytesContextKey stores the configured request body size limit (in bytes) on the Gin context.
const MaxRequestBodyBytesContextKey = "maxRequestBodyBytes"

// ReadRequestBody reads the incoming request body and decodes supported
// Content-Encoding values before handlers inspect JSON fields.
func ReadRequestBody(c *gin.Context) ([]byte, error) {
	maxBytes := int64(0)
	if c != nil {
		maxBytes = c.GetInt64(MaxRequestBodyBytesContextKey)
	}
	raw, err := c.GetRawData()
	if err != nil {
		return nil, err
	}

	encoding := ""
	if c != nil && c.Request != nil {
		encoding = strings.TrimSpace(c.Request.Header.Get("Content-Encoding"))
	}
	if encoding == "" || strings.EqualFold(encoding, "identity") {
		return raw, nil
	}

	var decoderOptions []zstd.DOption
	if c != nil && c.Request != nil && channelscope.KiloPayload(c.Request.Context()) {
		decoderOptions = []zstd.DOption{zstd.WithDecoderMaxWindow(64 << 20), zstd.WithDecoderMaxMemory(64 << 20), zstd.WithDecoderConcurrency(1)}
	}
	decoded, err := decodeRequestBody(raw, encoding, maxBytes, decoderOptions...)
	if err != nil {
		if len(decoderOptions) > 0 && (errors.Is(err, zstd.ErrWindowSizeExceeded) || errors.Is(err, zstd.ErrDecoderSizeExceeded)) {
			limit := int64(64 << 20)
			if maxBytes > 0 && maxBytes < limit {
				limit = maxBytes
			}
			return nil, &http.MaxBytesError{Limit: limit}
		}
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return nil, err
		}
		if json.Valid(raw) {
			return raw, nil
		}
		return nil, err
	}
	// The body is now plaintext: drop Content-Encoding so nothing downstream
	// (executors, plugins, header passthrough) decodes or forwards it again.
	c.Request.Header.Del("Content-Encoding")
	return decoded, nil
}

func decodeRequestBody(raw []byte, encoding string, maxBytes int64, decoderOptions ...zstd.DOption) ([]byte, error) {
	parts := strings.Split(encoding, ",")
	body := raw
	for i := len(parts) - 1; i >= 0; i-- {
		enc := strings.ToLower(strings.TrimSpace(parts[i]))
		switch enc {
		case "", "identity":
			continue
		case "zstd":
			decoded, err := decodeZstdRequestBody(body, maxBytes, decoderOptions...)
			if err != nil {
				return nil, err
			}
			body = decoded
		case "gzip", "x-gzip":
			decoded, err := decodeGzipRequestBody(body, maxBytes)
			if err != nil {
				return nil, err
			}
			body = decoded
		default:
			return nil, fmt.Errorf("unsupported request content encoding: %s", enc)
		}
	}
	return body, nil
}

func decodeGzipRequestBody(raw []byte, maxBytes int64) ([]byte, error) {
	gzReader, errNewReader := gzip.NewReader(bytes.NewReader(raw))
	if errNewReader != nil {
		return nil, fmt.Errorf("failed to create gzip request decoder: %w", errNewReader)
	}
	defer func() {
		if errClose := gzReader.Close(); errClose != nil {
			log.Warnf("failed to close gzip request decoder: %v", errClose)
		}
	}()

	var reader io.Reader = gzReader
	if maxBytes > 0 && maxBytes < math.MaxInt64 {
		reader = io.LimitReader(gzReader, maxBytes+1)
	}
	decoded, errReadAll := io.ReadAll(reader)
	if errReadAll != nil {
		return nil, fmt.Errorf("failed to decode gzip request body: %w", errReadAll)
	}
	if maxBytes > 0 && int64(len(decoded)) > maxBytes {
		return nil, &http.MaxBytesError{Limit: maxBytes}
	}
	return decoded, nil
}

func decodeZstdRequestBody(raw []byte, maxBytes int64, decoderOptions ...zstd.DOption) ([]byte, error) {
	decoder, err := zstd.NewReader(bytes.NewReader(raw), decoderOptions...)
	if err != nil {
		return nil, fmt.Errorf("failed to create zstd request decoder: %w", err)
	}
	defer decoder.Close()

	var reader io.Reader = decoder
	if maxBytes > 0 && maxBytes < math.MaxInt64 {
		reader = io.LimitReader(decoder, maxBytes+1)
	}
	decoded, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to decode zstd request body: %w", err)
	}
	if maxBytes > 0 && int64(len(decoded)) > maxBytes {
		return nil, &http.MaxBytesError{Limit: maxBytes}
	}
	return decoded, nil
}
