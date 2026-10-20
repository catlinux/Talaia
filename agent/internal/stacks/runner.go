package stacks

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
)

// maxCommandOutput bounds how much output of a docker compose run is kept.
const maxCommandOutput = 64 << 10

// Runner runs docker with fixed arguments in a directory and returns what it
// printed. The default runner never goes through a shell.
type Runner interface {
	Run(ctx context.Context, dir string, args ...string) (string, error)
}

// ExecRunner runs the docker binary.
type ExecRunner struct{}

// Run executes docker with args in dir. The environment is reduced to what
// docker compose needs, so nothing from the environment of the agent leaks into
// a stack.
func (ExecRunner) Run(ctx context.Context, dir string, args ...string) (string, error) {
	binary, err := exec.LookPath("docker")
	if err != nil {
		return "", errors.New("the docker command was not found")
	}

	var output limitedBuffer
	command := exec.CommandContext(ctx, binary, args...)
	command.Dir = dir
	command.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + homeDir()}
	command.Stdout = &output
	command.Stderr = &output

	err = command.Run()
	text := output.String()
	if err != nil {
		if ctx.Err() != nil {
			return text, fmt.Errorf("docker compose did not finish in time: %w", ctx.Err())
		}
		return text, fmt.Errorf("docker compose failed: %w", err)
	}
	return text, nil
}

func homeDir() string {
	if home := os.Getenv("HOME"); home != "" {
		return home
	}
	return "/root"
}

// limitedBuffer keeps the last maxCommandOutput bytes written to it.
type limitedBuffer struct {
	buffer bytes.Buffer
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	written := len(p)
	b.buffer.Write(p)
	if extra := b.buffer.Len() - maxCommandOutput; extra > 0 {
		b.buffer.Next(extra)
	}
	return written, nil
}

func (b *limitedBuffer) String() string { return b.buffer.String() }
