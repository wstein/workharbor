package docscheck

import (
	"os"
	"strings"
	"testing"
)

// The base image and the console image share one stock image (D44), and a test
// fails when only one digest moves. So Dependabot must bump the directories that
// hold the Containerfiles together (issue #120): the docker entry lists them and
// puts them in one group. The check reads the file as text, because the module has
// no YAML reader and needs none for this.
func TestDependabotBumpsTheContainerfilesTogether(t *testing.T) {
	raw, err := os.ReadFile("../../.github/dependabot.yml")
	if err != nil {
		t.Fatal(err)
	}
	var docker string
	for _, block := range strings.Split(string(raw), "\n  - package-ecosystem: ")[1:] {
		if strings.HasPrefix(block, "docker\n") {
			docker = block
		}
	}
	if docker == "" {
		t.Fatal("dependabot.yml has no docker entry")
	}
	for _, dir := range []string{"/internal/baseimage", "/internal/console"} {
		if !strings.Contains(docker, dir) {
			t.Errorf("Dependabot does not watch %s", dir)
		}
	}
	if !strings.Contains(docker, "    groups:\n") || !strings.Contains(docker, `patterns: ["*"]`) {
		t.Error("the docker entry has no group, so a bump of the shared digest arrives as one pull request per directory and each fails alone")
	}
	if strings.Contains(docker, "is unverified; the devcontainer") {
		t.Error("the comment that said Dependabot may not read Containerfile.<base> is stale: it opened #114 and #115 for one")
	}
}
