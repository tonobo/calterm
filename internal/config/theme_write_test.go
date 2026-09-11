package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetThemeRoundTrip(t *testing.T) {
	path := write(t, validAccount+`
[ui]
theme = "system:default"
`)
	if err := SetTheme(path, "system:catppuccin-mocha"); err != nil {
		t.Fatalf("SetTheme: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.Theme != "system:catppuccin-mocha" {
		t.Errorf("UI.Theme = %q, want %q", cfg.UI.Theme, "system:catppuccin-mocha")
	}
}

func TestSetThemeReplacesTheExistingKeyAndKeepsEveryComment(t *testing.T) {
	path := write(t, validAccount+`
# UI settings.
[ui]
# which theme to use
theme = "system:default"
# trailing note
`)
	if err := SetTheme(path, "user:my-theme"); err != nil {
		t.Fatalf("SetTheme: %v", err)
	}
	want := validAccount + `
# UI settings.
[ui]
# which theme to use
theme = "user:my-theme"
# trailing note
`
	if got := read(t, path); got != want {
		t.Errorf("file mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestSetThemeInsertsTheKeyWhenTheSectionHasNone(t *testing.T) {
	path := write(t, validAccount+`
[ui]
default_view = "week"
`)
	if err := SetTheme(path, "system:catppuccin-mocha"); err != nil {
		t.Fatalf("SetTheme: %v", err)
	}
	want := validAccount + `
[ui]
theme = "system:catppuccin-mocha"
default_view = "week"
`
	if got := read(t, path); got != want {
		t.Errorf("file mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestSetThemeAppendsTheSectionWhenAbsent(t *testing.T) {
	path := write(t, validAccount)
	if err := SetTheme(path, "system:catppuccin-mocha"); err != nil {
		t.Fatalf("SetTheme: %v", err)
	}
	want := validAccount + `
[ui]
theme = "system:catppuccin-mocha"
`
	if got := read(t, path); got != want {
		t.Errorf("file mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestSetThemeUnrelatedKeysSurviveVerbatim(t *testing.T) {
	path := write(t, validAccount+`
[waybar]
lead_time = "10m"

[ui]
default_view = "day"
week_start = "sunday"
theme = "system:default"

[calendars]
hidden = ["personal/work"]
`)
	if err := SetTheme(path, "system:solarized-dark"); err != nil {
		t.Fatalf("SetTheme: %v", err)
	}
	got := read(t, path)
	if !strings.Contains(got, `lead_time = "10m"`) {
		t.Errorf("waybar section was disturbed:\n%s", got)
	}
	if !strings.Contains(got, `default_view = "day"`) || !strings.Contains(got, `week_start = "sunday"`) {
		t.Errorf("other ui keys were disturbed:\n%s", got)
	}
	if !strings.Contains(got, `hidden = ["personal/work"]`) {
		t.Errorf("calendars section was disturbed:\n%s", got)
	}
	if !strings.Contains(got, `theme = "system:solarized-dark"`) {
		t.Errorf("theme was not updated:\n%s", got)
	}
}

// A theme key in a DIFFERENT table must not be mistaken for the real one.
func TestSetThemeIgnoresAThemeKeyInAnotherTable(t *testing.T) {
	path := write(t, validAccount+`
[calendars]
theme = "not the one"

[ui]
theme = "system:default"
`)
	if err := SetTheme(path, "system:catppuccin-mocha"); err != nil {
		t.Fatalf("SetTheme: %v", err)
	}
	got := read(t, path)
	if !strings.Contains(got, `theme = "not the one"`) {
		t.Errorf("the [calendars] theme key was modified:\n%s", got)
	}
	if !strings.Contains(got, `theme = "system:catppuccin-mocha"`) {
		t.Errorf("the [ui] theme key was not updated:\n%s", got)
	}
}

// A commented-out theme line must not be mistaken for the real key.
func TestSetThemeIgnoresACommentedOutKey(t *testing.T) {
	path := write(t, validAccount+`
[ui]
# theme = "system:commented"
theme = "system:default"
`)
	if err := SetTheme(path, "system:solarized-light"); err != nil {
		t.Fatalf("SetTheme: %v", err)
	}
	got := read(t, path)
	if !strings.Contains(got, `# theme = "system:commented"`) {
		t.Errorf("the commented line was modified:\n%s", got)
	}
	if !strings.Contains(got, `theme = "system:solarized-light"`) {
		t.Errorf("the real key was not updated:\n%s", got)
	}
}

func TestSetThemeAllowsAnEmptyValue(t *testing.T) {
	path := write(t, validAccount+`
[ui]
theme = "system:default"
`)
	if err := SetTheme(path, ""); err != nil {
		t.Fatalf("SetTheme: %v", err)
	}
	if got := read(t, path); !strings.Contains(got, `theme = ""`) {
		t.Errorf("want an empty theme value, got:\n%s", got)
	}
	if _, err := Load(path); err != nil {
		t.Errorf("empty theme should still load: %v", err)
	}
}

// The whole point of validating before the rename: a write that would
// produce an unloadable config must leave the original alone.
// This exercises the general validate-before-rename guard in
// writeValidated, not a theme-specific rejection -- Theme deliberately has
// no value-level validation of its own; any string is a legal theme value
// to this package.
func TestSetThemeLeavesTheOriginalWhenTheResultWouldNotLoad(t *testing.T) {
	// no [[account]] block, so Load rejects it
	original := "[ui]\ntheme = \"system:default\"\n"
	path := write(t, original)
	if err := SetTheme(path, "system:catppuccin-mocha"); err == nil {
		t.Fatal("expected an error when the result would not load")
	}
	if got := read(t, path); got != original {
		t.Errorf("the original was modified despite the failure:\n%s", got)
	}
}

func TestSetThemeOnAMissingFile(t *testing.T) {
	if err := SetTheme(filepath.Join(t.TempDir(), "absent.toml"), "system:default"); err == nil {
		t.Fatal("expected an error for a missing config file")
	}
}

// If the config path is a symlink -- as it is once a dotfiles manager like
// chezmoi or stow takes it over -- SetTheme must write through the symlink
// to the real file rather than renaming a regular file over the link,
// destroying it and orphaning the managed copy.
func TestSetThemeWritesThroughASymlinkWithoutReplacingIt(t *testing.T) {
	realDir := t.TempDir()
	realPath := filepath.Join(realDir, "real-config.toml")
	if err := os.WriteFile(realPath, []byte(validAccount+"\n[ui]\ntheme = \"system:default\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	linkDir := t.TempDir()
	linkPath := filepath.Join(linkDir, "config.toml")
	if err := os.Symlink(realPath, linkPath); err != nil {
		t.Fatal(err)
	}

	if err := SetTheme(linkPath, "system:catppuccin-mocha"); err != nil {
		t.Fatalf("SetTheme: %v", err)
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
	if got := read(t, realPath); !strings.Contains(got, `theme = "system:catppuccin-mocha"`) {
		t.Errorf("the real file's content was not updated:\n%s", got)
	}
}

func TestSetThemePreservesPermissions(t *testing.T) {
	path := write(t, validAccount+"\n[ui]\ntheme = \"system:default\"\n")
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SetTheme(path, "user:my-theme"); err != nil {
		t.Fatalf("SetTheme: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("perm = %v, want 0600 -- this file holds a password command", info.Mode().Perm())
	}
}

func TestSetThemeLeavesNoTempFiles(t *testing.T) {
	path := write(t, validAccount+"\n[ui]\ntheme = \"system:default\"\n")
	for i := 0; i < 3; i++ {
		if err := SetTheme(path, "user:my-theme"); err != nil {
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
