package serve

import (
	"context"
	"fmt"
	"time"

	"github.com/wstein/workharbor/internal/service"
	"github.com/wstein/workharbor/internal/store"
	"github.com/wstein/workharbor/internal/web"
)

// changesOf gives the web UI the changes that need a passkey (issue #107): the
// workflow changes the host has not confirmed, and the revocation of the forge
// tokens. The UI calls Confirm and RevokeTokens only after a step-up that named
// them.
type changesOf struct {
	st     *store.Store
	svc    *service.Service
	revoke bool
}

func (c changesOf) Open(ctx context.Context) ([]web.Change, error) {
	open, err := c.st.OpenChanges(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]web.Change, len(open))
	for i, p := range open {
		out[i] = web.Change{ID: p.ID, Repo: p.Repo, From: describe(p.From), To: describe(p.To), Text: p.Text()}
	}
	return out, nil
}

func (c changesOf) Confirm(ctx context.Context, id, by string, at time.Time) error {
	_, err := c.st.ConfirmChange(ctx, id, by, at)
	return err
}

func (c changesOf) CanRevokeTokens() bool { return c.revoke }

func (c changesOf) RevokeTokens(ctx context.Context, actor string) (int, error) {
	return c.svc.RevokeForgeTokens(ctx, actor)
}

// describe says what a workflow record is in words for the page.
func describe(r store.WorkflowRecord) string {
	if r.Branch == "" {
		return r.Workflow
	}
	return fmt.Sprintf("%s on %s", r.Workflow, r.Branch)
}
