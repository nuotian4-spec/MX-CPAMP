package kilobinding

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/google/uuid"
)

const Lifetime = time.Hour

var ErrMissing = errors.New("Kilo continuation is missing or expired; use a new session key")
var ErrConflict = errors.New("Kilo continuation no longer matches its original account or session")
var ErrStorage = errors.New("Kilo binding storage is unavailable")

// Identity deliberately excludes tokens and credential versions changed by refresh.
type Identity struct {
	DeviceProfileID string
	AuthID          string
	Provider        string
	AccountID       string
	Subject         string
	Organization    string
	Revision        uint64
}
type Record struct {
	ID             string
	Caller         string
	Identity       Identity
	Session        string
	Sessions       []string
	Responses      []string
	Calls          []string
	CallTypes      map[string]string
	SourceID       string
	InheritedCalls map[string]string
	Expires        time.Time
}
type Store struct {
	requestContext context.Context
	replaceWait    *time.Duration
	Dir            string
	Now            func() time.Time
}

var diskMu sync.Mutex

func (s Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// One atomic caller document commits the record and all its alias indexes together.
// The digest provides a direct, path-safe lookup instead of scanning other callers.
func (s Store) path(caller string) string {
	return filepath.Join(s.Dir, fmt.Sprintf("%x.json", sha256.Sum256([]byte(caller))))
}
func (s Store) read(caller string) ([]Record, error) {
	raw, err := os.ReadFile(s.path(caller))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, storageError("read", err)
	}
	if !ojson.ValidBytes(raw) {
		return nil, storageError("validate_json", nil)
	}
	root := ojson.ParseBytes(raw)
	if root.Get("caller_scope").String() != caller || !root.Get("bindings").IsArray() {
		return nil, storageError("validate_document", nil)
	}
	var records []Record
	for _, v := range root.Get("bindings").Array() {
		expires, e := time.Parse(time.RFC3339Nano, v.Get("expires_at").String())
		r := Record{ID: v.Get("id").String(), Caller: caller, Session: v.Get("upstream_session").String(), Expires: expires, Identity: Identity{DeviceProfileID: v.Get("device_profile_id").String(), AuthID: v.Get("auth_id").String(), Provider: v.Get("provider").String(), AccountID: v.Get("account_id").String(), Subject: v.Get("subject").String(), Organization: v.Get("organization_id").String(), Revision: v.Get("identity_revision").Uint()}}
		if e != nil || r.ID == "" || r.Session == "" || r.Identity.AuthID == "" || r.Identity.Provider != "kilo" {
			return nil, storageError("validate_record", nil)
		}
		for _, x := range v.Get("sessions").Array() {
			r.Sessions = append(r.Sessions, x.String())
		}
		for _, x := range v.Get("responses").Array() {
			r.Responses = append(r.Responses, x.String())
		}
		for _, x := range v.Get("calls").Array() {
			r.Calls = append(r.Calls, x.String())
		}
		r.CallTypes = map[string]string{}
		v.Get("call_types").ForEach(func(key, value ojson.Result) bool { r.CallTypes[key.String()] = value.String(); return true })
		r.SourceID = v.Get("source_record_id").String()
		r.InheritedCalls = map[string]string{}
		v.Get("inherited_calls").ForEach(func(key, value ojson.Result) bool { r.InheritedCalls[key.String()] = value.String(); return true })
		records = append(records, r)
	}
	return records, nil
}
func (s Store) write(caller string, records []Record) error {
	if err := os.MkdirAll(s.Dir, 0700); err != nil {
		return storageError("mkdir", err)
	}
	values := make([]any, 0, len(records))
	for _, r := range records {
		values = append(values, map[string]any{"device_profile_id": r.Identity.DeviceProfileID, "id": r.ID, "auth_id": r.Identity.AuthID, "provider": r.Identity.Provider, "account_id": r.Identity.AccountID, "subject": r.Identity.Subject, "organization_id": r.Identity.Organization, "identity_revision": r.Identity.Revision, "upstream_session": r.Session, "sessions": r.Sessions, "responses": r.Responses, "calls": r.Calls, "call_types": r.CallTypes, "source_record_id": r.SourceID, "inherited_calls": r.InheritedCalls, "expires_at": r.Expires.UTC().Format(time.RFC3339Nano)})
	}
	doc := ojson.NewDocString("{}")
	if err := doc.Set("caller_scope", caller); err != nil {
		return storageError("encode_caller", err)
	}
	if err := doc.Set("bindings", values); err != nil {
		return storageError("encode_bindings", err)
	}
	file, err := os.CreateTemp(s.Dir, ".binding-")
	if err != nil {
		return storageError("create", err)
	}
	tmp := file.Name()
	defer func() { _ = os.Remove(tmp) }()
	operation := "chmod"
	err = file.Chmod(0600)
	if err == nil {
		operation = "write"
		_, err = file.Write(doc.Bytes())
	}
	if err == nil {
		operation = "sync"
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return storageError(operation, err)
	}
	if closeErr != nil {
		return storageError("close", closeErr)
	}
	if err = replaceBindingFile(s.requestContext, s.replaceWait, tmp, s.path(caller)); err != nil {
		return storageError("replace", err)
	}
	return nil
}

func find(records []Record, kind, key string) (Record, bool) {
	for _, r := range records {
		var aliases []string
		switch kind {
		case "session":
			aliases = r.Sessions
		case "response":
			aliases = r.Responses
		case "call":
			aliases = r.Calls
		case "id":
			if r.ID == key {
				return r, true
			}
		}
		if slices.Contains(aliases, key) {
			return r, true
		}
	}
	return Record{}, false
}

// Lookup retains expired records so a later lookup cannot reinterpret them as new.
func (s Store) Lookup(caller, kind, key string) (Record, bool, error) {
	diskMu.Lock()
	defer diskMu.Unlock()
	records, err := s.read(caller)
	if err != nil {
		return Record{}, false, err
	}
	r, ok := find(records, kind, key)
	if ok && !s.now().Before(r.Expires) {
		return r, true, ErrMissing
	}
	return r, ok, nil
}
func (s Store) Commit(caller string, r Record, identity Identity, sessionKey string, preferSession bool, firstProfile ...string) (Record, error) {
	diskMu.Lock()
	defer diskMu.Unlock()
	if caller == "" || identity.AuthID == "" || identity.Provider != "kilo" {
		return Record{}, ErrConflict
	}
	records, err := s.read(caller)
	if err != nil {
		return Record{}, err
	}
	match := func(previous Identity) bool {
		if previous.DeviceProfileID == "" && len(firstProfile) == 1 && identity.DeviceProfileID == firstProfile[0] {
			previous.DeviceProfileID = identity.DeviceProfileID
		}
		return previous == identity
	}
	if r.SourceID != "" {
		source, ok := find(records, "id", r.SourceID)
		if !ok || !s.now().Before(source.Expires) {
			return Record{}, ErrMissing
		}
		if !match(source.Identity) || len(r.InheritedCalls) == 0 || preferSession {
			return Record{}, ErrConflict
		}
		for id, kind := range r.InheritedCalls {
			if source.CallTypes[id] != kind || !slices.Contains(source.Calls, id) {
				return Record{}, ErrConflict
			}
		}
	}
	if r.ID != "" {
		current, ok := find(records, "id", r.ID)
		if !ok || !s.now().Before(current.Expires) {
			return Record{}, ErrMissing
		}
		if !match(current.Identity) || current.Session != r.Session {
			return Record{}, ErrConflict
		}
		r = current
	} else {
		if sessionKey != "" {
			if existing, ok := find(records, "session", sessionKey); ok {
				if !s.now().Before(existing.Expires) {
					return Record{}, ErrMissing
				}
				if !match(existing.Identity) {
					return Record{}, ErrConflict
				}
				r = existing
			}
		}
		if r.ID == "" {
			r = Record{ID: uuid.NewString(), Caller: caller, Identity: identity, Session: "ses_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:26], SourceID: r.SourceID, InheritedCalls: r.InheritedCalls}
		}
	}
	r.Identity = identity
	// Explicit response ownership wins over the session hint. Remove the old
	// alias in the same document commit, keeping its response/call ownership intact.
	if sessionKey != "" {
		for i := range records {
			if records[i].ID == r.ID || !slices.Contains(records[i].Sessions, sessionKey) {
				continue
			}
			if !preferSession {
				return Record{}, ErrConflict
			}
			records[i].Sessions = slices.DeleteFunc(records[i].Sessions, func(key string) bool { return key == sessionKey })
		}
	}
	if sessionKey != "" && !slices.Contains(r.Sessions, sessionKey) {
		r.Sessions = append(r.Sessions, sessionKey)
	}
	r.Expires = s.now().Add(Lifetime).UTC()
	replaced := false
	for i := range records {
		if records[i].ID == r.ID {
			records[i] = r
			replaced = true
			break
		}
	}
	if !replaced {
		records = append(records, r)
	}
	if err = s.write(caller, records); err != nil {
		return Record{}, err
	}
	return r, nil
}

// Publish starts the response lifetime when its first identifier is observed.
// Loading the same record after restart never renews its absolute expiry.
func (s Store) Publish(r Record, response string, calls map[string]string) error {
	diskMu.Lock()
	defer diskMu.Unlock()
	records, err := s.read(r.Caller)
	if err != nil {
		return err
	}
	current, ok := find(records, "id", r.ID)
	if !ok || current.Identity != r.Identity || current.Session != r.Session {
		return ErrConflict
	}
	changed := false
	add := func(kind, key string, aliases *[]string) error {
		if key == "" {
			return nil
		}
		if other, ok := find(records, kind, key); ok && other.ID != r.ID {
			return ErrConflict
		}
		if !slices.Contains(*aliases, key) {
			*aliases = append(*aliases, key)
			changed = true
		}
		return nil
	}
	newResponse := response != "" && !slices.Contains(current.Responses, response)
	if err = add("response", response, &current.Responses); err != nil {
		return err
	}
	if current.CallTypes == nil {
		current.CallTypes = map[string]string{}
	}
	for id, kind := range calls {
		if err = add("call", id, &current.Calls); err != nil {
			return err
		}
		if previous := current.CallTypes[id]; previous != "" && previous != kind {
			return ErrConflict
		}
		if current.CallTypes[id] != kind {
			current.CallTypes[id] = kind
			changed = true
		}
	}
	if newResponse {
		current.Expires = s.now().Add(Lifetime).UTC()
	}
	if !changed {
		return nil
	}
	for i := range records {
		if records[i].ID == r.ID {
			records[i] = current
			break
		}
	}
	return s.write(r.Caller, records)
}

// A short keyed gate covers only unresolved first selection and its durable commit.
// It never holds an account reservation while another request creates the binding.
type gate struct {
	token chan struct{}
	refs  int
}

var gateMu sync.Mutex
var gates = map[string]*gate{}

func Lock(ctx context.Context, key string) (func(), error) {
	return LockWithWait(ctx, key, func(ctx context.Context, ready <-chan struct{}) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ready:
			return nil
		}
	})
}

// LockWithWait consumes the ready token through the caller's admission budget.
// The callback is invoked only when the gate is occupied, never for upstream I/O.
func LockWithWait(ctx context.Context, key string, wait func(context.Context, <-chan struct{}) error) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	gateMu.Lock()
	g := gates[key]
	if g == nil {
		g = &gate{token: make(chan struct{}, 1)}
		g.token <- struct{}{}
		gates[key] = g
	}
	g.refs++
	gateMu.Unlock()
	drop := func() {
		gateMu.Lock()
		g.refs--
		if g.refs == 0 {
			delete(gates, key)
		}
		gateMu.Unlock()
	}
	select {
	case <-g.token:
	default:
		if err := wait(ctx, g.token); err != nil {
			drop()
			return nil, err
		}
	}
	var once sync.Once
	return func() { once.Do(func() { g.token <- struct{}{}; drop() }) }, nil
}

type Plan struct {
	Store         Store
	Caller        string
	SessionKey    string
	PreferSession bool
	Record        Record
	Release       func()
	FirstResponse string
}
type planKey struct{}

func WithPlan(ctx context.Context, p *Plan) context.Context {
	return context.WithValue(ctx, planKey{}, p)
}
func FromContext(ctx context.Context) *Plan { p, _ := ctx.Value(planKey{}).(*Plan); return p }
