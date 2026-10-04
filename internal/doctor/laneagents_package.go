package doctor

import (
	"fmt"
	"strings"

	"github.com/wstein/workharbor/internal/skillset"
)

func selectedLanePackage(selection skillset.Config, forbidden []string) (Status, string) {
	pin, err := selection.Resolve()
	if err != nil {
		return Fail, "selected lane package: " + oneLine(err.Error())
	}
	if pin == nil {
		return NotVerified, "explicit none: no external package skills or roles selected; native isolation and project policy are unverified"
	}
	pkg, err := (skillset.Store{Root: selection.Store, Forbidden: forbidden}).Load(*pin)
	if err != nil {
		return Fail, "selected lane package inventory: " + oneLine(err.Error())
	}
	bindings := make([]string, 0, len(pkg.Manifest.Adapters))
	for _, binding := range pkg.Manifest.Adapters {
		bindings = append(bindings, fmt.Sprintf("%s version=%s model=%s effort=%s", binding.Name, binding.Version, binding.Model, binding.Effort))
	}
	return NotVerified, fmt.Sprintf("selected package %s commit=%s manifest=%s inventory=%s root=%s; declared adapter tuples: %s; contract v%d validates text inventory only; canonical role bindings, exact native binary/schema, effective model/effort, account access and configuration provenance are unverified; Copilot retains its historical order; no AGY binding inferred", pkg.Pin.Identity, pkg.Pin.Commit, pkg.Pin.ManifestSHA256, pkg.Pin.InventorySHA256, pkg.Target(), strings.Join(bindings, "; "), pkg.Pin.ContractVersion)
}
