package config

import (
	"reflect"
	"testing"
)

func TestSplitCommand(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		want  []string
		error bool
	}{
		{name: "simple", in: "pass show caldav/nc", want: []string{"pass", "show", "caldav/nc"}},
		{name: "extra spaces", in: "  pass   show  x ", want: []string{"pass", "show", "x"}},
		{name: "double quotes", in: `op read "op://Private/CalDAV/password"`, want: []string{"op", "read", "op://Private/CalDAV/password"}},
		{name: "single quotes", in: `sh -c 'echo hi'`, want: []string{"sh", "-c", "echo hi"}},
		{name: "quoted empty arg", in: `cmd ""`, want: []string{"cmd", ""}},
		{name: "unterminated quote", in: `cmd "oops`, error: true},
		{name: "empty", in: "   ", error: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SplitCommand(tt.in)
			if tt.error {
				if err == nil {
					t.Fatalf("SplitCommand(%q) = %v, want error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("SplitCommand(%q) returned error: %v", tt.in, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("SplitCommand(%q) = %#v, want %#v", tt.in, got, tt.want)
			}
		})
	}
}
