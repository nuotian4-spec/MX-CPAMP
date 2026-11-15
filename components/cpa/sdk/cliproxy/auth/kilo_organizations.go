package auth

import (
	"context"
	"errors"

	profile "github.com/router-for-me/CLIProxyAPI/v8/internal/auth/kilo"
)

var ErrKiloIdentityChanged = errors.New("Kilo account identity changed; reload and retry")
var ErrKiloOrganizationDenied = errors.New("Kilo credential does not permit the selected organization or personal space")

type kiloProfileReader interface {
	KiloOrganizations(context.Context, *Auth) (profile.OrganizationChoices, error)
}

// Profile I/O is outside the selection and mutation gates. The returned snapshot
// is compared with the live identity when the local credential commit begins.
func (m *Manager) validateKiloOrganization(ctx context.Context, id string, change KiloAccountChanges) (*Auth, error) {
	if change.APIKey == nil && change.OrganizationID == nil {
		return nil, nil
	}
	current, ok := m.GetByID(id)
	if !ok || current.Provider != "kilo" {
		return nil, ErrKiloAccountMissing
	}
	target := authMetadataString(current, "organization_id")
	if change.OrganizationID != nil {
		target = *change.OrganizationID
	}
	keyChanged := change.APIKey != nil && *change.APIKey != authMetadataString(current, "api_key")
	if change.OrganizationID == nil && (!keyChanged || target == "") {
		return current, nil
	}
	e, ok := m.Executor("kilo")
	if !ok {
		return nil, ErrKiloAccountStorage
	}
	reader, ok := e.(kiloProfileReader)
	if !ok {
		return nil, ErrKiloAccountStorage
	}
	candidate := current.Clone()
	if change.APIKey != nil {
		if candidate.Metadata == nil {
			candidate.Metadata = map[string]any{}
		}
		candidate.Metadata["api_key"] = *change.APIKey
		delete(candidate.Attributes, "api_key")
	}
	choices, err := reader.KiloOrganizations(ctx, candidate)
	if err != nil {
		return nil, err
	}
	if target == "" {
		if !choices.HasPersonalAccount {
			return nil, ErrKiloOrganizationDenied
		}
		return current, nil
	}
	for _, organization := range choices.Organizations {
		if organization.ID == target {
			return current, nil
		}
	}
	return nil, ErrKiloOrganizationDenied
}
