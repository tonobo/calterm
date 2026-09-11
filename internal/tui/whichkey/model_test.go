package whichkey

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/tonobo/calterm/internal/tui/keys"
)

const (
	modeA keys.Mode = iota
	modeB
)

func testStyles() Styles { return Styles{} }

func fixture() *keys.Group {
	return &keys.Group{
		Description: "leader",
		Entries: map[string]keys.Entry{
			"v": &keys.Group{
				Description: "+view",
				Entries:     map[string]keys.Entry{"a": &keys.Command{ID: "view.agenda", Description: "agenda"}},
			},
			"s": &keys.Command{ID: "sync", Description: "sync now"},
			"x": &keys.Command{ID: "only.a", Description: "mode A only", Modes: []keys.Mode{modeA}},
		},
	}
}

func plain(s string) string {
	var b strings.Builder
	skip := false
	for _, r := range s {
		switch {
		case r == '\x1b':
			skip = true
		case skip && r == 'm':
			skip = false
		case !skip:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func TestRenderShowsBreadcrumbAndEntries(t *testing.T) {
	got := plain(Render(fixture(), []string{" ", "v"}, modeA, 60, 20, testStyles()))
	for _, want := range []string{"space", "sync now", "+view", "esc", "bksp"} {
		if !strings.Contains(got, want) {
			t.Errorf("popup is missing %q:\n%s", want, got)
		}
	}
}

func TestRenderRendersLeaderKeyReadably(t *testing.T) {
	got := plain(Render(fixture(), []string{" "}, modeA, 60, 20, testStyles()))
	if strings.Contains(got, "»  ·") {
		t.Errorf("the leader should render as \"space\", not a raw blank:\n%s", got)
	}
	if !strings.Contains(got, "space") {
		t.Errorf("breadcrumb should name the leader:\n%s", got)
	}
}

func TestRenderDropsModeInvalidEntries(t *testing.T) {
	got := plain(Render(fixture(), []string{" "}, modeB, 60, 20, testStyles()))
	if strings.Contains(got, "mode A only") {
		t.Errorf("a mode-gated command must not be listed:\n%s", got)
	}
	if !strings.Contains(got, "sync now") {
		t.Errorf("ungated commands should still be listed:\n%s", got)
	}
}

func TestRenderUsesGinboxBoxAndRowLayout(t *testing.T) {
	got := plain(Render(fixture(), []string{" "}, modeA, 60, 20, testStyles()))
	for _, want := range []string{"╭", "╮", "╰", "╯", "↳", "→", "+leader", "q/esc close   bksp back"} {
		if !strings.Contains(got, want) {
			t.Errorf("popup is missing ginbox layout element %q:\n%s", want, got)
		}
	}
}

// The popup must never exceed the box it is given -- this project has shipped
// three separate defects of exactly that kind.
func TestRenderNeverExceedsItsBounds(t *testing.T) {
	big := &keys.Group{Description: "big", Entries: map[string]keys.Entry{}}
	for i := 0; i < 40; i++ {
		k := string(rune('a' + i%26))
		big.Entries[k+string(rune('0'+i/26))] = &keys.Command{
			ID:          "cmd",
			Description: strings.Repeat("long description ", 5),
		}
	}
	for _, w := range []int{20, 30, 50, 80, 120} {
		for _, h := range []int{3, 6, 12, 30} {
			out := Render(big, []string{" "}, modeA, w, h, testStyles())
			lines := strings.Split(out, "\n")
			if len(lines) > h {
				t.Errorf("w=%d h=%d: %d lines, want <= %d", w, h, len(lines), h)
			}
			for i, ln := range lines {
				if got := lipgloss.Width(ln); got > w {
					t.Errorf("w=%d h=%d: line %d is %d cells: %q", w, h, i, got, plain(ln))
				}
			}
		}
	}
}

func TestRenderIndicatesHiddenEntriesWhenItRunsOutOfHeight(t *testing.T) {
	big := &keys.Group{Description: "big", Entries: map[string]keys.Entry{}}
	for i := 0; i < 20; i++ {
		big.Entries[string(rune('a'+i))] = &keys.Command{ID: "c", Description: "does a thing"}
	}
	got := plain(Render(big, []string{" "}, modeA, 60, 8, testStyles()))
	if !strings.Contains(got, "more") {
		t.Errorf("a clipped popup should say how many entries are hidden:\n%s", got)
	}
}

func TestRenderEmptyGroup(t *testing.T) {
	empty := &keys.Group{Description: "empty", Entries: map[string]keys.Entry{}}
	got := plain(Render(empty, []string{" "}, modeA, 40, 10, testStyles()))
	if strings.TrimSpace(got) == "" {
		t.Error("an empty group should still render its chrome, not nothing")
	}
}

func TestRenderDegenerateSizes(t *testing.T) {
	for _, size := range []struct{ w, h int }{{0, 0}, {1, 1}, {5, 2}, {-3, -3}} {
		out := Render(fixture(), []string{" "}, modeA, size.w, size.h, testStyles())
		for _, ln := range strings.Split(out, "\n") {
			if size.w > 0 && lipgloss.Width(ln) > size.w {
				t.Errorf("w=%d: line too wide: %q", size.w, plain(ln))
			}
		}
	}
}
