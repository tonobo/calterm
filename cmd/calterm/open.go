package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/tonobo/calterm/internal/eventopen"
	"github.com/tonobo/calterm/internal/model"
	"github.com/tonobo/calterm/internal/tui"
)

var (
	syncForOpen   = runSync
	runFocusedTUI = tui.RunFocused
)

func openCommand(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("open", flag.ContinueOnError)
	fs.SetOutput(stderr)
	doSync := fs.Bool("sync", false, "sync before opening the event")
	configPath := fs.String("config", "", "configuration file")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: calterm open [--sync] [-config <path>] <event.ics>")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return 2
	}

	cfg, s, resolvedConfigPath, err := loadEnvironment(*configPath)
	if err != nil {
		fmt.Fprintf(stderr, "calterm: %v\n", err)
		return 1
	}

	var syncErr error
	if *doSync {
		// The normal all-account sync is intentionally attempted before the
		// file is resolved. A failed network refresh does not make the local
		// occurrence cache unusable, so matching continues below.
		syncErr = syncForOpen(cfg, s, time.Now(), io.Discard)
	}

	f, err := os.Open(fs.Arg(0))
	if err != nil {
		fmt.Fprintf(stderr, "calterm: opening %s: %v\n", fs.Arg(0), err)
		return 1
	}
	ref, parseErr := model.ParseEventReference(f, time.Local)
	closeErr := f.Close()
	if parseErr != nil {
		fmt.Fprintf(stderr, "calterm: %v\n", parseErr)
		return 1
	}
	if closeErr != nil {
		fmt.Fprintf(stderr, "calterm: closing %s: %v\n", fs.Arg(0), closeErr)
		return 1
	}

	idx, err := s.LoadOccurrences()
	if err != nil {
		fmt.Fprintf(stderr, "calterm: loading occurrence cache: %v\n", err)
		return 1
	}
	occurrence, found := eventopen.Match(ref, idx, cfg)
	if !found {
		occurrence = ref.Occurrence
	}

	var notices []string
	if !found {
		notices = append(notices, "not present in synced calendar")
	}
	if syncErr != nil {
		notices = append(notices, "sync failed; using existing cache: "+syncErr.Error())
	}

	wireTUISync()
	opts := tui.FocusOptions{
		Occurrence:    occurrence,
		ReadOnly:      !found,
		Notice:        strings.Join(notices, " · "),
		NoticeIsError: syncErr != nil,
	}
	if err := runFocusedTUI(cfg, s, time.Now(), resolvedConfigPath, opts); err != nil {
		fmt.Fprintf(stderr, "calterm: %v\n", err)
		return 1
	}
	return 0
}
