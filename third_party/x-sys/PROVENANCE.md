# Go x/sys provenance

The whr CLI directly pins golang.org/x/sys v0.49.0 for descriptor-relative,
no-follow leaf opens on Unix. Go os.Root follows in-root symbolic links even
when its caller supplies O_NOFOLLOW; syscall.Openat is unavailable on Darwin.
The unix package supplies portable Openat, nonblocking flag control and close.
No module source is vendored. Exact module checksums are in apps/whr/go.sum;
the same version was already pinned by apps/host_agent.

Primary source: https://cs.opensource.google/go/x/sys/+/refs/tags/v0.49.0:
Module: https://pkg.go.dev/golang.org/x/sys@v0.49.0/unix

The exact cached module LICENSE (BSD-3-Clause) and PATENTS grant are retained
beside this file. Preserve both in later T023 artifact assembly. These notices
apply to this dependency; WorkHarbor remains EUPL-1.2. No build or distribution
was performed to add the notices, and no host-agent dependency audit is claimed.
