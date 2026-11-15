package helps

import (
	"strings"

	"github.com/MoeForge/moego/ojson"
)

// Publish compaction only after terminal uniqueness validation; text remains streamed.
type bpsCompactionLifecycle struct {
	required bool
	id       string
	cipher   string
	index    int64
	seen     bool
	tools    [][]byte
}

func (s *bpsCompactionLifecycle) observe(item ojson.Result, index int64, complete bool) error {
	id, cipher := item.Get("id").String(), item.Get("encrypted_content").String()
	if id == "" || complete && cipher == "" {
		return NewBPSFailure("compaction_validation", "invalid_compaction_state")
	}
	if s.seen && (s.id != id || s.index != index || s.cipher != "" && cipher != "" && s.cipher != cipher) {
		return NewBPSFailure("compaction_validation", "conflicting_compaction_states")
	}
	s.id, s.index, s.seen = id, index, true
	if cipher != "" {
		s.cipher = cipher
	}
	return nil
}

func (s *bpsCompactionLifecycle) accept(raw []byte, emit func([]byte) error) error {
	doc := ojson.NewDoc(raw)
	kind := doc.Get("type").String()
	itemType := doc.Get("item.type").String()
	if itemType == "function_call" || itemType == "custom_tool_call" || strings.HasPrefix(kind, "response.function_call_arguments.") || strings.HasPrefix(kind, "response.custom_tool_call_input.") {
		s.tools = append(s.tools, append([]byte(nil), raw...))
		return nil
	}
	if doc.Get("item.type").String() == "compaction" {
		if err := s.observe(doc.Get("item"), doc.Get("output_index").Int(), kind == "response.output_item.done"); err != nil {
			return err
		}
		return nil
	}
	if kind != "response.completed" {
		if output := doc.Get("response.output"); output.IsArray() {
			filtered := []any{}
			for _, item := range output.Array() {
				if item.Get("type").String() != "compaction" {
					filtered = append(filtered, item.Value())
				}
			}
			_ = doc.Set("response.output", filtered)
		}
		return emit(doc.Bytes())
	}
	count := 0
	var state ojson.Result
	var index int
	for i, item := range doc.Get("response.output").Array() {
		if item.Get("type").String() != "compaction" {
			continue
		}
		count++
		state, index = item, i
		if err := s.observe(item, int64(i), true); err != nil {
			return err
		}
	}
	if count > 1 || (s.required || s.seen) && count != 1 {
		return NewBPSFailure("compaction_validation", "compaction_count_invalid")
	}
	for _, tool := range s.tools {
		if err := emit(tool); err != nil {
			return err
		}
	}
	s.tools = nil
	if count == 1 {
		for _, eventType := range []string{"response.output_item.added", "response.output_item.done"} {
			event := ojson.NewDocString(`{}`)
			_ = event.Set("type", eventType)
			_ = event.Set("response_id", doc.Get("response.id").String())
			_ = event.Set("output_index", index)
			_ = event.Set("item", state.Value())
			if err := emit(event.Bytes()); err != nil {
				return err
			}
		}
	}
	return emit(doc.Bytes())
}
