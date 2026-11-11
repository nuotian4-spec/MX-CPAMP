package quotasnapshot

import (
	"context"
	"testing"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
)

func TestBPSInspectionSnapshotRoundTripKeepsProviderAndUnknownModels(t *testing.T) {
	const at = int64(1794355200000)
	s := newQuotaSnapshotTestService(t, at+1000)
	used, duration := 17.0, 2592000.0
	r := model.CodexInspectionResult{Provider: "bps", FileName: "one.json", AuthIndex: "one", AccountID: "account-one", AccountSnapshot: "one@example.test", CreatedAtMS: at, QuotaInventoryObserved: true, QuotaWindows: []model.CodexInspectionQuotaWindow{{ID: "bps-primary", UsedPercent: &used, ResetAtMS: 1796977220000, ResetAccuracy: "exact", LimitWindowSeconds: &duration, ModelScope: &model.CodexInspectionQuotaModelScope{Kind: "feature", Key: "bps_account_usage", Complete: false}}}}
	if err := s.WriteCodexInspectionResult(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	account := AccountTarget{AuthFileSnapshot: "one.json", AuthProviderSnapshot: "bps", AuthIndex: "one", AuthAccountIDSnapshot: "account-one", AccountSnapshot: "one@example.test", Source: "one.json"}
	q, err := s.Query(context.Background(), QueryRequest{Accounts: []QueryAccount{{Provider: "bps", RowKey: "bps", Account: account}}})
	if err != nil || len(q.Items) != 1 || len(q.Items[0].Windows) != 1 {
		t.Fatal(q, err)
	}
	w := q.Items[0].Windows[0]
	if q.Items[0].Provider != "bps" || w.UsedPercent == nil || *w.UsedPercent != 17 || w.ModelScopeKind != "feature" || w.ModelScopeKey != "bps_account_usage" {
		t.Fatal(q)
	}
	account.AuthProviderSnapshot = "codex"
	other, err := s.Query(context.Background(), QueryRequest{Accounts: []QueryAccount{{Provider: "codex", RowKey: "codex", Account: account}}})
	if err != nil || len(other.Items[0].Windows) != 0 {
		t.Fatal("BPS污染Codex", other, err)
	}
}

func TestBPSQuotaSnapshotDoesNotReuseSameFileIndexAfterAccountReplacement(t *testing.T) {
	const at = int64(1794355200000)
	s := newQuotaSnapshotTestService(t, at+1000)
	used := 17.0
	result := model.CodexInspectionResult{
		Provider: "bps", FileName: "one.json", AuthIndex: "one", AccountID: "account-one",
		AccountSnapshot: "one@example.test", CreatedAtMS: at, QuotaInventoryObserved: true,
		QuotaWindows: []model.CodexInspectionQuotaWindow{{ID: "bps-primary", UsedPercent: &used,
			ModelScope: &model.CodexInspectionQuotaModelScope{Kind: "feature", Key: "bps_account_usage", Complete: false}}},
	}
	if err := s.WriteCodexInspectionResult(context.Background(), result); err != nil {
		t.Fatal(err)
	}
	old := AccountTarget{AuthFileSnapshot: "one.json", AuthProviderSnapshot: "bps", AuthIndex: "one",
		AuthAccountIDSnapshot: "account-one", AccountSnapshot: "one@example.test", Source: "one.json"}
	for _, field := range []string{"account_id", "account_snapshot"} {
		t.Run(field, func(t *testing.T) {
			replaced := old
			if field == "account_id" {
				replaced.AuthAccountIDSnapshot = "account-two"
			} else {
				replaced.AccountSnapshot = "two@example.test"
			}
			query, err := s.Query(context.Background(), QueryRequest{IncludeInactive: true,
				Accounts: []QueryAccount{{Provider: "bps", RowKey: "same-row", Account: replaced}}})
			if err != nil || len(query.Items) != 1 || len(query.Items[0].Windows) != 0 {
				t.Fatalf("换账号仍读到旧BPS窗口: %#v, %v", query, err)
			}
		})
	}
	if _, ok := (AccountTarget{AuthFileSnapshot: "one.json", AuthProviderSnapshot: "bps", AuthIndex: "one"}).accountKey("bps"); ok {
		t.Fatal("缺账户身份仍建立BPS快照键")
	}
	query, err := s.Query(context.Background(), QueryRequest{Accounts: []QueryAccount{{Provider: "bps", RowKey: "old", Account: old}}})
	if err != nil || len(query.Items) != 1 || len(query.Items[0].Windows) != 1 {
		t.Fatalf("原账号窗口被改写: %#v, %v", query, err)
	}
}
