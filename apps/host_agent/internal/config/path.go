// SPDX-License-Identifier: EUPL-1.2
// Package config contains pure configuration predicates. Lexical path validation
// alone establishes no containment, symlink safety, capability or owner trust.
package config

import (
	"strings"
	"unicode/utf8"
)

// Error contains only a fixed diagnostic, never the supplied path or options.
type Error struct{ Code string }

func (e *Error) Error() string { return e.Code }
func invalidPath() error       { return &Error{Code: "context_schema"} }

// CanonicalPath validates an exact native string with dot defaulting to false.
// It returns its unchanged spelling; it does not normalize or access a filesystem.
func CanonicalPath(value any) (string, error) { return CanonicalPathWithDot(value, false) }

// CanonicalPathWithDot additionally accepts an exact native bool option. Defined
// string/bool types, byte slices, pointers and other values are context_schema.
// Invalid UTF-8, empty paths, NUL, leading slash, backslash, CR/LF/tab, *?[], empty
// components and components equal to ., .. or .git are context_schema. Only the
// whole-string "." exception is accepted when dot is true. There is no length
// bound, case folding, percent decoding, expansion or filesystem resolution.
func CanonicalPathWithDot(value any, dot any) (string, error) {
	allowDot, ok := dot.(bool)
	if !ok {
		return "", invalidPath()
	}
	path, ok := value.(string)
	if !ok || !utf8.ValidString(path) {
		return "", invalidPath()
	}
	if allowDot && path == "." {
		return path, nil
	}
	if path == "" || strings.HasPrefix(path, "/") || strings.ContainsAny(path, "\\\x00\r\n\t*?[]") {
		return "", invalidPath()
	}
	for _, part := range strings.Split(path, "/") {
		if part == "" || part == "." || part == ".." || part == ".git" {
			return "", invalidPath()
		}
	}
	return path, nil
}
