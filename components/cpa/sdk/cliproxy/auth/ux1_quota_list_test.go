package auth

import "testing"

func TestUX1QuotaListStableIdentityAndLocalSnapshot(t *testing.T) {
	m := NewManager(nil, nil, nil)
	c1 := &Auth{ID: "c1", Provider: "codex", Label: "same", Metadata: map[string]any{"account_id": "account1", "access_token": "synthetic"}}
	c2 := &Auth{ID: "c2", Provider: "codex", Label: "same", Metadata: map[string]any{"account_id": "account2", "access_token": "synthetic"}}
	a := &Auth{ID: "k", Provider: "kilo", Metadata: map[string]any{}}
	applyKiloQuotaBinding(a, KiloQuotaBinding{Kind: "cpa_codex", CredentialID: "c1", AccountID: "account1", Revision: 3})
	m.auths = map[string]*Auth{"c1": c1, "c2": c2, "k": a}
	got := m.KiloQuotaList()["k"]
	if got.Summary.CredentialID != "c1" || got.Summary.DisplayLabel != "same" || got.Summary.BindingRevision != 3 || got.Snapshot.Query.Status != "never" || len(got.Snapshot.Windows) != 0 {
		t.Fatal(got)
	}
	c1.Label = "renamed"
	if got = m.KiloQuotaList()["k"]; got.Summary.DisplayLabel != "renamed" || got.Summary.CredentialID != "c1" {
		t.Fatal(got)
	}
	delete(m.auths, "c1")
	if got = m.KiloQuotaList()["k"]; got.Summary.Status != "source_missing" || got.Summary.DisplayLabel != "c1" {
		t.Fatal(got)
	}
	m.auths["c1"] = c2
	if got = m.KiloQuotaList()["k"]; got.Summary.Status != "source_changed" || got.Summary.DisplayLabel != "c1" {
		t.Fatal(got)
	}
	applyKiloQuotaBinding(a, KiloQuotaBinding{Kind: "none", Revision: 4})
	if got = m.KiloQuotaList()["k"]; got.Summary.Kind != "none" || got.Summary.CredentialID != "" || got.Snapshot.BindingRevision != 4 {
		t.Fatal(got)
	}
	applyKiloQuotaBinding(a, KiloQuotaBinding{Kind: "imported", AccessToken: "synthetic", Revision: 5})
	if got = m.KiloQuotaList()["k"]; got.Summary.Kind != "imported" || got.Summary.DisplayLabel != "" || got.Summary.Status != "ready" {
		t.Fatal(got)
	}
}
