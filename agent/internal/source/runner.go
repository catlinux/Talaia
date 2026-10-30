package source

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// maxGitOutput bounds how much output of one git command is kept.
const maxGitOutput = 64 << 10

// Runner runs git with fixed arguments in a directory and returns what it
// printed. The default runner never goes through a shell.
type Runner interface {
	Run(ctx context.Context, dir string, env []string, args ...string) (string, error)
}

// ExecRunner runs the git binary.
type ExecRunner struct{}

// Run executes git with args in dir and env. It uses no shell.
func (ExecRunner) Run(ctx context.Context, dir string, env []string, args ...string) (string, error) {
	binary, err := exec.LookPath("git")
	if err != nil {
		return "", errors.New("the git command was not found")
	}

	var output limitedBuffer
	command := exec.CommandContext(ctx, binary, args...)
	command.Dir = dir
	command.Env = env
	command.Stdout = &output
	command.Stderr = &output

	err = command.Run()
	text := output.String()
	if err != nil {
		if ctx.Err() != nil {
			return text, fmt.Errorf("git did not finish in time: %w", ctx.Err())
		}
		if detail := firstLine(text); detail != "" {
			return text, fmt.Errorf("git failed: %w: %s", err, detail)
		}
		return text, fmt.Errorf("git failed: %w", err)
	}
	return text, nil
}

func firstLine(text string) string {
	if i := strings.IndexAny(text, "\r\n"); i >= 0 {
		text = text[:i]
	}
	return strings.TrimSpace(text)
}

// limitedBuffer keeps the last maxGitOutput bytes written to it.
type limitedBuffer struct {
	buffer bytes.Buffer
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	written := len(p)
	b.buffer.Write(p)
	if extra := b.buffer.Len() - maxGitOutput; extra > 0 {
		b.buffer.Next(extra)
	}
	return written, nil
}

func (b *limitedBuffer) String() string { return b.buffer.String() }
