package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/catlinux/Talaia/core/internal/auth"
	"github.com/catlinux/Talaia/core/internal/store"
)

// userCreateCommand creates the administrator. It reads the password from the
// terminal twice, or once from the standard input with --password-stdin.
func userCreateCommand(args []string, in io.Reader, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("user create", flag.ContinueOnError)
	flags.SetOutput(stderr)
	username := flags.String("username", "", "administrator username (required)")
	passwordStdin := flags.Bool("password-stdin", false, "read the password from standard input instead of prompting")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("usage: talaia-core user create --username <name> [--password-stdin]")
	}
	if strings.TrimSpace(*username) == "" {
		return errors.New("--username is required")
	}
	name := strings.TrimSpace(*username)

	password, err := readNewPassword(*passwordStdin, in, stderr)
	if err != nil {
		return err
	}
	if err := auth.ValidatePasswordPolicy(password); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	_, st, err := openCore(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	id, err := st.CreateUser(ctx, name, auth.HashPassword(password))
	if errors.Is(err, store.ErrUserExists) {
		return errors.New("a user already exists")
	}
	if err != nil {
		return err
	}

	if err := st.AppendAudit(ctx, store.AuditEvent{
		Actor:   cliActor,
		Action:  "user_created",
		Result:  "success",
		Details: map[string]any{"username": name, "user_id": id},
	}); err != nil {
		return err
	}

	fmt.Fprintf(stdout, "created the user %s\n", name)
	return nil
}

// userReset2FACommand clears the second factor of a user and closes all its
// sessions, for when the authenticator is lost.
func userReset2FACommand(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("user reset-2fa", flag.ContinueOnError)
	flags.SetOutput(stderr)
	username := flags.String("username", "", "user to reset (needed when there are several users)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("usage: talaia-core user reset-2fa [--username <name>]")
	}

	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	_, st, err := openCore(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	userIDs, err := resolveUserIDs(ctx, st, *username, false)
	if err != nil {
		return err
	}
	userID := userIDs[0]
	if err := st.ResetTOTP(ctx, userID); err != nil {
		return err
	}
	revoked, err := st.RevokeUserSessions(ctx, userID, "")
	if err != nil {
		return err
	}

	if err := st.AppendAudit(ctx, store.AuditEvent{
		Actor:   cliActor,
		Action:  "mfa_reset",
		Result:  "success",
		Details: map[string]any{"sessions_revoked": revoked},
	}); err != nil {
		return err
	}

	fmt.Fprintf(stdout, "two-factor authentication reset; %d session(s) revoked\n", revoked)
	return nil
}

// userUnlockCommand clears the failed-attempt counter and the lock of a user,
// for when the sign-in was blocked by too many wrong passwords or codes. The
// per-address limit lives in memory and is cleared by restarting the core.
func userUnlockCommand(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("user unlock", flag.ContinueOnError)
	flags.SetOutput(stderr)
	username := flags.String("username", "", "user to unlock (needed when there are several users)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("usage: talaia-core user unlock [--username <name>]")
	}

	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	_, st, err := openCore(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	userIDs, err := resolveUserIDs(ctx, st, *username, false)
	if err != nil {
		return err
	}
	if err := st.ResetFailures(ctx, userIDs[0]); err != nil {
		return err
	}
	if err := st.AppendAudit(ctx, store.AuditEvent{
		Actor:   cliActor,
		Action:  "user_unlocked",
		Result:  "success",
		Details: map[string]any{"user_id": userIDs[0]},
	}); err != nil {
		return err
	}

	fmt.Fprintln(stdout, "the sign-in lock was cleared")
	return nil
}

// readNewPassword asks for the new password. From a terminal it asks twice, so a
// typo cannot lock the administrator out; from a pipe it reads one line.
func readNewPassword(fromStdin bool, in io.Reader, out io.Writer) (string, error) {
	if fromStdin {
		return readLine(bufio.NewReader(in))
	}

	prompt := newPrompter(in, out)
	first, err := prompt("Password: ")
	if err != nil {
		return "", err
	}
	second, err := prompt("Repeat password: ")
	if err != nil {
		return "", err
	}
	if first != second {
		return "", errors.New("the passwords do not match")
	}
	return first, nil
}

// readLine reads one line and drops its line ending.
func readLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// newPrompter returns a function that asks for a password. On a terminal it
// turns the echo off; from any other reader it reads a line, which is also how
// the tests drive it.
func newPrompter(in io.Reader, out io.Writer) func(string) (string, error) {
	file, isFile := in.(*os.File)
	if isFile && term.IsTerminal(int(file.Fd())) {
		return func(prompt string) (string, error) {
			fmt.Fprint(out, prompt)
			password, err := term.ReadPassword(int(file.Fd()))
			fmt.Fprintln(out)
			if err != nil {
				return "", err
			}
			return strings.TrimRight(string(password), "\r\n"), nil
		}
	}

	reader := bufio.NewReader(in)
	return func(prompt string) (string, error) {
		fmt.Fprint(out, prompt)
		return readLine(reader)
	}
}
