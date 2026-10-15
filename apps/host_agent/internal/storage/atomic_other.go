// SPDX-License-Identifier: EUPL-1.2
//go:build !linux && !darwin

package storage

import (
	"os"
)

func platformSupported() bool                    { return false }
func privatePath(string, os.FileMode, bool) bool { return false }
func openSource(string) (*os.File, error)        { return nil, os.ErrPermission }
func openLock(string) (*os.File, error)          { return nil, os.ErrPermission }
func tryLock(*os.File) error                     { return os.ErrPermission }
func unlock(*os.File) error                      { return os.ErrPermission }
func lockBusy(error) bool                        { return false }
func renameNoReplace(string, string) error       { return os.ErrPermission }
func canChown(uint32, uint32) bool               { return false }
