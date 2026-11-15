package auth

import (
	"context"
	"testing"
	"testing/synctest"
	"time"
)

func TestKiloM8BVirtualLifecycle(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := NewManager(nil, nil, nil)
		a := &Auth{ID: "clock-kilo", Provider: "kilo", Status: StatusActive, Metadata: map[string]any{}}
		applyKiloProbeConfig(a, KiloProbeConfig{Revision: 3, HealthEnabled: true, IntervalSeconds: 300})
		if _, err := m.Register(context.Background(), a); err != nil {
			t.Fatal(err)
		}
		ticks := make(chan time.Time)
		ctx, cancel := context.WithCancel(context.Background())
		m.startKiloProbes(ctx, ticks, func() {})
		start := time.Now()
		view, _ := m.KiloProbes(a.ID)
		if view.Health.NextRunAt == nil || !view.Health.NextRunAt.Equal(start.Add(300*time.Second)) {
			t.Fatalf("start=%+v", view)
		}
		time.Sleep(299 * time.Second)
		ticks <- time.Now()
		synctest.Wait()
		view, _ = m.KiloProbes(a.ID)
		if view.Health.Status != "scheduled" || view.Health.LastStartedAt != nil {
			t.Fatalf("early=%+v", view)
		}
		time.Sleep(time.Second)
		ticks <- time.Now()
		synctest.Wait()
		view, _ = m.KiloProbes(a.ID)
		if view.Health.Status != "failed" || view.Health.LastStartedAt == nil || view.Health.ModelCalls != 0 {
			t.Fatalf("unavailable executor=%+v", view)
		}
		cancel()
		synctest.Wait()
		view, _ = m.KiloProbes(a.ID)
		if view.Health.NextRunAt != nil {
			t.Fatal("canceled host retained schedule")
		}
		<-m.ProcessKiloProbes(context.Background(), time.Now().Add(time.Hour))
		m.startKiloProbes(context.Background(), ticks, func() {})
		view, _ = m.KiloProbes(a.ID)
		if view.Health.Status != "scheduled" || view.Health.LastStartedAt != nil || view.Revision != 3 || !view.Health.NextRunAt.Equal(time.Now().Add(300*time.Second)) {
			t.Fatalf("restart=%+v", view)
		}
		m.StopKiloProbes()
		t.Log("virtual299/300 boundary; host cancel stops; restart config only/full next cycle; executor absent model0")
	})
}
