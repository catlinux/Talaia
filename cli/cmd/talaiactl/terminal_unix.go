//go:build !windows

package main

import (
	"os"
	"os/exec"
)

// isTerminal reports whether f is a character device. The stty call is the
// final authority for echo handling on Unix.
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// disableEcho turns terminal echo off and returns the restore function.
func disableEcho(f *os.File) (func(), error) {
	off := exec.Command("stty", "-echo")
	off.Stdin = f
	if err := off.Run(); err != nil {
		return nil, err
	}
	restore := func() {
		on := exec.Command("stty", "echo")
		on.Stdin = f
		_ = on.Run()
	}
	return restore, nil
}
