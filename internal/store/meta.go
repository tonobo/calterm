package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// CalendarMeta is what discovery learned about one calendar collection.
type CalendarMeta struct {
	ID    string `json:"id"`
	Path  string `json:"path"`
	Name  string `json:"name"`
	Color string `json:"color,omitempty"`
}

// AccountMeta is one account's discovery result plus the instant at which it
// last completed a sync with no errors. LastSync is per-account on purpose:
// an account that has been failing for three days must not look as fresh as
// the healthy account sitting next to it in the same run.
type AccountMeta struct {
	Name      string         `json:"name"`
	LastSync  time.Time      `json:"last_sync"`
	Calendars []CalendarMeta `json:"calendars"`
}

// Meta records discovery results and the last successful sync.
type Meta struct {
	LastSync time.Time     `json:"last_sync"`
	Accounts []AccountMeta `json:"accounts"`
}

func (s *Store) metaPath() string {
	return filepath.Join(s.root, "meta.json")
}

func (s *Store) LoadMeta() (*Meta, error) {
	b, err := os.ReadFile(s.metaPath())
	if os.IsNotExist(err) {
		return &Meta{}, nil
	}
	if err != nil {
		return nil, err
	}
	var m Meta
	if err := json.Unmarshal(b, &m); err != nil {
		return &Meta{}, nil
	}
	return &m, nil
}

func (s *Store) SaveMeta(m *Meta) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding meta: %w", err)
	}
	return WriteFileAtomic(s.metaPath(), b, 0o600)
}
