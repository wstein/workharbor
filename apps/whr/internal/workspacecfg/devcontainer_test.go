// SPDX-License-Identifier: EUPL-1.2

package workspacecfg_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/workspacecfg"
)

const digest = "docker.io/library/buildpack-deps@sha256:4163ac2c58660c533f3cf3d178551287ddd0ac18269ef84f3a382d0698c89254"

func codesOf(fs []workspacecfg.Finding) string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Code)
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

func TestDevcontainerExamples(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..", "examples", "workspace")
	for dir, want := range map[string]string{
		"admitted-image":      "",
		"admitted-dockerfile": "",
		".":                   "devcontainer.image_not_digest_pinned,devcontainer.users_not_numeric",
	} {
		raw, err := os.ReadFile(filepath.Join(root, dir, ".devcontainer", "devcontainer.json"))
		if err != nil {
			t.Fatal(err)
		}
		if got := codesOf(workspacecfg.CheckDevcontainer(raw)); got != want {
			t.Errorf("%s: %q, want %q", dir, got, want)
		}
	}
}

func TestDevcontainerJSONC(t *testing.T) {
	for name, tc := range map[string]struct{ in, want string }{
		"comments and trailing commas": {"// c\n{ /* b */ \"image\": \"" + digest + "\", \"x\": [1,2,],\n}", "devcontainer.unsupported_key"},
		"slashes inside strings":       {`{"image":"` + digest + `","name":"a//b/*c*/"}`, ""},
		"empty object":                 {`{}`, ""},
		"unterminated comment":         {`{} /* x`, "devcontainer.syntax"},
		"not an object":                {`[1]`, "devcontainer.syntax"},
		"empty":                        {``, "devcontainer.syntax"},
		"leading comma":                {`{,}`, "devcontainer.syntax"},
		"duplicate":                    {`{"a":1,"a":2}`, "devcontainer.duplicate_key"},
		"nested duplicate":             {`{"build":{"args":{"A":"1","A":"2"}}}`, "devcontainer.duplicate_key"},
		"escaped duplicate":            {`{"image":"x","image":"y"}`, "devcontainer.duplicate_key"},
		"duplicate in comment ignored": {`{"a":1 /* "a":2 */}`, "devcontainer.unsupported_key"},
		"bom":                          {"\xef\xbb\xbf{}", "devcontainer.encoding"},
		"nul":                          {"{}\x00", "devcontainer.encoding"},
		"invalid utf8":                 {"{\"a\":\"\xff\"}", "devcontainer.encoding"},
		"depth bomb":                   {strings.Repeat("[", 5000), "devcontainer.syntax"},
		"too large":                    {"{}" + strings.Repeat(" ", 256<<10), "devcontainer.too_large"},
	} {
		if got := codesOf(workspacecfg.CheckDevcontainer([]byte(tc.in))); got != tc.want {
			t.Errorf("%s: %q, want %q", name, got, tc.want)
		}
	}
}

func TestDevcontainerWarnings(t *testing.T) {
	for name, tc := range map[string]struct{ in, want string }{
		"tag":              {`{"image":"ubuntu:22.04"}`, "devcontainer.image_not_digest_pinned"},
		"short digest":     {`{"image":"x@sha256:abc"}`, "devcontainer.image_not_digest_pinned"},
		"image not string": {`{"image":1}`, "devcontainer.image_not_digest_pinned"},
		"no image":         {`{"build":{"dockerfile":"Dockerfile"}}`, ""},
		"named user":       {`{"image":"` + digest + `","remoteUser":"vscode"}`, "devcontainer.users_not_numeric"},
		"bare uid":         {`{"image":"` + digest + `","containerUser":"1000"}`, "devcontainer.users_not_numeric"},
		"pair":             {`{"image":"` + digest + `","containerUser":"0:0","remoteUser":"1000:1000"}`, ""},
		"features":         {`{"image":"` + digest + `","features":{}}`, "devcontainer.unsupported_key"},
		"compose":          {`{"dockerComposeFile":"x","service":"s"}`, "devcontainer.unsupported_key,devcontainer.unsupported_key"},
		"mounts runArgs":   {`{"image":"` + digest + `","mounts":[],"runArgs":[]}`, "devcontainer.unsupported_key,devcontainer.unsupported_key"},
		"initialize":       {`{"image":"` + digest + `","initializeCommand":"x"}`, "devcontainer.host_command"},
		"attach":           {`{"image":"` + digest + `","postAttachCommand":"x"}`, "devcontainer.host_command"},
		"supported hooks":  {`{"image":"` + digest + `","onCreateCommand":"x","postStartCommand":["a"],"containerEnv":{},"remoteEnv":{},"overrideCommand":true}`, ""},
	} {
		fs := workspacecfg.CheckDevcontainer([]byte(tc.in))
		if got := codesOf(fs); got != tc.want {
			t.Errorf("%s: %q, want %q", name, got, tc.want)
		}
		for _, f := range fs {
			if f.Severity != workspacecfg.SevWarning && !strings.HasPrefix(name, "not") && f.Code != "devcontainer.syntax" {
				t.Errorf("%s: %s is not a warning", name, f.Code)
			}
		}
	}
}

func TestDevcontainerWarningsNameTheMatrix(t *testing.T) {
	for _, f := range workspacecfg.CheckDevcontainer([]byte(`{"features":{}}`)) {
		if !strings.Contains(f.Remediation+f.Message, "devcontainer-matrix") {
			t.Errorf("%+v does not name the capability matrix", f)
		}
	}
}
