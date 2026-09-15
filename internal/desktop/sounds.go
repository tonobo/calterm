package desktop

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SoundNames returns the event IDs exposed by installed freedesktop sound
// themes. The notification D-Bus API can report whether sound is supported,
// but it cannot enumerate sounds, so discovery follows the Sound Theme and
// XDG Base Directory specifications instead.
func SoundNames() []string {
	var dataDirs []string
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		if home, err := os.UserHomeDir(); err == nil {
			dataHome = filepath.Join(home, ".local", "share")
		}
	}
	if filepath.IsAbs(dataHome) {
		dataDirs = append(dataDirs, dataHome)
	}

	systemDirs := os.Getenv("XDG_DATA_DIRS")
	if systemDirs == "" {
		systemDirs = "/usr/local/share:/usr/share"
	}
	for _, dir := range filepath.SplitList(systemDirs) {
		if filepath.IsAbs(dir) {
			dataDirs = append(dataDirs, dir)
		}
	}
	return soundNamesIn(dataDirs)
}

// soundNamesIn is the filesystem-only half of SoundNames. Keeping its roots
// explicit makes discovery testable without consulting the host workstation.
func soundNamesIn(dataDirs []string) []string {
	themes := make(map[string]bool)
	for _, dataDir := range dataDirs {
		entries, err := os.ReadDir(filepath.Join(dataDir, "sounds"))
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			if info, err := os.Stat(filepath.Join(dataDir, "sounds", entry.Name(), "index.theme")); err == nil && !info.IsDir() {
				themes[entry.Name()] = true
			}
		}
	}

	names := make(map[string]bool)
	for _, dataDir := range dataDirs {
		soundsDir := filepath.Join(dataDir, "sounds")
		addSoundFiles(soundsDir, false, names)
		for themeName := range themes {
			addSoundFiles(filepath.Join(soundsDir, themeName), true, names)
		}
	}

	result := make([]string, 0, len(names))
	for name := range names {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

func addSoundFiles(root string, recursive bool, names map[string]bool) {
	add := func(path string, entry fs.DirEntry) {
		if entry.IsDir() {
			return
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".oga" && ext != ".ogg" && ext != ".wav" {
			return
		}
		name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		if name != "" {
			names[name] = true
		}
	}

	if !recursive {
		entries, err := os.ReadDir(root)
		if err != nil {
			return
		}
		for _, entry := range entries {
			add(filepath.Join(root, entry.Name()), entry)
		}
		return
	}
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		add(path, entry)
		return nil
	})
}
