// SPDX-License-Identifier: EUPL-1.2
package catalogue

import _ "embed"

//go:embed assets/catalogue.json
var initialJSON []byte

//go:embed assets/templates/fish.fish
var initialFish []byte

//go:embed assets/templates/starship.toml
var initialStarship []byte

//go:embed assets/templates/fzf.fish
var initialFZF []byte

//go:embed assets/templates/zoxide.fish
var initialZoxide []byte

// LoadInitial validates the release's embedded production snapshot through Load.
// Each call returns independent items and template bytes, without reading host
// files, fetching a catalogue or executing a version probe.
func LoadInitial() (*Catalogue, error) {
	return Load(initialJSON, map[string][]byte{
		"fish.fish":     initialFish,
		"starship.toml": initialStarship,
		"fzf.fish":      initialFZF,
		"zoxide.fish":   initialZoxide,
	})
}
