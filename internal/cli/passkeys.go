package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"time"

	"github.com/spf13/cobra"
)

type passkeyRow struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	CreatedAt      time.Time `json:"created_at"`
	LastUsed       time.Time `json:"last_used"`
	BackedUp       bool      `json:"backed_up"`
	BackupEligible bool      `json:"backup_eligible"`
}

// newPasskey builds `whr passkey add|ls|rm` (provisional, issue #101, D45). A
// passkey is enrolled and revoked only here, on the host, with the API token:
// the web UI has no way to do either.
func newPasskey(s *state) *cobra.Command {
	pk := &cobra.Command{Use: "passkey", Short: "Passkeys that sign in to the web UI and answer reviews (provisional)"}
	group(pk)

	add := &cobra.Command{
		Use:   "add [name]",
		Short: "Print a one-time link that enrols a passkey; it works once, for 5 minutes",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := s.api()
			if err != nil {
				return err
			}
			body := map[string]string{}
			if len(args) == 1 {
				body["name"] = args[0]
			}
			raw, data, err := c.Do(cmd.Context(), "POST", "/v1/passkeys/enrolments", body, "")
			if err != nil {
				return err
			}
			var r struct {
				URL     string    `json:"url"`
				Expires time.Time `json:"expires_at"`
			}
			if err := json.Unmarshal(data, &r); err != nil {
				return fmt.Errorf("the enrolment is not what this whr expects: %w", err)
			}
			fmt.Fprintf(s.env.Stderr, "open this link in the browser on the device that will hold the passkey; it works once and ends at %s\n", r.Expires.Local().Format("15:04:05"))
			return s.emit(raw, func(w io.Writer) error { _, err := fmt.Fprintln(w, r.URL); return err })
		},
	}

	ls := &cobra.Command{
		Use:   "ls",
		Short: "List the enrolled passkeys",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := s.api()
			if err != nil {
				return err
			}
			raw, data, err := c.Do(cmd.Context(), "GET", "/v1/passkeys", nil, "")
			if err != nil {
				return err
			}
			var rows []passkeyRow
			if err := json.Unmarshal(data, &rows); err != nil {
				return fmt.Errorf("the passkey list is not what this whr expects: %w", err)
			}
			return s.emit(raw, func(w io.Writer) error {
				out := make([][]string, len(rows))
				for i, r := range rows {
					last := "never"
					if !r.LastUsed.IsZero() {
						last = r.LastUsed.Local().Format("2006-01-02 15:04")
					}
					sync := "this device"
					if r.BackedUp {
						sync = "synced"
					}
					id := r.ID
					if len(id) > 12 {
						id = id[:12]
					}
					out[i] = []string{id, clean(r.Name), r.CreatedAt.Local().Format("2006-01-02"), last, sync}
				}
				return table(w, []string{"ID", "NAME", "ADDED", "LAST USED", "KEPT"}, out)
			})
		},
	}

	rm := &cobra.Command{
		Use:   "rm <id>",
		Short: "Revoke a passkey, by its ID or a unique prefix of it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := s.api()
			if err != nil {
				return err
			}
			raw, _, err := c.Do(cmd.Context(), "DELETE", "/v1/passkeys/"+url.PathEscape(args[0]), nil, newKey())
			if err != nil {
				return err
			}
			return s.emit(raw, func(w io.Writer) error { _, err := fmt.Fprintln(w, clean(args[0])); return err })
		},
	}
	pk.AddCommand(add, ls, rm)
	return pk
}
