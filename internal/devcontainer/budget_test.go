package devcontainer

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
)

// budgetGit is a hostile repository's answers: blobs whose sizes are told but
// whose content is never produced unless asked for, and a listing. It records
// every command and refuses to produce more than the cap, as the real runner does.
type budgetGit struct {
	ls    string
	sizes map[string]int64 // oid -> size
	calls []string
	reads int // cat-file blob calls
}

func (g *budgetGit) RunCapped(_ context.Context, limit int64, args ...string) ([]byte, error) {
	g.calls = append(g.calls, strings.Join(args, " "))
	var out []byte
	switch {
	case args[0] == "ls-tree":
		out = []byte(g.ls)
	case args[0] == "cat-file" && args[1] == "-s":
		out = []byte(strconv.FormatInt(g.sizes[args[2]], 10) + "\n")
	case args[0] == "cat-file":
		g.reads++
		if g.sizes[args[2]] > limit {
			return nil, errors.New("output over the cap")
		}
		out = make([]byte, g.sizes[args[2]])
	default:
		return nil, errors.New("unexpected git command")
	}
	if int64(len(out)) > limit {
		return nil, errors.New("output over the cap")
	}
	return out, nil
}

func entryLine(mode, oid, p string) string { return mode + " blob " + oid + "\t" + p + "\x00" }

func TestFileChecksTheSizeBeforeReadingTheBlob(t *testing.T) {
	g := &budgetGit{
		ls:    entryLine("100644", "big", "Dockerfile"),
		sizes: map[string]int64{"big": 1<<20 + 1},
	}
	_, err := file(context.Background(), g, "main", "Dockerfile")
	if !errors.Is(err, ErrRefused) {
		t.Fatalf("err = %v, want ErrRefused", err)
	}
	if g.reads != 0 {
		t.Errorf("the blob was read (%v) before its size was refused", g.calls)
	}
	// A blob that fits is read with a cap of exactly its size.
	g = &budgetGit{ls: entryLine("100644", "ok", "Dockerfile"), sizes: map[string]int64{"ok": 10}}
	if data, err := file(context.Background(), g, "main", "Dockerfile"); err != nil || len(data) != 10 {
		t.Fatalf("data = %d err = %v", len(data), err)
	}
}

func TestExportRefusesAnOverBudgetContextBeforeReadingIt(t *testing.T) {
	g := &budgetGit{
		ls:    entryLine("100644", "a", "a") + entryLine("100644", "b", "b") + entryLine("100644", "huge", "c"),
		sizes: map[string]int64{"a": 4, "b": 4, "huge": 1 << 40},
	}
	_, err := Export(context.Background(), g, "main", ".", t.TempDir(), Limits{Files: 10, Bytes: 100})
	if !errors.Is(err, ErrRefused) {
		t.Fatalf("err = %v, want ErrRefused", err)
	}
	if g.reads != 2 {
		t.Errorf("only the two blobs inside the budget may be read, got %d: %v", g.reads, g.calls)
	}
	for _, c := range g.calls {
		if c == "cat-file blob huge" {
			t.Errorf("the huge blob was read: %v", g.calls)
		}
	}
}

func TestExportRefusesATreeOverTheFileLimitBeforeReadingAnyBlob(t *testing.T) {
	var ls strings.Builder
	sizes := map[string]int64{}
	for i := range 50 {
		oid := fmt.Sprintf("o%d", i)
		ls.WriteString(entryLine("100644", oid, fmt.Sprintf("f%d", i)))
		sizes[oid] = 1
	}
	g := &budgetGit{ls: ls.String(), sizes: sizes}
	_, err := Export(context.Background(), g, "main", ".", t.TempDir(), Limits{Files: 10, Bytes: 1 << 20})
	if !errors.Is(err, ErrRefused) {
		t.Fatalf("err = %v, want ErrRefused", err)
	}
	if g.reads != 0 || len(g.calls) != 1 {
		t.Errorf("a blob was sized or read before the file limit was checked: %v", g.calls)
	}
}

// A listing longer than the cap is an error, not a truncated list.
func TestExportRefusesAListingOverItsCap(t *testing.T) {
	g := &budgetGit{ls: strings.Repeat("x", 10<<20)}
	if _, err := Export(context.Background(), g, "main", ".", t.TempDir(), Limits{Files: 10, Bytes: 1 << 20}); err == nil {
		t.Fatal("a listing over the cap was accepted")
	}
	for _, c := range g.calls {
		if strings.HasPrefix(c, "cat-file") {
			t.Errorf("content was read after the listing failed: %v", g.calls)
		}
	}
}

// A symbolic link is never read, however large the object it names.
func TestExportNeverReadsASymbolicLink(t *testing.T) {
	g := &budgetGit{ls: entryLine("120000", "link", "l") + entryLine("100644", "a", "a"), sizes: map[string]int64{"link": 1 << 40, "a": 1}}
	res, err := Export(context.Background(), g, "main", ".", t.TempDir(), Limits{Files: 10, Bytes: 100})
	if err != nil || res.Files != 1 || g.reads != 1 {
		t.Fatalf("res = %+v err = %v calls = %v", res, err, g.calls)
	}
}
