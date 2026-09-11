package store

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestLoadIndexMissingReturnsEmpty(t *testing.T) {
	s := New(t.TempDir())
	idx, err := s.LoadIndex("personal", "work")
	if err != nil {
		t.Fatalf("LoadIndex on a fresh store: %v", err)
	}
	if idx.CTag != "" {
		t.Errorf("CTag = %q, want empty", idx.CTag)
	}
	if idx.Objects == nil {
		t.Fatal("Objects map must be non-nil so callers can assign into it")
	}
	if len(idx.Objects) != 0 {
		t.Errorf("got %d objects, want 0", len(idx.Objects))
	}
}

func TestSaveAndLoadIndexRoundTrip(t *testing.T) {
	s := New(t.TempDir())
	want := &Index{
		CTag: "ctag-1",
		Objects: map[string]ObjectRef{
			"/dav/work/a.ics": {Href: "/dav/work/a.ics", ETag: "etag-a", UID: "uid-a"},
			"/dav/work/b.ics": {Href: "/dav/work/b.ics", ETag: "etag-b", UID: "uid-b"},
		},
	}
	if err := s.SaveIndex("personal", "work", want); err != nil {
		t.Fatalf("SaveIndex: %v", err)
	}
	got, err := s.LoadIndex("personal", "work")
	if err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}
	if got.CTag != want.CTag {
		t.Errorf("CTag = %q, want %q", got.CTag, want.CTag)
	}
	if len(got.Objects) != 2 {
		t.Fatalf("got %d objects, want 2", len(got.Objects))
	}
	if got.Objects["/dav/work/a.ics"].ETag != "etag-a" {
		t.Errorf("ETag = %q, want etag-a", got.Objects["/dav/work/a.ics"].ETag)
	}
}

func TestObjectWriteReadDelete(t *testing.T) {
	s := New(t.TempDir())
	body := []byte("BEGIN:VCALENDAR\r\nEND:VCALENDAR\r\n")
	if err := s.WriteObject("personal", "work", "uid-a", body); err != nil {
		t.Fatalf("WriteObject: %v", err)
	}
	got, err := s.ReadObject("personal", "work", "uid-a")
	if err != nil {
		t.Fatalf("ReadObject: %v", err)
	}
	if string(got) != string(body) {
		t.Errorf("object content mismatch")
	}
	if err := s.DeleteObject("personal", "work", "uid-a"); err != nil {
		t.Fatalf("DeleteObject: %v", err)
	}
	if _, err := s.ReadObject("personal", "work", "uid-a"); !os.IsNotExist(err) {
		t.Errorf("after delete, ReadObject error = %v, want not-exist", err)
	}
}

func TestDeleteObjectIsIdempotent(t *testing.T) {
	s := New(t.TempDir())
	if err := s.DeleteObject("personal", "work", "never-existed"); err != nil {
		t.Errorf("deleting an absent object should be a no-op, got %v", err)
	}
}

func TestListObjects(t *testing.T) {
	s := New(t.TempDir())
	for _, uid := range []string{"b", "a", "c"} {
		if err := s.WriteObject("personal", "work", uid, []byte("x")); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.ListObjects("personal", "work")
	if err != nil {
		t.Fatalf("ListObjects: %v", err)
	}
	sort.Strings(got)
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Errorf("ListObjects = %v, want [a b c]", got)
	}
}

func TestListObjectsOnMissingCalendar(t *testing.T) {
	s := New(t.TempDir())
	got, err := s.ListObjects("personal", "nope")
	if err != nil {
		t.Fatalf("ListObjects on a missing calendar should not error, got %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}

// UIDs arrive from the network and may contain path separators. They must never
// escape the calendar directory.
func TestObjectPathsAreSanitised(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	if err := s.WriteObject("personal", "work", "../../escape", []byte("x")); err != nil {
		t.Fatalf("WriteObject: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "..", "..", "escape.ics")); err == nil {
		t.Fatal("object escaped the store root")
	}
	got, err := s.ReadObject("personal", "work", "../../escape")
	if err != nil {
		t.Fatalf("ReadObject after sanitising: %v", err)
	}
	if string(got) != "x" {
		t.Errorf("content = %q, want x", got)
	}
}
