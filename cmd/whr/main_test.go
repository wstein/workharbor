package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/exitcode"
)

func TestVersionPrintsDataOnStdoutAndNothingOnStderr(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"version"}, &out, &errOut); code != exitcode.OK {
		t.Fatalf("exit %d", code)
	}
	if errOut.Len() != 0 {
		t.Errorf("stderr = %q, want nothing", errOut.String())
	}
	if !regexp.MustCompile(`^v\d+\.\d+\.\d+\S* \(commit \S+, (clean|dirty)\)\n$`).MatchString(out.String()) {
		t.Errorf("stdout = %q", out.String())
	}
}

func TestVersionJSON(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"version", "--json"}, &out, &errOut); code != exitcode.OK {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	var got struct {
		SchemaVersion int    `json:"schema_version"`
		Version       string `json:"version"`
		Commit        string `json:"commit"`
		Dirty         *bool  `json:"dirty"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v: %q", err, out.String())
	}
	if got.SchemaVersion != 1 || got.Version == "" || got.Commit == "" || got.Dirty == nil {
		t.Errorf("json = %+v, want a schema_version, a version, a commit and the dirty flag", got)
	}
	if strings.Count(out.String(), "\n") != 1 {
		t.Errorf("the data must be one line: %q", out.String())
	}
}

func TestVersionRefusesUnknownOptions(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"version", "--yaml"}, &out, &errOut); code != exitcode.Usage {
		t.Errorf("exit %d, want Usage", code)
	}
	if out.Len() != 0 || !strings.Contains(errOut.String(), "--yaml") {
		t.Errorf("stdout %q, stderr %q: the error is human text on stderr", out.String(), errOut.String())
	}
	if code := run([]string{"--version"}, &out, &errOut); code != exitcode.OK {
		t.Errorf("--version exit %d", code)
	}
}

// make build stamps the binary: the version is not empty, the commit is the
// checkout's, and the build is trimpath.
func TestMakeBuildStampsTheBinary(t *testing.T) {
	for _, tool := range []string{"make", "git", "go"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not available", tool)
		}
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
		t.Skip("not built from a git checkout")
	}
	bin := filepath.Join(t.TempDir(), "whr")
	build := exec.CommandContext(t.Context(), "make", "-s", "build", "BIN="+bin) //nolint:gosec // fixed arguments
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("make build: %v\n%s", err, out)
	}
	out, err := exec.CommandContext(t.Context(), bin, "version", "--json").Output() //nolint:gosec // the binary just built
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Version string `json:"version"`
		Commit  string `json:"commit"`
		Date    string `json:"date"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	head := exec.CommandContext(t.Context(), "git", "rev-parse", "--short=7", "HEAD") //nolint:gosec // fixed arguments
	head.Dir = root
	want, err := head.Output()
	if err != nil {
		t.Fatal(err)
	}
	if got.Commit != strings.TrimSpace(string(want)) {
		t.Errorf("commit = %q, want %q", got.Commit, strings.TrimSpace(string(want)))
	}
	if !regexp.MustCompile(`^v\d+\.\d+\.\d+(-\d+-g[0-9a-f]{7})?(-[0-9A-Za-z.]+)?$`).MatchString(got.Version) {
		t.Errorf("version = %q, want a tag version or v0.0.0-<n>-g<sha>", got.Version)
	}
	if got.Date == "" {
		t.Error("the build date was not stamped")
	}
	// -trimpath: the binary does not carry the build directory.
	data, err := os.ReadFile(bin) //nolint:gosec // the binary just built
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(root+"/cmd/whr")) {
		t.Error("the binary contains the build directory: -trimpath was not used")
	}
}
