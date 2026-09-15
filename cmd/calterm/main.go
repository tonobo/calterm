// Command calterm is a cache-first CalDAV calendar client for the terminal.
package main

import (
	"fmt"
	"io"
	"os"
)

// Version is overridden at build time with
// -ldflags "-X main.Version=$(git describe --tags)".
var Version = "dev"

const usage = `calterm — a CalDAV and WebCal calendar for the terminal

Usage:
  calterm [flags]           launch the interactive TUI (default)
  calterm open [--sync] [-config <path>] <event.ics>
                            open an iCalendar event in the TUI
  calterm sync [flags]      fetch calendar changes into the local cache
  calterm notify [flags]    send due desktop notifications from the cache
  calterm waybar [flags]    print a Waybar JSON status line and exit
  calterm version           print the version
  calterm help              print this message

Flags:
  -config <path>   configuration file
                   (default $XDG_CONFIG_HOME/calterm/config.toml)
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is the testable entry point: it never calls os.Exit and never writes to
// the real stdout or stderr directly.
func run(args []string, stdout, stderr io.Writer) int {
	cmd := ""
	if len(args) > 0 && !isFlag(args[0]) {
		cmd = args[0]
		args = args[1:]
	}

	switch cmd {
	case "", "tui":
		return runTUICommand(args, stdout, stderr)
	case "sync":
		return syncCommand(args, stdout, stderr)
	case "notify":
		return notifyCommand(args, stdout, stderr)
	case "open":
		return openCommand(args, stdout, stderr)
	case "waybar":
		return waybarCommand(args, stdout, stderr)
	case "version":
		fmt.Fprintf(stdout, "calterm %s\n", Version)
		return 0
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "calterm: unknown subcommand %q\n\n%s", cmd, usage)
		return 2
	}
}

func isFlag(s string) bool { return len(s) > 0 && s[0] == '-' }
