package executor

import (
	"context"
	"sync"
	"sync/atomic"
)

type generationAdmissionKey struct{}

// GenerationAdmission transfers the first dispatch lease to a duplex executor.
// Follow-up generations acquire their own lease; idle sockets retain none.
type GenerationAdmission struct {
	initialOnce    sync.Once
	InitialRelease func()
	Acquire        func(context.Context) (func(), error)
	claimed        atomic.Bool
	finished       atomic.Bool
	transferred    atomic.Bool
}

func WithGenerationAdmission(ctx context.Context, admission *GenerationAdmission) context.Context {
	return context.WithValue(ctx, generationAdmissionKey{}, admission)
}

// ResetGenerationAdmission starts a sequential bootstrap attempt after the previous source has ended.
func ResetGenerationAdmission(ctx context.Context) {
	if a, _ := ctx.Value(generationAdmissionKey{}).(*GenerationAdmission); a != nil {
		a.claimed.Store(false)
		a.finished.Store(false)
	}
}
func ClaimGenerationAdmission(ctx context.Context) *GenerationAdmission {
	admission, _ := ctx.Value(generationAdmissionKey{}).(*GenerationAdmission)
	if admission == nil || !admission.claimed.CompareAndSwap(false, true) {
		return nil
	}
	return admission
}
func (a *GenerationAdmission) Claimed() bool { return a != nil && a.claimed.Load() }

// ReleaseInitial records completion even while the conductor still owns bootstrap retries.
func (a *GenerationAdmission) ReleaseInitial() {
	if a != nil {
		a.finished.Store(true)
		if a.transferred.Load() {
			a.initialOnce.Do(a.InitialRelease)
		}
	}
}
func (a *GenerationAdmission) Transfer() {
	if a != nil {
		a.transferred.Store(true)
		if a.finished.Load() {
			a.initialOnce.Do(a.InitialRelease)
		}
	}
}
