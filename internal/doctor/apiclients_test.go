package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/config"
)

func TestAPIClientsCheck(t *testing.T) {
	const secretA, secretB = "throwaway-doctor-a-0001", "throwaway-doctor-b-0002"
	dir := t.TempDir()
	file := func(name, content string, mode os.FileMode) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p, mode); err != nil { //nolint:gosec // the test sets a mode on purpose
			t.Fatal(err)
		}
		return p
	}
	def := file("default.token", secretA+"\n", 0o600)
	good := file("good.token", secretB+"\n", 0o600)
	link := filepath.Join(dir, "link.token")
	if err := os.Symlink(good, link); err != nil {
		t.Fatal(err)
	}
	hard := file("hard.token", "throwaway-hard-0003\n", 0o600)
	if err := os.Link(hard, filepath.Join(dir, "hard-2")); err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		client config.APIClient
		want   string // "" is OK
	}{
		"ok":            {config.APIClient{Name: "ci", TokenFile: good}, ""},
		"missing":       {config.APIClient{Name: "ci", TokenFile: filepath.Join(dir, "nope")}, "does not exist"},
		"link":          {config.APIClient{Name: "ci", TokenFile: link}, "not a regular file"},
		"directory":     {config.APIClient{Name: "ci", TokenFile: dir}, "not a regular file"},
		"empty":         {config.APIClient{Name: "ci", TokenFile: file("empty.token", "", 0o600)}, "is empty"},
		"world-read":    {config.APIClient{Name: "ci", TokenFile: file("wide.token", "throwaway-wide-0004\n", 0o644)}, "0644"},
		"stricter":      {config.APIClient{Name: "ci", TokenFile: file("ro.token", "throwaway-ro-0005\n", 0o400)}, "0400"},
		"hard link":     {config.APIClient{Name: "ci", TokenFile: hard}, "hard links"},
		"too large":     {config.APIClient{Name: "ci", TokenFile: file("big.token", strings.Repeat("x", 64<<10+1), 0o600)}, "larger than"},
		"shared token":  {config.APIClient{Name: "ci", TokenFile: file("same.token", secretA+"\n", 0o600)}, "share one token"},
		"reserved name": {config.APIClient{Name: "default", TokenFile: good}, "reserved"},
		"invalid name":  {config.APIClient{Name: "Ci", TokenFile: good}, "must match"},
	} {
		t.Run(name, func(t *testing.T) {
			st, detail := apiClientsCheck(&config.Config{APITokenFile: def, APIClients: []config.APIClient{tc.client}})
			if tc.want == "" {
				if st != OK {
					t.Fatalf("status = %v: %s", st, detail)
				}
			} else if st != Fail || !strings.Contains(detail, tc.want) {
				t.Fatalf("status = %v, detail %q, want a Fail with %q", st, detail, tc.want)
			}
			if tc.want != "" && !strings.Contains(detail, `"ci"`) && !strings.Contains(detail, `"default"`) && !strings.Contains(detail, `"Ci"`) {
				t.Errorf("the finding names no client: %s", detail)
			}
			for _, secret := range []string{secretA, secretB, "throwaway-"} {
				if strings.Contains(detail, secret) {
					t.Errorf("detail leaks token material: %s", detail)
				}
			}
		})
	}
}

func TestAPIClientsCheckFailsOnTheDefaultFile(t *testing.T) {
	st, detail := apiClientsCheck(&config.Config{APITokenFile: filepath.Join(t.TempDir(), "gone")})
	if st != Fail || !strings.Contains(detail, `"default"`) {
		t.Errorf("status = %v, detail %q", st, detail)
	}
}
