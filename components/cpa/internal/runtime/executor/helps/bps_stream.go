package helps

import "github.com/MoeForge/moego/ojson"

type bpsStreamEventKey struct {
	kind    string
	output  int
	content int
}

// BPSStreamLifecycle completes only events absent from a real terminal response.
type BPSStreamLifecycle struct {
	seen map[bpsStreamEventKey]bool
}

func (s *BPSStreamLifecycle) Events(event []byte) ([][]byte, error) {
	if s.seen == nil {
		s.seen = make(map[bpsStreamEventKey]bool)
	}
	root := ojson.ParseBytes(event)
	kind := root.Get("type").String()
	key := bpsStreamEventKey{kind: kind, output: int(root.Get("output_index").Int()), content: int(root.Get("content_index").Int())}
	s.seen[key] = true
	if kind != "response.completed" {
		return [][]byte{event}, nil
	}
	var events [][]byte
	appendMissing := func(kind string, output, content int, fields map[string]any) error {
		key := bpsStreamEventKey{kind: kind, output: output, content: content}
		if s.seen[key] {
			return nil
		}
		doc := ojson.NewDocString(`{}`)
		if err := doc.Set("type", kind); err != nil {
			return err
		}
		for key, value := range fields {
			if err := doc.Set(key, value); err != nil {
				return err
			}
		}
		events = append(events, doc.Bytes())
		s.seen[key] = true
		return nil
	}
	responseID := root.Get("response.id").String()
	for outputIndex, item := range root.Get("response.output").Array() {
		added := ojson.NewDocString(`{}`)
		_ = added.Set("item", item.Value())
		_ = added.Set("item.status", "in_progress")
		_ = added.Set("item.content", []any{})
		if err := appendMissing("response.output_item.added", outputIndex, 0, map[string]any{"response_id": responseID, "output_index": outputIndex, "item": added.Get("item").Value()}); err != nil {
			return nil, err
		}
		for contentIndex, part := range item.Get("content").Array() {
			if part.Get("type").String() != "output_text" {
				continue
			}
			fields := map[string]any{"response_id": responseID, "item_id": item.Get("id").String(), "output_index": outputIndex, "content_index": contentIndex}
			emptyPart := ojson.NewDocString(`{}`)
			_ = emptyPart.Set("part", part.Value())
			_ = emptyPart.Set("part.text", "")
			fields["part"] = emptyPart.Get("part").Value()
			if err := appendMissing("response.content_part.added", outputIndex, contentIndex, fields); err != nil {
				return nil, err
			}
			delete(fields, "part")
			fields["delta"] = part.Get("text").String()
			if err := appendMissing("response.output_text.delta", outputIndex, contentIndex, fields); err != nil {
				return nil, err
			}
			delete(fields, "delta")
			fields["text"] = part.Get("text").String()
			if err := appendMissing("response.output_text.done", outputIndex, contentIndex, fields); err != nil {
				return nil, err
			}
			delete(fields, "text")
			fields["part"] = part.Value()
			if err := appendMissing("response.content_part.done", outputIndex, contentIndex, fields); err != nil {
				return nil, err
			}
		}
		if err := appendMissing("response.output_item.done", outputIndex, 0, map[string]any{"response_id": responseID, "output_index": outputIndex, "item": item.Value()}); err != nil {
			return nil, err
		}
	}
	return append(events, event), nil
}
