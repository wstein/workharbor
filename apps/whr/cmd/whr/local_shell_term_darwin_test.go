// SPDX-License-Identifier: EUPL-1.2
//go:build darwin

package main

import "golang.org/x/sys/unix"

const localTTYGet = unix.TIOCGETA
const localTTYSet = unix.TIOCSETA
