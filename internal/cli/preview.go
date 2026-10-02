package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"time"

	"github.com/spf13/cobra"
)

type previewRow struct {
	ID      string    `json:"id"`
	Task    string    `json:"task"`
	Port    int       `json:"port"`
	Listen  int       `json:"listen_port"`
	Expires time.Time `json:"expires_at"`
}

// newPreview builds `whr preview open|ls|link|close` (provisional, issue #72, D33):
// a dev server the agent runs in its environment, shown through the supervisor's
// preview proxy on a port and origin of its own.
func newPreview(s *state) *cobra.Command {
	pv := &cobra.Command{Use: "preview", Short: "Show a web app an agent runs in its environment (provisional)"}
	group(pv)

	open := &cobra.Command{
		Use:   "open <task> <port>",
		Short: "Open a preview of a declared port and print the link; it works once, for 5 minutes",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			port, err := strconv.Atoi(args[1])
			if err != nil {
				return fmt.Errorf("%q is not a port", args[1])
			}
			c, err := s.api()
			if err != nil {
				return err
			}
			raw, data, err := c.Do(cmd.Context(), "POST", "/v1/tasks/"+url.PathEscape(args[0])+"/previews", map[string]int{"port": port}, "")
			if err != nil {
				return err
			}
			var r struct {
				Preview previewRow `json:"preview"`
				URL     string     `json:"url"`
			}
			if err := json.Unmarshal(data, &r); err != nil {
				return fmt.Errorf("the preview is not what this whr expects: %w", err)
			}
			fmt.Fprintf(s.env.Stderr, "preview %s: open the link in your browser; it works once and the preview ends with its environment\n", clean(r.Preview.ID))
			return s.emit(raw, func(w io.Writer) error { _, err := fmt.Fprintln(w, r.URL); return err })
		},
	}

	ls := &cobra.Command{
		Use:   "ls",
		Short: "List the open previews",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := s.api()
			if err != nil {
				return err
			}
			raw, data, err := c.Do(cmd.Context(), "GET", "/v1/previews", nil, "")
			if err != nil {
				return err
			}
			var rows []previewRow
			if err := json.Unmarshal(data, &rows); err != nil {
				return fmt.Errorf("the preview list is not what this whr expects: %w", err)
			}
			return s.emit(raw, func(w io.Writer) error {
				out := make([][]string, len(rows))
				for i, r := range rows {
					out[i] = []string{clean(r.ID), clean(r.Task), strconv.Itoa(r.Port), strconv.Itoa(r.Listen), r.Expires.Local().Format("2006-01-02 15:04")}
				}
				return table(w, []string{"ID", "TASK", "PORT", "LISTENS ON", "ENDS"}, out)
			})
		},
	}

	link := &cobra.Command{
		Use:   "link <preview>",
		Short: "Print another link for an open preview; it works once, for 5 minutes",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := s.api()
			if err != nil {
				return err
			}
			raw, data, err := c.Do(cmd.Context(), "POST", "/v1/previews/"+url.PathEscape(args[0])+"/link", nil, "")
			if err != nil {
				return err
			}
			var r struct {
				URL string `json:"url"`
			}
			if err := json.Unmarshal(data, &r); err != nil {
				return fmt.Errorf("the link is not what this whr expects: %w", err)
			}
			return s.emit(raw, func(w io.Writer) error { _, err := fmt.Fprintln(w, r.URL); return err })
		},
	}

	closeCmd := &cobra.Command{
		Use:   "close <preview>",
		Short: "Close a preview now",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := s.api()
			if err != nil {
				return err
			}
			raw, _, err := c.Do(cmd.Context(), "DELETE", "/v1/previews/"+url.PathEscape(args[0]), nil, newKey())
			if err != nil {
				return err
			}
			return s.emit(raw, func(w io.Writer) error { _, err := fmt.Fprintln(w, clean(args[0])); return err })
		},
	}
	pv.AddCommand(open, ls, link, closeCmd)
	return pv
}
