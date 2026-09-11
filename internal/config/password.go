package config

import (
	"fmt"
	"os/exec"
	"strings"
)

// SplitCommand splits a command string into argv, honouring single and double
// quotes. It deliberately does not invoke a shell: password_cmd runs directly.
func SplitCommand(s string) ([]string, error) {
	var (
		args   []string
		cur    strings.Builder
		quote  rune
		inWord bool
	)
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote = r
			inWord = true
		case r == ' ' || r == '\t':
			if inWord {
				args = append(args, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteRune(r)
			inWord = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated %c quote in command %q", quote, s)
	}
	if inWord {
		args = append(args, cur.String())
	}
	if len(args) == 0 {
		return nil, fmt.Errorf("empty command")
	}
	return args, nil
}

// Password runs the account's password_cmd and returns its first output line.
func (a Account) Password() (string, error) {
	return commandValue("account", a.Name, a.PasswordCmd)
}

// Password runs the feed's password_cmd and returns its first output line.
func (w WebCal) Password() (string, error) {
	return commandValue("webcal", w.Name, w.PasswordCmd)
}

func commandValue(kind, name, command string) (string, error) {
	if command == "" {
		return "", fmt.Errorf("%s %q: password_cmd is not set", kind, name)
	}
	argv, err := SplitCommand(command)
	if err != nil {
		return "", fmt.Errorf("%s %q: %w", kind, name, err)
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s %q: password_cmd %q failed: %w: %s",
			kind, name, command, err, strings.TrimSpace(stderr.String()))
	}
	line, _, _ := strings.Cut(string(out), "\n")
	line = strings.TrimRight(line, " \t\r")
	if line == "" {
		return "", fmt.Errorf("%s %q: password_cmd %q produced no output", kind, name, command)
	}
	return line, nil
}
