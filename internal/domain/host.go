package domain

import "regexp"

// hostRe is a DNS name: lower-case labels, at least two of them. A wildcard, a
// port, a path and an address are not hosts a repository may request.
var hostRe = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z][a-z0-9-]{0,61}[a-z0-9]$`)

// ValidHost reports whether name can be requested as an egress host (design
// D38): a lower-case DNS name, never an IP address, a wildcard or a name with a
// port or a path.
func ValidHost(name string) bool { return len(name) <= 253 && hostRe.MatchString(name) }
