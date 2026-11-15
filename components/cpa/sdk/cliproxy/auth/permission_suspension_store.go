package auth

import (
	"context"
	"fmt"
)

// SavePermissionRelease commits only the target credential's complete runtime
// record in one file replacement. Unrelated writes and stale-file cleanup must
// never turn an already committed release into a reported failure.
func (s *FileCooldownStateStore) SavePermissionRelease(ctx context.Context, id string, records []CooldownStateRecord) error {
	if s == nil || s.dir == "" {
		return fmt.Errorf("permission state store is unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var path string
	var target []CooldownStateRecord
	for _, record := range records {
		if record.AuthID != id {
			continue
		}
		next, err := s.statePath(record)
		if err != nil {
			return err
		}
		if path != "" && path != next {
			return fmt.Errorf("permission target spans multiple state files")
		}
		path = next
		target = append(target, record)
	}
	if len(target) == 0 {
		return fmt.Errorf("permission release requires an epoch record")
	}
	return writeCooldownStateGroup(ctx, path, target)
}
