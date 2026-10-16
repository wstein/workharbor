// SPDX-License-Identifier: EUPL-1.2
package identity

import "os"

// These unexported native defaults have no environment, wire or configuration
// override. Package tests wrap individual syscalls to prove durability failures;
// they restore the table and never run those tests in parallel.
type durableFile interface {
	Name() string
	Chmod(os.FileMode) error
	Write([]byte) (int, error)
	Sync() error
	Close() error
}
type durableFiles struct {
	createTemp func(string, string) (durableFile, error)
	openDir    func(string) (durableFile, error)
	rename     func(string, string) error
	link       func(string, string) error
	remove     func(string) error
}

var identityFiles = durableFiles{
	createTemp: func(dir, pattern string) (durableFile, error) { return os.CreateTemp(dir, pattern) },
	openDir:    func(dir string) (durableFile, error) { return os.Open(dir) },
	rename:     os.Rename,
	link:       os.Link,
	remove:     os.Remove,
}
