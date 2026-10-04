package codex

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestSchemaInventory(t *testing.T) {
	digest := sha256.New()
	count := 0
	err := filepath.WalkDir("testdata/schema", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		content, err := fs.ReadFile(os.DirFS("."), filepath.ToSlash(path))
		if err != nil {
			return err
		}
		if !json.Valid(content) {
			t.Fatalf("invalid schema: %s", path)
		}
		name, err := filepath.Rel("testdata/schema", path)
		if err != nil {
			return err
		}
		_, _ = digest.Write([]byte(filepath.ToSlash(name) + "\x00"))
		_, _ = digest.Write(content)
		count++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if actual := hex.EncodeToString(digest.Sum(nil)); actual != "4b0ee56db9941bb1bec9fdfd10d3f0d28980663b98ef0e12ec5d19c48d4e3625" || count != 22 {
		t.Fatalf("schema inventory: digest %s, files %d", actual, count)
	}
}
