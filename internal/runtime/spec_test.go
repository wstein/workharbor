package runtime

import (
	"errors"
	"strings"
	"testing"
)

// validSpec is a hardened spec; each test breaks one thing in it.
func validSpec() Spec {
	return Spec{
		Image:        "docker.io/library/debian:stable-slim",
		Owner:        "workharbor-main",
		Labels:       map[string]string{"whr.task": "t-15"},
		CPUs:         2,
		MemoryMB:     2048,
		DiskMB:       20480,
		Network:      Network{Name: "wh-t-15", Internal: true},
		User:         "1000:1000",
		ReadOnlyRoot: true,
		CapDrop:      []string{"ALL"},
		Init:         true,
		Tmpfs:        []string{"/tmp"},
		Mounts: []Mount{
			{Kind: MountBind, Source: "/Users/me/src/app", Target: "/work"},
			{Kind: MountVolume, Source: "wh-cache-t-15", Target: "/cache"},
			{Kind: MountVolume, Source: "wh-tools-v1", Target: "/opt/tools", ReadOnly: true},
		},
	}
}

func TestValidSpecValidates(t *testing.T) {
	if err := validSpec().Validate(); err != nil {
		t.Fatalf("a hardened spec was rejected: %v", err)
	}
}

func TestSpecValidateRejectsWhatIsNotHardened(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Spec)
		want   string // a fragment of the problem
	}{
		{"no image", func(s *Spec) { s.Image = "" }, "image"},
		{"no owner", func(s *Spec) { s.Owner = "" }, "owner"},
		{"upper-case owner", func(s *Spec) { s.Owner = "Main" }, "owner"},
		{"owner label reserved", func(s *Spec) { s.Labels[OwnerLabel] = "someone-else" }, "reserved"},
		{"network label reserved", func(s *Spec) { s.Labels["workharbor.network"] = "other" }, "reserved"},
		{"reserved prefix in upper case", func(s *Spec) { s.Labels["Workharbor.Role"] = "x" }, "reserved"},
		{"label key with '='", func(s *Spec) { s.Labels["a=b"] = "x" }, "label key"},
		{"label key as a flag", func(s *Spec) { s.Labels["-x"] = "x" }, "label key"},
		{"label value with a newline", func(s *Spec) { s.Labels["whr.task"] = "t\nx" }, "control"},
		{"image as a flag", func(s *Spec) { s.Image = "--privileged" }, "image"},
		{"image with a space", func(s *Spec) { s.Image = "fedora latest" }, "image"},
		{"network as a flag", func(s *Spec) { s.Network.Name = "-net" }, "network name"},
		{"network with a slash", func(s *Spec) { s.Network.Name = "a/b" }, "network name"},
		{"bind source with ':'", func(s *Spec) { s.Mounts[0].Source = "/src/a:b" }, "':'"},
		{"target with ':'", func(s *Spec) { s.Mounts[0].Target = "/work:rw" }, "':'"},
		{"tmpfs with ':'", func(s *Spec) { s.Tmpfs = []string{"/tmp:x"} }, "':'"},
		{"empty label key", func(s *Spec) { s.Labels[""] = "x" }, "label key"},
		{"no cpus", func(s *Spec) { s.CPUs = 0 }, "cpus"},
		{"tiny memory", func(s *Spec) { s.MemoryMB = 16 }, "memory"},
		{"no disk quota", func(s *Spec) { s.DiskMB = 0 }, "disk quota"},
		{"internal network without a name", func(s *Spec) { s.Network = Network{Internal: true} }, "internal network"},
		{"no user", func(s *Spec) { s.User = "" }, "user"},
		{"root", func(s *Spec) { s.User = "root" }, "numeric"},
		{"uid 0", func(s *Spec) { s.User = "0" }, "root"},
		{"uid 0 with a gid", func(s *Spec) { s.User = "0:1000" }, "root"},
		{"gid 0", func(s *Spec) { s.User = "1000:0" }, "root"},
		{"a user name", func(s *Spec) { s.User = "agent" }, "numeric"},
		{"too many parts", func(s *Spec) { s.User = "1000:1000:1" }, "numeric"},
		{"negative uid", func(s *Spec) { s.User = "-1" }, "numeric"},
		{"no init", func(s *Spec) { s.Init = false }, "init"},
		{"no cap-drop", func(s *Spec) { s.CapDrop = nil }, "cap-drop"},
		{"cap-drop of one capability", func(s *Spec) { s.CapDrop = []string{"NET_RAW"} }, "cap-drop"},
		{"relative tmpfs", func(s *Spec) { s.Tmpfs = []string{"tmp"} }, "tmpfs"},
		{"relative bind source", func(s *Spec) { s.Mounts[0].Source = "src/app" }, "absolute"},
		{"bad volume name", func(s *Spec) { s.Mounts[1].Source = "Bad Name" }, "volume name"},
		{"unknown mount kind", func(s *Spec) { s.Mounts[0].Kind = "nfs" }, "unknown kind"},
		{"relative target", func(s *Spec) { s.Mounts[0].Target = "work" }, "absolute"},
		{"root target", func(s *Spec) { s.Mounts[0].Target = "/" }, "root filesystem"},
		{"unclean target", func(s *Spec) { s.Mounts[0].Target = "/work/../etc" }, "clean"},
		{"duplicate target", func(s *Spec) { s.Mounts[1].Target = "/work" }, "twice"},
		{"mount over tmpfs", func(s *Spec) { s.Mounts[1].Target = "/tmp" }, "twice"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := validSpec()
			tc.mutate(&s)
			err := s.Validate()
			if err == nil {
				t.Fatal("an unhardened spec was accepted")
			}
			if !errors.Is(err, ErrInvalidSpec) {
				t.Errorf("errors.Is(%v, ErrInvalidSpec) = false", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

func TestSpecValidateReportsEveryProblem(t *testing.T) {
	err := (Spec{}).Validate()
	var se *SpecError
	if !errors.As(err, &se) {
		t.Fatalf("error %T is not a *SpecError", err)
	}
	if len(se.Problems) < 6 {
		t.Errorf("an empty spec has %d problems, want several: %v", len(se.Problems), se.Problems)
	}
}

func TestAMountWithoutAKindIsABindMount(t *testing.T) {
	s := validSpec()
	s.Mounts = []Mount{{Source: "/Users/me/src/app", Target: "/work"}}
	if err := s.Validate(); err != nil {
		t.Fatalf("a mount without a kind should validate as a bind mount: %v", err)
	}
	// And so it is vetted: the default kind is the one that needs checking.
	s.Mounts[0].Source = "/Users/me/.ssh"
	if err := s.CheckMounts(testFS(), testHome); err == nil {
		t.Error("a mount without a kind must be checked as a bind mount")
	}
}

func TestSpecCheckMountsSkipsVolumes(t *testing.T) {
	s := validSpec()
	s.Mounts = []Mount{
		{Kind: MountBind, Source: "/Users/me/src/app", Target: "/work"},
		{Kind: MountVolume, Source: "wh-cache", Target: "/cache"}, // a name, not a host path
	}
	if err := s.CheckMounts(testFS(), testHome); err != nil {
		t.Errorf("volumes must not be checked as host paths: %v", err)
	}
	s.Mounts = append(s.Mounts, Mount{Kind: MountBind, Source: "/Users/me/.ssh", Target: "/root/.ssh", ReadOnly: true})
	err := s.CheckMounts(testFS(), testHome)
	if !errors.Is(err, ErrForbiddenMount) {
		t.Errorf("a forbidden bind mount in a spec: %v, want ErrForbiddenMount", err)
	}
}

func TestSpecEnvRefusesWhatTheSupervisorSets(t *testing.T) {
	s := validSpec()
	s.Env = map[string]string{"GOFLAGS": "-mod=mod", "NODE_ENV": "test"}
	if err := s.Validate(); err != nil {
		t.Errorf("ordinary variables: %v", err)
	}
	for _, name := range []string{"HTTPS_PROXY", "https_proxy", "PATH", "HOME", "LD_PRELOAD", "WHR_TASK", "CLAUDE_CONFIG_DIR", "ANTHROPIC_API_KEY", "FTP_PROXY"} {
		s.Env = map[string]string{name: "x"}
		if err := s.Validate(); !errors.Is(err, ErrInvalidSpec) {
			t.Errorf("Env[%s]: err = %v, want ErrInvalidSpec", name, err)
		}
	}
	for _, bad := range []map[string]string{{"A=B": "x"}, {"": "x"}, {"1A": "x"}, {"A": "x\ny"}, {"A": "x\x00"}, {"A": strings.Repeat("x", 5000)}} {
		s.Env = bad
		if err := s.Validate(); !errors.Is(err, ErrInvalidSpec) {
			t.Errorf("Env %q: err = %v, want ErrInvalidSpec", bad, err)
		}
	}
}

func TestBuildSpecValidate(t *testing.T) {
	ok := BuildSpec{Tag: "whr.invalid/whr-env/o1:abc", ContextDir: "/var/ctx", Dockerfile: "/var/ctx/Dockerfile", Args: map[string]string{"GO_VERSION": "1.27"}}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*BuildSpec){
		"tag looks like an option": func(b *BuildSpec) { b.Tag = "--privileged" },
		"relative context":         func(b *BuildSpec) { b.ContextDir = "ctx" },
		"unclean dockerfile":       func(b *BuildSpec) { b.Dockerfile = "/var/ctx/../Dockerfile" },
		"proxy build argument":     func(b *BuildSpec) { b.Args = map[string]string{"HTTPS_PROXY": "http://evil"} },
	} {
		b := ok
		mutate(&b)
		if err := b.Validate(); !errors.Is(err, ErrInvalidBuild) {
			t.Errorf("%s: err = %v, want ErrInvalidBuild", name, err)
		}
	}
}

func TestBuiltImagesLiveUnderTheReservedHost(t *testing.T) {
	for _, ref := range []string{BuiltImageHost + "whr-env/o1:abc", BuiltImageHost + "whr-base/fedora:0123456789ab", BuiltImageHost + "whr-console/fedora:0123456789ab"} {
		if !ValidImage(ref) || !IsBuiltImage(ref) {
			t.Errorf("%s: valid %v, built %v", ref, ValidImage(ref), IsBuiltImage(ref))
		}
	}
	for _, ref := range []string{"whr-env/o1:abc", "fedora", "docker.io/whr.invalid/x"} {
		if IsBuiltImage(ref) {
			t.Errorf("%s reads as built by whr", ref)
		}
	}
}
