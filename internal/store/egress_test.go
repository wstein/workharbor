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
		if err := s.SetEgressHost(bg, a.repo, a.host, a.allow, "d1", "digest-1", now); err != nil {
			t.Fatal(err)
		}
	}
	got, _ = s.EgressHosts(bg, "a/b")
	if !reflect.DeepEqual(got.Allowed, []string{"proxy.golang.org", "sum.golang.org"}) || !got.Answered["evil.example"] || got.Answered["pypi.org"] {
		t.Errorf("a/b = %+v", got)
	}
	// An allow is revoked by a later deny.
	if err := s.SetEgressHost(bg, "a/b", "sum.golang.org", false, "d2", "digest-1", now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	got, _ = s.EgressHosts(bg, "a/b")
	if !reflect.DeepEqual(got.Allowed, []string{"proxy.golang.org"}) || !got.Answered["sum.golang.org"] {
		t.Errorf("after the revoke: %+v", got)
	}
	if err := s.SetEgressHost(bg, "", "x.example", true, "", "", now); err == nil {
		t.Error("an answer without a repository must fail")
	}
}

func TestAnEgressAnswerRemembersItsSourceAndCanBeForgotten(t *testing.T) {
	s := openTemp(t)
	now := time.Unix(1_700_000_000, 0).UTC()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(s.SetEgressHost(bg, "a/b", "proxy.golang.org", true, "d1", "digest-1", now))
	must(s.SetEgressHost(bg, "a/b", "sum.golang.org", true, "d2", "lockfile", now))
	must(s.SetEgressHost(bg, "a/b", "old.example", true, "d3", "", now))
	got, _ := s.EgressHosts(bg, "a/b")
	if got.Sources["proxy.golang.org"] != "digest-1" || got.Sources["sum.golang.org"] != "lockfile" || got.Sources["old.example"] != "unknown" {
		t.Errorf("sources = %v", got.Sources)
	}
	must(s.ForgetEgressHosts(bg, "a/b", []string{"proxy.golang.org", "nope.example"}))
	got, _ = s.EgressHosts(bg, "a/b")
	if got.Answered["proxy.golang.org"] || !reflect.DeepEqual(got.Allowed, []string{"old.example", "sum.golang.org"}) {
		t.Errorf("after forgetting: %+v", got)
	}
}
