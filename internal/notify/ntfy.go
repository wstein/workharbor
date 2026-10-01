package notify

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Secrets is the credential service: the topic and the token live there, never
// in the repository, the configuration file or a log (design §9.4, §7.3).
type Secrets interface {
	Get(name string) (string, error)
}

// Names of the secrets the ntfy channel reads.
const (
	SecretTopic = "ntfy.topic" //nolint:gosec // the name of a secret in the credential service, not a secret
	SecretToken = "ntfy.token" //nolint:gosec // the name of a secret in the credential service, not a secret
)

// MinTopicLength is the shortest topic accepted: a topic is the only thing
// that keeps a public ntfy.sh channel private, so it must be long and random.
const MinTopicLength = 20

// NewTopic returns a long random topic for `whr doctor` to store in the
// credential service.
func NewTopic() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "whr-" + strings.ToLower(strings.TrimRight(base32.StdEncoding.EncodeToString(b), "=")), nil
}

// Ntfy delivers messages to an ntfy server (ntfy.sh or self-hosted).
type Ntfy struct {
	Server  string // base URL, https://ntfy.sh by default
	Secrets Secrets
	BaseURL string // the API address the link opens (D29)
	Client  *http.Client
}

// Notify sends one generic message: the title names the product, the body is
// the kind and the task ID, and the Click header is the link. Errors never
// contain the topic or the token.
func (n Ntfy) Notify(ctx context.Context, m Message) error {
	topic, err := n.Secrets.Get(SecretTopic)
	if err != nil || topic == "" {
		return errors.New("ntfy: no topic in the credential service")
	}
	if len(topic) < MinTopicLength || strings.ContainsAny(topic, "/?# \t\n") {
		return errors.New("ntfy: the topic is too short or has characters that are not allowed")
	}
	server := strings.TrimRight(n.Server, "/")
	if server == "" {
		server = "https://ntfy.sh"
	}
	u, err := url.Parse(server)
	if err != nil || (u.Scheme != "https" && u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost") {
		return errors.New("ntfy: the server must be an https URL")
	}
	body := fmt.Sprintf("%s: task %s", m.Kind, m.TaskID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server+"/"+topic, strings.NewReader(body))
	if err != nil {
		return errors.New("ntfy: cannot build the request")
	}
	req.Header.Set("Title", "workharbor")
	req.Header.Set("Click", Link(n.BaseURL, m))
	req.Header.Set("Tags", string(m.Kind))
	if token, _ := n.Secrets.Get(SecretToken); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := n.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		// The URL holds the topic: report the failure without it.
		return errors.New("ntfy: the server could not be reached")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("ntfy: the server answered %d", resp.StatusCode)
	}
	return nil
}
