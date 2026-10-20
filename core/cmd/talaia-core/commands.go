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

	"github.com/jackc/pgx/v5"

	"github.com/catlinux/Talaia/core/internal/ca"
	"github.com/catlinux/Talaia/core/internal/config"
	"github.com/catlinux/Talaia/core/internal/install"
	"github.com/catlinux/Talaia/core/internal/store"
)

// defaultTokenTTL is how long a one-time enrollment token stays valid.
const defaultTokenTTL = 15 * time.Minute

// commandTimeout bounds the database work of a command.
const commandTimeout = 30 * time.Second

// cliActor is the audit actor of the console commands.
const cliActor = "cli"

// printUsage writes the command help.
func printUsage(w io.Writer) {
	fmt.Fprint(w, `Usage: talaia-core [command] [options]

Commands:
  serve                               start the core server (default)
  token create --name <name>          create a one-time enrollment token
  server revoke <id>                  revoke an enrolled server
  user create --username <name>       create the administrator (asks for the password)
  user reset-2fa                      clear the second factor of the administrator
  session revoke-all                  revoke every panel session
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

func runUser(args []string, in io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "user: missing subcommand")
		printUsage(stderr)
		return 2
	}

	switch args[0] {
	case "create":
		if err := userCreateCommand(args[1:], in, stdout, stderr); err != nil {
			fmt.Fprintf(stderr, "user create: %v\n", err)
			return 1
		}
		return 0
	case "reset-2fa":
		if err := userReset2FACommand(args[1:], stdout, stderr); err != nil {
			fmt.Fprintf(stderr, "user reset-2fa: %v\n", err)
			return 1
		}
		return 0
	default:
		fmt.Fprintf(stderr, "user: unknown subcommand %q\n\n", args[0])
		printUsage(stderr)
		return 2
	}
}

func runSession(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "session: missing subcommand")
		printUsage(stderr)
		return 2
	}

	switch args[0] {
	case "revoke-all":
		if err := sessionRevokeAllCommand(args[1:], stdout, stderr); err != nil {
			fmt.Fprintf(stderr, "session revoke-all: %v\n", err)
			return 1
		}
		return 0
	default:
		fmt.Fprintf(stderr, "session: unknown subcommand %q\n\n", args[0])
		printUsage(stderr)
		return 2
	}
}

// openCore loads the configuration, opens the database and applies the
// migrations.
func openCore(ctx context.Context) (config.Config, *store.Store, error) {
	cfg, err := config.Load()
	if err != nil {
		return config.Config{}, nil, err
	}

	st, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return config.Config{}, nil, err
	}
	if err := st.Migrate(ctx); err != nil {
		_ = st.Close()
		return config.Config{}, nil, err
	}
	return cfg, st, nil
}

// singleUserID returns the id of the only administrator. The store has no list
// method and this phase allows a single user, so it is one query against the
// database the command is already using.
func singleUserID(ctx context.Context, databaseURL string) (string, error) {
	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		return "", fmt.Errorf("connect to the database: %w", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	var id string
	err = conn.QueryRow(ctx, `SELECT id FROM app_user LIMIT 1`).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", errors.New("there is no user yet: run talaia-core user create first")
	}
	if err != nil {
		return "", fmt.Errorf("look up the user: %w", err)
	}
	return id, nil
}

// sessionRevokeAllCommand revokes every panel session of the only user.
func sessionRevokeAllCommand(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("session revoke-all", flag.ContinueOnError)
	flags.SetOutput(stderr)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("usage: talaia-core session revoke-all")
	}

	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	cfg, st, err := openCore(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	userID, err := singleUserID(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	revoked, err := st.RevokeUserSessions(ctx, userID, "")
	if err != nil {
		return err
	}

	if err := st.AppendAudit(ctx, store.AuditEvent{
		Actor:   cliActor,
		Action:  "sessions_revoked",
		Result:  "success",
		Details: map[string]any{"count": revoked},
	}); err != nil {
		return err
	}

	fmt.Fprintf(stdout, "revoked %d session(s)\n", revoked)
	return nil
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

	return tokenCreate(ctx, stdout, st, *name, *ttl, authority.Fingerprint(), cfg.PublicURL())
}

// tokenCreate stores the token and prints the block the installation needs. The
// token is shown once and never stored in plaintext.
func tokenCreate(ctx context.Context, out io.Writer, creator tokenCreator, name string, ttl time.Duration, fingerprint, publicURL string) error {
	token, err := creator.CreateEnrollmentToken(ctx, name, ttl)
	if err != nil {
		return err
	}

	fmt.Fprintf(out, `Enrollment token for %s (valid for %s)

token: %s
ca-fingerprint: %s

This token is shown only once: store it now.
`, name, ttl, token, fingerprint)

	if command := install.Command(publicURL, token, fingerprint); command != "" {
		fmt.Fprintf(out, "\nTo install the agent, run this on the server (as root, or with sudo):\n\n%s\n", command)
	}

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
