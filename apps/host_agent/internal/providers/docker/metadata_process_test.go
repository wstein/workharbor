// SPDX-License-Identifier: EUPL-1.2
package docker

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestMetadataBufferCancelsBeforeCaptureExceedsLimit(t *testing.T) {
	canceled := false
	b := metadataBuffer{limit: 3, cancel: func() { canceled = true }}
	if n, e := b.Write([]byte("abc")); n != 3 || e != nil {
		t.Fatal(n, e)
	}
	if _, e := b.Write([]byte("d")); e == nil || !canceled || !b.overflow || b.buffer.String() != "abc" {
		t.Fatal("overflow not canceled before capture")
	}
}
func TestMetadataOwnedChildHelper(t *testing.T) {
	if os.Getenv("WHR_TEST_METADATA_CHILD") != "overflow" {
		return
	}
	os.Stdout.Write([]byte(strings.Repeat("x", metadataLimit+1)))
	for {
		time.Sleep(time.Second)
	}
}
func TestMetadataOverflowKillsAndReapsOwnedChild(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, e := captureMetadata(ctx, os.Args[0], []string{"-test.run=^TestMetadataOwnedChildHelper$"}, t.TempDir(), []string{"WHR_TEST_METADATA_CHILD=overflow"}, metadataLimit)
	errorCode(t, e, "metadata_unavailable")
	if ctx.Err() != nil {
		t.Fatal("overflow waited for outer deadline")
	}
}
func TestRegistryPrivateEnvironmentCannotUseAmbientCredentialFallback(t *testing.T) {
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Chmod(root, 0700); e != nil {
		t.Fatal(e)
	}
	t.Setenv("REGISTRY_AUTH_FILE", "/host/secret")
	t.Setenv("DOCKER_CONFIG", "/host/docker")
	t.Setenv("AWS_ACCESS_KEY_ID", "CANARY")
	t.Setenv("HTTPS_PROXY", "http://host-proxy")
	stage, e := registryPrivate(root)
	if e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(stage)
	for _, file := range []string{"home/.docker/config.json", "docker/config.json"} {
		raw, e := os.ReadFile(filepath.Join(stage, file))
		if e != nil || string(raw) != "{}" {
			t.Fatal(file, e)
		}
		info, _ := os.Stat(filepath.Join(stage, file))
		if info.Mode().Perm() != 0600 {
			t.Fatal("nonprivate config")
		}
	}
	env := registryEnvironment(stage, "/tools/crane")
	expected := []string{"PATH=/tools", "HOME=" + filepath.Join(stage, "home"), "DOCKER_CONFIG=" + filepath.Join(stage, "docker"), "XDG_CONFIG_HOME=" + filepath.Join(stage, "config"), "XDG_RUNTIME_DIR=" + filepath.Join(stage, "runtime"), "LANG=C.UTF-8"}
	if !reflect.DeepEqual(env, expected) {
		t.Fatal("ambient environment escaped", env)
	}
}
