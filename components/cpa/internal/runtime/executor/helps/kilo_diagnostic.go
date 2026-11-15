package helps

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/MoeForge/moego/ojson"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

const KiloAutoFreeURL = "https://ai-gateway.kilo.ai/api/v1/chat/completions"

type KiloDiagnosticIncomplete struct {
	Text  string
	Cause error
}

func (e *KiloDiagnosticIncomplete) Error() string {
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return "Kilo diagnostic response is incomplete"
}
func (e *KiloDiagnosticIncomplete) Unwrap() error { return e.Cause }
func (e *KiloDiagnosticIncomplete) StatusCode() int {
	if coded, ok := e.Cause.(interface{ StatusCode() int }); ok {
		return coded.StatusCode()
	}
	return http.StatusBadGateway
}
func (e *KiloDiagnosticIncomplete) ErrorCode() string {
	if coded, ok := e.Cause.(interface{ ErrorCode() string }); ok {
		return coded.ErrorCode()
	}
	return ""
}
func (e *KiloDiagnosticIncomplete) DiagnosticIncompleteText() string { return e.Text }

func kiloDiagnosticFailure(ctx context.Context, eventName string, data []byte, cause error) error {
	if !coreauth.KiloDiagnostic(ctx) || eventName == "error" {
		return cause
	}
	root := ojson.ParseBytes(data)
	response := root.Get("response")
	if !response.Exists() {
		response = root
	}
	if root.Get("type").String() == "response.incomplete" || response.Get("status").String() == "incomplete" {
		return &KiloDiagnosticIncomplete{Text: KiloDiagnosticText(response), Cause: cause}
	}
	return cause
}

func KiloDiagnosticText(response ojson.Result) string {
	var text strings.Builder
	for _, item := range response.Get("output").Array() {
		for _, content := range item.Get("content").Array() {
			if content.Get("type").String() == "output_text" {
				text.WriteString(content.Get("text").String())
			}
		}
	}
	return text.String()
}

// ReadKiloAutoFree consumes only the first Chat choice's generated text and
// requires its finish_reason. DONE, HTTP 200 and text alone are not success.
func ReadKiloAutoFree(ctx context.Context, response *http.Response) (string, error) {
	var reader io.Reader = response.Body
	encoding := strings.ToLower(strings.TrimSpace(response.Header.Get("Content-Encoding")))
	if encoding == "gzip" {
		compressed, err := gzip.NewReader(reader)
		if err != nil {
			return "", err
		}
		defer compressed.Close()
		reader = compressed
	} else if encoding != "" && encoding != "identity" {
		return "", errors.New("unsupported Kilo encoding")
	}
	var text strings.Builder
	consume := func(data []byte, eventName string) (bool, error) {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if bytes.Equal(data, []byte("[DONE]")) {
			return false, errors.New("Kilo response did not complete")
		}
		if err := kiloEventFailure(response.StatusCode, eventName, data); err != nil {
			return false, err
		}
		choice := ojson.GetBytes(data, "choices.0")
		part := choice.Get("delta.content")
		if !part.Exists() {
			part = choice.Get("message.content")
		}
		if part.Type == ojson.String {
			text.WriteString(part.String())
		}
		finish := choice.Get("finish_reason")
		if finish.Type != ojson.String || finish.String() == "" {
			return false, nil
		}
		if finish.String() != "stop" {
			return true, &KiloDiagnosticIncomplete{Text: text.String()}
		}
		return true, nil
	}
	if !strings.HasPrefix(response.Header.Get("Content-Type"), "text/event-stream") {
		data, err := io.ReadAll(io.LimitReader(reader, KiloMaxEventBytes+1))
		if err != nil {
			return "", err
		}
		if len(data) > KiloMaxEventBytes {
			return "", errors.New("Kilo response exceeds event limit")
		}
		done, err := consume(data, "")
		if err != nil {
			return "", err
		}
		if !done {
			return "", errors.New("Kilo response did not complete")
		}
		return text.String(), nil
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), KiloMaxEventBytes+1)
	var data []byte
	eventName := ""
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			if len(data) > 0 {
				done, err := consume(bytes.TrimSuffix(data, []byte("\n")), eventName)
				if err != nil {
					return "", err
				}
				if done {
					return text.String(), nil
				}
			}
			data, eventName = nil, ""
			continue
		}
		if bytes.HasPrefix(line, []byte("data:")) {
			data = append(data, bytes.TrimPrefix(line[5:], []byte(" "))...)
			data = append(data, '\n')
			if len(data) > KiloMaxEventBytes {
				return "", errors.New("Kilo event exceeds limit")
			}
		} else if bytes.HasPrefix(line, []byte("event:")) {
			eventName = strings.TrimSpace(string(line[6:]))
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	if len(data) > 0 {
		done, err := consume(bytes.TrimSuffix(data, []byte("\n")), eventName)
		if err != nil {
			return "", err
		}
		if done {
			return text.String(), nil
		}
	}
	return "", errors.New("Kilo response did not complete")
}
