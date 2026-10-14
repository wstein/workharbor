// SPDX-License-Identifier: EUPL-1.2
package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/base64"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPreparePersistsPrivatePossessionBeforeEnrollment(t *testing.T) {
	dir := filepath.Join(physicalTemp(t), "identity")
	state, err := Prepare(dir, "https://localhost:8443", "")
	if err != nil {
		t.Fatal(err)
	}
	if state.Active != nil || state.Pending == nil || state.OwnerID != "" || state.HostID != "" {
		t.Fatal("invented assignment")
	}
	der, err := base64.StdEncoding.DecodeString(state.Pending.CSR)
	if err != nil {
		t.Fatal(err)
	}
	csr, err := x509.ParseCertificateRequest(der)
	if err != nil {
		t.Fatal(err)
	}
	if err = csr.CheckSignature(); err != nil {
		t.Fatal(err)
	}
	if csr.Subject.CommonName != "" || len(csr.Subject.Names) != 0 || len(csr.Extensions) != 0 {
		t.Fatal("CSR invented authority")
	}
	if _, ok := csr.PublicKey.(ed25519.PublicKey); !ok {
		t.Fatal("wrong key")
	}
	st, err := os.Stat(filepath.Join(dir, "identity.json"))
	if err != nil || st.Mode().Perm() != 0600 {
		t.Fatal("unsafe file")
	}
	reloaded, err := Load(dir)
	if err != nil || reloaded.Pending.PrivateKey != state.Pending.PrivateKey {
		t.Fatal("lost pending possession")
	}
	if _, err = Prepare(dir, "https://localhost:8443", ""); err == nil {
		t.Fatal("replaced identity")
	}
}
func TestPrivateStateUnsafeFilesSymlinksAndSchemaFailClosed(t *testing.T) {
	for _, tc := range []string{"mode", "symlink", "schema", "corrupt", "parent"} {
		t.Run(tc, func(t *testing.T) {
			dir := filepath.Join(physicalTemp(t), "identity")
			if _, err := Prepare(dir, "https://localhost:8443", ""); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "identity.json")
			switch tc {
			case "mode":
				os.Chmod(path, 0644)
			case "symlink":
				os.Rename(path, path+".saved")
				os.Symlink(path+".saved", path)
			case "schema":
				os.WriteFile(path, []byte(`{"schema_version":99,"private_key":"SECRET_SENTINEL"}`), 0600)
			case "corrupt":
				os.WriteFile(path, []byte(`{"broken"`), 0600)
			case "parent":
				os.Chmod(dir, 0755)
			}
			_, err := Load(dir)
			if err == nil {
				t.Fatal("accepted unsafe state")
			}
			if strings.Contains(err.Error(), "SECRET_SENTINEL") {
				t.Fatal("leaked state")
			}
		})
	}
}
func TestPlaintextAndCredentialEndpointsRefusedBeforeStateWrite(t *testing.T) {
	for _, endpoint := range []string{"http://localhost:8443", "https://secret@localhost:8443", "https://localhost:8443/?token=secret", "https://localhost:8443/#secret"} {
		dir := filepath.Join(physicalTemp(t), "identity")
		if _, err := Prepare(dir, endpoint, ""); err == nil {
			t.Fatal("unsafe endpoint")
		}
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatal("wrote before validation")
		}
	}
}

func physicalTemp(t *testing.T) string {
	t.Helper()
	p, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func TestPrivateStateRequiresExactRecursiveNamesAndPresence(t *testing.T) {
	for _, name := range []string{"case", "missing", "collision", "null", "type", "decoded_duplicate"} {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(physicalTemp(t), "identity")
			if _, e := Prepare(dir, "https://localhost:8443", ""); e != nil {
				t.Fatal(e)
			}
			path := filepath.Join(dir, "identity.json")
			bytes, e := os.ReadFile(path)
			if e != nil {
				t.Fatal(e)
			}
			text := string(bytes)
			switch name {
			case "case":
				text = strings.Replace(text, `"schema_version"`, `"SCHEMA_VERSION"`, 1)
			case "missing":
				text = strings.Replace(text, `"server_ca_file":"",`, "", 1)
			case "null":
				text = strings.Replace(text, `"server_ca_file":""`, `"server_ca_file":null`, 1)
			case "type":
				text = strings.Replace(text, `"server_ca_file":""`, `"server_ca_file":12`, 1)
			case "decoded_duplicate":
				text = strings.Replace(text, `"schema_version":1`, `"schema_version":1,"schema_\u0076ersion":1`, 1)
			case "collision":
				text = strings.Replace(text, `"schema_version":1`, `"schema_version":1,"SCHEMA_VERSION":1`, 1)
			}
			os.WriteFile(path, []byte(text), 0600)
			if _, e = Load(dir); e == nil {
				t.Fatal("accepted private schema alias/missing field")
			}
		})
	}
}
func TestAncestorSymlinkRefusedBeforeIdentityWrites(t *testing.T) {
	root := physicalTemp(t)
	real := filepath.Join(root, "real")
	os.Mkdir(real, 0700)
	alias := filepath.Join(root, "alias")
	os.Symlink(real, alias)
	if _, e := Prepare(filepath.Join(alias, "identity"), "https://localhost:8443", ""); e == nil {
		t.Fatal("followed ancestor symlink")
	}
	if _, e := os.Stat(filepath.Join(real, "identity")); !os.IsNotExist(e) {
		t.Fatal("wrote through alias")
	}
}

func signedMaterial(t *testing.T, change func(*x509.Certificate, *x509.Certificate)) *Material {
	t.Helper()
	issuerPub, issuerKey, _ := ed25519.GenerateKey(rand.Reader)
	leafPub, leafKey, _ := ed25519.GenerateKey(rand.Reader)
	start := time.Now().Add(-time.Minute).Truncate(time.Second)
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "WorkHarbor client issuer"}, NotBefore: start.Add(-time.Hour), NotAfter: start.Add(-time.Hour).Add(5 * 365 * 24 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "22222222-2222-4222-8222-222222222222"}, NotBefore: start, NotAfter: start.Add(24 * time.Hour), BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	if change != nil {
		change(ca, leaf)
	}
	caDER, e := x509.CreateCertificate(rand.Reader, ca, ca, issuerPub, issuerKey)
	if e != nil {
		t.Fatal(e)
	}
	parsed, e := x509.ParseCertificate(caDER)
	if e != nil {
		t.Fatal(e)
	}
	leafDER, e := x509.CreateCertificate(rand.Reader, leaf, parsed, leafPub, issuerKey)
	if e != nil {
		t.Fatal(e)
	}
	keyDER, _ := x509.MarshalPKCS8PrivateKey(leafKey)
	return &Material{IdentityID: "33333333-3333-4333-8333-333333333333", PrivateKey: base64.StdEncoding.EncodeToString(keyDER), Certificate: base64.StdEncoding.EncodeToString(leafDER), IssuerCertificate: base64.StdEncoding.EncodeToString(caDER)}
}
func TestExactLeafAndIssuerProfilesRejectExtraPurposesAndCriticalExtensions(t *testing.T) {
	host := "22222222-2222-4222-8222-222222222222"
	if _, e := material(signedMaterial(t, nil), host, time.Now()); e != nil {
		t.Fatal(e)
	}
	cases := []struct {
		name   string
		change func(*x509.Certificate, *x509.Certificate)
	}{
		{"issuer_oversized", func(c, l *x509.Certificate) {
			c.ExtraExtensions = []pkix.Extension{{Id: asn1.ObjectIdentifier{1, 2, 3, 4}, Value: make([]byte, 12000)}}
		}},
		{"leaf_oversized", func(c, l *x509.Certificate) {
			l.ExtraExtensions = []pkix.Extension{{Id: asn1.ObjectIdentifier{1, 2, 3, 4}, Value: make([]byte, 12000)}}
		}},
		{"leaf_unknown_eku", func(c, l *x509.Certificate) { l.UnknownExtKeyUsage = []asn1.ObjectIdentifier{{1, 2, 3, 4}} }},
		{"issuer_eku", func(c, l *x509.Certificate) { c.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth} }},
		{"critical", func(c, l *x509.Certificate) {
			l.ExtraExtensions = []pkix.Extension{{Id: asn1.ObjectIdentifier{1, 2, 3, 4}, Critical: true, Value: []byte{5, 0}}}
		}},
		{"extra_subject", func(c, l *x509.Certificate) { l.Subject.Organization = []string{"foreign"} }},
		{"issuer_path", func(c, l *x509.Certificate) { c.MaxPathLen = 0; c.MaxPathLenZero = true }},
		{"issuer_duration", func(c, l *x509.Certificate) { c.NotAfter = c.NotAfter.Add(-time.Hour) }},
		{"leaf_duration", func(c, l *x509.Certificate) { l.NotAfter = l.NotBefore.Add(time.Hour) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, e := material(signedMaterial(t, tc.change), host, time.Now()); e == nil {
				t.Fatal("accepted extra identity purpose/profile")
			}
		})
	}
}
