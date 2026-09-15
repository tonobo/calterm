package desktop

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSoundNamesInDiscoversThemeAndUnthemedSounds(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	writeSoundFixture(t, first, "sounds/aurora/index.theme")
	writeSoundFixture(t, first, "sounds/aurora/stereo/notify-new.oga")
	writeSoundFixture(t, first, "sounds/aurora/stereo/notify-old.wav")
	writeSoundFixture(t, first, "sounds/aurora/stereo/metadata.sound")
	writeSoundFixture(t, first, "sounds/unindexed/stereo/not-a-theme.oga")
	writeSoundFixture(t, first, "sounds/direct-alert.ogg")
	// A theme may be spread over multiple XDG data roots. Only one of those
	// roots needs to provide its index.theme.
	writeSoundFixture(t, second, "sounds/aurora/stereo/notify-extra.oga")
	writeSoundFixture(t, second, "sounds/aurora/stereo/notify-new.wav")

	want := []string{"direct-alert", "notify-extra", "notify-new", "notify-old"}
	if got := soundNamesIn([]string{first, second}); !reflect.DeepEqual(got, want) {
		t.Fatalf("soundNamesIn() = %v, want %v", got, want)
	}
}

func TestSoundNamesInIgnoresMissingRoots(t *testing.T) {
	if got := soundNamesIn([]string{filepath.Join(t.TempDir(), "missing")}); len(got) != 0 {
		t.Fatalf("soundNamesIn() = %v, want no sounds", got)
	}
}

func writeSoundFixture(t *testing.T, root, relative string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("synthetic fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
}
