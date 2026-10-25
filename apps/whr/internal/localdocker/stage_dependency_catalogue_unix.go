//go:build unix

// SPDX-License-Identifier: EUPL-1.2
package localdocker

// Bounded inspected bodies, not a filename exemption or source-trust grant.
// Every entry must match its full bytes, then recursively admit its declared
// helpers. All source bytes still participate in the owner trust fingerprint.
type stageKnownDependency struct {
	Digest, Repository, Revision string
	Dependencies                 []string
	GitIdentity                  bool
}

var stageKnownDependencies = map[string]stageKnownDependency{
	".devcontainer/setup.sh":              {Digest: "sha256:744d4e62e52e4c1db39ad35684d04de3b1456b43c60d1ca911c7bacdd27239f9", Repository: "Anchorage", Revision: "0f72d7226b6921e7d0c2642aca034ef44fe26c56", Dependencies: []string{".devcontainer/greeting-info.sh", ".devcontainer/starship.toml", ".devcontainer/starship-plain.toml", ".devcontainer/logo.txt", ".devcontainer/devcontainer.json"}, GitIdentity: true},
	".devcontainer/initialize.sh":         {Digest: "sha256:fbe58ed198a79c07baf1a9eecd0eda33220df97f520f3b939799809c30b623a4", Repository: "Anchorage", Revision: "0f72d7226b6921e7d0c2642aca034ef44fe26c56", Dependencies: []string{"bundledock/scripts/bundle-volume.sh"}, GitIdentity: false},
	".devcontainer/host-info.sh":          {Digest: "sha256:926c1b52b3cdb46bd47627793042534c53c83a9d970d5f5404bc3b584acfa8f0", Repository: "Anchorage", Revision: "0f72d7226b6921e7d0c2642aca034ef44fe26c56", Dependencies: []string{}, GitIdentity: false},
	".devcontainer/smoke-container.sh":    {Digest: "sha256:84457963583a96770f0a4e5f4371b271326ba834e37bf05f6e18f96e85582214", Repository: "Anchorage", Revision: "0f72d7226b6921e7d0c2642aca034ef44fe26c56", Dependencies: []string{"Makefile", ".devcontainer/test-integration.py", ".devcontainer/test-pins.py", ".devcontainer/test-setup.sh", ".devcontainer/setup.sh", ".devcontainer/greeting-info.sh", ".devcontainer/pins-hash.sh", ".devcontainer/git-common-dir.sh", ".devcontainer/up.sh", ".devcontainer/smoke.sh", "bundledock/Makefile"}, GitIdentity: true},
	".devcontainer/greeting-info.sh":      {Digest: "sha256:cb9f47acf76fa08556316e002969c32fb368e4d17470d975ccc4ffff9d43d39c", Repository: "Anchorage", Revision: "0f72d7226b6921e7d0c2642aca034ef44fe26c56", Dependencies: []string{".devcontainer/pins-hash.sh"}, GitIdentity: true},
	".devcontainer/pins-hash.sh":          {Digest: "sha256:e9be7ea35911ea59a1808b707017d2590faff231454fb5e21c89da73013734dd", Repository: "Anchorage", Revision: "0f72d7226b6921e7d0c2642aca034ef44fe26c56", Dependencies: []string{".devcontainer/devcontainer.json"}, GitIdentity: false},
	".devcontainer/test-integration.py":   {Digest: "sha256:f9bdab58187b56f7ad078bcf85ece130cf29deca0adc6efa07839d6ca97117ca", Repository: "Anchorage", Revision: "0f72d7226b6921e7d0c2642aca034ef44fe26c56", Dependencies: []string{}, GitIdentity: false},
	".devcontainer/test-pins.py":          {Digest: "sha256:1c3a87d9efd7fafa98e5adab0e41bb776bbd71694fa346dc66d4adada47cd031", Repository: "Anchorage", Revision: "0f72d7226b6921e7d0c2642aca034ef44fe26c56", Dependencies: []string{}, GitIdentity: false},
	".devcontainer/test-setup.sh":         {Digest: "sha256:d77bec11a358533ce6c91fb26b5011b33dcee7555aa56c8057641e5b462d7ef1", Repository: "Anchorage", Revision: "0f72d7226b6921e7d0c2642aca034ef44fe26c56", Dependencies: []string{".devcontainer/setup.sh", ".devcontainer/greeting-info.sh", ".devcontainer/host-info.sh", ".devcontainer/pins-hash.sh"}, GitIdentity: true},
	".devcontainer/git-common-dir.sh":     {Digest: "sha256:14fd0ad4766b54be9eb118ccda58d2ad21ce1238cbb13925274c8be6cc543282", Repository: "Anchorage", Revision: "0f72d7226b6921e7d0c2642aca034ef44fe26c56", Dependencies: []string{}, GitIdentity: false},
	".devcontainer/up.sh":                 {Digest: "sha256:fb51ec37b4e32e2b80bef83aa0bd51145f197dfaa093bc3aaeedf2b90f7cfef0", Repository: "Anchorage", Revision: "0f72d7226b6921e7d0c2642aca034ef44fe26c56", Dependencies: []string{}, GitIdentity: false},
	".devcontainer/smoke.sh":              {Digest: "sha256:eeeb480c3da8e0d1c5dfd6def0c86091506709d9805c909d5f86369c33cb160e", Repository: "Anchorage", Revision: "0f72d7226b6921e7d0c2642aca034ef44fe26c56", Dependencies: []string{}, GitIdentity: false},
	".devcontainer/starship.toml":         {Digest: "sha256:9f59fe73e8ccd6eb759e5288d0f9361c241193fe50b628474707fcf89788b97a", Repository: "Anchorage", Revision: "0f72d7226b6921e7d0c2642aca034ef44fe26c56", Dependencies: []string{}, GitIdentity: false},
	".devcontainer/starship-plain.toml":   {Digest: "sha256:dbd6825ddd38014c311d5a4bc5c55d8667876b9acdbf4bfee27059a067b11d31", Repository: "Anchorage", Revision: "0f72d7226b6921e7d0c2642aca034ef44fe26c56", Dependencies: []string{}, GitIdentity: false},
	"Makefile":                            {Digest: "sha256:a0faacef0faa96482acfab8275fe34347613f87942c6ee9797e00a6d257a60b1", Repository: "Anchorage", Revision: "0f72d7226b6921e7d0c2642aca034ef44fe26c56", Dependencies: []string{}, GitIdentity: false},
	"bundledock/Makefile":                 {Digest: "sha256:3d7858e5e4af56a5d1df22ba10eeb7478e06496757f9b1506090cb6df6243cb3", Repository: "BundleDock", Revision: "9f16cb790c108cf7165fc8bfb1ede2180717dd3b", Dependencies: []string{}, GitIdentity: false},
	"bundledock/scripts/bundle-volume.sh": {Digest: "sha256:f71e359345cd03b6d515dc36f272d7b351f4c2da525e2da66538f4b069f35e85", Repository: "BundleDock", Revision: "9f16cb790c108cf7165fc8bfb1ede2180717dd3b", Dependencies: []string{}, GitIdentity: false},
}
