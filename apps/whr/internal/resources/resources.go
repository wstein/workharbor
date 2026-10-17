// SPDX-License-Identifier: EUPL-1.2

// Package resources parses and resolves the CPU and memory limits of
// "workspace create" (FR-039, FR-040, FR-059). It is pure: no I/O, no
// globals, no clock. CPU is canonicalized to integer thousandths of a CPU
// equivalent and memory to integer bytes, both within 2^63-1, using explicit
// 64-bit arithmetic so results do not depend on the architecture. The
// shared vectors are contracts/cli/resources-vectors.json.
package resources

import (
	"errors"
	"math"
	"math/bits"
	"strconv"
)

// Canonical limits and unit sizes, identical to the vector file's "limits".
const (
	MaxCanonical int64 = math.MaxInt64
	MiB          int64 = 1 << 20
	GiB          int64 = 1 << 30
)

// CPU is a count of thousandths of a CPU equivalent. Memory is a count of bytes.
type (
	CPU    int64
	Memory int64
)

// Field names the flag a value belongs to.
type Field string

const (
	FieldCPU    Field = "cpu"
	FieldMemory Field = "memory"
)

// Class is a rejection class; see classification_order in the vector file.
type Class string

const (
	ClassEmpty           Class = "empty"
	ClassSign            Class = "sign"
	ClassNonfinite       Class = "nonfinite"
	ClassExponent        Class = "exponent"
	ClassSyntax          Class = "syntax"
	ClassMissingUnit     Class = "missing_unit"
	ClassBadUnit         Class = "bad_unit"
	ClassExcessPrecision Class = "excess_precision"
	ClassNotPositive     Class = "not_positive"
	ClassOverflow        Class = "overflow"
)

// ParseError is the rejection of one explicit value.
type ParseError struct {
	Field Field
	Class Class
}

// Code is the stable error code of the field: invalid_cpu or invalid_memory.
func (e *ParseError) Code() string { return "invalid_" + string(e.Field) }

func (e *ParseError) Error() string { return e.Code() + ": " + string(e.Class) }

// ParseCPU accepts int [ "." 1*3DIGIT ] in ASCII base 10, positive, without
// sign, exponent, whitespace or unit, and returns thousandths of a CPU.
func ParseCPU(s string) (CPU, error) {
	fail := func(c Class) (CPU, error) { return 0, &ParseError{FieldCPU, c} }
	p, c := scan(s, FieldCPU)
	if c != "" {
		return fail(c)
	}
	if p.frac > 3 {
		return fail(ClassExcessPrecision)
	}
	whole, fracDigits := p.intDigits, p.fracDigits
	for len(fracDigits) < 3 {
		fracDigits += "0"
	}
	hi, lo, ok := mulDigits(whole, 1000)
	if !ok {
		// A nonzero value this large overflows; an all-zero one is not positive.
		if allZero(whole) && allZero(fracDigits) {
			return fail(ClassNotPositive)
		}
		return fail(ClassOverflow)
	}
	f, _ := strconv.ParseUint(fracDigits, 10, 64) // exactly 3 ASCII digits
	lo, carry := bits.Add64(lo, f, 0)
	hi += carry
	return finish[CPU](FieldCPU, hi, lo)
}

// ParseMemory accepts int ("MiB" / "GiB") in ASCII base 10, positive, with a
// case-sensitive unit, and returns bytes.
func ParseMemory(s string) (Memory, error) {
	p, c := scan(s, FieldMemory)
	if c != "" {
		return 0, &ParseError{FieldMemory, c}
	}
	var unit uint64
	switch p.letters {
	case "":
		return 0, &ParseError{FieldMemory, ClassMissingUnit}
	case "MiB":
		unit = uint64(MiB)
	case "GiB":
		unit = uint64(GiB)
	default:
		return 0, &ParseError{FieldMemory, ClassBadUnit}
	}
	hi, lo, ok := mulDigits(p.intDigits, unit)
	if !ok {
		if allZero(p.intDigits) {
			return 0, &ParseError{FieldMemory, ClassNotPositive}
		}
		return 0, &ParseError{FieldMemory, ClassOverflow}
	}
	return finish[Memory](FieldMemory, hi, lo)
}

type scanned struct {
	intDigits, fracDigits, letters string
	frac                           int
}

// scan classifies s by the first matching rule of classification_order up to
// and including the syntax rule, and splits the relaxed lexical form
// 1*DIGIT [ "." 1*DIGIT ] *LETTER (ASCII only). It never allocates in
// proportion to anything but the input.
func scan(s string, f Field) (scanned, Class) {
	var p scanned
	if s == "" {
		return p, ClassEmpty
	}
	if s[0] == '+' || s[0] == '-' {
		return p, ClassSign
	}
	if hasFoldedPrefix(s, "nan") || hasFoldedPrefix(s, "inf") {
		return p, ClassNonfinite
	}
	i := 0
	for i < len(s) && isDigit(s[i]) {
		i++
	}
	j := i
	for j < len(s) && (isDigit(s[j]) || s[j] == '.') {
		j++
	}
	if j < len(s) && (s[j] == 'e' || s[j] == 'E') && j > 0 {
		return p, ClassExponent
	}
	if i == 0 {
		return p, ClassSyntax
	}
	p.intDigits = s[:i]
	rest := s[i:]
	if rest != "" && rest[0] == '.' {
		k := 1
		for k < len(rest) && isDigit(rest[k]) {
			k++
		}
		if k == 1 {
			return p, ClassSyntax
		}
		p.fracDigits, p.frac = rest[1:k], k-1
		rest = rest[k:]
	}
	for _, c := range []byte(rest) {
		if !isLetter(c) {
			return p, ClassSyntax
		}
	}
	p.letters = rest
	if f == FieldCPU && p.letters != "" || f == FieldMemory && p.frac > 0 {
		return p, ClassSyntax
	}
	return p, ""
}

func finish[T ~int64](f Field, hi, lo uint64) (T, error) {
	switch {
	case hi == 0 && lo == 0:
		return 0, &ParseError{f, ClassNotPositive}
	case hi != 0 || lo > uint64(MaxCanonical):
		return 0, &ParseError{f, ClassOverflow}
	}
	return T(lo), nil
}

// mulDigits returns digits*m as a 128-bit value. ok is false when the digit
// string is too long to be held in 64 bits after dropping leading zeros, in
// which case any nonzero digit string overflows.
func mulDigits(digits string, m uint64) (hi, lo uint64, ok bool) {
	for len(digits) > 0 && digits[0] == '0' {
		digits = digits[1:]
	}
	if len(digits) > 20 {
		return 0, 0, false
	}
	var n uint64
	for i := 0; i < len(digits); i++ {
		h, l := bits.Mul64(n, 10)
		if h != 0 {
			return 0, 0, false
		}
		var c uint64
		n, c = bits.Add64(l, uint64(digits[i]-'0'), 0)
		if c != 0 {
			return 0, 0, false
		}
	}
	hi, lo = bits.Mul64(n, m)
	return hi, lo, true
}

func allZero(d string) bool {
	for i := 0; i < len(d); i++ {
		if d[i] != '0' {
			return false
		}
	}
	return true
}

func isDigit(c byte) bool  { return c >= '0' && c <= '9' }
func isLetter(c byte) bool { return c|0x20 >= 'a' && c|0x20 <= 'z' }

func hasFoldedPrefix(s, lower string) bool {
	if len(s) < len(lower) {
		return false
	}
	for i := 0; i < len(lower); i++ {
		if s[i]|0x20 != lower[i] {
			return false
		}
	}
	return true
}

// String renders the canonical spelling: shortest decimal with up to three
// fractional digits. It re-parses to the same value.
func (c CPU) String() string {
	w, f := int64(c)/1000, int64(c)%1000
	out := strconv.FormatInt(w, 10)
	if f == 0 {
		return out
	}
	frac := strconv.FormatInt(1000+f, 10)[1:]
	for frac[len(frac)-1] == '0' {
		frac = frac[:len(frac)-1]
	}
	return out + "." + frac
}

// String renders the canonical spelling: GiB when divisible, else MiB. A
// value that is not a whole number of MiB (never produced by ParseMemory) is
// rendered in bytes with a "B" suffix, which does not parse.
func (m Memory) String() string {
	switch {
	case m%Memory(GiB) == 0:
		return strconv.FormatInt(int64(m)/GiB, 10) + "GiB"
	case m%Memory(MiB) == 0:
		return strconv.FormatInt(int64(m)/MiB, 10) + "MiB"
	}
	return strconv.FormatInt(int64(m), 10) + "B"
}

// Mode says whether a human can be asked for a missing value.
type Mode string

const (
	ModeUnattended  Mode = "unattended"
	ModeInteractive Mode = "interactive"
)

// Origin says where a resolved value came from.
type Origin string

const (
	OriginNone        Origin = ""
	OriginFlag        Origin = "flag"
	OriginHostDefault Origin = "host_default"
)

// Status is the outcome of Resolve when no value was rejected.
type Status string

const (
	StatusComplete   Status = "complete"
	StatusNeedsHuman Status = "needs_human"
	StatusPrompt     Status = "prompt"
)

// Explicit holds the raw flag values; nil means the flag was not given, so an
// empty string is a rejected value, not an omission.
type Explicit struct{ CPU, Memory *string }

// Profile is the host default profile; a nil field is not defined by the host.
type Profile struct {
	CPU    *CPU
	Memory *Memory
}

// Value is a resolved field; Origin is OriginNone and Value zero when unresolved.
type Value[T ~int64] struct {
	Value  T
	Origin Origin
}

// Resolution is the per-field result. Unresolved lists the flag names
// ("--cpus", "--memory") still missing; omission never means unlimited.
type Resolution struct {
	CPU        Value[CPU]
	Memory     Value[Memory]
	Status     Status
	Unresolved []string
}

var (
	ErrInvalidMode    = errors.New("resources: invalid mode")
	ErrInvalidProfile = errors.New("resources: host profile value is not positive")
)

// Resolve applies, independently per field, the explicit flag, then the host
// profile, else leaves the field unresolved (needs-human when unattended,
// prompt when interactive). An invalid explicit value is an error and never
// falls back to the profile. The CPU error is reported before the memory one.
func Resolve(explicit Explicit, host Profile, mode Mode) (Resolution, error) {
	var r Resolution
	if mode != ModeUnattended && mode != ModeInteractive {
		return r, ErrInvalidMode
	}
	if (host.CPU != nil && *host.CPU <= 0) || (host.Memory != nil && *host.Memory <= 0) {
		return r, ErrInvalidProfile
	}
	switch {
	case explicit.CPU != nil:
		v, err := ParseCPU(*explicit.CPU)
		if err != nil {
			return Resolution{}, err
		}
		r.CPU = Value[CPU]{v, OriginFlag}
	case host.CPU != nil:
		r.CPU = Value[CPU]{*host.CPU, OriginHostDefault}
	default:
		r.Unresolved = append(r.Unresolved, "--cpus")
	}
	switch {
	case explicit.Memory != nil:
		v, err := ParseMemory(*explicit.Memory)
		if err != nil {
			return Resolution{}, err
		}
		r.Memory = Value[Memory]{v, OriginFlag}
	case host.Memory != nil:
		r.Memory = Value[Memory]{*host.Memory, OriginHostDefault}
	default:
		r.Unresolved = append(r.Unresolved, "--memory")
	}
	switch {
	case len(r.Unresolved) == 0:
		r.Status = StatusComplete
	case mode == ModeInteractive:
		r.Status = StatusPrompt
	default:
		r.Status = StatusNeedsHuman
	}
	return r, nil
}
