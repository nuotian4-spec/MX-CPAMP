package helps

import (
	"bytes"
	"net/http"
	"strings"

	"github.com/MoeForge/moego/ojson"
)

// KiloResponseError separates the upstream HTTP fact from the CPA status.
// Its internal JSON retains machine identifiers, never upstream prose.
type KiloResponseError struct {
	code, kind             string
	upstreamStatus, status int
}

func (e *KiloResponseError) ErrorCode() string { return e.code }
func (e *KiloResponseError) StatusCode() int   { return e.status }
func (e *KiloResponseError) Error() string {
	doc := ojson.NewDocString(`{"error":{"message":"Kilo upstream response failed"}}`)
	_ = doc.Set("error.code", e.code)
	_ = doc.Set("error.type", e.kind)
	_ = doc.Set("upstream_status", e.upstreamStatus)
	return doc.String()
}
func (e *KiloResponseError) IsRequestScoped() bool {
	return e.upstreamStatus == 413 || e.code == "invalid_encrypted_content"
}
func (e *KiloResponseError) PublicErrorBody() string {
	return PresentKiloError(e).Body("")
}

// KiloHTTPResponseError preserves real HTTP refusals without exposing the body.
func KiloHTTPResponseError(status int, body []byte) error {
	return kiloResponseError(status, body)
}
func kiloResponseError(upstreamStatus int, data []byte) *KiloResponseError {
	e := &KiloResponseError{upstreamStatus: upstreamStatus, status: http.StatusBadGateway}
	if ojson.ValidBytes(data) {
		value := ojson.ParseBytes(data)
		if nested := value.Get("response.error"); nested.IsObject() {
			value = nested
		} else if nested := value.Get("error"); nested.IsObject() {
			value = nested
		}
		if code := value.Get("code"); code.Type == ojson.String {
			e.code = code.String()
		}
		if kind := value.Get("type"); kind.Type == ojson.String && kind.String() != "error" && !strings.HasPrefix(kind.String(), "response.") {
			e.kind = kind.String()
		}
	}
	if upstreamStatus >= 400 {
		e.status = upstreamStatus
		return e
	}
	if e.code == "invalid_encrypted_content" {
		e.status = http.StatusBadRequest
		return e
	}
	for _, identifier := range []string{e.code, e.kind} {
		switch identifier {
		case "subscription_sharing_usage_limit_exceeded", "usage_limit_exceeded", "insufficient_quota", "rate_limit_exceeded", "rate_limit_error":
			e.status = http.StatusTooManyRequests
		case "authentication_error", "invalid_api_key":
			e.status = http.StatusUnauthorized
		}
	}
	return e
}

func kiloEventFailure(status int, eventName string, data []byte) error {
	value := ojson.ParseBytes(data)
	kind := value.Get("type").String()
	responseStatus := value.Get("response.status").String()
	bodyStatus := value.Get("status").String()
	hasError := func(field ojson.Result) bool {
		return field.Exists() && field.Type != ojson.Null && field.Raw != "false" && field.Raw != "0" && field.Raw != `""` && field.Raw != "{}" && field.Raw != "[]"
	}
	if eventName == "error" || kind == "error" || kind == "response.failed" || kind == "response.incomplete" || responseStatus == "failed" || responseStatus == "incomplete" || bodyStatus == "failed" || bodyStatus == "incomplete" || hasError(value.Get("error")) || hasError(value.Get("response.error")) || (value.Get("code").Exists() && value.Get("message").Exists()) {
		return kiloResponseError(status, data)
	}
	return nil
}

// KiloPublicEvent removes one provider prefix, preserving all other fields and
// non-data SSE lines. Control frames pass through without becoming JSON events.
func KiloPublicEvent(frame, event []byte) ([]byte, []byte) {
	model := ojson.GetBytes(event, "response.model")
	if model.Type != ojson.String || !strings.HasPrefix(model.String(), "openai/") {
		return frame, event
	}
	doc := ojson.NewDoc(event)
	_ = doc.Set("response.model", strings.TrimPrefix(model.String(), "openai/"))
	data := doc.Bytes()
	if len(frame) == 0 {
		return nil, data
	}
	var out bytes.Buffer
	written := false
	for _, line := range bytes.SplitAfter(frame, []byte("\n")) {
		if bytes.HasPrefix(line, []byte("data:")) {
			if !written {
				out.WriteString("data: ")
				out.Write(data)
				out.WriteByte('\n')
				written = true
			}
		} else {
			out.Write(line)
		}
	}
	return out.Bytes(), data
}
