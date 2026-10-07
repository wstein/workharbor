package config

import "testing"

func TestNormalizePublicURL(t *testing.T) {
	ok := map[string]string{
		"whr.example.ts.net":              "https://whr.example.ts.net",
		"  WHR.Example.TS.net/ ":          "https://whr.example.ts.net",
		"https://Whr.example.ts.net/":     "https://whr.example.ts.net",
		"HTTPS://whr.example.ts.net:8443": "https://whr.example.ts.net:8443",
		"localhost:8787":                  "https://localhost:8787",
		"[::1]":                           "https://[::1]",
	}
	for in, want := range ok {
		if got, err := NormalizePublicURL(in); err != nil || got != want {
			t.Errorf("%q = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{
		"", "   ", "http://whr.example.ts.net", "ftp://x.example", "javascript://x",
		"https://u:p@whr.example.ts.net", "whr.example.ts.net/path", "https://x.example/a",
		"x.example?next=//evil.example", "x.example#f", "evil.example\\@good.example",
		"a b.example", "-bad.example", "bad_.example", "x.example:0", "x.example:99999",
		"x.example:80a", "x..example", "//x.example",
	} {
		if got, err := NormalizePublicURL(in); err == nil {
			t.Errorf("%q accepted as %q", in, got)
		}
	}
}
