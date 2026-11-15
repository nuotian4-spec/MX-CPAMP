package helps

import (
	"strconv"

	"github.com/MoeForge/moego/ojson"
)

// PrepareKiloChatCallIDs keeps the shared Codex translator's 64-character limit
// local to Codex. Temporary request-local aliases are restored before ownership
// checks and payload rules; neither the client nor the upstream sees them.
func PrepareKiloChatCallIDs(doc *ojson.Doc) map[string]string {
	used := map[string]bool{}
	ids := []string{}
	for _, message := range doc.Get("messages").Array() {
		if id := message.Get("tool_call_id").String(); id != "" {
			used[id] = true
		}
		if message.Get("role").String() != "assistant" {
			continue
		}
		for _, call := range message.Get("tool_calls").Array() {
			id := call.Get("id").String()
			used[id] = true
			kind := call.Get("type").String()
			if len(id) > 64 && (kind == "function" || kind == "custom") {
				ids = append(ids, id)
			}
		}
	}
	aliases, originals := map[string]string{}, map[string]string{}
	for _, id := range ids {
		if aliases[id] != "" {
			continue
		}
		for n := len(aliases); ; n++ {
			alias := "kilo_local_call_" + strconv.Itoa(n)
			if !used[alias] {
				used[alias] = true
				aliases[id], originals[alias] = alias, id
				break
			}
		}
	}
	if len(aliases) == 0 {
		return originals
	}
	for i, message := range doc.Get("messages").Array() {
		path := "messages." + strconv.Itoa(i)
		if message.Get("role").String() == "tool" {
			if alias := aliases[message.Get("tool_call_id").String()]; alias != "" {
				_ = doc.Set(path+".tool_call_id", alias)
			}
		}
		if message.Get("role").String() == "assistant" {
			for j, call := range message.Get("tool_calls").Array() {
				if alias := aliases[call.Get("id").String()]; alias != "" {
					_ = doc.Set(path+".tool_calls."+strconv.Itoa(j)+".id", alias)
				}
			}
		}
	}
	return originals
}

// PreserveKiloChatFields restores upstream call IDs and image detail before the
// final payload barrier. Images are matched in source order by their exact URL;
// no removed or replaced image is recreated.
func PreserveKiloChatFields(doc *ojson.Doc, original []byte, originals map[string]string) {
	details := map[string][]ojson.Result{}
	for _, message := range ojson.GetBytes(original, "messages").Array() {
		if message.Get("role").String() != "user" {
			continue
		}
		for _, part := range message.Get("content").Array() {
			if part.Get("type").String() == "image_url" {
				url := part.Get("image_url.url").String()
				details[url] = append(details[url], part.Get("image_url.detail"))
			}
		}
	}
	for i, item := range doc.Get("input").Array() {
		path := "input." + strconv.Itoa(i)
		switch item.Get("type").String() {
		case "function_call", "custom_tool_call", "function_call_output", "custom_tool_call_output":
			if id := originals[item.Get("call_id").String()]; id != "" {
				_ = doc.Set(path+".call_id", id)
			}
		}
		if item.Get("role").String() != "user" {
			continue
		}
		for j, part := range item.Get("content").Array() {
			if part.Get("type").String() != "input_image" {
				continue
			}
			url := part.Get("image_url").String()
			if queue := details[url]; len(queue) > 0 {
				if queue[0].Exists() {
					_ = doc.Set(path+".content."+strconv.Itoa(j)+".detail", queue[0].Value())
				}
				details[url] = queue[1:]
			}
		}
	}
}
