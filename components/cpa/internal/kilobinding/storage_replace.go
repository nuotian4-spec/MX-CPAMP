package kilobinding

import (
	"context"
	"errors"
	"os"
	"runtime"
	"syscall"
	"time"
)

// WithContext shares the bounded replacement delay across one request's commits.
func (s Store) WithContext(ctx context.Context) Store {
	s.requestContext = ctx
	s.replaceWait = new(time.Duration)
	return s
}

// Only the atomic replacement is retried: at most five attempts and 55ms of waits.
// A request shares the wait allowance across Commit and Publish under diskMu.
// Windows readers may briefly deny delete sharing even when the file is writable.
func replaceBindingFile(ctx context.Context, spent *time.Duration, from, to string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if spent == nil {
		spent = new(time.Duration)
	}
	delays := [...]time.Duration{5 * time.Millisecond, 10 * time.Millisecond, 20 * time.Millisecond, 20 * time.Millisecond}
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := os.Rename(from, to)
		if err == nil || runtime.GOOS != "windows" || attempt == len(delays) {
			return err
		}
		var code syscall.Errno
		if !errors.As(err, &code) || (code != 5 && code != 32) {
			return err
		}
		if *spent+delays[attempt] > 55*time.Millisecond {
			return err
		}
		*spent += delays[attempt]
		timer := time.NewTimer(delays[attempt])
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
