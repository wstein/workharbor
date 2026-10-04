package skillset

import "testing"

func TestSelectionRequiresAnExplicitDefault(t *testing.T) {
	if _, err := (Config{}).Resolve(); err == nil {
		t.Fatal("an absent default must not become none")
	}
	selection, err := (Config{Selection: "none"}).Resolve()
	if err != nil || selection != nil {
		t.Fatalf("explicit none: %v, %v", selection, err)
	}
}

func TestInventoryEncoding(t *testing.T) {
	files := []File{{Path: ".agents/a.md", SHA256: Digest([]byte("a"))}, {Path: "SKILL.md", SHA256: Digest([]byte("b"))}}
	want := Digest([]byte(files[0].SHA256 + "  .agents/a.md\n" + files[1].SHA256 + "  SKILL.md\n"))
	if got, err := InventoryDigest(files); err != nil || got != want {
		t.Fatalf("inventory = %s, %v; want %s", got, err, want)
	}
	for _, path := range []string{"../a", "/a", "a\\b", "a\nb", "a/../b", ".git/config", "é.md", "a//b"} {
		if _, err := InventoryDigest([]File{{Path: path, SHA256: files[0].SHA256}}); err == nil {
			t.Errorf("accepted path %q", path)
		}
	}
}
