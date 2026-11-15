package helps

import (
	"context"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilobinding"
	codexchat "github.com/router-for-me/CLIProxyAPI/v8/internal/translator/codex/openai/chat-completions"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func CommitKiloBinding(ctx context.Context, a *coreauth.Auth) (string, error) {
	plan := kilobinding.FromContext(ctx)
	if plan == nil || a == nil || ((a.Disabled || a.Status == coreauth.StatusDisabled) && !coreauth.KiloManualDiagnostic(ctx, a)) {
		return "", coreauth.KiloBindingError(kilobinding.ErrConflict)
	}
	record, err := plan.Store.Commit(plan.Caller, plan.Record, coreauth.KiloIdentity(a), plan.SessionKey, plan.PreferSession, coreauth.KiloFirstDeviceProfileID(a))
	if err != nil {
		return "", coreauth.KiloBindingError(err)
	}
	plan.Record = record
	plan.Release()
	return record.Session, nil
}

// ObserveKiloBinding persists ownership before a frame reaches the public stream.
func ObserveKiloBinding(ctx context.Context, event []byte) error {
	plan := kilobinding.FromContext(ctx)
	if plan == nil {
		return coreauth.KiloBindingError(kilobinding.ErrConflict)
	}
	root := ojson.ParseBytes(event)
	response := ""
	if plan.FirstResponse == "" {
		response = root.Get("response.id").String()
	}
	calls := map[string]string{}
	collect := func(item ojson.Result) {
		kind := item.Get("type").String()
		if kind == "function_call" || kind == "custom_tool_call" || (kind == "tool_search_call" && item.Get("execution").String() == "client") {
			if kind == "tool_search_call" {
				kind = "tool_search"
			}
			if id := item.Get("call_id").String(); id != "" {
				calls[id] = kind
			}
		}
	}
	collect(root.Get("item"))
	for _, item := range root.Get("response.output").Array() {
		collect(item)
	}
	if response == "" && len(calls) == 0 {
		return nil
	}
	if err := plan.Store.Publish(plan.Record, response, calls); err != nil {
		return coreauth.KiloBindingError(err)
	}
	if response != "" {
		plan.FirstResponse = response
	}
	return nil
}

// ValidateKiloFinalHistory is read-only after the payload rules' final barrier.
func ValidateKiloFinalHistory(ctx context.Context, body []byte, localCalls map[string]bool, search ...*KiloToolSearch) error {
	p := kilobinding.FromContext(ctx)
	if p == nil {
		return coreauth.KiloBindingError(kilobinding.ErrConflict)
	}
	hints := kilobinding.ParseHints(nil, body)
	if len(search) == 1 && search[0] != nil && search[0].Active {
		for _, item := range ojson.GetBytes(body, "input").Array() {
			if item.Get("type").String() == "function_call" && item.Get("name").String() == KiloToolSearchFunction && item.Get("namespace").String() == "" {
				hints.Kinds[item.Get("call_id").String()] = "tool_search"
			}
		}
	}
	kept := make([]string, 0, len(hints.Calls))
	for _, id := range hints.Calls {
		if !localCalls[id] {
			kept = append(kept, id)
		}
	}
	hints.Calls = kept
	record := p.Record
	record.Caller = p.Caller
	if err := p.Store.ValidateHistory(record, hints); err != nil {
		return coreauth.KiloBindingError(err)
	}
	return nil
}

// PreserveKiloChatToolResults retains a registered tool-only continuation when
// the generic Chat translator omits orphan results without assistant history.
func PreserveKiloChatToolResults(ctx context.Context, doc *ojson.Doc, original []byte) error {
	p := kilobinding.FromContext(ctx)
	if p == nil {
		return coreauth.KiloBindingError(kilobinding.ErrConflict)
	}
	input := make([]any, 0)
	seen := map[string]bool{}
	for _, item := range doc.Get("input").Array() {
		input = append(input, item.Value())
		kind := item.Get("type").String()
		if kind == "function_call_output" || kind == "custom_tool_call_output" {
			seen[item.Get("call_id").String()] = true
		}
	}
	changed := false
	for _, message := range ojson.GetBytes(original, "messages").Array() {
		if message.Get("role").String() != "tool" {
			continue
		}
		id := message.Get("tool_call_id").String()
		if seen[id] {
			continue
		}
		kind := p.Record.CallTypes[id]
		if kind != "function_call" && kind != "custom_tool_call" {
			return coreauth.KiloBindingError(kilobinding.ErrConflict)
		}
		output := ojson.NewDocString(`{}`)
		_ = output.Set("type", kind+"_output")
		_ = output.Set("call_id", id)
		codexchat.SetToolCallOutputContentDoc(output, message.Get("content"))
		input = append(input, ojson.ParseBytes(output.Bytes()).Value())
		seen[id] = true
		changed = true
	}
	if changed {
		return doc.Set("input", input)
	}
	return nil
}
