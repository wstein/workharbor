package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/wstein/workharbor/internal/forge"
)

// BoardPermission is what the App asks for in addition to AppPermissions when a
// board is configured, for an organization's project (D15, D30). Whether it
// works for a user-owned project at all is unverified: GitHub's documentation
// lists no App permission for them (issue #70 tests it first).
const BoardPermission = "organization_projects"

// AppPermissionsFor is AppPermissions plus the board's permission, when a board
// is configured.
func AppPermissionsFor(board bool) map[string]string {
	p := AppPermissions()
	if board {
		p[BoardPermission] = "write"
	}
	return p
}

// BoardConfig names the project board the supervisor keeps current (D30).
type BoardConfig struct {
	// Owner is the login of the user or organization that owns the project.
	Owner string
	// Organization is true for an organization's project.
	Organization bool
	// Number is the project's number in its owner's URL.
	Number int
	// StatusField, SessionField and LinkField name the project's fields;
	// the defaults are "Status", "Session" and "Task". A field that does not
	// exist is not written.
	StatusField, SessionField, LinkField string
}

// ErrBoardNotWritable is GitHub refusing the App access to the project board:
// the App lacks the permission, or the project is one an App's token cannot
// reach (a user-owned project may be, which is unverified, issue #70). It is
// returned together with ErrBoard, and `whr doctor` names it.
var ErrBoardNotWritable = errors.New("github: the App cannot reach the project board")

// ErrBoardPermission is the installation refusing a token with the board's
// permission: the App has it, but the installation has not accepted it. It comes
// with ErrBoard and ErrBoardNotWritable, and only the board is affected.
var ErrBoardPermission = errors.New("github: the installation lacks organization_projects: write")

// ErrBoard is a board write that GitHub refused or could not do: the project,
// a field or an option is missing, or GraphQL returned errors.
var ErrBoard = errors.New("github: the project board could not be updated")

var loginRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,38}$`)

func (b BoardConfig) fields() (status, session, link string) {
	status, session, link = b.StatusField, b.SessionField, b.LinkField
	if status == "" {
		status = "Status"
	}
	if session == "" {
		session = "Session"
	}
	if link == "" {
		link = "Task"
	}
	return
}

// boardField is one field of the project, as the GraphQL schema reports it.
type boardField struct {
	ID       string
	Name     string
	DataType string // TEXT, SINGLE_SELECT, ...
	Options  map[string]string
}

type boardCache struct {
	mu      sync.Mutex
	project string
	fields  map[string]boardField // by lower-cased name
}

// graphQL sends one query as the installation of repo and returns data. GraphQL
// reports most failures as 200 with an errors list, so those are errors here.
func (c *Client) graphQL(ctx context.Context, repo, query string, vars map[string]any, out any) error {
	var resp struct {
		Data   map[string]any `json:"data"`
		Errors []struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"errors"`
	}
	if err := c.callWith(ctx, repo, true, http.MethodPost, "/graphql", map[string]any{"query": query, "variables": vars}, &resp); err != nil {
		return err
	}
	if len(resp.Errors) > 0 {
		msgs := make([]string, 0, len(resp.Errors))
		for _, e := range resp.Errors {
			msgs = append(msgs, e.Message)
		}
		msg := oneLine(strings.Join(msgs, "; "))
		if c.cfg.Redactor != nil {
			msg = c.cfg.Redactor.String(msg)
		}
		for _, e := range resp.Errors {
			if e.Type == "FORBIDDEN" || e.Type == "INSUFFICIENT_SCOPES" || strings.Contains(e.Message, "Resource not accessible") {
				return fmt.Errorf("%w: %w: %s", ErrBoard, ErrBoardNotWritable, msg)
			}
		}
		return fmt.Errorf("%w: %s", ErrBoard, msg)
	}
	return decodeInto(resp.Data, out)
}

const projectQuery = `query($login: String!, $number: Int!) {
	%s(login: $login) {
		projectV2(number: $number) {
			id
			fields(first: 50) {
				nodes {
					... on ProjectV2FieldCommon { id name dataType }
					... on ProjectV2SingleSelectField { id name dataType options { id name } }
				}
			}
		}
	}
}`

// loadBoard reads the project and its fields once, and again after a failed write.
func (c *Client) loadBoard(ctx context.Context, repo string) (string, map[string]boardField, error) {
	c.board.mu.Lock()
	defer c.board.mu.Unlock()
	if c.board.project != "" {
		return c.board.project, c.board.fields, nil
	}
	b := c.cfg.Board
	kind := "user"
	if b.Organization {
		kind = "organization"
	}
	var out map[string]struct {
		ProjectV2 *struct {
			ID     string `json:"id"`
			Fields struct {
				Nodes []struct {
					ID       string `json:"id"`
					Name     string `json:"name"`
					DataType string `json:"dataType"`
					Options  []struct {
						ID   string `json:"id"`
						Name string `json:"name"`
					} `json:"options"`
				} `json:"nodes"`
			} `json:"fields"`
		} `json:"projectV2"`
	}
	if err := c.graphQL(ctx, repo, fmt.Sprintf(projectQuery, kind), map[string]any{"login": b.Owner, "number": b.Number}, &out); err != nil {
		return "", nil, err
	}
	p := out[kind].ProjectV2
	if p == nil || p.ID == "" {
		// Not found and not accessible look the same to a token that cannot see it.
		return "", nil, fmt.Errorf("%w: %w: the %s project %d of %s was not found or is not visible to the App", ErrBoard, ErrBoardNotWritable, kind, b.Number, b.Owner)
	}
	fields := map[string]boardField{}
	for _, n := range p.Fields.Nodes {
		if n.ID == "" || n.Name == "" {
			continue
		}
		f := boardField{ID: n.ID, Name: n.Name, DataType: n.DataType, Options: map[string]string{}}
		for _, o := range n.Options {
			f.Options[o.Name] = o.ID
		}
		fields[strings.ToLower(n.Name)] = f
	}
	c.board.project, c.board.fields = p.ID, fields
	return p.ID, fields, nil
}

func (c *Client) forgetBoard() {
	c.board.mu.Lock()
	c.board.project, c.board.fields = "", nil
	c.board.mu.Unlock()
}

const issueCardQuery = `query($owner: String!, $name: String!, $number: Int!) {
	repository(owner: $owner, name: $name) {
		issue(number: $number) {
			id
			projectItems(first: 20) { nodes { id project { id } } }
		}
	}
}`

const addCardMutation = `mutation($project: ID!, $content: ID!) {
	addProjectV2ItemById(input: {projectId: $project, contentId: $content}) { item { id } }
}`

const setFieldMutation = `mutation($project: ID!, $item: ID!, $field: ID!, $value: ProjectV2FieldValue!) {
	updateProjectV2ItemFieldValue(input: {projectId: $project, itemId: $item, fieldId: $field, value: $value}) { projectV2Item { id } }
}`

// UpdateCard implements forge.Board. It writes only what the update carries:
// the status (an option of the project's single-select field), the session (a
// text field, or the option of that name in a single-select one) and the link
// (a text field). Nothing it writes comes from the issue's text.
func (c *Client) UpdateCard(ctx context.Context, repo string, issue int, u forge.CardUpdate) error {
	b := c.cfg.Board
	if b == nil {
		return errors.New("github: no project board is configured")
	}
	if err := c.allowed(repo); err != nil {
		return err
	}
	if issue <= 0 {
		return fmt.Errorf("%w: issue number %d", ErrBoard, issue)
	}
	err := c.updateCard(ctx, repo, issue, u)
	if err != nil {
		c.forgetBoard() // the project may have changed: read it again next time
	}
	return err
}

func (c *Client) updateCard(ctx context.Context, repo string, issue int, u forge.CardUpdate) error {
	b := c.cfg.Board
	project, fields, err := c.loadBoard(ctx, repo)
	if err != nil {
		return err
	}
	statusName, sessionName, linkName := b.fields()
	status, ok := fields[strings.ToLower(statusName)]
	if !ok || status.DataType != "SINGLE_SELECT" {
		return fmt.Errorf("%w: the project has no single-select field %q", ErrBoard, statusName)
	}
	option, ok := status.Options[u.Status]
	if !ok {
		return fmt.Errorf("%w: the %q field has no option %q", ErrBoard, statusName, u.Status)
	}

	owner, name, _ := strings.Cut(repo, "/")
	var found struct {
		Repository *struct {
			Issue *struct {
				ID           string `json:"id"`
				ProjectItems struct {
					Nodes []struct {
						ID      string `json:"id"`
						Project struct {
							ID string `json:"id"`
						} `json:"project"`
					} `json:"nodes"`
				} `json:"projectItems"`
			} `json:"issue"`
		} `json:"repository"`
	}
	if err := c.graphQL(ctx, repo, issueCardQuery, map[string]any{"owner": owner, "name": name, "number": issue}, &found); err != nil {
		return err
	}
	if found.Repository == nil || found.Repository.Issue == nil || found.Repository.Issue.ID == "" {
		return fmt.Errorf("%w: issue %s#%d was not found", ErrBoard, repo, issue)
	}
	item := ""
	for _, n := range found.Repository.Issue.ProjectItems.Nodes {
		if n.Project.ID == project {
			item = n.ID
		}
	}
	if item == "" { // the issue is not on the board yet
		var added struct {
			Add struct {
				Item struct {
					ID string `json:"id"`
				} `json:"item"`
			} `json:"addProjectV2ItemById"`
		}
		if err := c.graphQL(ctx, repo, addCardMutation, map[string]any{"project": project, "content": found.Repository.Issue.ID}, &added); err != nil {
			return err
		}
		if item = added.Add.Item.ID; item == "" {
			return fmt.Errorf("%w: the issue was not added to the project", ErrBoard)
		}
	}

	set := func(field boardField, value map[string]any) error {
		return c.graphQL(ctx, repo, setFieldMutation, map[string]any{"project": project, "item": item, "field": field.ID, "value": value}, nil)
	}
	if err := set(status, map[string]any{"singleSelectOptionId": option}); err != nil {
		return err
	}
	if f, ok := fields[strings.ToLower(sessionName)]; ok && u.Session != "" {
		switch f.DataType {
		case "TEXT":
			if err := set(f, map[string]any{"text": u.Session}); err != nil {
				return err
			}
		case "SINGLE_SELECT":
			if id, ok := f.Options[u.Session]; ok { // only an option that exists: the board's own list
				if err := set(f, map[string]any{"singleSelectOptionId": id}); err != nil {
					return err
				}
			}
		}
	}
	if f, ok := fields[strings.ToLower(linkName)]; ok && f.DataType == "TEXT" && u.Link != "" {
		if err := set(f, map[string]any{"text": u.Link}); err != nil {
			return err
		}
	}
	return nil
}

// BoardReport is what CheckBoard read.
type BoardReport struct {
	// MissingStatuses are the status options the project lacks.
	MissingStatuses []string
	// Session and Link say whether the optional fields were found and are usable.
	Session, Link bool
}

// CheckBoard reads the project and its fields as the App, without writing
// anything: it finds out whether the project is reachable and has the status
// options the mirror writes. It cannot tell whether a write would succeed.
func (c *Client) CheckBoard(ctx context.Context) (BoardReport, error) {
	b := c.cfg.Board
	if b == nil {
		return BoardReport{}, errors.New("github: no project board is configured")
	}
	_, fields, err := c.loadBoard(ctx, c.cfg.Repos[0])
	if err != nil {
		c.forgetBoard()
		return BoardReport{}, err
	}
	statusName, sessionName, linkName := b.fields()
	var rep BoardReport
	status, ok := fields[strings.ToLower(statusName)]
	for _, want := range []string{forge.StatusNeedsYou, forge.StatusInProgress, forge.StatusDone} {
		if !ok || status.DataType != "SINGLE_SELECT" || status.Options[want] == "" {
			rep.MissingStatuses = append(rep.MissingStatuses, want)
		}
	}
	if f, ok := fields[strings.ToLower(sessionName)]; ok && (f.DataType == "TEXT" || f.DataType == "SINGLE_SELECT") {
		rep.Session = true
	}
	if f, ok := fields[strings.ToLower(linkName)]; ok && f.DataType == "TEXT" {
		rep.Link = true
	}
	return rep, nil
}

// ValidateBoard checks a board configuration.
func ValidateBoard(b BoardConfig) error {
	if !loginRE.MatchString(b.Owner) {
		return fmt.Errorf("github: board owner %q is not a login", b.Owner)
	}
	if b.Number <= 0 {
		return fmt.Errorf("github: board project number %d must be positive", b.Number)
	}
	for _, f := range []string{b.StatusField, b.SessionField, b.LinkField} {
		if strings.ContainsFunc(f, func(r rune) bool { return r < ' ' }) || len(f) > 100 {
			return errors.New("github: a board field name has a control character or is too long")
		}
	}
	return nil
}

// decodeInto reads the data of a GraphQL answer into out, if it is wanted.
func decodeInto(data map[string]any, out any) error {
	if out == nil {
		return nil
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("github: the GraphQL answer is not what was expected: %w", err)
	}
	return nil
}

var _ forge.Board = (*Client)(nil)

const queueQuery = `query($project: ID!, $after: String) {
	node(id: $project) {
		... on ProjectV2 {
			items(first: 100, after: $after) {
				pageInfo { hasNextPage endCursor }
				nodes {
					updatedAt
					content { ... on Issue { number repository { nameWithOwner } } }
					fieldValues(first: 20) {
						nodes {
							... on ProjectV2ItemFieldSingleSelectValue { name field { ... on ProjectV2FieldCommon { name } } }
							... on ProjectV2ItemFieldTextValue { text field { ... on ProjectV2FieldCommon { name } } }
						}
					}
				}
			}
		}
	}
}`

// maxQueuePages bounds how much of a board one poll reads.
const maxQueuePages = 10

// QueuedCards implements forge.QueueReader: the issues whose card has the named
// status, with what its Session field says and when it was last changed. It reads
// the board as the App and changes nothing; a card of an issue in a repository the
// supervisor does not work on is left out. The mover is not on a project item, so it
// is never set.
func (c *Client) QueuedCards(ctx context.Context, status string) ([]forge.QueuedCard, error) {
	b := c.cfg.Board
	if b == nil {
		return nil, errors.New("github: no project board is configured")
	}
	if status == "" || len(c.cfg.Repos) == 0 {
		return nil, nil
	}
	repo := c.cfg.Repos[0] // the installation token is minted for a repository of the App
	project, _, err := c.loadBoard(ctx, repo)
	if err != nil {
		c.forgetBoard()
		return nil, err
	}
	statusName, sessionName, _ := b.fields()
	var out []forge.QueuedCard
	after := ""
	for range maxQueuePages {
		var page struct {
			Node struct {
				Items struct {
					PageInfo struct {
						HasNextPage bool   `json:"hasNextPage"`
						EndCursor   string `json:"endCursor"`
					} `json:"pageInfo"`
					Nodes []struct {
						UpdatedAt time.Time `json:"updatedAt"`
						Content   struct {
							Number     int `json:"number"`
							Repository struct {
								NameWithOwner string `json:"nameWithOwner"`
							} `json:"repository"`
						} `json:"content"`
						FieldValues struct {
							Nodes []struct {
								Name  string `json:"name"`
								Text  string `json:"text"`
								Field struct {
									Name string `json:"name"`
								} `json:"field"`
							} `json:"nodes"`
						} `json:"fieldValues"`
					} `json:"nodes"`
				} `json:"items"`
			} `json:"node"`
		}
		vars := map[string]any{"project": project, "after": nil}
		if after != "" {
			vars["after"] = after
		}
		if err := c.graphQL(ctx, repo, queueQuery, vars, &page); err != nil {
			c.forgetBoard()
			return nil, err
		}
		for _, n := range page.Node.Items.Nodes {
			if n.Content.Number <= 0 || n.Content.Repository.NameWithOwner == "" || c.allowed(n.Content.Repository.NameWithOwner) != nil {
				continue // a draft, a pull request or another repository
			}
			var inQueue bool
			var agent string
			for _, v := range n.FieldValues.Nodes {
				switch {
				case strings.EqualFold(v.Field.Name, statusName) && v.Name == status:
					inQueue = true
				case strings.EqualFold(v.Field.Name, sessionName):
					agent = v.Name + v.Text
				}
			}
			if inQueue {
				out = append(out, forge.QueuedCard{Repo: n.Content.Repository.NameWithOwner, Issue: n.Content.Number, Agent: strings.TrimSpace(agent), UpdatedAt: n.UpdatedAt})
			}
		}
		if !page.Node.Items.PageInfo.HasNextPage {
			break
		}
		after = page.Node.Items.PageInfo.EndCursor
	}
	return out, nil
}

var _ forge.QueueReader = (*Client)(nil)
