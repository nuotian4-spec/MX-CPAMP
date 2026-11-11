package oauthlogin

import (
	"encoding/hex"
	"strings"
)

func validTaskID(id string) bool {
	if len(id) != 36 || id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
		return false
	}
	raw := strings.ReplaceAll(id, "-", "")
	_, err := hex.DecodeString(raw)
	return err == nil && strings.ToLower(id) == id
}
