// SPDX-License-Identifier: EUPL-1.2

package resources

import (
	"errors"
	"strings"
	"testing"
)

func TestCPUEdgeCases(t *testing.T) {
	huge := strings.Repeat("9", 100000)
	cases := []struct {
		in    string
		want  int64
		class Class
	}{
		{"1", 1000, ""},
		{strings.Repeat("0", 100000) + "1", 1000, ""},
		{"0." + strings.Repeat("0", 100000), 0, ClassExcessPrecision},
		{"0.000", 0, ClassNotPositive},
		{strings.Repeat("0", 100000), 0, ClassNotPositive},
		{huge, 0, ClassOverflow},
		{huge + ".5", 0, ClassOverflow},
		{"9223372036854775.807", 9223372036854775807, ""},
		{"9223372036854775.808", 0, ClassOverflow},
		{"9223372036854776", 0, ClassOverflow},
		{"18446744073709551616", 0, ClassOverflow}, // 2^64
		{"18446744073709551.616", 0, ClassOverflow},
		{"\t2", 0, ClassSyntax},
		{"2\n", 0, ClassSyntax},
		{" 2", 0, ClassSyntax},
		{" 2", 0, ClassSyntax},
		{"٣", 0, ClassSyntax}, // ARABIC-INDIC DIGIT THREE
		{"2.５", 0, ClassSyntax},
		{"\x00", 0, ClassSyntax},
		{"2\x00", 0, ClassSyntax},
		{"\xff", 0, ClassSyntax},
		{"1.5.5", 0, ClassSyntax},
		{"1e", 0, ClassExponent},
		{"-", 0, ClassSign},
		{"INF", 0, ClassNonfinite},
		{"-Infinity", 0, ClassSign},
	}
	for _, c := range cases {
		got, err := ParseCPU(c.in)
		name := c.in
		if len(name) > 30 {
			name = name[:30] + "..."
		}
		if c.class == "" {
			if err != nil || int64(got) != c.want {
				t.Errorf("ParseCPU(%q) = %d, %v; want %d", name, got, err, c.want)
			}
		} else if err == nil || classOf(t, err) != c.class {
			t.Errorf("ParseCPU(%q) = %d, %v; want class %s", name, got, err, c.class)
		}
	}
}

func TestMemoryEdgeCases(t *testing.T) {
	huge := strings.Repeat("9", 100000)
	cases := []struct {
		in    string
		want  int64
		class Class
	}{
		{"1GiB", 1 << 30, ""},
		{"1024MiB", 1 << 30, ""},
		{strings.Repeat("0", 100000) + "1MiB", 1 << 20, ""},
		{strings.Repeat("0", 100000) + "GiB", 0, ClassNotPositive},
		{huge + "MiB", 0, ClassOverflow},
		{huge + "GiB", 0, ClassOverflow},
		{"8589934592GiB", 0, ClassOverflow}, // 2^33 GiB = 2^63
		{"8796093022208MiB", 0, ClassOverflow},
		{"17592186044416MiB", 0, ClassOverflow}, // 2^64 bytes
		{"1 GiB", 0, ClassSyntax},
		{"\t1GiB", 0, ClassSyntax},
		{"1GiB\n", 0, ClassSyntax},
		{"١GiB", 0, ClassSyntax},
		{"1GıB", 0, ClassSyntax}, // non-ASCII letter
		{"1GiB\x00", 0, ClassSyntax},
		{"1MIB", 0, ClassBadUnit},
		{"1mib", 0, ClassBadUnit},
		{"1MiBGiB", 0, ClassBadUnit},
		{"1.0GiB", 0, ClassSyntax},
		{"5E", 0, ClassExponent},
		{"-", 0, ClassSign},
	}
	for _, c := range cases {
		got, err := ParseMemory(c.in)
		name := c.in
		if len(name) > 30 {
			name = name[:30] + "..."
		}
		if c.class == "" {
			if err != nil || int64(got) != c.want {
				t.Errorf("ParseMemory(%q) = %d, %v; want %d", name, got, err, c.want)
			}
		} else if err == nil || classOf(t, err) != c.class {
			t.Errorf("ParseMemory(%q) = %d, %v; want class %s", name, got, err, c.class)
		}
	}
}

func TestParseErrorIdentifiesField(t *testing.T) {
	_, err := ParseCPU("x")
	var pe *ParseError
	if !errors.As(err, &pe) || pe.Field != FieldCPU || pe.Code() != "invalid_cpu" {
		t.Fatalf("cpu error = %#v", err)
	}
	_, err = ParseMemory("x")
	if !errors.As(err, &pe) || pe.Field != FieldMemory || pe.Code() != "invalid_memory" {
		t.Fatalf("memory error = %#v", err)
	}
	if pe.Error() == "" {
		t.Fatal("empty error text")
	}
}

func TestOneGiBEqualsThousandTwentyFourMiB(t *testing.T) {
	a, errA := ParseMemory("1GiB")
	b, errB := ParseMemory("1024MiB")
	if errA != nil || errB != nil || a != b || a.String() != b.String() {
		t.Fatalf("%v %v %v %v", a, b, errA, errB)
	}
}

func TestCanonicalFormatting(t *testing.T) {
	for want, v := range map[string]CPU{"1": 1000, "2.5": 2500, "0.001": 1, "0.5": 500, "16": 16000, "1.25": 1250} {
		if got := v.String(); got != want {
			t.Errorf("CPU(%d).String() = %q, want %q", v, got, want)
		}
	}
	for want, v := range map[string]Memory{"1MiB": Memory(MiB), "1GiB": Memory(GiB), "1536MiB": Memory(GiB + 512*MiB), "4GiB": Memory(4 * GiB)} {
		if got := v.String(); got != want {
			t.Errorf("Memory(%d).String() = %q, want %q", v, got, want)
		}
	}
}

func str(s string) *string { return &s }

func TestResolvePartialHostProfile(t *testing.T) {
	cpu, mem := CPU(1000), Memory(GiB)
	// Host profile with only CPU: memory stays unresolved unless flagged.
	got, err := Resolve(Explicit{}, Profile{CPU: &cpu}, ModeInteractive)
	if err != nil || got.Status != StatusPrompt || len(got.Unresolved) != 1 || got.Unresolved[0] != "--memory" {
		t.Fatalf("got %+v, %v", got, err)
	}
	if got.CPU.Origin != OriginHostDefault || got.CPU.Value != 1000 || got.Memory.Origin != OriginNone {
		t.Fatalf("origins %+v", got)
	}
	// Host profile with only memory, explicit CPU.
	got, err = Resolve(Explicit{CPU: str("0.5")}, Profile{Memory: &mem}, ModeUnattended)
	if err != nil || got.Status != StatusComplete || got.CPU.Origin != OriginFlag || got.Memory.Origin != OriginHostDefault {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestResolveErrors(t *testing.T) {
	cpu, mem := CPU(1000), Memory(GiB)
	full := Profile{CPU: &cpu, Memory: &mem}
	// An invalid explicit value never falls back to the host default.
	if _, err := Resolve(Explicit{Memory: str("4")}, full, ModeUnattended); err == nil || classOf(t, err) != ClassMissingUnit {
		t.Fatalf("err = %v", err)
	}
	// An explicit empty string is an error, not omission.
	if _, err := Resolve(Explicit{CPU: str("")}, full, ModeUnattended); err == nil || classOf(t, err) != ClassEmpty {
		t.Fatalf("err = %v", err)
	}
	// Both invalid: the CPU error is reported first.
	_, err := Resolve(Explicit{CPU: str("-1"), Memory: str("x")}, Profile{}, ModeUnattended)
	var pe *ParseError
	if !errors.As(err, &pe) || pe.Field != FieldCPU {
		t.Fatalf("err = %v", err)
	}
	// Unknown mode and unusable host profile values are refused.
	if _, err := Resolve(Explicit{}, Profile{}, Mode("batch")); !errors.Is(err, ErrInvalidMode) {
		t.Fatalf("err = %v", err)
	}
	zero, neg := CPU(0), Memory(-1)
	if _, err := Resolve(Explicit{}, Profile{CPU: &zero}, ModeUnattended); !errors.Is(err, ErrInvalidProfile) {
		t.Fatalf("err = %v", err)
	}
	if _, err := Resolve(Explicit{}, Profile{Memory: &neg}, ModeUnattended); !errors.Is(err, ErrInvalidProfile) {
		t.Fatalf("err = %v", err)
	}
	// A profile that is not needed is still validated.
	if _, err := Resolve(Explicit{CPU: str("1"), Memory: str("1GiB")}, Profile{CPU: &zero}, ModeUnattended); !errors.Is(err, ErrInvalidProfile) {
		t.Fatalf("err = %v", err)
	}
}

func TestNoUnlimitedOnOmission(t *testing.T) {
	for _, mode := range []Mode{ModeUnattended, ModeInteractive} {
		got, err := Resolve(Explicit{}, Profile{}, mode)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status == StatusComplete || len(got.Unresolved) != 2 {
			t.Fatalf("%s: omission resolved: %+v", mode, got)
		}
		if got.CPU.Origin != OriginNone || got.Memory.Origin != OriginNone || got.CPU.Value != 0 || got.Memory.Value != 0 {
			t.Fatalf("%s: values leaked: %+v", mode, got)
		}
	}
}

func TestEffectiveRefusesUnresolved(t *testing.T) {
	cpu := CPU(1000)
	for _, ex := range []Explicit{{}, {CPU: str("1")}, {Memory: str("1GiB")}} {
		for _, mode := range []Mode{ModeUnattended, ModeInteractive} {
			got, err := Resolve(ex, Profile{}, mode)
			if err != nil {
				t.Fatal(err)
			}
			if c, m, err := got.Effective(); !errors.Is(err, ErrIncomplete) || c != 0 || m != 0 {
				t.Fatalf("Effective = %d %d %v", c, m, err)
			}
		}
	}
	got, _ := Resolve(Explicit{Memory: str("1GiB")}, Profile{CPU: &cpu}, ModeUnattended)
	if c, m, err := got.Effective(); err != nil || c != 1000 || m != Memory(GiB) {
		t.Fatalf("Effective = %d %d %v", c, m, err)
	}
	if _, _, err := (Resolution{}).Effective(); !errors.Is(err, ErrIncomplete) {
		t.Fatalf("zero Resolution: %v", err)
	}
}

func TestNonPositiveStringsDoNotParse(t *testing.T) {
	for _, v := range []CPU{0, -1, -999, -1000, -1001, CPU(-1 << 63)} {
		s := v.String()
		if _, err := ParseCPU(s); err == nil {
			t.Errorf("CPU(%d).String() = %q parses", v, s)
		}
	}
	for _, v := range []Memory{0, -1, Memory(-MiB), Memory(-1 << 63)} {
		s := v.String()
		if _, err := ParseMemory(s); err == nil {
			t.Errorf("Memory(%d).String() = %q parses", v, s)
		}
	}
}

func TestProfileMemoryMustBeWholeMiB(t *testing.T) {
	bad := Memory(MiB + 1)
	if _, err := Resolve(Explicit{}, Profile{Memory: &bad}, ModeUnattended); !errors.Is(err, ErrInvalidProfile) {
		t.Fatalf("err = %v", err)
	}
}

func TestErrorPrecedence(t *testing.T) {
	zero := CPU(0)
	_, err := Resolve(Explicit{CPU: str("-1"), Memory: str("x")}, Profile{CPU: &zero}, ModeUnattended)
	if !errors.Is(err, ErrInvalidProfile) {
		t.Fatalf("profile must win: %v", err)
	}
	_, err = Resolve(Explicit{CPU: str("-1"), Memory: str("x")}, Profile{}, Mode("x"))
	if !errors.Is(err, ErrInvalidMode) {
		t.Fatalf("mode must win: %v", err)
	}
}

func TestOverflowAboveTwoToTheSixtyFour(t *testing.T) {
	// 2^65+10 wraps to 10 if the 64-bit accumulator overflow is not detected.
	for _, in := range []string{"36893488147419103242", "36893488147419103242.5"} {
		if _, err := ParseCPU(in); err == nil || classOf(t, err) != ClassOverflow {
			t.Errorf("ParseCPU(%q) = %v", in, err)
		}
	}
	for _, in := range []string{"36893488147419103242MiB", "18446744073709551626GiB"} {
		if _, err := ParseMemory(in); err == nil || classOf(t, err) != ClassOverflow {
			t.Errorf("ParseMemory(%q) = %v", in, err)
		}
	}
}

func TestEffectiveChecksStatusItself(t *testing.T) {
	r := Resolution{CPU: Value[CPU]{1000, OriginFlag}, Memory: Value[Memory]{Memory(GiB), OriginFlag}}
	if _, _, err := r.Effective(); !errors.Is(err, ErrIncomplete) {
		t.Fatalf("err = %v", err)
	}
	r.Status = StatusComplete
	if _, _, err := r.Effective(); err != nil {
		t.Fatal(err)
	}
}
