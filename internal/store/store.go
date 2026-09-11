package store

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
)

// Store is the on-disk cache rooted at a directory, typically
// $XDG_CACHE_HOME/calterm.
type Store struct {
	root string
}

func New(root string) *Store { return &Store{root: root} }

func (s *Store) Root() string { return s.root }

// CalendarDir returns the directory holding one calendar's cache.
func (s *Store) CalendarDir(account, calendar string) string {
	return filepath.Join(s.root, safeName(account), safeName(calendar))
}

func (s *Store) objectPath(account, calendar, uid string) string {
	return filepath.Join(s.CalendarDir(account, calendar), "objects", safeName(uid)+".ics")
}

// safeName maps an arbitrary identifier to a single path segment. Identifiers
// arrive from the network, so anything containing a separator or traversal is
// replaced by a hash rather than trusted.
func safeName(s string) string {
	if s == "" {
		return "_"
	}
	if s == "." || s == ".." || strings.ContainsAny(s, "/\\") || strings.HasPrefix(s, ".") {
		sum := sha256.Sum256([]byte(s))
		return "h_" + hex.EncodeToString(sum[:8])
	}
	return s
}

func (s *Store) WriteObject(account, calendar, uid string, data []byte) error {
	return WriteFileAtomic(s.objectPath(account, calendar, uid), data, 0o600)
}

func (s *Store) ReadObject(account, calendar, uid string) ([]byte, error) {
	return os.ReadFile(s.objectPath(account, calendar, uid))
}

// DeleteObject removes an object. Removing one that is not present is a no-op.
func (s *Store) DeleteObject(account, calendar, uid string) error {
	err := os.Remove(s.objectPath(account, calendar, uid))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// ListObjects returns the UIDs (as stored on disk) of every cached object. A
// calendar directory that does not exist yields an empty slice, not an error.
func (s *Store) ListObjects(account, calendar string) ([]string, error) {
	dir := filepath.Join(s.CalendarDir(account, calendar), "objects")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var uids []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".ics") {
			continue
		}
		uids = append(uids, strings.TrimSuffix(name, ".ics"))
	}
	return uids, nil
}
