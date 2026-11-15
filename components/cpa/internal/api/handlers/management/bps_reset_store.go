package management

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/MoeForge/moego/ojson"
)

// Low-volume management operations serialize ledger updates within this process.
var bpsResetMu sync.Mutex

type bpsResetReceipt struct {
	RequestID string `json:"redeem_request_id"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
	UpdatedAt string `json:"updated_at"`
}

type bpsResetRecord struct {
	Receipt    bpsResetReceipt `json:"receipt"`
	Account    string          `json:"account"`
	AuthID     string          `json:"auth_id"`
	Previous   string          `json:"previous_reset_id"`
	HTTPStatus int             `json:"http_status"`
}

type bpsResetLedger struct {
	Records map[string]bpsResetRecord `json:"records"`
	Last    map[string]string         `json:"last"`
}

func loadBPSResetLedger(path string) (bpsResetLedger, error) {
	ledger := bpsResetLedger{Records: map[string]bpsResetRecord{}, Last: map[string]string{}}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return ledger, nil
	}
	if err != nil {
		return ledger, err
	}
	root := ojson.ParseBytes(raw)
	if !ojson.ValidBytes(raw) || !root.IsObject() || !root.Get("records").IsObject() || !root.Get("last").IsObject() {
		return ledger, errors.New("reset state is incomplete")
	}
	for id, value := range root.Get("records").Map() {
		receipt := value.Get("receipt")
		status, parseErr := strconv.Atoi(value.Get("http_status").Raw)
		if !value.IsObject() || !receipt.IsObject() || parseErr != nil {
			return ledger, errors.New("reset record is invalid")
		}
		ledger.Records[id] = bpsResetRecord{Account: value.Get("account").String(), AuthID: value.Get("auth_id").String(), Previous: value.Get("previous_reset_id").String(), HTTPStatus: status, Receipt: bpsResetReceipt{RequestID: receipt.Get("redeem_request_id").String(), Status: receipt.Get("status").String(), Error: receipt.Get("error").String(), UpdatedAt: receipt.Get("updated_at").String()}}
	}
	for account, id := range root.Get("last").Map() {
		if id.Type != ojson.String {
			return ledger, errors.New("reset account state is invalid")
		}
		ledger.Last[account] = id.String()
	}
	return ledger, nil
}

func saveBPSResetLedger(path string, ledger bpsResetLedger) error {
	doc := ojson.NewDocString("{}")
	if err := doc.Set("records", ledger.Records); err != nil {
		return err
	}
	if err := doc.Set("last", ledger.Last); err != nil {
		return err
	}
	raw := doc.Bytes()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".bps-reset-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(raw)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(tmp, path)
}
