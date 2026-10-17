// SPDX-License-Identifier: EUPL-1.2

package resources

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const vectorsRel = "contracts/cli/resources-vectors.json"

type parseCase struct {
	Input  string `json:"input"`
	Expect *int64 `json:"expect"`
	Error  string `json:"error"`
}

type resolvedWant struct {
	Value  int64  `json:"value"`
	Origin string `json:"origin"`
}

type resolutionWant struct {
	CPU        *resolvedWant `json:"cpu"`
	Memory     *resolvedWant `json:"memory"`
	NeedsHuman []string      `json:"needs_human"`
	Prompt     []string      `json:"prompt"`
	Error      string        `json:"error"`
	Detail     string        `json:"detail"`
}

type resolutionCase struct {
	ID    string `json:"id"`
	Flags struct {
		CPU    *string `json:"cpu"`
		Memory *string `json:"memory"`
	} `json:"flags"`
	Profile *struct {
		CPU    *int64 `json:"cpu_thousandths"`
		Memory *int64 `json:"memory_bytes"`
	} `json:"profile"`
	Mode   string         `json:"mode"`
	Expect resolutionWant `json:"expect"`
}

type vectorFile struct {
	Limits struct {
		MaxCanonical int64 `json:"max_canonical"`
		MiB          int64 `json:"mib"`
		GiB          int64 `json:"gib"`
	} `json:"limits"`
	CPUCases        []parseCase      `json:"cpu_cases"`
	MemoryCases     []parseCase      `json:"memory_cases"`
	ResolutionCases []resolutionCase `json:"resolution_cases"`
	EquivalenceSets []struct {
		Expect int64    `json:"expect"`
		CPU    []string `json:"cpu"`
		Memory []string `json:"memory"`
	} `json:"equivalence_sets"`
}

// loadVectors finds the repository root by walking up from the test's working
// directory (the package directory under go test), so it does not depend on
// the depth of this package or on -trimpath.
func loadVectors(t *testing.T) vectorFile {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		path := filepath.Join(dir, filepath.FromSlash(vectorsRel))
		if raw, err := os.ReadFile(path); err == nil {
			var v vectorFile
			if err := json.Unmarshal(raw, &v); err != nil {
				t.Fatalf("decode %s: %v", path, err)
			}
			return v
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("%s not found above the test directory", vectorsRel)
		}
		dir = parent
	}
}

func classOf(t *testing.T, err error) Class {
	t.Helper()
	var pe *ParseError
	if !errors.As(err, &pe) {
		t.Fatalf("error %v is not a *ParseError", err)
	}
	return pe.Class
}

func TestVectorLimits(t *testing.T) {
	v := loadVectors(t)
	if v.Limits.MaxCanonical != MaxCanonical || v.Limits.MiB != MiB || v.Limits.GiB != GiB {
		t.Fatalf("limits differ from vectors: %+v", v.Limits)
	}
	if len(v.CPUCases) == 0 || len(v.MemoryCases) == 0 || len(v.ResolutionCases) == 0 {
		t.Fatal("vector file has an empty section")
	}
}

func TestCPUVectors(t *testing.T) {
	for _, c := range loadVectors(t).CPUCases {
		got, err := ParseCPU(c.Input)
		if c.Error != "" {
			if err == nil {
				t.Errorf("ParseCPU(%q) = %d, want error %s", c.Input, got, c.Error)
			} else if cl := classOf(t, err); string(cl) != c.Error {
				t.Errorf("ParseCPU(%q) class %s, want %s", c.Input, cl, c.Error)
			}
			continue
		}
		if err != nil || int64(got) != *c.Expect {
			t.Errorf("ParseCPU(%q) = %d, %v; want %d", c.Input, got, err, *c.Expect)
		}
	}
}

func TestMemoryVectors(t *testing.T) {
	for _, c := range loadVectors(t).MemoryCases {
		got, err := ParseMemory(c.Input)
		if c.Error != "" {
			if err == nil {
				t.Errorf("ParseMemory(%q) = %d, want error %s", c.Input, got, c.Error)
			} else if cl := classOf(t, err); string(cl) != c.Error {
				t.Errorf("ParseMemory(%q) class %s, want %s", c.Input, cl, c.Error)
			}
			continue
		}
		if err != nil || int64(got) != *c.Expect {
			t.Errorf("ParseMemory(%q) = %d, %v; want %d", c.Input, got, err, *c.Expect)
		}
	}
}

func TestResolutionVectors(t *testing.T) {
	for _, c := range loadVectors(t).ResolutionCases {
		var host Profile
		if c.Profile != nil {
			if c.Profile.CPU != nil {
				v := CPU(*c.Profile.CPU)
				host.CPU = &v
			}
			if c.Profile.Memory != nil {
				v := Memory(*c.Profile.Memory)
				host.Memory = &v
			}
		}
		got, err := Resolve(Explicit{CPU: c.Flags.CPU, Memory: c.Flags.Memory}, host, Mode(c.Mode))
		want := c.Expect
		switch {
		case want.Error != "":
			var pe *ParseError
			if !errors.As(err, &pe) {
				t.Errorf("%s: err = %v, want ParseError", c.ID, err)
			} else if pe.Code() != want.Error || string(pe.Class) != want.Detail {
				t.Errorf("%s: got %s/%s, want %s/%s", c.ID, pe.Code(), pe.Class, want.Error, want.Detail)
			}
		case err != nil:
			t.Errorf("%s: unexpected error %v", c.ID, err)
		case want.NeedsHuman != nil:
			if got.Status != StatusNeedsHuman || !reflect.DeepEqual(got.Unresolved, want.NeedsHuman) {
				t.Errorf("%s: got %v %v, want needs_human %v", c.ID, got.Status, got.Unresolved, want.NeedsHuman)
			}
		case want.Prompt != nil:
			if got.Status != StatusPrompt || !reflect.DeepEqual(got.Unresolved, want.Prompt) {
				t.Errorf("%s: got %v %v, want prompt %v", c.ID, got.Status, got.Unresolved, want.Prompt)
			}
		default:
			if got.Status != StatusComplete || len(got.Unresolved) != 0 {
				t.Errorf("%s: status %v unresolved %v, want complete", c.ID, got.Status, got.Unresolved)
			}
			if int64(got.CPU.Value) != want.CPU.Value || string(got.CPU.Origin) != want.CPU.Origin {
				t.Errorf("%s: cpu %+v, want %+v", c.ID, got.CPU, *want.CPU)
			}
			if int64(got.Memory.Value) != want.Memory.Value || string(got.Memory.Origin) != want.Memory.Origin {
				t.Errorf("%s: memory %+v, want %+v", c.ID, got.Memory, *want.Memory)
			}
		}
	}
}

func TestEquivalentSpellingsShareIdentity(t *testing.T) {
	v := loadVectors(t)
	if len(v.EquivalenceSets) == 0 {
		t.Fatal("no equivalence sets")
	}
	for _, set := range v.EquivalenceSets {
		for _, s := range set.CPU {
			got, err := ParseCPU(s)
			if err != nil || int64(got) != set.Expect {
				t.Errorf("ParseCPU(%q) = %d, %v; want %d", s, got, err, set.Expect)
			}
		}
		var ids []string
		for _, s := range set.Memory {
			got, err := ParseMemory(s)
			if err != nil || int64(got) != set.Expect {
				t.Errorf("ParseMemory(%q) = %d, %v; want %d", s, got, err, set.Expect)
			}
			ids = append(ids, got.String())
		}
		for _, id := range ids {
			if id != ids[0] {
				t.Errorf("identities differ within set: %v", ids)
			}
		}
	}
}
