package store

import (
	"reflect"
	"testing"
	"time"
)

func TestEgressAnswersArePerRepositoryAndTheLatestWins(t *testing.T) {
	s := openTemp(t)
	now := time.Unix(1_700_000_000, 0).UTC()
	got, err := s.EgressHosts(bg, "a/b")
	if err != nil || len(got.Allowed) != 0 || len(got.Answered) != 0 {
		t.Fatalf("nothing is allowed before an answer: %+v %v", got, err)
	}
	for _, a := range []struct {
		repo, host string
		allow      bool
	}{
		{"a/b", "proxy.golang.org", true}, {"a/b", "sum.golang.org", true}, {"a/b", "evil.example", false}, {"c/d", "pypi.org", true},
	} {
		if err := s.SetEgressHost(bg, a.repo, a.host, a.allow, "d1", now); err != nil {
			t.Fatal(err)
		}
	}
	got, _ = s.EgressHosts(bg, "a/b")
	if !reflect.DeepEqual(got.Allowed, []string{"proxy.golang.org", "sum.golang.org"}) || !got.Answered["evil.example"] || got.Answered["pypi.org"] {
		t.Errorf("a/b = %+v", got)
	}
	// An allow is revoked by a later deny.
	if err := s.SetEgressHost(bg, "a/b", "sum.golang.org", false, "d2", now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	got, _ = s.EgressHosts(bg, "a/b")
	if !reflect.DeepEqual(got.Allowed, []string{"proxy.golang.org"}) || !got.Answered["sum.golang.org"] {
		t.Errorf("after the revoke: %+v", got)
	}
	if err := s.SetEgressHost(bg, "", "x.example", true, "", now); err == nil {
		t.Error("an answer without a repository must fail")
	}
}
