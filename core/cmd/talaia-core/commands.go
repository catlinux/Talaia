package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/catlinux/Talaia/core/internal/ca"
	"github.com/catlinux/Talaia/core/internal/config"
	"github.com/catlinux/Talaia/core/internal/store"
)

// defaultTokenTTL is how long a one-time enrollment token stays valid.
const defaultTokenTTL = 15 * time.Minute

// commandTimeout bounds the database work of a command.
const commandTimeout = 30 * time.Second

// printUsage writes the command help.
func printUsage(w io.Writer) {
	fmt.Fprint(w, `Usage: talaia-core [command] [options]

Commands:
  serve                        start the core server (default)
  token create --name <name>   create a one-time enrollment token
  server revoke <id>           revoke an enrolled server
`)
}

func runToken(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "token: missing subcommand")
		printUsage(stderr)
		return 2
	}

	switch args[0] {
	case "create":
		if err := tokenCreateCommand(args[1:], stdout, stderr); err != nil {
			fmt.Fprintf(stderr, "token create: %v\n", err)
			return 1
		}
		return 0
	default:
		fmt.Fprintf(stderr, "token: unknown subcommand %q\n\n", args[0])
		printUsage(stderr)
		return 2
	}
}

func runServer(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "server: missing subcommand")
		printUsage(stderr)
		return 2
	}

	switch args[0] {
	case "revoke":
		if err := serverRevokeCommand(args[1:], stdout, stderr); err != nil {
			fmt.Fprintf(stderr, "server revoke: %v\n", err)
			return 1
		}
		return 0
	default:
		fmt.Fprintf(stderr, "server: unknown subcommand %q\n\n", args[0])
		printUsage(stderr)
		return 2
	}
}

// tokenCreator is the part of the store the token command needs.
type tokenCreator interface {
	CreateEnrollmentToken(ctx context.Context, serverName string, ttl time.Duration) (string, error)
}

// tokenCreateCommand creates an enrollment token. It loads the configuration
// and the CA, opens the database and runs the migrations.
func tokenCreateCommand(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("token create", flag.ContinueOnError)
	flags.SetOutput(stderr)
	name := flags.String("name", "", "name of the server the token is for (required)")
	ttl := flags.Duration("ttl", defaultTokenTTL, "how long the token stays valid")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*name) == "" {
		return errors.New("--name is required")
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// public_hostnames is not required here yet: this command only needs the CA
	// fingerprint, and the TLS certificate that needs the hostnames arrives in
	// task E2. See config.ValidateCertificateHosts.
	authority, err := ca.LoadOrCreate(caDirectory(cfg))
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	st, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	if err := st.Migrate(ctx); err != nil {
		return err
	}

	return tokenCreate(ctx, stdout, st, *name, *ttl, authority.Fingerprint())
}

// tokenCreate stores the token and prints the block the installation needs. The
// token is shown once and never stored in plaintext.
func tokenCreate(ctx context.Context, out io.Writer, creator tokenCreator, name string, ttl time.Duration, fingerprint string) error {
	token, err := creator.CreateEnrollmentToken(ctx, name, ttl)
	if err != nil {
		return err
	}

	fmt.Fprintf(out, `Enrollment token for %s (valid for %s)

token: %s
ca-fingerprint: %s

This token is shown only once: store it now.
`, name, ttl, token, fingerprint)

	return nil
}

// serverRevoker is the part of the store the revoke command needs.
type serverRevoker interface {
	RevokeServer(ctx context.Context, id string) error
}

// serverRevokeCommand revokes an enrolled server. It does not need the CA.
func serverRevokeCommand(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("server revoke", flag.ContinueOnError)
	flags.SetOutput(stderr)
	if err := flags.Parse(args); err != nil {
		return err
	}

	rest := flags.Args()
	if len(rest) != 1 {
		return errors.New("usage: talaia-core server revoke <id>")
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	st, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	if err := st.Migrate(ctx); err != nil {
		return err
	}

	return serverRevoke(ctx, stdout, st, rest[0])
}

// serverRevoke revokes the server and prints the confirmation.
func serverRevoke(ctx context.Context, out io.Writer, revoker serverRevoker, id string) error {
	if err := revoker.RevokeServer(ctx, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("no server with id %s", id)
		}
		return err
	}

	fmt.Fprintf(out, "revoked %s\n", id)
	return nil
}

// caDirectory is where the CA files live inside the data directory.
func caDirectory(cfg config.Config) string {
	return filepath.Join(cfg.DataDir, "ca")
}
