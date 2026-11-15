package helps

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/MoeForge/moego/ojson"
)

const KiloMaxEventBytes = 4 << 20

// AdaptKiloAgentMessages changes only the source-defined agent_message input shape.
func AdaptKiloAgentMessages(doc *ojson.Doc) error {
	return adaptKiloAgentMessages(doc, nil)
}

// AdaptKiloAgentMessagesWithCalls returns only references synthesized by the
// local agent_message adapter, never arbitrary client-supplied tool declarations.
func AdaptKiloAgentMessagesWithCalls(doc *ojson.Doc) (map[string]bool, error) {
	calls := map[string]bool{}
	err := adaptKiloAgentMessages(doc, calls)
	return calls, err
}
func adaptKiloAgentMessages(doc *ojson.Doc, localCalls map[string]bool) error {

	input := doc.Get("input")
	if !input.IsArray() {
		return nil
	}
	out := make([]any, 0, len(input.Array()))
	changed := false
	for i, item := range input.Array() {
		if item.Get("type").String() != "agent_message" {
			out = append(out, item.Value())
			continue
		}
		changed = true
		content := item.Get("content")
		if !content.IsArray() || item.Get("author").Type != ojson.String || item.Get("recipient").Type != ojson.String {
			return errors.New("unsupported Kilo agent_message")
		}
		routing := ojson.NewDocString(item.Raw)
		_ = routing.Delete("type")
		_ = routing.Delete("content")
		message := []any{map[string]any{"type": "input_text", "text": "Received agent communication:\n" + routing.String()}}
		for j, part := range content.Array() {
			switch part.Get("type").String() {
			case "input_text":
				if part.Get("text").Type != ojson.String {
					return errors.New("unsupported Kilo agent_message content")
				}
				message = append(message, part.Value())
			case "encrypted_content":
				if part.Get("encrypted_content").Type != ojson.String {
					return errors.New("unsupported Kilo agent_message content")
				}
				identity := ojson.NewDocString("{}")
				_ = identity.Set("item", item.Value())
				_ = identity.Set("index", i)
				_ = identity.Set("part_index", j)
				digest := sha256.Sum256(identity.Bytes())
				callID := fmt.Sprintf("call_kilo_agent_%x", digest[:16])
				if localCalls != nil {
					localCalls[callID] = true
				}
				args := ojson.NewDocString("{}")
				_ = args.Set("target", item.Get("recipient").String())
				_ = args.Set("message", part.Get("encrypted_content").String())
				output := ojson.NewDocString("{}")
				_ = output.Set("transport", "received_agent_message")
				_ = output.Set("author", item.Get("author").String())
				_ = output.Set("recipient", item.Get("recipient").String())
				_ = output.Set("content_index", j)
				out = append(out, map[string]any{"type": "function_call", "call_id": callID, "namespace": "collaboration", "name": "send_message", "arguments": args.String()}, map[string]any{"type": "function_call_output", "call_id": callID, "output": output.String()})
				message = append(message, map[string]any{"type": "input_text", "text": "The content of this part is the message argument of recorded transport call " + callID + "."})
			default:
				return errors.New("unsupported Kilo agent_message content")
			}
		}
		out = append(out, map[string]any{"type": "message", "role": "user", "content": message})
	}
	if changed {
		_ = doc.Set("input", out)
	}
	return nil
}

// ReadKiloEvents delivers complete SSE frames without changing successful fields.
func ReadKiloEvents(ctx context.Context, resp *http.Response, emit func([]byte, []byte) error) error {
	return ReadKiloEventsObserved(ctx, resp, nil, emit)
}

func ReadKiloEventsObserved(ctx context.Context, resp *http.Response, observe func([]byte) error, emit func([]byte, []byte) error) error {
	var reader io.Reader = resp.Body
	encoding := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Encoding")))
	if encoding == "gzip" {
		gz, err := gzip.NewReader(reader)
		if err != nil {
			return errors.New("invalid Kilo compression")
		}
		defer gz.Close()
		reader = gz
	} else if encoding != "" && encoding != "identity" {
		return errors.New("unsupported Kilo encoding")
	}
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		body, err := io.ReadAll(io.LimitReader(reader, KiloMaxEventBytes+1))
		if err != nil {
			return err
		}
		if len(body) > KiloMaxEventBytes {
			return errors.New("Kilo response exceeds event limit")
		}
		if !ojson.ValidBytes(body) || !ojson.ParseBytes(body).IsObject() {
			return errors.New("Kilo response did not complete")
		}
		if err := kiloEventFailure(resp.StatusCode, "", body); err != nil {
			return kiloDiagnosticFailure(ctx, "", body, err)
		}
		event := ojson.NewDocString("{}")
		_ = event.Set("type", "response.completed")
		_ = event.SetRaw("response", string(body))
		data := event.Bytes()
		if observe != nil {
			if err := observe(data); err != nil {
				return err
			}
		}
		if ojson.GetBytes(body, "status").String() != "completed" {
			return errors.New("Kilo response did not complete")
		}
		return emit(append(append([]byte("data: "), data...), []byte("\n\n")...), data)
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), KiloMaxEventBytes+1)
	var frame, data []byte
	var eventName string
	deliver := func() (bool, error) {
		if len(data) == 0 {
			if len(frame) > 0 {
				if err := emit(append(frame, '\n'), nil); err != nil {
					return true, err
				}
			}
			frame = nil
			eventName = ""
			return false, nil
		}
		data = bytes.TrimSuffix(data, []byte("\n"))
		if bytes.Equal(data, []byte("[DONE]")) {
			return true, errors.New("Kilo stream ended before response terminal")
		}
		if !ojson.ValidBytes(data) || !ojson.ParseBytes(data).IsObject() {
			return true, errors.New("invalid Kilo event")
		}
		if err := kiloEventFailure(resp.StatusCode, eventName, data); err != nil {
			return true, kiloDiagnosticFailure(ctx, eventName, data, err)
		}
		if observe != nil {
			if err := observe(data); err != nil {
				return true, err
			}
		}
		kind := ojson.GetBytes(data, "type").String()
		status := ojson.GetBytes(data, "response.status").String()
		if kind == "response.completed" && status != "completed" {
			return true, errors.New("invalid Kilo terminal response")
		}
		frame = append(frame, '\n')
		if err := emit(frame, data); err != nil {
			return true, err
		}
		done := kind == "response.completed"
		frame = nil
		data = nil
		eventName = ""
		return done, nil
	}
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := scanner.Bytes()
		extra := 2
		if len(line) == 0 {
			extra = 1
		}
		if len(frame)+len(line)+extra > KiloMaxEventBytes {
			return errors.New("Kilo event exceeds limit")
		}
		if len(line) == 0 {
			done, err := deliver()
			if done || err != nil {
				return err
			}
			continue
		}
		frame = append(append(frame, line...), '\n')
		if bytes.HasPrefix(line, []byte("event:")) {
			eventName = strings.TrimSpace(string(line[6:]))
		}
		if bytes.HasPrefix(line, []byte("data:")) {
			value := bytes.TrimPrefix(line[5:], []byte(" "))
			data = append(append(data, value...), '\n')
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := scanner.Err(); err != nil {
		return errors.New("Kilo stream read failed")
	}
	if len(data) > 0 {
		done, err := deliver()
		if done || err != nil {
			return err
		}
	}
	return errors.New("Kilo stream missing terminal response")
}
