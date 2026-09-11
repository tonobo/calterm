package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// setConfigValue is the shared mechanics behind SetHidden and SetTheme: it
// resolves a symlink to its target, reads the file, computes the updated
// content via transform, and writes it back through writeValidated.
//
// Resolving the symlink first matters for every operation that follows --
// read, stat, and the temp-file write and rename -- so they all target the
// real file. Renaming a regular file over a symlink would destroy the link
// and orphan whatever manages it (a dotfiles tool such as chezmoi or stow,
// symlinking the config into its own repo). A path that does not exist is
// left alone: EvalSymlinks errors and path falls through unchanged to the
// ReadFile below, which reports the familiar missing-file error.
func setConfigValue(path string, transform func(string) string) error {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}

	original, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading config: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat config: %w", err)
	}

	updated := transform(string(original))

	// Validate before replacing. This file may hold the command that fetches
	// the user's password; a write that produced an unloadable config would
	// be far worse than a failed toggle.
	return writeValidated(path, updated, info.Mode().Perm())
}

// SetHidden rewrites only the `hidden` entry of the [calendars] table in the
// TOML file at path, leaving every comment, blank line and key order intact.
//
// It is a line-oriented edit rather than a decode/encode round trip on purpose:
// re-encoding would discard every comment in the file the first time a calendar
// was toggled, permanently. The file is the user's own, and typically carries
// notes about where its values came from.
func SetHidden(path string, hidden []string) error {
	line := renderHidden(hidden)
	return setConfigValue(path, func(src string) string {
		return replaceHidden(src, line)
	})
}

// SetTheme rewrites only the `theme` entry of the [ui] table in the TOML
// file at path, leaving every comment, blank line and key order intact.
//
// It mirrors SetHidden exactly, for the same reason: a decode/encode round
// trip would silently discard every comment in the user's file. theme may
// be empty -- that means no theme configured, use the default -- which is a
// valid state, not an error.
func SetTheme(path, theme string) error {
	line := renderTheme(theme)
	return setConfigValue(path, func(src string) string {
		return replaceTheme(src, line)
	})
}

// renderTheme formats the key=value line, quoting the value.
func renderTheme(theme string) string {
	return "theme = " + strconv.Quote(theme)
}

// replaceTheme returns src with the [ui] table's theme entry replaced by
// line. It handles three shapes: the key present, the table present without
// the key, and no table at all. It follows replaceHidden's structure.
func replaceTheme(src, line string) string {
	lines := strings.Split(src, "\n")

	inUI := false
	tableStart := -1 // index of the [ui] header
	keyLine := -1    // index of the theme key

	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, "#") {
			continue // a commented-out key is not the key
		}
		if strings.HasPrefix(trimmed, "[") {
			header := strings.TrimSpace(stripTrailingComment(trimmed))
			inUI = header == "[ui]"
			if inUI {
				tableStart = i
			}
			continue
		}
		if !inUI || keyLine != -1 {
			continue
		}
		if key, _, ok := strings.Cut(trimmed, "="); ok && strings.TrimSpace(key) == "theme" {
			keyLine = i
		}
	}

	switch {
	case keyLine != -1:
		out := append([]string{}, lines[:keyLine]...)
		out = append(out, line)
		out = append(out, lines[keyLine+1:]...)
		return strings.Join(out, "\n")

	case tableStart != -1:
		out := append([]string{}, lines[:tableStart+1]...)
		out = append(out, line)
		out = append(out, lines[tableStart+1:]...)
		return strings.Join(out, "\n")

	default:
		trimmed := strings.TrimRight(src, "\n")
		return trimmed + "\n\n[ui]\n" + line + "\n"
	}
}

// renderHidden formats the array on one line, quoting each entry.
func renderHidden(hidden []string) string {
	quoted := make([]string, 0, len(hidden))
	for _, h := range hidden {
		quoted = append(quoted, strconv.Quote(h))
	}
	return "hidden = [" + strings.Join(quoted, ", ") + "]"
}

// replaceHidden returns src with the [calendars] table's hidden entry replaced
// by line. It handles three shapes: the key present, the table present without
// the key, and no table at all.
func replaceHidden(src, line string) string {
	lines := strings.Split(src, "\n")

	inCalendars := false
	tableStart := -1 // index of the [calendars] header
	keyStart := -1   // index of the hidden key
	keyEnd := -1     // index of its last line, for a multi-line array

	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, "#") {
			continue // a commented-out key is not the key
		}
		if strings.HasPrefix(trimmed, "[") {
			// A table header may carry a trailing comment -- "[calendars] #
			// which ones to hide" is legal TOML -- so the comparison strips
			// it first. Without this, a header like that is never recognised
			// as [calendars], and a second [calendars] table gets appended
			// below, which the post-write validation then rejects outright.
			header := strings.TrimSpace(stripTrailingComment(trimmed))
			inCalendars = header == "[calendars]"
			if inCalendars {
				tableStart = i
			}
			continue
		}
		if !inCalendars || keyStart != -1 {
			continue
		}
		if key, _, ok := strings.Cut(trimmed, "="); ok && strings.TrimSpace(key) == "hidden" {
			keyStart = i
			keyEnd = i
			// A value may span lines: consume until the brackets balance.
			for depth := bracketDepth(lines[i]); depth > 0 && keyEnd+1 < len(lines); {
				keyEnd++
				depth += bracketDepth(lines[keyEnd])
			}
		}
	}

	switch {
	case keyStart != -1:
		out := append([]string{}, lines[:keyStart]...)
		out = append(out, line)
		out = append(out, lines[keyEnd+1:]...)
		return strings.Join(out, "\n")

	case tableStart != -1:
		out := append([]string{}, lines[:tableStart+1]...)
		out = append(out, line)
		out = append(out, lines[tableStart+1:]...)
		return strings.Join(out, "\n")

	default:
		trimmed := strings.TrimRight(src, "\n")
		return trimmed + "\n\n[calendars]\n" + line + "\n"
	}
}

// stripTrailingComment removes a "# ..." suffix from line, ignoring any '#'
// that appears inside a double-quoted string.
func stripTrailingComment(line string) string {
	inString, escaped := false, false
	for i, r := range line {
		switch {
		case escaped:
			escaped = false
		case r == '\\' && inString:
			escaped = true
		case r == '"':
			inString = !inString
		case r == '#' && !inString:
			return line[:i]
		}
	}
	return line
}

// bracketDepth reports how many square brackets a line opens minus how many it
// closes, ignoring anything inside a double-quoted string.
func bracketDepth(line string) int {
	depth, inString, escaped := 0, false, false
	for _, r := range line {
		switch {
		case escaped:
			escaped = false
		case r == '\\' && inString:
			escaped = true
		case r == '"':
			inString = !inString
		case inString:
			// brackets inside a string are literal
		case r == '[':
			depth++
		case r == ']':
			depth--
		case r == '#':
			return depth // rest of the line is a comment
		}
	}
	return depth
}

// writeValidated writes content to a temp file beside path, confirms it parses
// as a config, and only then renames it over the original.
func writeValidated(path, content string, perm os.FileMode) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp*")
	if err != nil {
		return fmt.Errorf("creating temp file in %s: %w", dir, err)
	}
	tmp := f.Name()
	defer os.Remove(tmp) // no-op once the rename has succeeded

	if err := f.Chmod(perm); err != nil {
		f.Close()
		return fmt.Errorf("chmod %s: %w", tmp, err)
	}
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		return fmt.Errorf("writing %s: %w", tmp, err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("syncing %s: %w", tmp, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", tmp, err)
	}

	if _, err := Load(tmp); err != nil {
		return fmt.Errorf("refusing to write a config that would not load: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("renaming %s to %s: %w", tmp, path, err)
	}
	return nil
}
