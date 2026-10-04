package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/config"
)

func TestServeOnlyLogsTheDevelopmentPrefix(t *testing.T) {
	var logged []string
	logf := func(format string, args ...any) { logged = append(logged, fmt.Sprintf(format, args...)) }
	dir := t.TempDir()
	exe := filepath.Join(dir, "bin", "whr")

	if err := noteDevelopmentPrefix(&config.Config{}, "c.json", exe, logf); err != nil || len(logged) != 0 {
		t.Errorf("no key: %v, logged %v", err, logged)
	}
	cfg := &config.Config{DevelopmentPrefix: dir}
	if err := noteDevelopmentPrefix(cfg, "c.json", exe, logf); err != nil {
		t.Fatalf("a remembered prefix must not stop serve: %v", err)
	}
	if len(logged) != 1 || !strings.Contains(logged[0], "development_prefix "+dir) || !strings.Contains(logged[0], "c.json") || !strings.Contains(logged[0], "whr setup --managed") {
		t.Errorf("serve logs the key, the file and the way out: %v", logged)
	}
}

func TestServeRefusesTheKeyFromAManagedPrefix(t *testing.T) {
	if _, err := os.Stat("/usr/local"); err != nil {
		t.Skip("no /usr/local here")
	}
	logf := func(string, ...any) { t.Error("nothing is logged for a refused key") }
	err := noteDevelopmentPrefix(&config.Config{DevelopmentPrefix: t.TempDir()}, "c.json", "/usr/local/bin/whr", logf)
	if err == nil || !strings.Contains(err.Error(), "managed prefix") {
		t.Errorf("a managed whr with the key = %v", err)
	}
}
