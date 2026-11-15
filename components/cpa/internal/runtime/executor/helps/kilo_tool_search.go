package helps

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"reflect"

	"github.com/MoeForge/moego/ojson"
)

const KiloToolSearchFunction = "__kilo2api_tool_search"

var errKiloToolSearch = errors.New("invalid client tool search definition or history")

// KiloToolSearch preserves the public protocol around one upstream request.
type KiloToolSearch struct {
	Active   bool
	original any
	items    map[string]bool
}

func (c *KiloToolSearch) Prepare(doc *ojson.Doc) error {
	top := doc.Get("tools")
	if top.Exists() && !top.IsArray() {
		return nil
	}
	declarations := append([]ojson.Result(nil), top.Array()...)
	for _, item := range doc.Get("input").Array() {
		kind := item.Get("type").String()
		if kind == "additional_tools" || kind == "tool_search_output" {
			declarations = append(declarations, item.Get("tools").Array()...)
		}
		if (kind == "tool_search_call" || kind == "tool_search_output") && item.Get("execution").String() == "client" {
			c.Active = true
		}
	}
	for _, tool := range declarations {
		if isClientToolSearchDeclaration(tool) {
			c.Active = true
		}
	}
	if !c.Active {
		return nil
	}
	if err := kiloSearchCheckNames(declarations); err != nil {
		return err
	}
	converted, err := c.adaptTools(top.Array())
	if err != nil {
		return err
	}
	_ = doc.Set("tools", converted)
	choice := doc.Get("tool_choice")
	if choice.Get("type").String() == "tool_search" {
		_ = doc.Set("tool_choice", map[string]any{"type": "function", "name": KiloToolSearchFunction})
	} else if choice.Get("type").String() == "allowed_tools" && choice.Get("tools").IsArray() {
		mapped := ojson.NewDocString(choice.Raw)
		allowed := make([]any, 0)
		for _, tool := range choice.Get("tools").Array() {
			if tool.Get("type").String() == "tool_search" {
				allowed = append(allowed, map[string]any{"type": "function", "name": KiloToolSearchFunction})
			} else {
				allowed = append(allowed, tool.Value())
			}
		}
		_ = mapped.Set("tools", allowed)
		_ = doc.SetRaw("tool_choice", mapped.String())
	}
	if !doc.Get("input").Exists() {
		_ = doc.Set("input", []any{})
	}
	if !doc.Get("input").IsArray() {
		return nil
	}
	input := make([]any, 0)
	calls := map[string]bool{}
	for _, item := range doc.Get("input").Array() {
		kind := item.Get("type").String()
		if kind == "function_call" && item.Get("name").String() == KiloToolSearchFunction {
			return errKiloToolSearch
		}
		switch {
		case kind == "tool_search_call" && item.Get("execution").String() == "client":
			id := item.Get("call_id")
			if id.Type != ojson.String || id.String() == "" || calls[id.String()] || !item.Get("arguments").IsObject() {
				return errKiloToolSearch
			}
			calls[id.String()] = true
			mapped := ojson.NewDocString(`{}`)
			_ = mapped.Set("type", "function_call")
			_ = mapped.Set("name", KiloToolSearchFunction)
			_ = mapped.Set("call_id", id.String())
			_ = mapped.Set("arguments", ojson.NewDocString(item.Get("arguments").Raw).String())
			if item.Get("status").Exists() {
				_ = mapped.Set("status", item.Get("status").Value())
			}
			input = append(input, ojson.ParseBytes(mapped.Bytes()).Value())
		case kind == "tool_search_output" && item.Get("execution").String() == "client":
			id := item.Get("call_id").String()
			if !calls[id] || !item.Get("tools").IsArray() {
				return errKiloToolSearch
			}
			delete(calls, id)
			loaded, err := c.adaptTools(item.Get("tools").Array())
			if err != nil {
				return err
			}
			loaded = kiloSearchActivate(loaded)
			output := ojson.NewDocString(`{}`)
			_ = output.Set("tools", loaded)
			input = append(input, map[string]any{"type": "function_call_output", "call_id": id, "output": output.String()})
			if len(loaded) > 0 {
				input = append(input, map[string]any{"type": "additional_tools", "role": "developer", "tools": loaded})
			}
		case kind == "additional_tools" && item.Get("tools").IsArray():
			loaded, err := c.adaptTools(item.Get("tools").Array())
			if err != nil {
				return err
			}
			mapped := ojson.NewDocString(item.Raw)
			_ = mapped.Set("tools", loaded)
			input = append(input, ojson.ParseBytes(mapped.Bytes()).Value())
		default:
			input = append(input, item.Value())
		}
	}
	return doc.Set("input", input)
}

func kiloSearchCheckNames(tools []ojson.Result) error {
	for _, tool := range tools {
		if !tool.IsObject() || tool.Get("name").String() == KiloToolSearchFunction {
			return errKiloToolSearch
		}
		if tool.Get("type").String() == "namespace" {
			if err := kiloSearchCheckNames(tool.Get("tools").Array()); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *KiloToolSearch) adaptTools(tools []ojson.Result) ([]any, error) {
	result := make([]any, 0, len(tools))
	found := false
	for _, tool := range tools {
		if !isClientToolSearchDeclaration(tool) {
			result = append(result, tool.Value())
			continue
		}
		if found || !tool.Get("parameters").IsObject() || (c.original != nil && !reflect.DeepEqual(c.original, tool.Value())) {
			return nil, errKiloToolSearch
		}
		found = true
		c.original = tool.Value()
		var description any = "Search for available tools."
		if tool.Get("description").Exists() {
			description = tool.Get("description").Value()
		}
		result = append(result, map[string]any{"type": "function", "name": KiloToolSearchFunction, "description": description, "parameters": tool.Get("parameters").Value(), "strict": false})
	}
	return result, nil
}

func kiloSearchActivate(tools []any) []any {
	result := make([]any, 0, len(tools))
	for _, tool := range tools {
		doc := ojson.NewDocString(`{}`)
		_ = doc.Set("value", tool)
		_ = doc.Delete("value.defer_loading")
		if doc.Get("value.type").String() == "namespace" && doc.Get("value.tools").IsArray() {
			children := make([]any, 0)
			for _, item := range doc.Get("value.tools").Array() {
				children = append(children, item.Value())
			}
			_ = doc.Set("value.tools", kiloSearchActivate(children))
		}
		result = append(result, doc.Get("value").Value())
	}
	return result
}

func (c *KiloToolSearch) item(item ojson.Result, pending bool) (any, error) {
	if item.Get("type").String() != "function_call" || item.Get("name").String() != KiloToolSearchFunction || item.Get("namespace").String() != "" {
		return item.Value(), nil
	}
	mapped := ojson.NewDocString(`{"type":"tool_search_call","execution":"client"}`)
	for _, key := range []string{"id", "call_id", "status"} {
		if item.Get(key).Exists() {
			_ = mapped.Set(key, item.Get(key).Value())
		}
	}
	arguments := item.Get("arguments")
	if pending {
		_ = mapped.Set("status", "in_progress")
		_ = mapped.SetRaw("arguments", `{}`)
	} else {
		if arguments.Type == ojson.String {
			if !ojson.ValidBytes([]byte(arguments.String())) {
				return nil, errors.New("invalid upstream tool search arguments")
			}
			arguments = ojson.ParseBytes([]byte(arguments.String()))
		}
		if !arguments.IsObject() {
			return nil, errors.New("invalid upstream tool search arguments")
		}
		_ = mapped.Set("arguments", arguments.Value())
	}
	return ojson.ParseBytes(mapped.Bytes()).Value(), nil
}

func (c *KiloToolSearch) restoreResponse(doc *ojson.Doc) error {
	if doc.Get("output").IsArray() {
		output := make([]any, 0)
		for _, item := range doc.Get("output").Array() {
			mapped, err := c.item(item, false)
			if err != nil {
				return err
			}
			output = append(output, mapped)
		}
		_ = doc.Set("output", output)
	}
	if c.original != nil && doc.Get("tools").IsArray() {
		tools := make([]any, 0)
		for _, tool := range doc.Get("tools").Array() {
			if tool.Get("type").String() == "function" && tool.Get("name").String() == KiloToolSearchFunction {
				tools = append(tools, c.original)
			} else {
				tools = append(tools, tool.Value())
			}
		}
		_ = doc.Set("tools", tools)
	}
	choice := doc.Get("tool_choice")
	if choice.Get("type").String() == "function" && choice.Get("name").String() == KiloToolSearchFunction {
		_ = doc.SetRaw("tool_choice", `{"type":"tool_search"}`)
	} else if choice.Get("type").String() == "allowed_tools" {
		tools := make([]any, 0)
		for _, tool := range choice.Get("tools").Array() {
			if tool.Get("type").String() == "function" && tool.Get("name").String() == KiloToolSearchFunction {
				tools = append(tools, map[string]any{"type": "tool_search"})
			} else {
				tools = append(tools, tool.Value())
			}
		}
		_ = doc.Set("tool_choice.tools", tools)
	}
	return nil
}

// Event restores public kinds before ownership is persisted or a frame is emitted.
func (c *KiloToolSearch) Event(event []byte) ([]byte, error) {
	if c == nil || !c.Active || len(event) == 0 {
		return event, nil
	}
	doc := ojson.NewDoc(event)
	kind := doc.Get("type").String()
	item := doc.Get("item")
	if (kind == "response.output_item.added" || kind == "response.output_item.done") && item.Get("type").String() == "function_call" && item.Get("name").String() == KiloToolSearchFunction && item.Get("namespace").String() == "" {
		if c.items == nil {
			c.items = map[string]bool{}
		}
		if item.Get("id").Type == ojson.String {
			c.items[item.Get("id").String()] = true
		}
		mapped, err := c.item(item, kind == "response.output_item.added")
		if err != nil {
			return nil, err
		}
		_ = doc.Set("item", mapped)
	}
	if (kind == "response.function_call_arguments.delta" || kind == "response.function_call_arguments.done") && c.items[doc.Get("item_id").String()] {
		return nil, nil
	}
	if doc.Get("response").IsObject() {
		response := ojson.NewDocString(doc.Get("response").Raw)
		if err := c.restoreResponse(response); err != nil {
			return nil, err
		}
		_ = doc.SetRaw("response", response.String())
	}
	return doc.Bytes(), nil
}

func ReadKiloPublicEvents(ctx context.Context, response *http.Response, search *KiloToolSearch, observe func([]byte) error, emit func([]byte, []byte) error) error {
	return ReadKiloEventsObserved(ctx, response, nil, func(frame, event []byte) error {
		public, err := search.Event(event)
		if err != nil {
			return err
		}
		if len(event) > 0 && len(public) == 0 {
			return nil
		}
		if len(public) > 0 && observe != nil {
			if err := observe(public); err != nil {
				return err
			}
		}
		if !bytes.Equal(event, public) {
			var out bytes.Buffer
			written := false
			for _, line := range bytes.SplitAfter(frame, []byte("\n")) {
				if bytes.HasPrefix(line, []byte("data:")) {
					if !written {
						out.WriteString("data: ")
						out.Write(public)
						out.WriteByte('\n')
						written = true
					}
				} else {
					out.Write(line)
				}
			}
			frame = out.Bytes()
		}
		return emit(frame, public)
	})
}
