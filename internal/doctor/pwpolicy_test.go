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

func TestPolicyDescriptionOnlyFromPasswordContent(t *testing.T) {
	other := `<?xml version="1.0"?><plist><dict>
<key>policyCategoryAuthentication</key><array><dict>
<key>policyContentDescription</key><dict><key>en</key><string>wrong category</string></dict></dict></array>
</dict></plist>`
	if got := PolicyDescription([]byte(other), "en"); got != "" {
		t.Errorf("got %q", got)
	}
	mixed := `<?xml version="1.0"?><plist><dict>
<key>policyCategoryAuthentication</key><array><dict>
<key>policyContentDescription</key><dict><key>en</key><string>wrong category</string></dict></dict></array>
<key>policyCategoryPasswordContent</key><array><dict>
<key>policyContentDescription</key><dict><key>en</key><string>right</string></dict></dict></array>
</dict></plist>`
	if got := PolicyDescription([]byte(mixed), "en"); got != "right" {
		t.Errorf("got %q", got)
	}
}

func TestPolicyDescriptionWithTheCategoriesInReverseOrder(t *testing.T) {
	reverse := `<?xml version="1.0"?><plist><dict>
<key>policyCategoryPasswordContent</key><array><dict>
<key>policyContentDescription</key><dict><key>en</key><string>right</string></dict></dict></array>
<key>policyCategoryAuthentication</key><array><dict>
<key>policyContentDescription</key><dict><key>en</key><string>wrong category</string></dict></dict></array>
</dict></plist>`
	if got := PolicyDescription([]byte(reverse), "en"); got != "right" {
		t.Errorf("got %q", got)
	}
	// a rule of another category after an empty password category is not it
	empty := `<?xml version="1.0"?><plist><dict>
<key>policyCategoryPasswordContent</key><array></array>
<key>policyCategoryAuthentication</key><array><dict>
<key>policyContentDescription</key><dict><key>en</key><string>wrong category</string></dict></dict></array>
</dict></plist>`
	if got := PolicyDescription([]byte(empty), "en"); got != "" {
		t.Errorf("got %q", got)
	}
}
