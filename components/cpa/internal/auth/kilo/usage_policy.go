package kilo

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

// SourcePolicyScope validates a complete current observation without rounding.
// Query-start containment rejects observations whose cycle changed during I/O.
func SourcePolicyScope(observation UsageObservation, started, completed time.Time) (string, bool, *time.Time) {
	if observation.MainWindowsInvalid || started.IsZero() || completed.Before(started) {
		return "", false, nil
	}
	durations := []int{}
	seen := map[int]bool{}
	exhausted := false
	var next *time.Time
	for _, w := range observation.Windows {
		if w.Group != "rate_limit" {
			continue
		}
		if w.DurationSeconds == nil || w.UsedPercent == nil || w.ResetAt == nil {
			return "", false, nil
		}
		d, used := *w.DurationSeconds, *w.UsedPercent
		if math.IsNaN(d) || math.IsInf(d, 0) || d != math.Trunc(d) || (d != 18000 && d != 604800 && d != 2592000) || math.IsNaN(used) || math.IsInf(used, 0) || used < 0 || !w.ResetAt.After(completed) || w.ResetAt.Add(-time.Duration(d)*time.Second).After(started) || seen[int(d)] {
			return "", false, nil
		}
		seen[int(d)] = true
		durations = append(durations, int(d))
		exhausted = exhausted || used >= 100
		if next == nil || w.ResetAt.Before(*next) {
			at := *w.ResetAt
			next = &at
		}
	}
	sort.Ints(durations)
	parts := []string{}
	for _, d := range durations {
		parts = append(parts, strconv.Itoa(d))
	}
	scope := strings.Join(parts, "+")
	if scope != "18000+604800" && scope != "604800" && scope != "2592000" {
		return "", false, nil
	}
	return scope, exhausted, next
}
