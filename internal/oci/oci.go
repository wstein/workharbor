// Package oci is the host's small client for fetching a devcontainer feature, an OCI
// artifact, from a registry (design D38, issue #108). It does only what that needs:
// resolve a reference to its manifest digest, and fetch the one layer by digest with
// a size cap and a timeout. Everything a registry sends is untrusted bytes: the
// manifest is hashed and compared, the blob is hashed while it streams and refused
// when it is longer than its declared size or the cap, a redirect must stay on https
// and on a public address, and the token realm must be the registry's own host.
package oci

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/wstein/workharbor/internal/egress"
)

// Limits and media types.
const (
	DefaultTimeout     = 60 * time.Second
	DefaultMaxManifest = 1 << 20
	DefaultMaxBlob     = 16 << 20
	maxRedirects       = 3
	// MediaManifest is the one manifest type a feature has.
	MediaManifest = "application/vnd.oci.image.manifest.v1+json"
	// MediaFeatureLayer is the layer type of a devcontainer feature: a plain tar.
	MediaFeatureLayer = "application/vnd.devcontainers.layer.v1+tar"
)

// Errors a caller can tell apart.
var (
	ErrBadRef    = errors.New("oci: not a usable reference")
	ErrDigest    = errors.New("oci: the content does not match its digest")
	ErrTooLarge  = errors.New("oci: the content is larger than allowed")
	ErrManifest  = errors.New("oci: the manifest is not what a feature has")
	ErrNotPublic = errors.New("oci: the registry address is not public")
)

var (
	hostRe   = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*(:[0-9]{1,5})?$`)
	repoRe   = regexp.MustCompile(`^[a-z0-9]+([._-][a-z0-9]+)*(/[a-z0-9]+([._-][a-z0-9]+)*)*$`)
	tagRe    = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]{0,127}$`)
	digestRe = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

// Ref is a registry, a repository and a tag or a digest.
type Ref struct {
	Registry string // host[:port], lower case
	Repo     string // for example devcontainers/features/node
	Tag      string // empty when Digest is set
	Digest   string // sha256:<hex>, when the reference pins one
}

// ParseRef reads registry/repo[:tag][@sha256:hex]. A reference without a tag or a
// digest is not accepted: a feature always names a version.
func ParseRef(s string) (Ref, error) {
	if s == "" || len(s) > 300 || strings.ContainsAny(s, " \t\n\r\\?#") || strings.HasPrefix(s, "-") {
		return Ref{}, fmt.Errorf("%w: %q", ErrBadRef, s)
	}
	var r Ref
	rest := s
	if i := strings.Index(rest, "@"); i >= 0 {
		r.Digest, rest = rest[i+1:], rest[:i]
		if !digestRe.MatchString(r.Digest) {
			return Ref{}, fmt.Errorf("%w: %q has no sha256 digest", ErrBadRef, s)
		}
	}
	slash := strings.Index(rest, "/")
	if slash < 0 {
		return Ref{}, fmt.Errorf("%w: %q has no registry", ErrBadRef, s)
	}
	r.Registry, rest = strings.ToLower(rest[:slash]), rest[slash+1:]
	if i := strings.LastIndex(rest, ":"); i >= 0 && !strings.Contains(rest[i:], "/") {
		r.Tag, rest = rest[i+1:], rest[:i]
	}
	r.Repo = rest
	switch {
	case !hostRe.MatchString(r.Registry), !repoRe.MatchString(r.Repo):
		return Ref{}, fmt.Errorf("%w: %q", ErrBadRef, s)
	case r.Tag == "" && r.Digest == "":
		return Ref{}, fmt.Errorf("%w: %q names no version", ErrBadRef, s)
	case r.Tag != "" && !tagRe.MatchString(r.Tag):
		return Ref{}, fmt.Errorf("%w: %q has a bad tag", ErrBadRef, s)
	}
	return r, nil
}

func (r Ref) String() string {
	s := r.Registry + "/" + r.Repo
	if r.Tag != "" {
		s += ":" + r.Tag
	}
	if r.Digest != "" {
		s += "@" + r.Digest
	}
	return s
}

// Descriptor is a blob a manifest names.
type Descriptor struct {
	MediaType string `json:"mediaType"`
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
}

// Manifest is a resolved image manifest. Digest is the sha256 of its exact bytes: the
// identity a feature is pinned to.
type Manifest struct {
	Digest string
	Layers []Descriptor
}

// Config configures a Client.
type Config struct {
	// HTTP is the transport. By default a client that follows few redirects, only to
	// https, and dials only public addresses.
	HTTP *http.Client
	// Timeout bounds every request. Default DefaultTimeout.
	Timeout time.Duration
	// MaxManifest and MaxBlob cap what is read. Defaults DefaultMaxManifest and
	// DefaultMaxBlob.
	MaxManifest, MaxBlob int64
	// BaseURL maps a registry host to its base URL; tests use it. Default https://host.
	BaseURL func(registry string) string
}

// Client fetches from registries.
type Client struct {
	cfg Config
}

// New returns a Client.
func New(cfg Config) *Client {
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultTimeout
	}
	if cfg.MaxManifest <= 0 {
		cfg.MaxManifest = DefaultMaxManifest
	}
	if cfg.MaxBlob <= 0 {
		cfg.MaxBlob = DefaultMaxBlob
	}
	if cfg.BaseURL == nil {
		cfg.BaseURL = func(h string) string { return "https://" + h }
	}
	if cfg.HTTP == nil {
		cfg.HTTP = publicClient()
	}
	// The redirect policy is ours whatever the transport: a bounded number of hops, never
	// from https to plain http, and the token goes only to the host it was made for
	// (net/http compares host names without the port).
	hc := *cfg.HTTP
	hc.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > maxRedirects {
			return errors.New("oci: too many redirects")
		}
		if via[0].URL.Scheme == "https" && req.URL.Scheme != "https" {
			return errors.New("oci: a redirect from https to plain http")
		}
		if req.URL.Host != via[0].URL.Host {
			req.Header.Del("Authorization")
		}
		return nil
	}
	cfg.HTTP = &hc
	return &Client{cfg: cfg}
}

// publicClient follows at most maxRedirects redirects, only to https, and refuses to
// connect to a loopback, private or otherwise non-public address, so a registry that
// redirects to the host's own network gets nowhere.
func publicClient() *http.Client {
	d := &net.Dialer{Timeout: 15 * time.Second, Control: func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		a, err := netip.ParseAddr(host)
		if err != nil || !egress.Public(a) {
			return ErrNotPublic
		}
		return nil
	}}
	return &http.Client{
		Transport: &http.Transport{DialContext: d.DialContext, Proxy: nil, TLSHandshakeTimeout: 15 * time.Second, ResponseHeaderTimeout: 30 * time.Second},
	}
}

// get does one authenticated GET, answering a token challenge once. The caller
// closes the body.
func (c *Client) get(ctx context.Context, ref Ref, path, accept string) (*http.Response, error) {
	u := c.cfg.BaseURL(ref.Registry) + path
	do := func(token string) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		if accept != "" {
			req.Header.Set("Accept", accept)
		}
		req.Header.Set("User-Agent", "workharbor")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		return c.cfg.HTTP.Do(req)
	}
	resp, err := do("")
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusUnauthorized {
		return resp, nil
	}
	challenge := resp.Header.Get("Www-Authenticate")
	_ = resp.Body.Close()
	token, err := c.token(ctx, ref, challenge)
	if err != nil {
		return nil, err
	}
	return do(token)
}

var paramRe = regexp.MustCompile(`(\w+)="([^"]*)"`)

// token gets an anonymous pull token from the realm a challenge names, which must be
// https on the registry's own host: a registry cannot send the client elsewhere.
func (c *Client) token(ctx context.Context, ref Ref, challenge string) (string, error) {
	params := map[string]string{}
	for _, m := range paramRe.FindAllStringSubmatch(challenge, -1) {
		params[strings.ToLower(m[1])] = m[2]
	}
	realm := params["realm"]
	ru, err := url.Parse(realm)
	if err != nil || realm == "" {
		return "", fmt.Errorf("oci: %s asked for a token without a realm", ref.Registry)
	}
	base, _ := url.Parse(c.cfg.BaseURL(ref.Registry))
	if ru.Host != base.Host || ru.Scheme != base.Scheme {
		return "", fmt.Errorf("oci: the token realm %q is not on %s", realm, ref.Registry)
	}
	q := ru.Query()
	if s := params["service"]; s != "" {
		q.Set("service", s)
	}
	q.Set("scope", "repository:"+ref.Repo+":pull")
	ru.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ru.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "workharbor")
	resp, err := c.cfg.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("oci: the token request answered %d", resp.StatusCode)
	}
	var v struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&v); err != nil {
		return "", fmt.Errorf("oci: the token answer: %w", err)
	}
	if v.Token != "" {
		return v.Token, nil
	}
	if v.AccessToken != "" {
		return v.AccessToken, nil
	}
	return "", errors.New("oci: the token answer holds no token")
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}

// Resolve fetches the manifest of a reference and returns its digest, the sha256 of
// the bytes received, with its layers. A reference that pins a digest must match it,
// and so must the registry's own Docker-Content-Digest header when it sends one. Only
// a plain OCI image manifest is accepted: an index, or a manifest with no layer, is
// not a feature.
func (c *Client) Resolve(ctx context.Context, ref Ref) (Manifest, error) {
	ctx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()
	id := ref.Tag
	if ref.Digest != "" {
		id = ref.Digest
	}
	resp, err := c.get(ctx, ref, "/v2/"+ref.Repo+"/manifests/"+id, MediaManifest)
	if err != nil {
		return Manifest{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return Manifest{}, fmt.Errorf("oci: %s: the manifest request answered %d", ref, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, c.cfg.MaxManifest+1))
	if err != nil {
		return Manifest{}, err
	}
	if int64(len(body)) > c.cfg.MaxManifest {
		return Manifest{}, fmt.Errorf("%w: the manifest of %s", ErrTooLarge, ref)
	}
	digest := sum(body)
	if ref.Digest != "" && digest != ref.Digest {
		return Manifest{}, fmt.Errorf("%w: %s is %s, not %s", ErrDigest, ref, digest, ref.Digest)
	}
	if h := resp.Header.Get("Docker-Content-Digest"); h != "" && h != digest {
		return Manifest{}, fmt.Errorf("%w: the registry says %s for a manifest that hashes to %s", ErrDigest, h, digest)
	}
	var m struct {
		SchemaVersion int          `json:"schemaVersion"`
		MediaType     string       `json:"mediaType"`
		Layers        []Descriptor `json:"layers"`
	}
	if err := json.Unmarshal(body, &m); err != nil {
		return Manifest{}, fmt.Errorf("%w: %v", ErrManifest, err) //nolint:errorlint // the cause is detail
	}
	if m.SchemaVersion != 2 || (m.MediaType != "" && m.MediaType != MediaManifest) || len(m.Layers) == 0 || len(m.Layers) > 8 {
		return Manifest{}, fmt.Errorf("%w: %s is not an OCI image manifest with layers", ErrManifest, ref)
	}
	for _, l := range m.Layers {
		if !digestRe.MatchString(l.Digest) || l.Size < 0 {
			return Manifest{}, fmt.Errorf("%w: a layer of %s has a bad digest or size", ErrManifest, ref)
		}
	}
	return Manifest{Digest: digest, Layers: m.Layers}, nil
}

// Blob streams a layer into w and checks it: no more than its declared size and the
// cap, and the sha256 of what was received must be the digest. On any failure the
// error says so, and the caller must discard what it wrote. The registry's redirect
// to its storage is followed under the client's redirect rules.
func (c *Client) Blob(ctx context.Context, ref Ref, d Descriptor, w io.Writer) error {
	if !digestRe.MatchString(d.Digest) {
		return fmt.Errorf("%w: %q", ErrBadRef, d.Digest)
	}
	limit := c.cfg.MaxBlob
	if d.Size > limit {
		return fmt.Errorf("%w: %s is %d bytes, the limit is %d", ErrTooLarge, d.Digest, d.Size, limit)
	}
	ctx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()
	resp, err := c.get(ctx, ref, "/v2/"+ref.Repo+"/blobs/"+d.Digest, "")
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("oci: the blob request answered %d", resp.StatusCode)
	}
	maxBytes := d.Size
	if maxBytes <= 0 || maxBytes > limit {
		maxBytes = limit
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(w, h), io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return err
	}
	if n > maxBytes {
		return fmt.Errorf("%w: %s is longer than its declared %d bytes", ErrTooLarge, d.Digest, maxBytes)
	}
	if got := "sha256:" + hex.EncodeToString(h.Sum(nil)); got != d.Digest {
		return fmt.Errorf("%w: %s hashes to %s", ErrDigest, d.Digest, got)
	}
	if d.Size > 0 && n != d.Size {
		return fmt.Errorf("%w: %s is %d bytes, not the declared %d", ErrDigest, d.Digest, n, d.Size)
	}
	return nil
}
