//go:build !linux && !darwin

// SPDX-License-Identifier: EUPL-1.2
package docker

import "os"

func ownedDirectory(string) bool { return false }

func ownedSocket(string) bool { return false }

func ownedRegular(os.FileInfo) bool { return false }

func nativeDockerSocket(string) bool { return false }
