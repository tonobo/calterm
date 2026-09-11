package theme

import (
	"testing"

	"github.com/BurntSushi/toml"
)

// TestColorUnmarshalBareString pins the shorthand shape: a bare hex string
// applies to both light and dark mode.
func TestColorUnmarshalBareString(t *testing.T) {
	var doc struct {
		Fg Color `toml:"fg"`
	}
	if _, err := toml.Decode(`fg = "#ffffff"`, &doc); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if doc.Fg.Light != "#ffffff" || doc.Fg.Dark != "#ffffff" {
		t.Fatalf("bare string: got Light=%q Dark=%q, want both #ffffff", doc.Fg.Light, doc.Fg.Dark)
	}
}

// TestColorUnmarshalTable pins the table shape: separate light/dark values.
func TestColorUnmarshalTable(t *testing.T) {
	var doc struct {
		Fg Color `toml:"fg"`
	}
	src := `
[fg]
light = "#111111"
dark = "#eeeeee"
`
	if _, err := toml.Decode(src, &doc); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if doc.Fg.Light != "#111111" {
		t.Errorf("Light = %q, want #111111", doc.Fg.Light)
	}
	if doc.Fg.Dark != "#eeeeee" {
		t.Errorf("Dark = %q, want #eeeeee", doc.Fg.Dark)
	}
}

// TestColorUnmarshalInvalid rejects a shape that is neither string nor table.
func TestColorUnmarshalInvalid(t *testing.T) {
	var doc struct {
		Fg Color `toml:"fg"`
	}
	if _, err := toml.Decode(`fg = 12345`, &doc); err == nil {
		t.Fatalf("Decode of a bare int: want error, got nil")
	}
}

func TestColorResolve(t *testing.T) {
	c := Color{Light: "#111111", Dark: "#eeeeee"}
	if got := c.Resolve(true); got != "#eeeeee" {
		t.Errorf("Resolve(true) = %q, want #eeeeee", got)
	}
	if got := c.Resolve(false); got != "#111111" {
		t.Errorf("Resolve(false) = %q, want #111111", got)
	}
}
