package bpsbinding

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/google/uuid"
)

const Prefix = "call_bps_"
const Lifetime = 7 * 24 * time.Hour

var ErrMissing = errors.New("BPS tool binding is missing or expired; start a new session")
var ErrConflict = errors.New("BPS tool binding no longer matches its original credential")
var ErrStorage = errors.New("BPS tool binding storage is unavailable")

// Identity excludes tokens so a normal refresh preserves existing bindings.
type Identity struct {
	AuthID    string `json:"auth_id"`
	AccountID string `json:"account_id"`
	Subject   string `json:"subject,omitempty"`
}

type Record struct {
	Identity
	CallID   string    `json:"call_id"`
	NativeID string    `json:"native_id"`
	Caller   string    `json:"caller"`
	Expires  time.Time `json:"expires"`
}

type Store struct {
	Dir string
	Now func() time.Time
}

var diskMu sync.Mutex

func (s Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func IsCallID(id string) bool { return strings.HasPrefix(id, Prefix) }

func validCallID(id string) bool {
	if !IsCallID(id) {
		return false
	}
	_, err := uuid.Parse(strings.TrimPrefix(id, Prefix))
	return err == nil
}

func (s Store) Save(caller, nativeID string, identity Identity) (Record, error) {
	if identity.AuthID == "" || identity.AccountID == "" || identity.Subject == "" || nativeID == "" {
		return Record{}, ErrConflict
	}
	diskMu.Lock()
	defer diskMu.Unlock()
	if err := os.MkdirAll(s.Dir, 0700); err != nil {
		return Record{}, ErrStorage
	}
	// Bindings contain identities only and expire independently from image data.
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return Record{}, ErrStorage
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		info, errInfo := entry.Info()
		if errInfo == nil && s.now().Sub(info.ModTime()) >= Lifetime {
			_ = os.Remove(filepath.Join(s.Dir, entry.Name()))
		}
	}
	record := Record{Identity: identity, CallID: Prefix + uuid.NewString(), NativeID: nativeID, Caller: caller, Expires: s.now().Add(Lifetime).UTC()}
	doc := ojson.NewDocString(`{}`)
	for field, value := range map[string]any{"auth_id": record.AuthID, "account_id": record.AccountID, "subject": record.Subject, "call_id": record.CallID, "native_id": record.NativeID, "caller": record.Caller, "expires": record.Expires.Format(time.RFC3339Nano)} {
		if err := doc.Set(field, value); err != nil {
			return Record{}, ErrStorage
		}
	}
	raw := doc.Bytes()
	file, err := os.CreateTemp(s.Dir, ".binding-")
	if err != nil {
		return Record{}, ErrStorage
	}
	tmp := file.Name()
	defer func() { _ = os.Remove(tmp) }()
	err = file.Chmod(0600)
	if err == nil {
		_, err = file.Write(raw)
	}
	if err == nil {
		err = file.Sync()
	}
	err = errors.Join(err, file.Close())
	if err == nil {
		err = os.Rename(tmp, filepath.Join(s.Dir, record.CallID+".json"))
	}
	if err != nil {
		return Record{}, ErrStorage
	}
	return record, nil
}

func (s Store) Load(caller, callID string) (Record, error) {
	if !validCallID(callID) {
		return Record{}, ErrMissing
	}
	path := filepath.Join(s.Dir, callID+".json")
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Record{}, ErrMissing
	}
	if err != nil {
		return Record{}, ErrStorage
	}
	root := ojson.ParseBytes(raw)
	expires, err := time.Parse(time.RFC3339Nano, root.Get("expires").String())
	record := Record{Identity: Identity{AuthID: root.Get("auth_id").String(), AccountID: root.Get("account_id").String(), Subject: root.Get("subject").String()}, CallID: root.Get("call_id").String(), NativeID: root.Get("native_id").String(), Caller: root.Get("caller").String(), Expires: expires}
	if !ojson.ValidBytes(raw) || err != nil || record.CallID != callID || record.AuthID == "" || record.AccountID == "" || record.Subject == "" || record.NativeID == "" {
		return Record{}, ErrStorage
	}
	if !s.now().Before(record.Expires) {
		_ = os.Remove(path)
		return Record{}, ErrMissing
	}
	if record.Caller != caller {
		return Record{}, ErrMissing
	}
	return record, nil
}

// CallIDs reads only protocol identity fields, never arbitrary text/tool output.
func CallIDs(raw []byte) []string {
	root := ojson.ParseBytes(raw)
	var ids []string
	seen := map[string]bool{}
	add := func(id string) {
		if id != "" && !seen[id] {
			ids = append(ids, id)
			seen[id] = true
		}
	}
	for _, item := range root.Get("input").Array() {
		switch item.Get("type").String() {
		case "function_call", "function_call_output", "custom_tool_call", "custom_tool_call_output":
			add(item.Get("call_id").String())
		}
	}
	for _, item := range root.Get("messages").Array() {
		if item.Get("role").String() == "tool" {
			add(item.Get("tool_call_id").String())
		}
		for _, call := range item.Get("tool_calls").Array() {
			add(call.Get("id").String())
		}
	}
	return ids
}
