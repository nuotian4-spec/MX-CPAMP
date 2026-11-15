package kilobinding

// Resolve keeps explicit thread ownership separate from legacy session aliases.
func (s Store) Resolve(caller string, h Hints) (Record, error) {
	if h.Err != nil {
		return Record{}, h.Err
	}
	if h.Thread == "" {
		return s.resolveLegacy(caller, h)
	}
	record, exists, err := s.Lookup(caller, "session", h.Session)
	if err != nil {
		return Record{}, err
	}
	if exists {
		return record, s.ValidateHistory(record, h)
	}
	if h.Initialization != "warm_fork" {
		if h.History || h.Previous != "" {
			return Record{}, ErrMissing
		}
		return Record{}, nil
	}
	if h.Previous != "" || len(h.Calls) == 0 {
		return Record{}, ErrConflict
	}
	var source Record
	inherited := map[string]string{}
	for _, id := range h.Calls {
		if !h.Pairs[id] {
			return Record{}, ErrConflict
		}
		owner, ok, err := s.Lookup(caller, "call", id)
		if err != nil {
			return Record{}, err
		}
		if !ok {
			return Record{}, ErrMissing
		}
		if (source.ID != "" && source.ID != owner.ID) || owner.CallTypes[id] != h.Kinds[id] {
			return Record{}, ErrConflict
		}
		source = owner
		inherited[id] = h.Kinds[id]
	}
	return Record{Caller: caller, Identity: source.Identity, SourceID: source.ID, InheritedCalls: inherited}, nil
}

func (s Store) resolveLegacy(caller string, h Hints) (Record, error) {
	var record Record
	load := func(kind, id string, required bool) error {
		owner, ok, err := s.Lookup(caller, kind, id)
		if err != nil {
			return err
		}
		if !ok {
			if required {
				return ErrMissing
			}
			return nil
		}
		if record.ID != "" && record.ID != owner.ID {
			return ErrConflict
		}
		if kind == "call" && owner.CallTypes[id] != h.Kinds[id] {
			return ErrConflict
		}
		record = owner
		return nil
	}
	if h.Previous != "" {
		if err := load("response", h.Previous, true); err != nil {
			return Record{}, err
		}
	}
	for _, id := range h.Calls {
		if err := load("call", id, true); err != nil {
			return Record{}, err
		}
	}
	if record.ID == "" && h.Session != "" {
		if err := load("session", h.Session, h.History); err != nil {
			return Record{}, err
		}
	}
	if record.ID == "" && h.History {
		return Record{}, ErrMissing
	}
	return record, nil
}

// ValidateHistory permits only this branch's own references or its fixed,
// paired inheritance snapshot. Inherited calls retain their original owner.
func (s Store) ValidateHistory(record Record, h Hints) error {
	if h.Err != nil {
		return h.Err
	}
	if h.Previous != "" {
		owner, ok, err := s.Lookup(record.Caller, "response", h.Previous)
		if err != nil {
			return err
		}
		if !ok || record.ID == "" || owner.ID != record.ID {
			return ErrConflict
		}
	}
	for _, id := range h.Calls {
		owner, ok, err := s.Lookup(record.Caller, "call", id)
		if err != nil {
			return err
		}
		if !ok || owner.CallTypes[id] != h.Kinds[id] {
			return ErrConflict
		}
		if record.ID != "" && owner.ID == record.ID {
			continue
		}
		if record.SourceID == "" || owner.ID != record.SourceID || owner.Identity != record.Identity || record.InheritedCalls[id] != h.Kinds[id] || !h.Pairs[id] {
			return ErrConflict
		}
	}
	return nil
}
