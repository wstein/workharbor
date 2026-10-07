package doctor

import "testing"

const policyFixture = `Getting global account policies
<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict>
<key>policyCategoryPasswordContent</key><array><dict>
<key>policyContent</key><string>policyAttributePassword matches '.{4,}+'</string>
<key>policyContentDescription</key><dict>
<key>de</key><string>Mindestens vier Zeichen.</string>
<key>en</key><string>Four characters or more.</string>
</dict></dict></array></dict></plist>`

func TestPolicyDescription(t *testing.T) {
	for lang, want := range map[string]string{
		"de_DE.UTF-8": "Mindestens vier Zeichen.", "en_US.UTF-8": "Four characters or more.",
		"fr_FR.UTF-8": "Four characters or more.", "": "Four characters or more.",
	} {
		if got := PolicyDescription([]byte(policyFixture), lang); got != want {
			t.Errorf("%q: got %q", lang, got)
		}
	}
	for _, bad := range []string{"", "not a plist", "<plist><dict></dict></plist>", "<plist><dict><key>policyContentDescription</key><dict><key>en"} {
		if got := PolicyDescription([]byte(bad), "en"); got != "" {
			t.Errorf("%q: want nothing, got %q", bad, got)
		}
	}
}
