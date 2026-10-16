// SPDX-License-Identifier: EUPL-1.2
package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func traceFiles(t *testing.T, log *[]string, failRemove bool) {
	t.Helper()
	before := identityFiles
	ops := before
	ops.openDir = func(d string) (durableFile, error) {
		*log = append(*log, "opendir:"+d)
		return before.openDir(d)
	}
	ops.link = func(a, b string) error { *log = append(*log, "link"); return before.link(a, b) }
	ops.remove = func(p string) error {
		*log = append(*log, "remove")
		if failRemove {
			return os.ErrPermission
		}
		return before.remove(p)
	}
	identityFiles = ops
	t.Cleanup(func() { identityFiles = before })
}

func TestPrepareSyncsParentAfterMkdirAndRemovesTempBeforeDirectorySync(t *testing.T) {
	root := physicalTemp(t)
	dir := filepath.Join(root, "identity")
	var log []string
	traceFiles(t, &log, false)
	if _, e := Prepare(dir, "https://localhost:8443", ""); e != nil {
		t.Fatal(e)
	}
	want := []string{"opendir:" + root, "link", "remove", "opendir:" + dir}
	if len(log) != len(want) {
		t.Fatalf("order %v", log)
	}
	for i := range want {
		if log[i] != want[i] {
			t.Fatalf("order %v", log)
		}
	}
	left, _ := filepath.Glob(filepath.Join(dir, ".identity-*"))
	if len(left) != 0 {
		t.Fatal("temp key copy remains")
	}
}

func TestPrepareRefusesWhenTempRemovalFails(t *testing.T) {
	var log []string
	traceFiles(t, &log, true)
	if _, e := Prepare(filepath.Join(physicalTemp(t), "identity"), "https://localhost:8443", ""); e == nil {
		t.Fatal("reported success without durable removal")
	}
}

func writeCA(t *testing.T, mode os.FileMode) string {
	t.Helper()
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "ca"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, e := x509.CreateCertificate(rand.Reader, tpl, tpl, pub, key)
	if e != nil {
		t.Fatal(e)
	}
	p := filepath.Join(physicalTemp(t), "ca.pem")
	if e = os.WriteFile(p, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.Chmod(p, mode); e != nil {
		t.Fatal(e)
	}
	return p
}

func TestServerCAPathMustBeAbsoluteAndClean(t *testing.T) {
	for _, p := range []string{"ca.pem", "./ca.pem", "/tmp/../tmp/ca.pem", "/tmp//ca.pem"} {
		if _, e := Prepare(filepath.Join(physicalTemp(t), "identity"), "https://localhost:8443", p); e == nil {
			t.Fatalf("Prepare accepted %q", p)
		}
	}
	dir := filepath.Join(physicalTemp(t), "identity")
	s, e := Prepare(dir, "https://localhost:8443", writeCA(t, 0600))
	if e != nil {
		t.Fatal(e)
	}
	s.ServerCAFile = "relative.pem"
	if validate(s) == nil || Save(dir, s) == nil {
		t.Fatal("validate accepted relative CA path")
	}
}

func TestServerCAFileRefusesWritableAndSymlinkAndAcceptsPrivate(t *testing.T) {
	for _, mode := range []os.FileMode{0620, 0602, 0666} {
		s := &State{ServerCAFile: writeCA(t, mode)}
		if _, e := TLSConfig(s, false); e == nil {
			t.Fatalf("accepted CA mode %o", mode)
		}
	}
	for _, mode := range []os.FileMode{0600, 0644} {
		if _, e := TLSConfig(&State{ServerCAFile: writeCA(t, mode)}, false); e != nil {
			t.Fatalf("refused CA mode %o", mode)
		}
	}
	link := filepath.Join(physicalTemp(t), "link.pem")
	if e := os.Symlink(writeCA(t, 0600), link); e != nil {
		t.Fatal(e)
	}
	if _, e := TLSConfig(&State{ServerCAFile: link}, false); e == nil {
		t.Fatal("accepted symlink CA")
	}
}

func TestPrepareRefusesWhenParentSyncAfterMkdirFails(t *testing.T) {
	for _, stage := range []string{"parent_open", "parent_sync", "parent_close"} {
		dir := filepath.Join(physicalTemp(t), "identity")
		reset := failFiles(t, stage)
		s, e := Prepare(dir, "https://localhost:8443", "")
		reset()
		if s != nil || e == nil {
			t.Fatalf("%s: success without durable directory", stage)
		}
		if _, err := os.Lstat(filepath.Join(dir, "identity.json")); err == nil {
			t.Fatalf("%s: identity written before directory was durable", stage)
		}
	}
}

func TestPrepareRetrySyncsParentWhenDirectoryAlreadyExists(t *testing.T) {
	dir := filepath.Join(physicalTemp(t), "identity")
	reset := failFiles(t, "parent_sync")
	_, e := Prepare(dir, "https://localhost:8443", "")
	reset()
	if e == nil {
		t.Fatal("first attempt should fail")
	}
	var log []string
	traceFiles(t, &log, false)
	if _, e = Prepare(dir, "https://localhost:8443", ""); e != nil {
		t.Fatal(e)
	}
	if len(log) == 0 || log[0] != "opendir:"+filepath.Dir(dir) {
		t.Fatalf("retry skipped parent sync: %v", log)
	}
}

func TestServerCAFifoRefusedWithoutBlocking(t *testing.T) {
	p := filepath.Join(physicalTemp(t), "ca.fifo")
	if e := syscall.Mkfifo(p, 0600); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { _, e := TLSConfig(&State{ServerCAFile: p}, false); done <- e }()
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("accepted FIFO CA")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("FIFO CA blocked TLS setup")
	}
}
