package kilo

import (
	"context"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/MoeForge/moego/ojson"
)

const ProfileURL = "https://api.kilo.ai/api/profile"
const ProfileMaxBytes = 128 * 1024

var organizationID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func ValidOrganizationID(id string) bool { return organizationID.MatchString(id) }

type Organization struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type OrganizationChoices struct {
	Organizations      []Organization `json:"organizations"`
	HasPersonalAccount bool           `json:"has_personal_account"`
}
type ProfileError struct {
	Status  int
	Message string
}

func (e *ProfileError) Error() string { return e.Message }
func ProfileUnavailable() error       { return &ProfileError{503, "Kilo profile egress is unavailable"} }
func invalidProfile() error           { return &ProfileError{502, "Kilo profile response is invalid"} }

func FetchProfile(ctx context.Context, client *http.Client, token string) (OrganizationChoices, error) {
	return fetchProfileBudget(ctx, client, token, 5*time.Second)
}
func fetchProfileBudget(parent context.Context, client *http.Client, token string, budget time.Duration) (OrganizationChoices, error) {
	ctx, cancel := context.WithTimeout(parent, budget)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ProfileURL, nil)
	if err != nil {
		return OrganizationChoices{}, invalidProfile()
	}
	token = strings.TrimPrefix(strings.TrimSpace(token), "Bearer ")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	response, err := client.Do(req)
	if err != nil {
		if parent.Err() != nil {
			return OrganizationChoices{}, parent.Err()
		}
		return OrganizationChoices{}, &ProfileError{503, "Kilo profile connection failed"}
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, ProfileMaxBytes+1))
	if parent.Err() != nil {
		return OrganizationChoices{}, parent.Err()
	}
	if err != nil || ctx.Err() != nil {
		return OrganizationChoices{}, &ProfileError{503, "Kilo profile connection failed"}
	}
	if response.StatusCode != http.StatusOK || len(body) > ProfileMaxBytes || !ojson.ValidBytes(body) {
		return OrganizationChoices{}, invalidProfile()
	}
	root := ojson.ParseBytes(body)
	if !root.IsObject() {
		return OrganizationChoices{}, invalidProfile()
	}
	organizations := root.Get("organizations")
	if organizations.Exists() && organizations.Type != ojson.Null && !organizations.IsArray() {
		return OrganizationChoices{}, invalidProfile()
	}
	choices := OrganizationChoices{Organizations: []Organization{}, HasPersonalAccount: root.Get("hasPersonalAccount").Type != ojson.False}
	for _, item := range organizations.Array() {
		id := item.Get("id")
		if !item.IsObject() || id.Type != ojson.String || !ValidOrganizationID(id.String()) {
			continue
		}
		name := item.Get("name")
		label := id.String()
		if name.Type == ojson.String && name.String() != "" {
			runes := []rune(name.String())
			if len(runes) > 256 {
				runes = runes[:256]
			}
			label = string(runes)
		}
		choices.Organizations = append(choices.Organizations, Organization{ID: id.String(), Name: label})
	}
	return choices, nil
}
