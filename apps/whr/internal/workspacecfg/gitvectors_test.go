// SPDX-License-Identifier: EUPL-1.2

package workspacecfg_test

import (
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/workspacecfg"
)

type gitCase struct {
	ID           string   `json:"id"`
	ProjectsPath string   `json:"projects_path"`
	Head         *bool    `json:"head"`
	Tree         []string `json:"tree"`
	Index        []string `json:"index"`
	Gitmodules   *string  `json:"gitmodules"`
	BytesHex     *string  `json:"bytes_hex"`
	GenerateB    *struct {
		Total    int    `json:"total"`
		Template string `json:"template"`
		Padding  string `json:"padding"`
	} `json:"generate_bytes"`
	GenerateE *struct {
		Count        int  `json:"count"`
		WithGitlinks bool `json:"with_gitlinks"`
	} `json:"generate_entries"`
	ExpectCodes []string `json:"expect_codes"`
}

func parseEntries(t *testing.T, id string, list []string) []workspacecfg.TreeEntry {
	t.Helper()
	var out []workspacecfg.TreeEntry
	for _, s := range list {
		f := strings.SplitN(s, " ", 3)
		if len(f) != 3 {
			t.Fatalf("%s: entry %q", id, s)
		}
		sha := f[1]
		if len(sha) == 1 {
			sha = strings.Repeat(sha, 40)
		}
		out = append(out, workspacecfg.TreeEntry{Mode: f[0], SHA: sha, Path: f[2]})
	}
	return out
}

func (c gitCase) snapshot(t *testing.T) workspacecfg.GitSnapshot {
	t.Helper()
	s := workspacecfg.GitSnapshot{ProjectsPath: c.ProjectsPath, HeadExists: c.Head == nil || *c.Head}
	if s.ProjectsPath == "" {
		s.ProjectsPath = "projects"
	}
	tree := parseEntries(t, c.ID, c.Tree)
	index := tree
	if c.Index != nil {
		index = parseEntries(t, c.ID, c.Index)
	}
	if s.HeadExists {
		s.Head = tree
	}
	s.Index = index
	switch {
	case c.BytesHex != nil:
		b, err := hex.DecodeString(*c.BytesHex)
		if err != nil {
			t.Fatal(err)
		}
		s.Gitmodules = b
	case c.GenerateB != nil:
		g := c.GenerateB
		s.Gitmodules = []byte(g.Template + strings.Repeat("\n", g.Total-len(g.Template)))
	case c.GenerateE != nil:
		var b strings.Builder
		for i := 0; i < c.GenerateE.Count; i++ {
			p := fmt.Sprintf("projects/s%03d", i)
			fmt.Fprintf(&b, "[submodule \"s%03d\"]\n\tpath = %s\n\turl = https://f.example/o/s%03d.git\n", i, p, i)
			if c.GenerateE.WithGitlinks {
				e := workspacecfg.TreeEntry{Mode: "160000", SHA: strings.Repeat("a", 40), Path: p}
				s.Index = append(s.Index, e)
				if s.HeadExists {
					s.Head = append(s.Head, e)
				}
			}
		}
		g := workspacecfg.TreeEntry{Mode: "100644", SHA: strings.Repeat("b", 40), Path: ".gitmodules"}
		s.Index = append(s.Index, g)
		if s.HeadExists {
			s.Head = append(s.Head, g)
		}
		s.Gitmodules = []byte(b.String())
	case c.Gitmodules != nil:
		s.Gitmodules = []byte(*c.Gitmodules)
	}
	return s
}

func distinctCodes(fs []workspacecfg.Finding) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range fs {
		if !seen[f.Code] {
			seen[f.Code] = true
			out = append(out, f.Code)
		}
	}
	sort.Strings(out)
	return out
}

// TestGitmodulesResolverParity runs every committed-blob and listing vector of
// contracts/workspace/git-vectors.json through the pure Git-state analysis.
func TestGitmodulesResolverParity(t *testing.T) {
	var v struct {
		Cases []gitCase `json:"cases"`
	}
	readJSON(t, contractPath("workspace", "git-vectors.json"), &v)
	if len(v.Cases) < 90 {
		t.Fatalf("%d cases", len(v.Cases))
	}
	for _, c := range v.Cases {
		t.Run(c.ID, func(t *testing.T) {
			fs, _ := workspacecfg.AnalyzeGit(c.snapshot(t))
			got := strings.Join(distinctCodes(fs), ",")
			want := strings.Join(c.ExpectCodes, ",")
			if got != want {
				t.Errorf("codes %q, want %q\n%+v", got, want, fs)
			}
			for _, f := range fs {
				if strings.Contains(f.Message+f.Subject+f.Remediation, "CANARY") {
					t.Errorf("credential echoed: %+v", f)
				}
			}
		})
	}
}

func TestAnalyzeGitInfoGitlinks(t *testing.T) {
	_, info := workspacecfg.AnalyzeGit(workspacecfg.GitSnapshot{
		ProjectsPath: "projects", HeadExists: true,
		Head:  []workspacecfg.TreeEntry{{Mode: "160000", SHA: strings.Repeat("a", 40), Path: "projects/a"}},
		Index: []workspacecfg.TreeEntry{{Mode: "160000", SHA: strings.Repeat("b", 40), Path: "projects/b"}},
	})
	if !info.HasGitlink("projects/a") || !info.HasGitlink("projects/b") || info.HasGitlink("projects/c") {
		t.Errorf("%+v", info)
	}
}

func TestAnalyzeGitFindingsBounded(t *testing.T) {
	var head []workspacecfg.TreeEntry
	for i := 0; i < 5000; i++ {
		head = append(head, workspacecfg.TreeEntry{Mode: "100644", SHA: strings.Repeat("a", 40), Path: fmt.Sprintf("projects/f%d", i)})
	}
	fs, _ := workspacecfg.AnalyzeGit(workspacecfg.GitSnapshot{ProjectsPath: "projects", HeadExists: true, Head: head, Index: head})
	if len(fs) != 5000 {
		t.Fatalf("%d findings", len(fs))
	}
	if r := workspacecfg.NewReport("d", fs); !r.Truncated || r.Counts.Warning != 5000 || len(r.Findings) != workspacecfg.MaxFindings {
		t.Errorf("%+v", r.Counts)
	}
}
