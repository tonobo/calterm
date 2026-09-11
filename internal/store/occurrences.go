package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/tonobo/calterm/internal/model"
)

// OccurrenceIndex is the derived, pre-expanded view of every cached calendar.
// It exists so that the waybar subcommand is a read, a parse, and a
// first-match, with no iCalendar parsing and no recurrence arithmetic on a
// 30-second poll interval.
type OccurrenceIndex struct {
	GeneratedAt time.Time          `json:"generated_at"`
	From        time.Time          `json:"from"`
	To          time.Time          `json:"to"`
	Occurrences []model.Occurrence `json:"occurrences"`
}

// Age reports how long ago the index was generated. An index that has never
// been generated is reported as very old rather than fresh, so that a missing
// cache surfaces as stale instead of silently looking current.
func (i *OccurrenceIndex) Age(now time.Time) time.Duration {
	if i.GeneratedAt.IsZero() {
		return time.Duration(1<<62 - 1)
	}
	return now.Sub(i.GeneratedAt)
}

func (s *Store) occurrencesPath() string {
	return filepath.Join(s.root, "occurrences.json")
}

// LoadOccurrences reads the derived index. A missing or corrupt index yields an
// empty one: callers rebuild rather than fail.
func (s *Store) LoadOccurrences() (*OccurrenceIndex, error) {
	b, err := os.ReadFile(s.occurrencesPath())
	if os.IsNotExist(err) {
		return &OccurrenceIndex{}, nil
	}
	if err != nil {
		return nil, err
	}
	var idx OccurrenceIndex
	if err := json.Unmarshal(b, &idx); err != nil {
		return &OccurrenceIndex{}, nil
	}
	return &idx, nil
}

func (s *Store) SaveOccurrences(idx *OccurrenceIndex) error {
	b, err := json.Marshal(idx)
	if err != nil {
		return fmt.Errorf("encoding occurrence index: %w", err)
	}
	return WriteFileAtomic(s.occurrencesPath(), b, 0o600)
}
