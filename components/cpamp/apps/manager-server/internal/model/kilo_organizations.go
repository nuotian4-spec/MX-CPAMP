package model

type KiloOrganization struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type KiloOrganizations struct {
	Organizations      []KiloOrganization `json:"organizations"`
	HasPersonalAccount bool               `json:"has_personal_account"`
	ConnectionRef      string             `json:"connection_ref"`
}
