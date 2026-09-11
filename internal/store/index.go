package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ObjectRef records what the server told us about one calendar object.
type ObjectRef struct {
	Href         string `json:"href"`
	ETag         string `json:"etag"`
	LastModified string `json:"last_modified,omitempty"`
	UID          string `json:"uid"`
}

// Index is the per-calendar sync state. SyncToken is reserved for a future
// RFC 6578 sync-collection implementation and is unused today; keeping the
// field means adding it later needs no cache migration.
type Index struct {
	CTag      string               `json:"ctag"`
	SyncToken string               `json:"sync_token,omitempty"`
	Objects   map[string]ObjectRef `json:"objects"`
}

func (s *Store) indexPath(account, calendar string) string {
	return filepath.Join(s.CalendarDir(account, calendar), "index.json")
}

// LoadIndex reads a calendar's index. A missing index yields an empty one, so
// the first sync needs no special case.
func (s *Store) LoadIndex(account, calendar string) (*Index, error) {
	b, err := os.ReadFile(s.indexPath(account, calendar))
	if os.IsNotExist(err) {
		return &Index{Objects: map[string]ObjectRef{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var idx Index
	if err := json.Unmarshal(b, &idx); err != nil {
		// A corrupt index costs one full resync, which is better than failing.
		return &Index{Objects: map[string]ObjectRef{}}, nil
	}
	if idx.Objects == nil {
		idx.Objects = map[string]ObjectRef{}
	}
	return &idx, nil
}

func (s *Store) SaveIndex(account, calendar string, idx *Index) error {
	b, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding index: %w", err)
	}
	return WriteFileAtomic(s.indexPath(account, calendar), b, 0o600)
}
