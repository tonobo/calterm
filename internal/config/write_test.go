package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// write puts body in a temp file and returns its path.
func write(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// validAccount is the minimum a config needs to survive SetHidden's
// post-write validation.
const validAccount = `[[account]]
name = "personal"
url = "https://example.com/dav"
username = "u"
password_cmd = "echo p"
`

func TestSetHiddenReplacesTheExistingKeyAndKeepsEveryComment(t *testing.T) {
	path := write(t, validAccount+`
# Which calendars to hide.
[calendars]
# one entry per calendar
hidden = ["old"]
# trailing note
`)
	if err := SetHidden(path, []string{"personal/work", "personal/home"}); err != nil {
		t.Fatalf("SetHidden: %v", err)
	}
	want := validAccount + `
# Which calendars to hide.
[calendars]
# one entry per calendar
hidden = ["personal/work", "personal/home"]
# trailing note
`
	if got := read(t, path); got != want {
		t.Errorf("file mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestSetHiddenInsertsTheKeyWhenTheTableHasNone(t *testing.T) {
	path := write(t, validAccount+`
[calendars]
# nothing here yet
`)
	if err := SetHidden(path, []string{"personal/work"}); err != nil {
		t.Fatalf("SetHidden: %v", err)
	}
	want := validAccount + `
[calendars]
hidden = ["personal/work"]
# nothing here yet
`
	if got := read(t, path); got != want {
		t.Errorf("file mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestSetHiddenAppendsTheTableWhenAbsent(t *testing.T) {
	path := write(t, validAccount)
	if err := SetHidden(path, []string{"personal/work"}); err != nil {
		t.Fatalf("SetHidden: %v", err)
	}
	want := validAccount + `
[calendars]
hidden = ["personal/work"]
`
	if got := read(t, path); got != want {
		t.Errorf("file mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// A table header with a trailing comment -- "[calendars] # which ones to
// hide" is legal TOML -- must still be recognised as the [calendars] table.
// Missing it made replaceHidden append a SECOND [calendars] table, which
// Load rejects, permanently breaking the toggle for anyone whose file has
// this shape.
func TestSetHiddenRecognisesATableHeaderWithATrailingComment(t *testing.T) {
	path := write(t, validAccount+`
[calendars] # which ones to hide
hidden = ["old"]
`)
	if err := SetHidden(path, []string{"a"}); err != nil {
		t.Fatalf("SetHidden: %v", err)
	}
	got := read(t, path)
	if strings.Count(got, "[calendars]") != 1 {
		t.Errorf("want exactly one [calendars] table, got:\n%s", got)
	}
	if !strings.Contains(got, `hidden = ["a"]`) {
		t.Errorf("the hidden key was not updated:\n%s", got)
	}
	if _, err := Load(path); err != nil {
		t.Errorf("the written file does not load: %v", err)
	}
}

// A hidden key in a DIFFERENT table must not be mistaken for the real one.
func TestSetHiddenIgnoresAHiddenKeyInAnotherTable(t *testing.T) {
	path := write(t, validAccount+`
[ui]
hidden = "not the one"

[calendars]
hidden = ["old"]
`)
	if err := SetHidden(path, []string{"a"}); err != nil {
		t.Fatalf("SetHidden: %v", err)
	}
	got := read(t, path)
	if !strings.Contains(got, `hidden = "not the one"`) {
		t.Errorf("the [ui] table's hidden key was modified:\n%s", got)
	}
	if !strings.Contains(got, `hidden = ["a"]`) {
		t.Errorf("the [calendars] hidden key was not updated:\n%s", got)
	}
}

// A commented-out hidden line must not be mistaken for the real key.
func TestSetHiddenIgnoresACommentedOutKey(t *testing.T) {
	path := write(t, validAccount+`
[calendars]
# hidden = ["commented"]
hidden = ["real"]
`)
	if err := SetHidden(path, []string{"a"}); err != nil {
		t.Fatalf("SetHidden: %v", err)
	}
	got := read(t, path)
	if !strings.Contains(got, `# hidden = ["commented"]`) {
		t.Errorf("the commented line was modified:\n%s", got)
	}
	if !strings.Contains(got, `hidden = ["a"]`) {
		t.Errorf("the real key was not updated:\n%s", got)
	}
}

// A multi-line array must be replaced whole, leaving no orphan lines behind.
func TestSetHiddenReplacesAMultiLineArray(t *testing.T) {
	path := write(t, validAccount+`
[calendars]
hidden = [
  "one",
  "two",
]
# after
`)
	if err := SetHidden(path, []string{"a"}); err != nil {
		t.Fatalf("SetHidden: %v", err)
	}
	want := validAccount + `
[calendars]
hidden = ["a"]
# after
`
	if got := read(t, path); got != want {
		t.Errorf("file mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestSetHiddenWritesAnEmptyList(t *testing.T) {
	path := write(t, validAccount+`
[calendars]
hidden = ["old"]
`)
	if err := SetHidden(path, nil); err != nil {
		t.Fatalf("SetHidden: %v", err)
	}
	if got := read(t, path); !strings.Contains(got, "hidden = []") {
		t.Errorf("want an empty list, got:\n%s", got)
	}
}

func TestSetHiddenQuotesEntries(t *testing.T) {
	path := write(t, validAccount+`
[calendars]
hidden = []
`)
	if err := SetHidden(path, []string{`odd"name`, "with space"}); err != nil {
		t.Fatalf("SetHidden: %v", err)
	}
	got := read(t, path)
	if !strings.Contains(got, `"odd\"name"`) {
		t.Errorf("entry was not quoted safely:\n%s", got)
	}
	// and the result must still load
	if _, err := Load(path); err != nil {
		t.Errorf("the written file does not parse: %v", err)
	}
}

func TestSetHiddenPreservesPermissions(t *testing.T) {
	path := write(t, validAccount+"\n[calendars]\nhidden = []\n")
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SetHidden(path, []string{"a"}); err != nil {
		t.Fatalf("SetHidden: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("perm = %v, want 0600 -- this file holds a password command", info.Mode().Perm())
	}
}

// The whole point of validating before the rename: a write that would produce
// an unloadable config must leave the original alone.
func TestSetHiddenLeavesTheOriginalWhenTheResultWouldNotLoad(t *testing.T) {
	// no [[account]] block, so Load rejects it
	original := "[calendars]\nhidden = [\"old\"]\n"
	path := write(t, original)
	if err := SetHidden(path, []string{"a"}); err == nil {
		t.Fatal("expected an error when the result would not load")
	}
	if got := read(t, path); got != original {
		t.Errorf("the original was modified despite the failure:\n%s", got)
	}
}

func TestSetHiddenOnAMissingFile(t *testing.T) {
	if err := SetHidden(filepath.Join(t.TempDir(), "absent.toml"), []string{"a"}); err == nil {
		t.Fatal("expected an error for a missing config file")
	}
}

// If the config path is a symlink -- as it is once a dotfiles manager like
// chezmoi or stow takes it over -- SetHidden must write through the symlink
// to the real file rather than renaming a regular file over the link,
// destroying it and orphaning the managed copy.
func TestSetHiddenWritesThroughASymlinkWithoutReplacingIt(t *testing.T) {
	realDir := t.TempDir()
	realPath := filepath.Join(realDir, "real-config.toml")
	if err := os.WriteFile(realPath, []byte(validAccount+"\n[calendars]\nhidden = []\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	linkDir := t.TempDir()
	linkPath := filepath.Join(linkDir, "config.toml")
	if err := os.Symlink(realPath, linkPath); err != nil {
		t.Fatal(err)
	}

	if err := SetHidden(linkPath, []string{"a"}); err != nil {
		t.Fatalf("SetHidden: %v", err)
	}

	info, err := os.Lstat(linkPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the symlink was replaced with a regular file")
	}
	target, err := os.Readlink(linkPath)
	if err != nil {
		t.Fatal(err)
	}
	if target != realPath {
		t.Errorf("symlink now points to %q, want %q", target, realPath)
	}
	if got := read(t, realPath); !strings.Contains(got, `hidden = ["a"]`) {
		t.Errorf("the real file's content was not updated:\n%s", got)
	}
}

func TestSetHiddenLeavesNoTempFiles(t *testing.T) {
	path := write(t, validAccount+"\n[calendars]\nhidden = []\n")
	for i := 0; i < 3; i++ {
		if err := SetHidden(path, []string{"a"}); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("directory holds %v, want only config.toml", names)
	}
}

// A round trip through Load must see exactly what was written.
func TestSetHiddenRoundTripsThroughLoad(t *testing.T) {
	path := write(t, validAccount+"\n[calendars]\nhidden = []\n")
	want := []string{"personal/work", "work/personal"}
	if err := SetHidden(path, want); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Calendars.Hidden) != 2 ||
		cfg.Calendars.Hidden[0] != want[0] || cfg.Calendars.Hidden[1] != want[1] {
		t.Errorf("Hidden = %v, want %v", cfg.Calendars.Hidden, want)
	}
}
