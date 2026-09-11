package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/tonobo/calterm/internal/model"
	"github.com/tonobo/calterm/internal/store"
	"github.com/tonobo/calterm/internal/waybar"
)

// waybarCommand prints one JSON object and exits. It never touches the
// network: a slow or unreachable server must never stall the status bar.
//
// It exits 0 whenever it produced valid JSON, including the none and stale
// cases, so that a broken cache degrades to a visible warning in the bar
// rather than an empty module slot.
func waybarCommand(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("waybar", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "", "configuration file")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	out, err := waybarOutput(*configPath, time.Now(), time.Local)
	if err != nil {
		// Even a configuration failure is reported through the bar.
		out = waybar.Output{
			Text:    "calendar error",
			Alt:     waybar.ClassStale,
			Class:   waybar.ClassStale,
			Tooltip: fmt.Sprintf("calterm: %v", err),
		}
	}
	enc := json.NewEncoder(stdout)
	if err := enc.Encode(out); err != nil {
		fmt.Fprintf(stderr, "calterm: %v\n", err)
		return 1
	}
	return 0
}

func waybarOutput(configPath string, now time.Time, loc *time.Location) (waybar.Output, error) {
	cfg, s, _, err := loadEnvironment(configPath)
	if err != nil {
		return waybar.Output{}, err
	}
	idx, err := s.LoadOccurrences()
	if err != nil {
		return waybar.Output{}, err
	}
	meta, err := s.LoadMeta()
	if err != nil {
		meta = &store.Meta{}
	}
	return waybar.Render(idx, meta, cfg.Waybar, model.HiddenSet(cfg.Calendars.Hidden), now, loc), nil
}
