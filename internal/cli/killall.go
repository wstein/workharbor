package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
)

// confirmWord is what the human types to pull the kill switch.
const confirmWord = "kill-all"

func newKillAll(s *state) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "kill-all",
		Short: "Stop every run, cancel every unfinished task and revoke the forge tokens",
		Long: "The kill switch (design §7.7). It stops every run, cancels every unfinished task and revokes the forge " +
			"tokens the supervisor holds, goes on after a failure and says what failed. It cannot revoke the agent's own " +
			"credentials, which stay with you. Without --yes it asks you to type " + confirmWord + ".",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !yes {
				fmt.Fprintf(s.env.Stderr, "This stops every run, cancels every unfinished task and revokes the forge tokens.\nType %q to go on: ", confirmWord)
				line, err := bufio.NewReader(s.env.Stdin).ReadString('\n')
				if err != nil && !errors.Is(err, io.EOF) {
					return err
				}
				if strings.TrimSpace(line) != confirmWord {
					fmt.Fprintln(s.env.Stderr)
					return usageError{"not confirmed: nothing was stopped"}
				}
			}
			c, err := s.api()
			if err != nil {
				return err
			}
			raw, data, err := c.Do(cmd.Context(), "POST", "/v1/kill-all", map[string]bool{"confirm": true}, newKey())
			if err != nil {
				return err
			}
			var rep struct {
				Cancelled     []string `json:"cancelled"`
				TokensRevoked int      `json:"tokens_revoked"`
				Problems      []string `json:"problems"`
			}
			if err := json.Unmarshal(data, &rep); err != nil {
				return fmt.Errorf("the answer is not a kill-all report: %w", err)
			}
			if err := s.emit(raw, func(w io.Writer) error {
				for _, id := range rep.Cancelled {
					fmt.Fprintln(w, clean(id))
				}
				return nil
			}); err != nil {
				return err
			}
			fmt.Fprintf(s.env.Stderr, "cancelled %d task(s), revoked %d forge token(s)\n", len(rep.Cancelled), rep.TokensRevoked)
			if len(rep.Problems) > 0 {
				for _, p := range rep.Problems {
					fmt.Fprintln(s.env.Stderr, "problem:", clean(p))
				}
				return errors.New("kill-all finished with problems")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "do not ask for confirmation")
	return cmd
}
