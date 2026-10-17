// SPDX-License-Identifier: EUPL-1.2

//go:build !linux && !darwin

package ptytest

import (
	"errors"
	"os"
	"os/exec"
)

var errUnsupported = errors.New("ptytest: unsupported platform")

func platformSupported() bool { return false }

func setWinsize(*os.File, int, int) error { return errUnsupported }

// WindowSize is unsupported on this platform.
func WindowSize(uintptr) (int, int, error) { return 0, 0, errUnsupported }

// MakeRaw is unsupported on this platform.
func MakeRaw(uintptr) (func(), error) { return nil, errUnsupported }

func openPTY() (*os.File, *os.File, error) { return nil, nil, errUnsupported }

func startProcess(*exec.Cmd, *os.File) error { return errUnsupported }

func killGroup(int) {}

// ProcessGone is unsupported on this platform and reports false.
func ProcessGone(int) bool { return false }
