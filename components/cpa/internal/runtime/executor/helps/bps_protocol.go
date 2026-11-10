package helps

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/MoeForge/moego/ojson"
	"github.com/google/uuid"
)

const BPSResponsesURL = "https://bps.openai.com/basispoints/api/responses"

func PrepareBPSDoc(doc *ojson.Doc, model string) error {
	var input []any
	raw := doc.Get("input")
	if raw.Type == ojson.String {
		input = []any{bpsTextMessage("user", raw.String())}
	} else if raw.IsArray() {
		for _, item := range raw.Array() {
			input = append(input, item.Value())
		}
	} else {
		return errors.New("BPS requires text input")
	}
	prologue := []any{}
	if instructions := doc.Get("instructions").String(); instructions != "" {
		prologue = append(prologue, bpsTextMessage("developer", instructions))
	}
	prologue = append(prologue, bpsTextMessage("developer", "This is an external Responses client without an Excel workbook. Return assistant text and do not call server-injected Excel or Office tools."))
	effort := doc.Get("reasoning.effort").String()
	if effort == "" {
		effort = doc.Get("reasoning_effort").String()
	}
	if effort == "" {
		effort = "medium"
	}
	switch effort {
	case "low", "medium", "high", "xhigh":
	default:
		return errors.New("invalid BPS reasoning effort")
	}
	for _, key := range []string{"instructions", "reasoning", "tools", "tool_choice", "parallel_tool_calls", "include", "stream_options", "service_tier", "generate", "prompt_cache_retention", "safety_identifier"} {
		_ = doc.Delete(key)
	}
	for key, value := range map[string]any{"model": model, "model_selection": "explicit", "stream": true, "store": false, "input": append(prologue, input...), "reasoning_effort": effort, "metadata": map[string]any{"task_id": uuid.NewString(), "turn_id": uuid.NewString(), "agent_iteration": "0"}} {
		if err := doc.Set(key, value); err != nil {
			return err
		}
	}
	return nil
}

func bpsTextMessage(role, text string) map[string]any {
	return map[string]any{"type": "message", "role": role, "content": []any{map[string]any{"type": "input_text", "text": text}}}
}

func ApplyBPSHeaders(req *http.Request, token, accountID string) error {
	if token == "" || accountID == "" || strings.ContainsAny(token+accountID, "\r\n") {
		return errors.New("BPS credentials are incomplete")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("chatgpt-account-id", accountID)
	req.Header.Set("x-openai-account-id", accountID)
	req.Header.Set("x-basispoints-auth-mode", "chatgpt")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("Origin", "https://bps.openai.com")
	for key, value := range map[string]string{"client-agent-profile": "excel", "client-editor": "excel", "client-host": "office", "client-platform": "excel", "client-platform-class": "PC", "client-product": "basispoints-excel-plugin", "client-runtime": "desktop", "office-host": "Excel", "office-platform": "PC"} {
		req.Header.Set("x-openai-internal-basispoints-"+key, value)
	}
	return nil
}

// ReadBPSEvents requires a real terminal event; an EOF never becomes success.
func ReadBPSEvents(ctx context.Context, response *http.Response, consume func([]byte) error) error {
	const max = 16 << 20
	terminal := false
	outputIndices := make(map[int64]int)
	publicOutputIndex := func(upstream int64) int {
		if index, ok := outputIndices[upstream]; ok {
			return index
		}
		index := len(outputIndices)
		outputIndices[upstream] = index
		return index
	}
	dispatch := func(data []byte) error {
		if len(data) == 0 || bytes.Equal(data, []byte("[DONE]")) {
			return nil
		}
		if !ojson.ValidBytes(data) || !ojson.ParseBytes(data).IsObject() {
			return errors.New("malformed BPS event")
		}
		doc := ojson.NewDoc(data)
		for _, path := range []string{"response.id", "response_id"} {
			if id := doc.Get(path).String(); id != "" && !strings.HasPrefix(id, "resp_bps_") {
				_ = doc.Set(path, "resp_bps_"+strings.TrimPrefix(id, "resp_"))
			}
		}
		kind := doc.Get("type").String()
		if strings.HasPrefix(kind, "response.reasoning") || doc.Get("item.type").String() == "reasoning" {
			return nil
		}
		if kind == "response.done" {
			kind = "response.completed"
			_ = doc.Set("type", kind)
		}
		if kind == "response.failed" || kind == "response.incomplete" || kind == "error" {
			return errors.New("BPS upstream response failed")
		}
		if strings.Contains(kind, "function_call") || strings.Contains(kind, "custom_tool") {
			return errors.New("BPS returned an unsupported tool call")
		}
		if item := doc.Get("item.type").String(); item != "" && item != "message" && item != "reasoning" {
			return errors.New("BPS returned an unsupported output item")
		}
		if index := doc.Get("output_index"); index.Exists() {
			_ = doc.Set("output_index", publicOutputIndex(index.Int()))
		}
		if kind == "response.completed" {
			if status := doc.Get("response.status").String(); status != "completed" {
				return errors.New("BPS terminal status is not completed")
			}
			terminal = true
		}
		if source := doc.Get("response.output"); source.IsArray() {
			output := []any{}
			for upstreamIndex, item := range source.Array() {
				if kind := item.Get("type").String(); kind != "message" && kind != "reasoning" {
					return errors.New("BPS returned an unsupported output item")
				}
				if item.Get("type").String() == "message" {
					index := publicOutputIndex(int64(upstreamIndex))
					for len(output) <= index {
						output = append(output, nil)
					}
					output[index] = item.Value()
				}
			}
			_ = doc.Set("response.output", output)
		}
		return consume(doc.Bytes())
	}
	if !strings.Contains(strings.ToLower(response.Header.Get("Content-Type")), "text/event-stream") {
		data, err := io.ReadAll(io.LimitReader(response.Body, max+1))
		if err != nil {
			return err
		}
		if len(data) > max {
			return errors.New("BPS response exceeds maximum size")
		}
		doc := ojson.NewDocString(`{"type":"response.completed"}`)
		if err = doc.SetRaw("response", string(data)); err != nil {
			return errors.New("malformed BPS response")
		}
		return dispatch(doc.Bytes())
	}
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 64<<10), max)
	var data []byte
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := scanner.Bytes()
		if len(line) == 0 {
			if err := dispatch(bytes.TrimSpace(data)); err != nil {
				return err
			}
			data = nil
			if terminal {
				return nil
			}
		} else if bytes.HasPrefix(line, []byte("data:")) {
			if len(data) > 0 {
				data = append(data, '\n')
			}
			data = append(data, bytes.TrimPrefix(line[5:], []byte(" "))...)
			if len(data) > max {
				return errors.New("BPS event exceeds maximum size")
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if err := dispatch(bytes.TrimSpace(data)); err != nil {
		return err
	}
	if !terminal {
		return fmt.Errorf("BPS stream ended before response.completed")
	}
	return nil
}
