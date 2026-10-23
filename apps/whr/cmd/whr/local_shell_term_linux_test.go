// SPDX-License-Identifier: EUPL-1.2
//go:build linux

package main

import "golang.org/x/sys/unix"

const localTTYGet = unix.TCGETS
const localTTYSet = unix.TCSETS
