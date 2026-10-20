package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/catlinux/Talaia/core/internal/auth"
	"github.com/catlinux/Talaia/core/internal/config"
	"github.com/catlinux/Talaia/core/internal/store"
	"github.com/catlinux/Talaia/core/internal/terminal"
)

// terminalLimit is how many sessions `terminal list` prints.
const terminalLimit = 100

func runTerminal(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "terminal: missing subcommand")
		printUsage(stderr)
		return 2
	}

	switch args[0] {
	case "list":
		if err := terminalListCommand(args[1:], stdout, stderr); err != nil {
			fmt.Fprintf(stderr, "terminal list: %v\n", err)
			return 1
		}
		return 0
	case "export":
		if err := terminalExportCommand(args[1:], stdout, stderr); err != nil {
			fmt.Fprintf(stderr, "terminal export: %v\n", err)
			return 1
		}
		return 0
	default:
		fmt.Fprintf(stderr, "terminal: unknown subcommand %q\n\n", args[0])
		printUsage(stderr)
		return 2
	}
}

// sessionLister is the part of the store the list command needs.
type sessionLister interface {
	ListTerminalSessions(ctx context.Context, limit int) ([]store.TerminalSession, error)
}

func terminalListCommand(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("terminal list", flag.ContinueOnError)
	flags.SetOutput(stderr)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("usage: talaia-core terminal list")
	}

	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	_, st, err := openCore(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	return terminalList(ctx, stdout, st)
}

// terminalList prints one line per session, the newest first: id, state, user,
// container, when it opened, how long it lasted, how much was recorded.
func terminalList(ctx context.Context, out io.Writer, lister sessionLister) error {
	sessions, err := lister.ListTerminalSessions(ctx, terminalLimit)
	if err != nil {
		return err
	}
	if len(sessions) == 0 {
		fmt.Fprintln(out, "no terminal sessions")
		return nil
	}

	for _, session := range sessions {
		state, lasted := "open", "-"
		if !session.Open() {
			state = session.CloseReason
			lasted = session.ClosedAt.Sub(session.OpenedAt).Round(time.Second).String()
		}
		note := ""
		if session.Privileged {
			note = "  root-equivalent"
		}
		fmt.Fprintf(out, "%s  %s  %s  %s  %s  %s  %d bytes%s\n", session.ID, session.OpenedAt.UTC().Format("2006-01-02T15:04:05Z"),
			state, session.CreatedBy, session.Container, lasted, session.RecordedBytes, note)
	}
	return nil
}

// terminalExportCommand writes the recording of a session to the standard output
// as an asciicast v2 file, for `asciinema play`.
func terminalExportCommand(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("terminal export", flag.ContinueOnError)
	flags.SetOutput(stderr)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("usage: talaia-core terminal export <session id>")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cfg, st, err := openCore(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	box, err := openSecretBox(cfg)
	if err != nil {
		return err
	}
	return terminal.Export(ctx, st, box, flags.Arg(0), stdout)
}

// openSecretBox loads the secrets key of the core. Unlike serve it never creates
// one: a new key could not open anything that was recorded before.
func openSecretBox(cfg config.Config) (*auth.SecretBox, error) {
	path := filepath.Join(cfg.DataDir, "secrets.key")
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("the secrets key %s cannot be read: %w", path, err)
	}
	key, err := auth.LoadOrCreateKey(path)
	if err != nil {
		return nil, err
	}
	return auth.NewSecretBox(key)
}
