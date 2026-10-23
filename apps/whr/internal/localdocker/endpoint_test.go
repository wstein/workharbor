// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/doctor"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
)

func TestLocalEndpointUsesDoctorCandidateOrder(t *testing.T) {
	var got []string
	p := doctor.Ports{Home: func() (string, error) { return "/home/test", nil }, Getenv: func(k string) string {
		if k == "XDG_RUNTIME_DIR" {
			return "/run/test"
		}
		return ""
	}, DialUnix: func(_ context.Context, path string) error {
		got = append(got, path)
		if path == "/home/test/.rd/docker.sock" {
			return nil
		}
		return os.ErrNotExist
	}}
	endpoint, err := ResolveEndpoint(context.Background(), p)
	want := []string{"/var/run/docker.sock", "/run/test/docker.sock", "/home/test/.orbstack/run/docker.sock", "/home/test/.docker/run/docker.sock", "/home/test/.colima/default/docker.sock", "/home/test/.rd/docker.sock"}
	if err != nil || endpoint != "unix:///home/test/.rd/docker.sock" || !reflect.DeepEqual(got, want) {
		t.Fatalf("endpoint=%q err=%v dials=%v", endpoint, err, got)
	}
	p.Getenv = func(k string) string {
		if k == "DOCKER_HOST" {
			return "UNIX:///custom/docker.sock"
		}
		return ""
	}
	got = nil
	p.DialUnix = func(_ context.Context, path string) error { got = append(got, path); return nil }
	endpoint, err = ResolveEndpoint(context.Background(), p)
	if err != nil || endpoint != "unix:///custom/docker.sock" || !reflect.DeepEqual(got, []string{"/custom/docker.sock"}) {
		t.Fatalf("override %q %v %v", endpoint, err, got)
	}
}

func TestLocalEndpointRefusesNonUnixBeforeDocker(t *testing.T) {
	for _, host := range []string{"tcp://secret.invalid:2375", "ssh://secret.invalid", "npipe://pipe/docker_engine", "unix://relative", "garbage"} {
		t.Run(host, func(t *testing.T) {
			p := doctor.Ports{Getenv: func(k string) string {
				if k == "DOCKER_HOST" {
					return host
				}
				return ""
			}, DialUnix: func(context.Context, string) error { t.Fatal("unsupported endpoint was dialed"); return nil }}
			_, err := ResolveEndpoint(context.Background(), p)
			var ee *exitcode.Error
			if !errors.As(err, &ee) || ee.Path.Code() != 68 {
				t.Fatalf("want 68, got %v", err)
			}
		})
	}
}

func TestLocalDiscoverySharesFiveSecondBudget(t *testing.T) {
	start := time.Now()
	now := start
	p := doctor.Ports{Now: func() time.Time { return now }, Home: func() (string, error) { return "/home/test", nil }, DialUnix: func(ctx context.Context, _ string) error {
		d, ok := ctx.Deadline()
		if !ok || time.Until(d) > 501*time.Millisecond {
			t.Fatal("doctor per-candidate cap missing")
		}
		now = now.Add(5 * time.Second)
		return os.ErrNotExist
	}}
	_, err := ResolveEndpoint(context.Background(), p)
	if err == nil {
		t.Fatal("exhausted discovery must refuse")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = ResolveEndpoint(ctx, doctor.Ports{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}
