//go:build windows

package main

import (
	"os"
	"syscall"
	"unsafe"
)

const enableEchoInput = 0x0004

var (
	kernel32       = syscall.NewLazyDLL("kernel32.dll")
	setConsoleMode = kernel32.NewProc("SetConsoleMode")
	getConsoleMode = kernel32.NewProc("GetConsoleMode")
)

// isTerminal reports whether f is a Windows console.
func isTerminal(f *os.File) bool {
	var mode uint32
	result, _, _ := getConsoleMode.Call(uintptr(syscall.Handle(f.Fd())), uintptr(unsafe.Pointer(&mode)))
	return result != 0
}

// disableEcho turns console echo off and returns the restore function.
func disableEcho(f *os.File) (func(), error) {
	var mode uint32
	handle := uintptr(syscall.Handle(f.Fd()))
	result, _, callErr := getConsoleMode.Call(handle, uintptr(unsafe.Pointer(&mode)))
	if result == 0 {
		return nil, callErr
	}
	old := mode
	result, _, callErr = setConsoleMode.Call(handle, uintptr(mode&^enableEchoInput))
	if result == 0 {
		return nil, callErr
	}
	restore := func() {
		_, _, _ = setConsoleMode.Call(handle, uintptr(old))
	}
	return restore, nil
}
