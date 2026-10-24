// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
)

func candidateHomeScope(destination string) CandidateHomeScope {
	return CandidateHomeScope{
		OperationID: "fedcba9876543210fedcba9876543210",
		DaemonID:    "daemon-id:opaque", Endpoint: "unix:///run/docker.sock",
		PrimaryID: strings.Repeat("a", 64), PrimaryHome: "anchorage-home-owner",
		Destination: destination,
	}
}

func candidateHomeObservation(plan CandidateHomePlan) CandidateHomeObservation {
	return CandidateHomeObservation{
		DaemonID: plan.DaemonID, Endpoint: plan.Endpoint, PrimaryID: plan.PrimaryID,
		CandidateID: strings.Repeat("b", 64), CandidateName: "whr-anchorage-staging",
		Labels: map[string]string{"anchorage-staging": "1"},
		Mounts: []CandidateHomeMount{plan.Mount},
	}
}

func TestCandidateHomeNewOpaqueName(t *testing.T) {
	for _, destination := range []string{"/home/captain", "/home/vscode"} {
		t.Run(destination, func(t *testing.T) {
			scope := candidateHomeScope(destination)
			intent, err := newCandidateHomeIntent(scope, bytes.NewReader([]byte{
				0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15,
			}))
			if err != nil {
				t.Fatal(err)
			}
			plan := intent.Plan()
			if plan.Nonce != "000102030405060708090a0b0c0d0e0f" || plan.OperationID != scope.OperationID || plan.Nonce == plan.OperationID {
				t.Fatalf("home nonce must encode 128 independent random bits: %+v", plan)
			}
			if plan.Mount != (CandidateHomeMount{Type: "volume", Name: "whr-stage-home-" + plan.Nonce, Destination: destination}) {
				t.Fatalf("unexpected isolated mount: %+v", plan.Mount)
			}
			if plan.DaemonID != scope.DaemonID || plan.Endpoint != scope.Endpoint || plan.PrimaryID != scope.PrimaryID || plan.PrimaryHome != scope.PrimaryHome {
				t.Fatalf("scope binding lost: %+v", plan)
			}
			observed := candidateHomeObservation(plan)
			// Official-CLI candidates retain ordinary workspace identity labels.
			// The established role parser still recognizes the explicit staging role.
			observed.Labels["devcontainer.local_folder"] = "/workspace/project"
			observed.Labels["devcontainer.config_file"] = "/workspace/project/.devcontainer/devcontainer.json"
			observed.Labels["workharbor.workspace"] = "fixture-workspace"
			binding, err := intent.Bind(observed)
			if err != nil {
				t.Fatal(err)
			}
			identity := binding.Identity()
			if identity.Plan != plan || identity.CandidateID != observed.CandidateID || identity.CandidateName != observed.CandidateName {
				t.Fatalf("actual identities not bound: %+v", identity)
			}
			if err := binding.Check(observed); err != nil {
				t.Fatal(err)
			}
			// All projections are values; edits cannot change the private binding.
			plan.Mount.Name = "foreign"
			identity.Plan.Endpoint = "unix:///run/other.sock"
			observed.Labels["anchorage-staging"] = "changed"
			observed.Mounts[0].Name = "changed"
			if got := binding.Identity(); got.Plan != intent.Plan() || got.CandidateID != strings.Repeat("b", 64) {
				t.Fatalf("mutable alias changed identity: %+v", got)
			}
		})
	}
	// The public constructor uses fresh cryptographic entropy, with no source or
	// user input in its request or volume name. This is no exclusivity proof.
	seen := map[string]bool{}
	for range 32 {
		intent, err := NewCandidateHomeIntent(candidateHomeScope("/home/captain"))
		if err != nil {
			t.Fatal(err)
		}
		name := intent.Plan().Mount.Name
		if !regexp.MustCompile(`^whr-stage-home-[0-9a-f]{32}$`).MatchString(name) || seen[name] || strings.Contains(name, "owner") || strings.Contains(name, "daemon") {
			t.Fatalf("unexpected home name %q", name)
		}
		seen[name] = true
	}
}

func TestCandidateHomeScopeAdmission(t *testing.T) {
	cases := map[string]func(*CandidateHomeScope){
		"operation missing":    func(s *CandidateHomeScope) { s.OperationID = "" },
		"operation uppercase":  func(s *CandidateHomeScope) { s.OperationID = strings.Repeat("A", 32) },
		"operation short":      func(s *CandidateHomeScope) { s.OperationID = "abc" },
		"daemon missing":       func(s *CandidateHomeScope) { s.DaemonID = "" },
		"daemon controls":      func(s *CandidateHomeScope) { s.DaemonID = "daemon\nforged" },
		"daemon invalid UTF8":  func(s *CandidateHomeScope) { s.DaemonID = "\xff" },
		"daemon too long":      func(s *CandidateHomeScope) { s.DaemonID = strings.Repeat("d", 1025) },
		"remote endpoint":      func(s *CandidateHomeScope) { s.Endpoint = "tcp://host:2375" },
		"relative endpoint":    func(s *CandidateHomeScope) { s.Endpoint = "unix://relative" },
		"unclean endpoint":     func(s *CandidateHomeScope) { s.Endpoint = "unix:///run/../run/docker.sock" },
		"endpoint query":       func(s *CandidateHomeScope) { s.Endpoint = "unix:///run/docker.sock?context=other" },
		"endpoint encoded":     func(s *CandidateHomeScope) { s.Endpoint = "unix:///run/%64ocker.sock" },
		"endpoint controls":    func(s *CandidateHomeScope) { s.Endpoint = "unix:///run/docker\n.sock" },
		"primary short":        func(s *CandidateHomeScope) { s.PrimaryID = "abc" },
		"primary uppercase":    func(s *CandidateHomeScope) { s.PrimaryID = strings.Repeat("A", 64) },
		"primary home missing": func(s *CandidateHomeScope) { s.PrimaryHome = "" },
		"primary home path":    func(s *CandidateHomeScope) { s.PrimaryHome = "/home/owner" },
		"other destination":    func(s *CandidateHomeScope) { s.Destination = "/root" },
		"unclean destination":  func(s *CandidateHomeScope) { s.Destination = "/home/../home/captain" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			scope := candidateHomeScope("/home/captain")
			mutate(&scope)
			// Invalid requests must be refused before entropy is consumed.
			intent, err := newCandidateHomeIntent(scope, nil)
			if exitcode.FromError(err) != 68 || intent != (CandidateHomeIntent{}) {
				t.Fatalf("intent=%+v error=%v; want zero value and conflict", intent, err)
			}
		})
	}
}

func TestCandidateHomeEntropyFailureAndNoRetry(t *testing.T) {
	for name, reader := range map[string]io.Reader{"empty": bytes.NewReader(nil), "short": bytes.NewReader(make([]byte, 15)), "failure": candidateEntropyFailure{}} {
		t.Run(name, func(t *testing.T) {
			intent, err := newCandidateHomeIntent(candidateHomeScope("/home/vscode"), reader)
			if exitcode.FromError(err) != 74 || intent != (CandidateHomeIntent{}) || strings.Contains(err.Error(), "private entropy detail") {
				t.Fatalf("intent=%+v error=%v; want sanitized internal failure", intent, err)
			}
		})
	}
	for _, collision := range []string{"operation", "primary home"} {
		t.Run(collision, func(t *testing.T) {
			scope := candidateHomeScope("/home/captain")
			if collision == "operation" {
				scope.OperationID = strings.Repeat("0", 32)
			} else {
				scope.PrimaryHome = "whr-stage-home-" + strings.Repeat("0", 32)
			}
			reader := bytes.NewReader(make([]byte, 32))
			intent, err := newCandidateHomeIntent(scope, reader)
			if exitcode.FromError(err) != 68 || intent != (CandidateHomeIntent{}) || reader.Len() != 16 {
				t.Fatalf("intent=%+v error=%v unread=%d; must refuse without another draw", intent, err, reader.Len())
			}
		})
	}
}

type candidateEntropyFailure struct{}

func (candidateEntropyFailure) Read([]byte) (int, error) {
	return 0, errors.New("private entropy detail")
}

func TestCandidateHomeBindingRefusesDrift(t *testing.T) {
	intent, err := NewCandidateHomeIntent(candidateHomeScope("/home/vscode"))
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*CandidateHomeObservation){
		"daemon":               func(o *CandidateHomeObservation) { o.DaemonID = "another-daemon" },
		"endpoint":             func(o *CandidateHomeObservation) { o.Endpoint = "unix:///run/other.sock" },
		"primary":              func(o *CandidateHomeObservation) { o.PrimaryID = strings.Repeat("c", 64) },
		"candidate short":      func(o *CandidateHomeObservation) { o.CandidateID = "abc" },
		"candidate uppercase":  func(o *CandidateHomeObservation) { o.CandidateID = strings.Repeat("B", 64) },
		"candidate is primary": func(o *CandidateHomeObservation) { o.CandidateID = o.PrimaryID },
		"candidate name":       func(o *CandidateHomeObservation) { o.CandidateName = "other-staging" },
		"label missing":        func(o *CandidateHomeObservation) { o.Labels = nil },
		"label foreign":        func(o *CandidateHomeObservation) { o.Labels["anchorage-staging"] = "0" },
		"role ambiguous":       func(o *CandidateHomeObservation) { o.Labels["anchorage-temp"] = "1" },
		"mount missing":        func(o *CandidateHomeObservation) { o.Mounts = nil },
		"mount bind":           func(o *CandidateHomeObservation) { o.Mounts[0].Type = "bind" },
		"mount foreign":        func(o *CandidateHomeObservation) { o.Mounts[0].Name = "foreign" },
		"mount protected":      func(o *CandidateHomeObservation) { o.Mounts[0].Name = "anchorage-home-owner" },
		"mount destination":    func(o *CandidateHomeObservation) { o.Mounts[0].Destination = "/root" },
		"mount readonly":       func(o *CandidateHomeObservation) { o.Mounts[0].ReadOnly = true },
		"mount duplicate":      func(o *CandidateHomeObservation) { o.Mounts = append(o.Mounts, o.Mounts[0]) },
		"mount extra home": func(o *CandidateHomeObservation) {
			o.Mounts = append(o.Mounts, CandidateHomeMount{Type: "volume", Name: "other", Destination: "/home/captain"})
		},
		"mount shared at tool path": func(o *CandidateHomeObservation) {
			m := o.Mounts[0]
			m.Destination = "/opt/bdock/tool"
			o.Mounts = append(o.Mounts, m)
		},
	}
	// The original primary home must not be exposed anywhere in the candidate,
	// even if supplied metadata assigns it a different mount type or destination.
	for _, mountType := range []string{"volume", "bind", "tmpfs", ""} {
		for _, destination := range []string{"/mnt/primary", "/opt/bdock/primary"} {
			cases["mount primary elsewhere "+mountType+destination] = func(o *CandidateHomeObservation) {
				o.Mounts = append(o.Mounts, CandidateHomeMount{
					Type: mountType, Name: intent.Plan().PrimaryHome, Destination: destination,
				})
			}
		}
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			observed := candidateHomeObservation(intent.Plan())
			bound, err := intent.Bind(observed)
			if err != nil {
				t.Fatal(err)
			}
			mutate(&observed)
			refused, err := intent.Bind(observed)
			if exitcode.FromError(err) != 68 || refused != (CandidateHomeBinding{}) {
				t.Fatalf("binding=%+v error=%v", refused, err)
			}
			if err := bound.Check(observed); exitcode.FromError(err) != 68 {
				t.Fatalf("drift accepted: %v", err)
			}
		})
	}
	observed := candidateHomeObservation(intent.Plan())
	bound, err := intent.Bind(observed)
	if err != nil {
		t.Fatal(err)
	}
	observed.CandidateID = strings.Repeat("c", 64)
	if err := bound.Check(observed); exitcode.FromError(err) != 68 {
		t.Fatalf("replacement candidate accepted: %v", err)
	}
	if _, err := (CandidateHomeIntent{}).Bind(observed); exitcode.FromError(err) != 68 {
		t.Fatalf("zero intent accepted: %v", err)
	}
	if err := (CandidateHomeBinding{}).Check(observed); exitcode.FromError(err) != 68 {
		t.Fatalf("zero binding accepted: %v", err)
	}
}

func TestCandidateHomePlanningExecutesNoEffects(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "effect")
	for _, executable := range []string{"docker", "devcontainer", "git", "bash"} {
		if err := os.WriteFile(filepath.Join(dir, executable), []byte("#!/bin/sh\n/usr/bin/touch '"+marker+"'\nexit 1\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	intent, err := NewCandidateHomeIntent(candidateHomeScope("/home/captain"))
	if err != nil {
		t.Fatal(err)
	}
	binding, err := intent.Bind(candidateHomeObservation(intent.Plan()))
	if err != nil {
		t.Fatal(err)
	}
	if err := binding.Check(candidateHomeObservation(intent.Plan())); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{marker, filepath.Join(dir, "state")} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("unexpected effect at %s: %v", path, err)
		}
	}
}
