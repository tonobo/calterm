package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/tonobo/calterm/internal/config"
	"github.com/tonobo/calterm/internal/store"
	"github.com/tonobo/calterm/internal/tui"
)

func runTUICommand(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("calterm", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "", "configuration file")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	cfg, s, resolvedConfigPath, err := loadEnvironment(*configPath)
	if err != nil {
		fmt.Fprintf(stderr, "calterm: %v\n", err)
		return 1
	}

	wireTUISync()

	now := time.Now()
	if err := tui.Run(cfg, s, now, resolvedConfigPath); err != nil {
		fmt.Fprintf(stderr, "calterm: %v\n", err)
		return 1
	}
	return 0
}

func wireTUISync() {
	// Wire the sync implementation in here so internal/tui does not depend on
	// the whole command layer.
	// The clock is read HERE, when the sync actually runs, and the `now` the
	// TUI passes in is deliberately ignored: internal/tui captures its clock
	// once at launch and never advances it, so a TUI left open for three hours
	// would otherwise record a three-hour-old timestamp on a sync that just
	// succeeded -- understating freshness exactly as a failed sync used to
	// overstate it. cmd/ is where the clock is allowed to enter.
	tui.SyncFunc = func(ctx context.Context, cfg *config.Config, s *store.Store, _ time.Time) error {
		return runSync(cfg, s, time.Now(), io.Discard)
	}
	tui.SyncCalendarFunc = func(ctx context.Context, cfg *config.Config, s *store.Store, accountID, calendarID string, _ time.Time) error {
		return runCalendarSync(ctx, cfg, s, accountID, calendarID, time.Now())
	}
}
