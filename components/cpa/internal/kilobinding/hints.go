package kilobinding

import (
	"net/http"
	"slices"
	"strings"

	"github.com/MoeForge/moego/ojson"
)

type Hints struct {
	Previous       string
	Session        string
	Thread         string
	Initialization string
	Parent         string
	Calls          []string
	Kinds          map[string]string
	Pairs          map[string]bool
	History        bool
	Err            error
}

func ParseHints(headers http.Header, body []byte) Hints {
	root := ojson.ParseBytes(body)
	h := Hints{Previous: strings.TrimSpace(root.Get("previous_response_id").String()), Kinds: map[string]string{}, Pairs: map[string]bool{}}
	merge := func(target *string, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if *target != "" && *target != value {
			h.Err = ErrConflict
		}
		*target = value
	}
	for _, name := range []string{"thread-id", "thread_id"} {
		merge(&h.Thread, headers.Get(name))
	}
	var rootSession string
	for _, name := range []string{"session-id", "session_id"} {
		merge(&rootSession, headers.Get(name))
	}
	merge(&h.Parent, headers.Get("x-codex-parent-thread-id"))
	metadata := []ojson.Result{root.Get("client_metadata")}
	if raw := root.Get("client_metadata.x-codex-turn-metadata"); raw.Exists() {
		if raw.Type == ojson.String {
			if !ojson.ValidBytes([]byte(raw.String())) {
				h.Err = ErrConflict
			}
			raw = ojson.ParseBytes([]byte(raw.String()))
		}
		if !raw.IsObject() {
			h.Err = ErrConflict
		}
		metadata = append(metadata, raw)
	}
	if raw := headers.Get("x-codex-turn-metadata"); raw != "" {
		if !ojson.ValidBytes([]byte(raw)) || !ojson.ParseBytes([]byte(raw)).IsObject() {
			h.Err = ErrConflict
		} else {
			metadata = append(metadata, ojson.ParseBytes([]byte(raw)))
		}
	}
	for _, meta := range metadata {
		for name, target := range map[string]*string{"thread_id": &h.Thread, "session_id": &rootSession, "history_initialization": &h.Initialization, "parent_thread_id": &h.Parent, "x-codex-parent-thread-id": &h.Parent} {
			value := meta.Get(name)
			if value.Exists() && value.Type != ojson.String {
				h.Err = ErrConflict
			}
			merge(target, value.String())
		}
	}
	h.Session = h.Thread
	if h.Session == "" {
		h.Session = rootSession
	}
	if h.Session == "" {
		for _, name := range []string{"conversation_id", "x-session-affinity", "x-session-id", "x-opencode-session", "x-conversation-id"} {
			if value := strings.TrimSpace(headers.Get(name)); value != "" {
				h.Session = value
				break
			}
		}
	}
	if h.Session == "" && root.Get("prompt_cache_key").Type == ojson.String {
		h.Session = strings.TrimSpace(root.Get("prompt_cache_key").String())
	}
	add := func(id, kind string) {
		if id == "" {
			return
		}
		if previous := h.Kinds[id]; previous != "" && previous != kind {
			h.Err = ErrConflict
		}
		h.Kinds[id] = kind
		if !slices.Contains(h.Calls, id) {
			h.Calls = append(h.Calls, id)
		}
	}
	open := map[string]string{}
	for _, item := range root.Get("input").Array() {
		kind := item.Get("type").String()
		if item.Get("role").String() == "assistant" || kind == "item_reference" {
			h.History = true
		}
		canonical, output := "", false
		switch kind {
		case "function_call", "custom_tool_call":
			canonical = kind
		case "function_call_output", "custom_tool_call_output":
			canonical, output = strings.TrimSuffix(kind, "_output"), true
		case "tool_search_call", "tool_search_output":
			if item.Get("execution").String() == "client" {
				canonical, output = "tool_search", kind == "tool_search_output"
			}
		}
		if canonical == "" {
			continue
		}
		h.History = true
		id := item.Get("call_id").String()
		add(id, canonical)
		if output {
			if open[id] == canonical {
				h.Pairs[id] = true
			}
		} else {
			open[id] = canonical
		}
	}
	for _, item := range root.Get("messages").Array() {
		role := item.Get("role").String()
		if role == "assistant" || role == "tool" {
			h.History = true
		}
		if role == "tool" {
			add(item.Get("tool_call_id").String(), "function_call")
		}
		if role == "assistant" {
			for _, call := range item.Get("tool_calls").Array() {
				add(call.Get("id").String(), "function_call")
			}
		}
	}
	return h
}
