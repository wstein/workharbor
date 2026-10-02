package githubapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/wstein/workharbor/internal/redact"
)

// Conversion is what workharbor keeps of GitHub's answer to a manifest code.
// The client secret and the webhook secret are not here: they are not needed
// (no OAuth, no webhook) and are dropped.
type Conversion struct {
	ID      int64
	Slug    string
	HTMLURL string
	PEM     []byte
}

var (
	codeRE = regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`)
	slugRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,62}$`)
)

// ValidCode reports whether code has the shape of a manifest code. The shape
// is unverified (not measured), so it is wide: it exists to keep a path
// separator or a query out of the URL.
func ValidCode(code string) bool { return codeRE.MatchString(code) }

// CheckBaseURL accepts https, or http to a loopback address (a test double).
func CheckBaseURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("%q is not a usable base URL", raw)
	}
	switch u.Scheme {
	case "https":
	case "http":
		host := u.Hostname()
		if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return nil, fmt.Errorf("%q: http is only for a loopback address", raw)
		}
	default:
		return nil, fmt.Errorf("%q: the scheme must be https", raw)
	}
	return u, nil
}

// Convert exchanges the one-time code for the App
// (POST /app-manifests/{code}/conversions; no credential is sent, the code is
// the credential). The private key and both secrets are registered with rd
// before anything else happens to them. An error never carries the code, a
// header or GitHub's body.
func Convert(ctx context.Context, hc *http.Client, apiBase *url.URL, code string, rd *redact.Redactor) (Conversion, error) {
	if !ValidCode(code) {
		return Conversion{}, errors.New("the code is not valid")
	}
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	u := *apiBase
	u.Path = strings.TrimRight(apiBase.Path, "/") + "/app-manifests/" + code + "/conversions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), nil)
	if err != nil {
		return Conversion{}, errors.New("building the conversion request failed")
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "workharbor")
	resp, err := hc.Do(req)
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err // the URL holds the code
		}
		return Conversion{}, fmt.Errorf("GitHub did not answer the conversion: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Conversion{}, fmt.Errorf("reading GitHub's conversion answer: %w", err)
	}
	var out struct {
		ID            int64  `json:"id"`
		Slug          string `json:"slug"`
		HTMLURL       string `json:"html_url"`
		PEM           string `json:"pem"`
		ClientSecret  string `json:"client_secret"`
		WebhookSecret string `json:"webhook_secret"`
	}
	decodeErr := json.Unmarshal(raw, &out)
	if rd != nil { // learn every secret first, so nothing below can leak one
		for _, s := range []string{out.PEM, out.ClientSecret, out.WebhookSecret} {
			if s != "" {
				rd.Add(s)
				rd.Add(strings.Join(strings.Fields(s), ""))
			}
		}
	}
	out.ClientSecret, out.WebhookSecret = "", "" // dropped: not needed
	if resp.StatusCode != http.StatusCreated {
		return Conversion{}, fmt.Errorf("GitHub refused the conversion: %d (a code works once and for about an hour)", resp.StatusCode)
	}
	if decodeErr != nil || out.ID <= 0 || out.PEM == "" || !slugRE.MatchString(out.Slug) {
		return Conversion{}, errors.New("GitHub's conversion answer is not what was expected")
	}
	return Conversion{ID: out.ID, Slug: out.Slug, HTMLURL: out.HTMLURL, PEM: []byte(out.PEM)}, nil
}
