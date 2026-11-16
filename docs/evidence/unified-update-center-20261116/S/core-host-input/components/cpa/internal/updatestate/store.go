package updatestate

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/MoeForge/moego/ojson"
)

var validID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,95}$`)

type Operation struct {
	ID            string `json:"id"`
	Type          string `json:"type"`
	PluginID      string `json:"plugin_id,omitempty"`
	TargetVersion string `json:"target_version"`
	Status        string `json:"status"`
	Stage         string `json:"stage"`
	ActiveVersion string `json:"active_version"`
	Registered    bool   `json:"registered"`
	LicenseStatus string `json:"license_status"`
	Message       string `json:"message"`
	StartedAt     string `json:"started_at"`
	FinishedAt    string `json:"finished_at,omitempty"`
	Baseline      string `json:"baseline,omitempty"`
}

func Terminal(status string) bool {
	switch status {
	case "completed", "completed_disabled", "failed", "license_unverified", "license_error":
		return true
	default:
		return false
	}
}

type BusyError struct{ OperationID string }

func (e *BusyError) Error() string { return "site maintenance is active; operation=" + e.OperationID }

type Store struct {
	LockPath  string
	Directory string
}

func New(lockPath, directory string) (*Store, error) {
	if !filepath.IsAbs(lockPath) || !filepath.IsAbs(directory) {
		return nil, fmt.Errorf("update site lock and state directory must be registered absolute paths")
	}
	if err := os.MkdirAll(filepath.Join(directory, "operations"), 0o700); err != nil {
		return nil, err
	}
	return &Store{LockPath: lockPath, Directory: directory}, nil
}

func (s *Store) Get(id string) (Operation, error) {
	if !validID.MatchString(id) {
		return Operation{}, fmt.Errorf("invalid operation id")
	}
	data, err := os.ReadFile(filepath.Join(s.Directory, "operations", id+".json"))
	if err != nil {
		return Operation{}, err
	}
	if !ojson.ValidBytes(data) {
		return Operation{}, fmt.Errorf("invalid operation record")
	}
	d := ojson.ParseBytes(data)
	return Operation{ID: d.Get("id").String(), Type: d.Get("type").String(), PluginID: d.Get("plugin_id").String(),
		TargetVersion: d.Get("target_version").String(), Status: d.Get("status").String(), Stage: d.Get("stage").String(),
		ActiveVersion: d.Get("active_version").String(), Registered: d.Get("registered").Bool(), LicenseStatus: d.Get("license_status").String(),
		Message: d.Get("message").String(), StartedAt: d.Get("started_at").String(), FinishedAt: d.Get("finished_at").String(), Baseline: d.Get("baseline").String()}, nil
}

func (s *Store) ActiveID() (string, error) {
	data, err := os.ReadFile(filepath.Join(s.Directory, "active.json"))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	id := ojson.GetBytes(data, "id").String()
	if !ojson.ValidBytes(data) || !validID.MatchString(id) {
		return "", fmt.Errorf("invalid active operation record; recovery required")
	}
	return id, nil
}

func (s *Store) Latest(kind string) (Operation, error) {
	entries, err := os.ReadDir(filepath.Join(s.Directory, "operations"))
	if err != nil { return Operation{}, err }
	var latest Operation
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || filepath.Ext(name) != ".json" { continue }
		op, errRead := s.Get(name[:len(name)-5])
		if errRead != nil || (kind != "" && op.Type != kind) { continue }
		if op.StartedAt > latest.StartedAt { latest = op }
	}
	if latest.ID == "" { return Operation{}, os.ErrNotExist }
	return latest, nil
}

// Begin shares the registered flock with mx-update; an abandoned owner stays busy.
func (s *Store) Begin(op Operation) (*Lease, error) {
	if !validID.MatchString(op.ID) {
		return nil, fmt.Errorf("invalid operation id")
	}
	file, err := os.OpenFile(s.LockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err = lockFile(file); err != nil {
		_ = file.Close()
		active, _ := s.ActiveID()
		return nil, &BusyError{OperationID: active}
	}
	closeLock := func() { _ = unlockFile(file); _ = file.Close() }
	active, errActive := s.ActiveID()
	if errActive != nil {
		closeLock()
		return nil, errActive
	}
	if active != "" {
		previous, errPrevious := s.Get(active)
		if errPrevious != nil || !Terminal(previous.Status) {
			closeLock()
			return nil, &BusyError{OperationID: active}
		}
	}
	if op.StartedAt == "" {
		op.StartedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if op.Status == "" {
		op.Status = "queued"
	}
	lease := &Lease{store: s, file: file, operation: op}
	if err = s.writeOperation(op); err != nil {
		closeLock()
		return nil, err
	}
	if err = WriteJSON(filepath.Join(s.Directory, "active.json"), map[string]string{"id": op.ID}); err != nil {
		closeLock()
		return nil, err
	}
	return lease, nil
}

type Lease struct {
	mu        sync.Mutex
	store     *Store
	file      *os.File
	operation Operation
}

func (l *Lease) Operation() Operation { l.mu.Lock(); defer l.mu.Unlock(); return l.operation }

func (l *Lease) Update(change func(*Operation)) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return fmt.Errorf("operation no longer owns the site lock")
	}
	change(&l.operation)
	return l.store.writeOperation(l.operation)
}

// Finish publishes the factual terminal record before relinquishing ownership.
func (l *Lease) Finish(change func(*Operation)) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	change(&l.operation)
	if !Terminal(l.operation.Status) {
		return fmt.Errorf("nonterminal operations retain ownership")
	}
	l.operation.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err := l.store.writeOperation(l.operation); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(l.store.Directory, "active.json")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	errUnlock := unlockFile(l.file)
	errClose := l.file.Close()
	l.file = nil
	return errors.Join(errUnlock, errClose)
}

// Detach leaves a durable unfinished owner for explicit, state-aware recovery.
func (l *Lease) Detach(message string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	l.operation.Status = "recovery_required"
	l.operation.Stage = "recovery_required"
	l.operation.Message = message
	errWrite := l.store.writeOperation(l.operation)
	errUnlock := unlockFile(l.file)
	errClose := l.file.Close()
	l.file = nil
	return errors.Join(errWrite, errUnlock, errClose)
}

func (s *Store) writeOperation(op Operation) error {
	return WriteJSON(filepath.Join(s.Directory, "operations", op.ID+".json"), op)
}

func WriteJSON(filename string, value any) error {
	doc := ojson.NewDocString("{}")
	if err := doc.Set("record", value); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(filename), ".update-*.tmp")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer func() { _ = os.Remove(name) }()
	if err = temporary.Chmod(0o600); err == nil {
		_, err = io.WriteString(temporary, ojson.GetBytes(doc.Bytes(), "record").Raw)
	}
	if err == nil {
		err = temporary.Sync()
	}
	errClose := temporary.Close()
	if err != nil {
		return err
	}
	if errClose != nil {
		return errClose
	}
	return os.Rename(name, filename)
}
