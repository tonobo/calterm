package theme

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

//go:embed themes/*.toml
var builtinFS embed.FS

// Registry holds every theme calterm knows about: the embedded built-ins,
// plus whatever the user's theme directory contributed. Built-ins register
// under "system:<name>"; user themes under "user:<name>". Both stay
// registered and reachable through Get and Names, even when they share a
// bare name -- deleting the built-in would take system:default down with
// it, breaking the spec's "an unknown configured theme falls back to
// system:default" guarantee for the one case that most needs it (a user's
// own default.toml going bad later). A user theme only shadows the
// same-named built-in for bare-name resolution; see Resolve.
type Registry struct {
	palettes map[string]Palette
	// warnings records, in load order, one message per theme file that was
	// skipped for being unreadable or malformed. A malformed or missing
	// theme must never stop the TUI starting, so LoadRegistry itself never
	// fails because of one broken file -- callers that want to surface the
	// warnings can read this.
	warnings []string
}

// LoadRegistry loads the embedded built-in themes, then overlays any *.toml
// files found in userDir. userDir may be empty or point to a directory that
// does not exist or cannot be read -- that is "no user themes", not an
// error. A malformed theme file, built-in or user, is skipped with a
// warning; the rest still load.
func LoadRegistry(userDir string) (*Registry, error) {
	r := &Registry{palettes: map[string]Palette{}}

	entries, err := builtinFS.ReadDir("themes")
	if err != nil {
		return nil, fmt.Errorf("theme: reading embedded themes: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".toml") {
			continue
		}
		b, err := builtinFS.ReadFile("themes/" + e.Name())
		if err != nil {
			r.warn(e.Name(), err)
			continue
		}
		p, err := decodePalette(b)
		if err != nil {
			r.warn(e.Name(), err)
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".toml")
		r.palettes["system:"+name] = p
	}

	if userDir == "" {
		return r, nil
	}
	userEntries, err := os.ReadDir(userDir)
	if err != nil {
		// A missing or unreadable user directory is "no user themes".
		return r, nil
	}
	for _, e := range userEntries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".toml") {
			continue
		}
		path := filepath.Join(userDir, e.Name())
		b, err := os.ReadFile(path)
		if err != nil {
			r.warn(e.Name(), err)
			continue
		}
		p, err := decodePalette(b)
		if err != nil {
			r.warn(e.Name(), err)
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".toml")
		r.palettes["user:"+name] = p
	}

	return r, nil
}

func (r *Registry) warn(file string, err error) {
	r.warnings = append(r.warnings, fmt.Sprintf("theme: skipping %s: %v", file, err))
}

// Warnings returns one message per theme file that was skipped while
// loading, in load order. Empty if every theme file loaded cleanly.
func (r *Registry) Warnings() []string {
	return r.warnings
}

func decodePalette(b []byte) (Palette, error) {
	var p Palette
	if _, err := toml.Decode(string(b), &p); err != nil {
		return Palette{}, err
	}
	if err := p.validate(); err != nil {
		return Palette{}, err
	}
	return p, nil
}

// Get looks up a theme by its full registered name (e.g. "system:default"
// or "user:mine"). It does not resolve bare names -- see Resolve.
func (r *Registry) Get(name string) (Palette, bool) {
	p, ok := r.palettes[name]
	return p, ok
}

// Resolve looks up a theme by either its full registered name or a bare
// name (no "system:"/"user:" prefix). A full name is passed straight to
// Get. A bare name prefers the user theme when both a user and a built-in
// theme share it, falling back to the built-in otherwise. It reports the
// canonical name the bare name resolved to, so a caller can persist or
// display it.
func (r *Registry) Resolve(name string) (Palette, string, bool) {
	if strings.HasPrefix(name, "system:") || strings.HasPrefix(name, "user:") {
		p, ok := r.Get(name)
		return p, name, ok
	}
	if p, ok := r.Get("user:" + name); ok {
		return p, "user:" + name, true
	}
	if p, ok := r.Get("system:" + name); ok {
		return p, "system:" + name, true
	}
	return Palette{}, "", false
}

// Names returns every registered theme name, sorted so the result is stable
// across calls and process runs.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.palettes))
	for name := range r.palettes {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
