package helps

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/MoeForge/moego/ojson"
	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/basispoints"
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
	model    string
}

func (b *BPSBindings) SetModel(model string) { b.model = model }

func bpsStateDigest(cipher string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(cipher))) }

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
		value, _ := item.Value().(map[string]any)
		if basispoints.IsCodexThreadDelegation(value) {
			continue
		}
		switch item.Get("type").String() {
		case "compaction":
			record, err := b.store.Load(b.caller, item.Get("id").String())
			if err != nil {
				return err
			}
			if record.Identity != b.identity || record.Kind != "compaction" || record.Model != b.model || record.StateDigest != bpsStateDigest(item.Get("encrypted_content").String()) {
				return bpsbinding.ErrConflict
			}
			if err = doc.Set(fmt.Sprintf("input.%d.id", i), record.NativeID); err != nil {
				return err
			}
		case "function_call", "function_call_output", "custom_tool_call", "custom_tool_call_output":
			id := item.Get("call_id").String()
			record, err := b.store.Load(b.caller, id)
			if err != nil {
				return err
			}
			if record.Identity != b.identity || record.Kind != "" {
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
		if kind != "function_call" && kind != "custom_tool_call" && kind != "compaction" {
			return nil
		}
		field := "call_id"
		if kind == "compaction" {
			field = "id"
		}
		nativeID := item.Get(field).String()
		key := kind + ":" + nativeID
		publicID := b.issued[key]
		if publicID == "" {
			var record bpsbinding.Record
			var err error
			if kind == "compaction" {
				record, err = b.store.SaveState(b.caller, nativeID, b.identity, b.model, bpsStateDigest(item.Get("encrypted_content").String()))
			} else {
				record, err = b.store.Save(b.caller, nativeID, b.identity)
			}
			if err != nil {
				if errors.Is(err, bpsbinding.ErrConflict) {
					return NewBPSFailure("binding_identity", "identity_incomplete_or_changed")
				}
				if errors.Is(err, bpsbinding.ErrStorage) {
					return NewBPSFailure("binding_storage", "storage_unavailable")
				}
				return err
			}
			publicID = record.CallID
			b.issued[key] = publicID
		}
		return doc.Set(path+"."+field, publicID)
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
