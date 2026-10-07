package doctor

import (
	"bytes"
	"encoding/xml"
	"strings"
)

// PolicyDescription picks, from the output of `pwpolicy -getaccountpolicies`,
// the description of the host's password rules for the language lang (a locale
// such as "de_DE.UTF-8"), falling back to "en". It does not evaluate the policy.
// It returns "" for anything it cannot read.
//
// Verified on a development Mac: the output is a plist whose key
// policyCategoryPasswordContent holds dicts with a policyContent rule and a
// policyContentDescription dict of localized strings keyed by language.
// UNVERIFIED: hosts whose policy is set by MDM or a configuration profile (the
// key layout and the languages there are not known).
func PolicyDescription(out []byte, lang string) string {
	if i := bytes.Index(out, []byte("<?xml")); i >= 0 {
		out = out[i:]
	}
	dec := xml.NewDecoder(bytes.NewReader(out))
	texts := map[string]string{}
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "key" {
			continue
		}
		var k string
		if dec.DecodeElement(&k, &se) != nil || k != "policyContentDescription" {
			continue
		}
		var key string
		depth := 0
		for depth >= 0 {
			t, err := dec.Token()
			if err != nil {
				return ""
			}
			switch e := t.(type) {
			case xml.StartElement:
				switch e.Name.Local {
				case "dict":
					depth++
				case "key", "string":
					var v string
					if dec.DecodeElement(&v, &e) != nil {
						return ""
					}
					if e.Name.Local == "key" {
						key = v
					} else if depth == 1 {
						texts[key] = strings.TrimSpace(v)
					}
				}
			case xml.EndElement:
				if e.Name.Local == "dict" {
					depth--
					if depth == 0 {
						depth = -1
					}
				}
			}
		}
		break
	}
	l := lang
	if i := strings.IndexAny(l, ".@"); i >= 0 {
		l = l[:i]
	}
	for _, c := range []string{l, strings.SplitN(l, "_", 2)[0], "en"} {
		if v := texts[c]; v != "" && c != "" {
			return v
		}
	}
	return ""
}
