package helps

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/MoeForge/moego/ojson"
	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/bpsbinding"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

type BPSBindings struct {
	store    bpsbinding.Store
	identity bpsbinding.Identity
	caller   string
	issued   map[string]string
}

func NewBPSBindings(cfg *config.Config, auth *coreauth.Auth, opts coreexecutor.Options) *BPSBindings {
	b := &BPSBindings{store: bpsbinding.Store{Dir: filepath.Join(config.BPSStateDirectory(cfg), "bindings")}, issued: make(map[string]string)}
	b.caller, _ = opts.Metadata[coreexecutor.CallerScopeMetadataKey].(string)
	if auth != nil {
		b.identity.AuthID = auth.ID
		b.identity.AccountID, _ = auth.Metadata["account_id"].(string)
		b.identity.Subject, _ = auth.Metadata["sub"].(string)
	}
	return b
}

func (b *BPSBindings) Scope(opts coreexecutor.Options) string {
	session, _ := opts.Metadata[coreexecutor.CanonicalSessionIDMetadataKey].(string)
	if session == "" {
		session = "request:" + uuid.NewString()
	}
	return strings.Join([]string{b.caller, b.identity.AuthID, b.identity.AccountID, b.identity.Subject, session}, "\x00")
}

// Restore validates the current credential and restores native IDs before protocol translation.
func (b *BPSBindings) Restore(doc *ojson.Doc) error {
	for i, item := range doc.Get("input").Array() {
		switch item.Get("type").String() {
		case "function_call", "function_call_output", "custom_tool_call", "custom_tool_call_output":
			id := item.Get("call_id").String()
			record, err := b.store.Load(b.caller, id)
			if err != nil {
				return err
			}
			if record.Identity != b.identity {
				return bpsbinding.ErrConflict
			}
			if err = doc.Set(fmt.Sprintf("input.%d.call_id", i), record.NativeID); err != nil {
				return err
			}
		}
	}
	return nil
}

// Publish commits each binding before any public tool item is emitted.
func (b *BPSBindings) Publish(event []byte) ([]byte, error) {
	doc := ojson.NewDoc(event)
	bind := func(path string) error {
		item := doc.Get(path)
		kind := item.Get("type").String()
		if kind != "function_call" && kind != "custom_tool_call" {
			return nil
		}
		nativeID := item.Get("call_id").String()
		publicID := b.issued[nativeID]
		if publicID == "" {
			record, err := b.store.Save(b.caller, nativeID, b.identity)
			if err != nil {
				return err
			}
			publicID = record.CallID
			b.issued[nativeID] = publicID
		}
		return doc.Set(path+".call_id", publicID)
	}
	if err := bind("item"); err != nil {
		return nil, err
	}
	for i := range doc.Get("response.output").Array() {
		if err := bind(fmt.Sprintf("response.output.%d", i)); err != nil {
			return nil, err
		}
	}
	return doc.Bytes(), nil
}
