package chat_completions

import (
	"context"
	"testing"

	"github.com/MoeForge/moego/ojson"
)

func TestBPSActualServiceTierPreserved(t *testing.T) {
	response := ojson.NewDocString(textResponse)
	_ = response.Set("service_tier", "default")
	var param any
	out := ConvertResponse(context.Background(), "model", nil, nil, response.Bytes(), &param)
	if ojson.GetBytes(out, "service_tier").String() != "default" {
		t.Fatalf("nonstream tier=%s", out)
	}
	param = nil
	chunks := ConvertStream(context.Background(), "model", nil, nil, []byte(`{"type":"response.created","response":{"id":"resp","service_tier":"priority"}}`), &param)
	if len(chunks) != 0 {
		t.Fatal("Chat created must remain absorbed")
	}
	terminal := ojson.NewDocString(`{"type":"response.completed"}`)
	_ = terminal.Set("response", ojson.ParseBytes(response.Bytes()).Value())
	chunks = ConvertStream(context.Background(), "model", nil, nil, terminal.Bytes(), &param)
	if len(chunks) == 0 {
		t.Fatal("missing terminal")
	}
	for _, chunk := range chunks {
		if ojson.GetBytes(chunk, "service_tier").String() != "default" {
			t.Fatalf("stream tier=%s", chunk)
		}
	}
}
