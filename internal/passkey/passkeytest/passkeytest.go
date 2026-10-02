// Package passkeytest is a virtual WebAuthn authenticator for tests: an ES256 key
// that makes attestations of format "none" and assertions the way a platform
// authenticator does, so tests run the real verification of the WebAuthn library
// against it without a browser.
package passkeytest

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-webauthn/webauthn/protocol"
)

// Authn is a virtual authenticator. UV and the backup flags are what it claims.
type Authn struct {
	key            *ecdsa.PrivateKey
	CredID         []byte
	Counter        uint32
	UV             bool
	BackupEligible bool
	BackupState    bool
	userHandle     []byte
}

// New returns an authenticator that holds credentials for the given user handle.
func New(t *testing.T, userHandle []byte) *Authn {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id := make([]byte, 32)
	_, _ = rand.Read(id)
	return &Authn{key: k, CredID: id, UV: true, userHandle: userHandle}
}

// ---- a minimal CBOR encoder: unsigned and negative integers, byte and text strings,
// and maps, which is all a COSE key and an attestation object are ----

type kv struct{ k, v any }

func head(major byte, n uint64) []byte {
	switch {
	case n < 24:
		return []byte{major<<5 | byte(n)}
	case n < 256:
		return []byte{major<<5 | 24, byte(n)}
	default:
		return []byte{major<<5 | 25, byte(n >> 8 & 0xff), byte(n & 0xff)} //nolint:gosec // masked
	}
}

func cbor(v any) []byte {
	switch x := v.(type) {
	case int:
		if x >= 0 {
			return head(0, uint64(x))
		}
		return head(1, uint64(-1-x))
	case []byte:
		return append(head(2, uint64(len(x))), x...)
	case string:
		return append(head(3, uint64(len(x))), x...)
	case []kv:
		out := head(5, uint64(len(x)))
		for _, e := range x {
			out = append(out, cbor(e.k)...)
			out = append(out, cbor(e.v)...)
		}
		return out
	}
	panic("cbor: unsupported type")
}

// B64 is base64url without padding, as WebAuthn writes it.
func B64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func (a *Authn) flags(attested bool) byte {
	f := byte(0x01) // user present
	if a.UV {
		f |= 0x04
	}
	if a.BackupEligible {
		f |= 0x08
	}
	if a.BackupState {
		f |= 0x10
	}
	if attested {
		f |= 0x40
	}
	return f
}

func (a *Authn) authData(rpID string, attested bool) []byte {
	h := sha256.Sum256([]byte(rpID))
	out := append([]byte(nil), h[:]...)
	out = append(out, a.flags(attested))
	var c [4]byte
	binary.BigEndian.PutUint32(c[:], a.Counter)
	out = append(out, c[:]...)
	if attested {
		out = append(out, make([]byte, 16)...)                                   // AAGUID
		out = append(out, byte(len(a.CredID)>>8&0xff), byte(len(a.CredID)&0xff)) //nolint:gosec // masked
		out = append(out, a.CredID...)
		pub, _ := a.key.PublicKey.Bytes() // 0x04, X, Y
		x, y := pub[1:33], pub[33:]
		out = append(out, cbor([]kv{{1, 2}, {3, -7}, {-1, 1}, {-2, x}, {-3, y}})...)
	}
	return out
}

func clientData(typ string, challenge protocol.URLEncodedBase64, origin string) []byte {
	b, _ := json.Marshal(map[string]any{"type": typ, "challenge": challenge.String(), "origin": origin, "crossOrigin": false})
	return b
}

// Post makes a JSON POST request of a body.
func Post(body any) *http.Request {
	b, _ := json.Marshal(body)
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/", bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	return r
}

// Register answers a registration challenge as the browser would relay it.
func (a *Authn) Register(opts *protocol.CredentialCreation, origin string) *http.Request {
	cd := clientData("webauthn.create", opts.Response.Challenge, origin)
	att := cbor([]kv{{"fmt", "none"}, {"attStmt", []kv{}}, {"authData", a.authData(opts.Response.RelyingParty.ID, true)}})
	return Post(map[string]any{
		"id": B64(a.CredID), "rawId": B64(a.CredID), "type": "public-key",
		"response": map[string]any{"clientDataJSON": B64(cd), "attestationObject": B64(att)},
	})
}

// Assert answers an authentication challenge; the counter goes up by one first.
func (a *Authn) Assert(opts *protocol.CredentialAssertion, origin string) *http.Request {
	a.Counter++
	return a.AssertAt(opts.Response.Challenge, opts.Response.RelyingPartyID, origin)
}

// AssertAt answers a challenge for an RP ID and an origin with the counter as it is.
func (a *Authn) AssertAt(challenge protocol.URLEncodedBase64, rpID, origin string) *http.Request {
	cd := clientData("webauthn.get", challenge, origin)
	ad := a.authData(rpID, false)
	h := sha256.Sum256(cd)
	sum := sha256.Sum256(append(append([]byte(nil), ad...), h[:]...))
	sig, _ := ecdsa.SignASN1(rand.Reader, a.key, sum[:])
	return Post(map[string]any{
		"id": B64(a.CredID), "rawId": B64(a.CredID), "type": "public-key",
		"response": map[string]any{"clientDataJSON": B64(cd), "authenticatorData": B64(ad), "signature": B64(sig), "userHandle": B64(a.userHandle)},
	})
}
