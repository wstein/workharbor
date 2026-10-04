//go:build !unix

package cli

import (
	"errors"
	"regexp"
)

var termName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,39}$`)

func execReplace(string, []string, []string) error {
	return errors.New("this platform cannot replace the whr process")
}
