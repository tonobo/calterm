package reminder

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/tonobo/calterm/internal/model"
	"github.com/tonobo/calterm/internal/store"
)

const stateFileName = "notifications.json"

// State records the scheduled start of every occurrence already delivered.
// Values make old entries prunable without retaining readable event data.
type State struct {
	Notified map[string]time.Time `json:"notified"`
}

func NewState() *State {
	return &State{Notified: make(map[string]time.Time)}
}

// LoadState reads notification deduplication state. A missing file is a clean
// first run; corrupt state is an error because treating it as empty could
// replay every notification in the active window.
func LoadState(s *store.Store) (*State, error) {
	path := filepath.Join(s.Root(), stateFileName)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return NewState(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading notification state: %w", err)
	}
	state := NewState()
	if err := json.Unmarshal(data, state); err != nil {
		return nil, fmt.Errorf("decoding notification state: %w", err)
	}
	if state.Notified == nil {
		state.Notified = make(map[string]time.Time)
	}
	return state, nil
}

func (s *State) Has(occurrence model.Occurrence, before time.Duration) bool {
	if s == nil {
		return false
	}
	_, ok := s.Notified[occurrenceKey(occurrence, before)]
	return ok
}

func (s *State) Mark(occurrence model.Occurrence, before time.Duration) {
	if s.Notified == nil {
		s.Notified = make(map[string]time.Time)
	}
	s.Notified[occurrenceKey(occurrence, before)] = occurrence.Start
}

// Prune removes old start times so a long-running installation keeps a small
// state file. A day is comfortably longer than StartGrace and any timer jitter.
func (s *State) Prune(before time.Time) bool {
	if s == nil {
		return false
	}
	changed := false
	for key, start := range s.Notified {
		if start.Before(before) {
			delete(s.Notified, key)
			changed = true
		}
	}
	return changed
}

func (s *State) Save(storage *store.Store) error {
	data, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("encoding notification state: %w", err)
	}
	if err := store.WriteFileAtomic(filepath.Join(storage.Root(), stateFileName), data, 0o600); err != nil {
		return fmt.Errorf("saving notification state: %w", err)
	}
	return nil
}
