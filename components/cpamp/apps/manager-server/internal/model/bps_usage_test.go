package model

import "testing"

func TestBPSUsagePreservesZeroResetOnlyAndUnknownScope(t *testing.T) {
	zero, duration, absolute, relative := 0.0, 2592000.0, 1796977220.0, 9.0
	u := BPSUsage{QueriedAt: "2026-11-11T00:00:00Z", RateLimit: &BPSUsageRateLimit{PrimaryWindow: &BPSUsageWindow{UsedPercent: &zero, LimitWindowSeconds: &duration, ResetAt: &absolute, ResetAfterSeconds: &relative}, SecondaryWindow: &BPSUsageWindow{ResetAt: &absolute}}}
	w := u.QuotaWindows()
	if len(w) != 2 || w[0].UsedPercent == nil || *w[0].UsedPercent != 0 || w[0].ResetAtMS != 1796977220000 || w[1].UsedPercent != nil || w[1].ResetAtMS == 0 || w[0].ModelScope.Complete {
		t.Fatalf("用量归一化错误：%+v", w)
	}
	errorKind := "unauthorized"
	u.Error = &errorKind
	if len(u.QuotaWindows()) != 0 {
		t.Fatal("失败泄漏旧额度")
	}
}
