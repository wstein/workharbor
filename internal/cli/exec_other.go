//go:build !unix

package cli

import (
	"context"
	"errors"
	"regexp"
)

var termName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,39}$`)

func runShellChild(context.Context, string, []string, []string) (int, error) {
	return 0, errors.New("this platform cannot open the sign-in shell child")
}
