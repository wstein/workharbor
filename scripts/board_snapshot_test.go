package scripts

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/gittest"
)

const fakeNodes = `[
{"content":{"__typename":"Issue","number":20,"title":"b","url":"https://example/20","labels":{"nodes":[{"name":"x"}]}},"status":{"name":"Todo"},"session":{"name":"wh/platform"},"priority":{"name":"P2"}},
{"content":{"__typename":"Issue","number":30,"title":"a","url":"https://example/30"},"status":{"name":"Todo"},"session":{"name":"wh/platform"},"priority":{"name":"P1"}},
{"content":{"__typename":"Issue","number":10,"title":"c","url":"https://example/10"},"status":{"name":"Todo"},"session":{"name":"wh/platform"},"priority":{"name":"P2"}},
{"content":{"__typename":"Issue","number":40,"title":"d","url":"https://example/40"},"status":{"name":"In progress"},"session":{"name":"wh/platform"},"priority":{"name":"P1"}},
{"content":{"__typename":"Issue","number":50,"title":"e","url":"https://example/50"},"status":{"name":"Todo"},"session":{"name":"wh/runtime"},"priority":{"name":"P1"}}
]`

func page(nodes string, next string) string {
	nodes = strings.ReplaceAll(nodes, `"__typename":"Issue",`, `"__typename":"Issue","repository":{"nameWithOwner":"wstein/workharbor"},`)
	if next == "" {
		return `{"data":{"node":{"items":{"pageInfo":{"hasNextPage":false,"endCursor":null},"nodes":` + nodes + `}}}}`
	}
	return `{"data":{"node":{"items":{"pageInfo":{"hasNextPage":true,"endCursor":"` + next + `"},"nodes":` + nodes + `}}}}`
}

type board struct {
	bin, snap, log string
}

func TestBoardSnapshotExplicitRepositoryRequiresProject(t *testing.T) {
	t.Parallel()
	fixture := newBoard(t)
	_, stderr, err := fixture.runEnv(t, []string{"WHR_BOARD_REPOSITORY=wstein/crewbook"}, "move", "7", "Todo")
	if err == nil || !strings.Contains(stderr, "explicit project") || fixture.calls(t) != 0 {
		t.Fatalf("err %v stderr %q calls %d", err, stderr, fixture.calls(t))
	}
}

func TestBoardSnapshotScopedCacheAndRepositoryCollision(t *testing.T) {
	t.Parallel()
	fixture := newBoard(t)
	if _, _, err := fixture.run(t); err != nil {
		t.Fatal(err)
	}
	nodes := `[{"content":{"__typename":"Issue","number":7,"repository":{"nameWithOwner":"wstein/crewbook"}},"session":{"name":"cb/platform"},"status":{"name":"Todo"}},{"content":{"__typename":"Issue","number":7,"repository":{"nameWithOwner":"wstein/workharbor"}}},{"content":{"__typename":"PullRequest","number":7,"repository":{"nameWithOwner":"wstein/crewbook"}}}]`
	fixture.pages(t, map[string]string{"first": page(nodes, "")})
	env := []string{"WHR_BOARD_REPOSITORY=wstein/crewbook", "WHR_BOARD_PROJECT_ID=PVT_crewbook", "WHR_BOARD_LANE_PREFIX=cb"}
	stdout, stderr, err := fixture.runEnv(t, env)
	if err != nil {
		t.Fatalf("%v %s", err, stderr)
	}
	var snapshot struct{ Items []map[string]any }
	if err := json.Unmarshal([]byte(stdout), &snapshot); err != nil || len(snapshot.Items) != 1 {
		t.Fatalf("snapshot %s err %v", stdout, err)
	}
	if fixture.calls(t) != 3 {
		t.Fatal("another target reused the default snapshot")
	}
	_, _, err = fixture.runEnv(t, env, "move", "7", "Ready to push")
	if err == nil || fixture.calls(t) != 3 {
		t.Fatal("move bypassed review gate")
	}
}

func TestBoardSnapshotScopedMetadataAndAdd(t *testing.T) {
	t.Parallel()
	fixture := newBoard(t)
	env := []string{"WHR_BOARD_REPOSITORY=wstein/crewbook", "WHR_BOARD_PROJECT_NUMBER=10", "WHR_BOARD_LANE_PREFIX=cb"}
	stdout, stderr, err := fixture.runEnv(t, env, "metadata")
	if err != nil || !strings.Contains(stdout, "PVT_crewbook") {
		t.Fatalf("metadata: %v %s %s", err, stderr, stdout)
	}
	if _, stderr, err := fixture.runEnv(t, env, "add", "1", "2", "3", "4", "5", "6", "7", "8"); err != nil {
		t.Fatalf("add: %v %s", err, stderr)
	}
	for _, call := range fixture.lines(t) {
		if strings.Contains(call, "PVT_kwHNjWrOAZVCuA") || strings.Contains(call, "fields(first") {
			t.Fatalf("add touched default project or required schema: %s", call)
		}
		if strings.Contains(call, "issue(number") && !strings.Contains(call, "repo=crewbook") {
			t.Fatalf("wrong repository: %s", call)
		}
	}
	mutations := 0
	for _, call := range fixture.lines(t) {
		if strings.Contains(call, "addProjectV2ItemById") {
			mutations++
			if !strings.Contains(call, "p=PVT_crewbook") || !strings.Contains(call, "c=I_"+strconv.Itoa(mutations)) {
				t.Fatalf("unexpected resolved mutation: %s", call)
			}
		}
	}
	if mutations != 8 {
		t.Fatalf("mutations %d, want 8", mutations)
	}
	before := fixture.calls(t)
	_, _, err = fixture.runEnv(t, append(env, "WHR_BOARD_PROJECT_ID=PVT_wrong"), "add", "7")
	if err == nil || fixture.calls(t) != before+1 {
		t.Fatal("inconsistent ID and number allowed mutation")
	}
	_, _, err = fixture.runEnv(t, []string{"WHR_BOARD_REPOSITORY=wstein/crewbook", "WHR_BOARD_PROJECT_NUMBER=6"}, "add", "7")
	if err == nil || fixture.calls(t) != before+1 {
		t.Fatal("nondefault repository targeted project 6")
	}
	_, _, err = fixture.runEnv(t, []string{"WHR_BOARD_REPOSITORY=wstein/other", "WHR_BOARD_PROJECT_NUMBER=10"}, "add", "7")
	if err == nil || fixture.calls(t) != before+2 {
		t.Fatal("repository metadata mismatch allowed mutation")
	}
	before = fixture.calls(t)
	_, stderr, err = fixture.runEnv(t, env, "add", "1", "99", "7")
	if err == nil || !strings.Contains(stderr, "#99") || fixture.calls(t) != before+6 {
		t.Fatalf("partial add: %v %s calls %d", err, stderr, fixture.calls(t)-before)
	}
	if _, stderr, err := fixture.runEnv(t, env, "add", "1", "7"); err != nil {
		t.Fatalf("retry: %v %s", err, stderr)
	}
	before = fixture.calls(t)
	_, _, err = fixture.runEnv(t, []string{"WHR_BOARD_PROJECT_ID=PVT_kwHNjWrOAZVCuA", "WHR_BOARD_PROJECT_NUMBER=10"}, "add", "7")
	if err == nil || fixture.calls(t) != before+1 {
		t.Fatal("explicit default ID bypassed identity validation")
	}
}

func TestBoardSnapshotMismatchedFallbackRefused(t *testing.T) {
	t.Parallel()
	for _, busy := range []bool{false, true} {
		t.Run(strconv.FormatBool(busy), func(t *testing.T) {
			fixture := newBoard(t)
			if _, _, err := fixture.run(t); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(fixture.snap, []byte(`{"target":"another-project","fetched_at":1,"items":[{"title":"must-not-leak"}]}`), 0o600); err != nil {
				t.Fatal(err)
			}
			if busy {
				lock := filepath.Join(filepath.Dir(fixture.snap), ".board.lock")
				if err := os.Mkdir(lock, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(lock, "ts"), []byte(strconv.FormatInt(time.Now().Unix(), 10)), 0o600); err != nil {
					t.Fatal(err)
				}
				clock := strconv.FormatInt(time.Now().Unix(), 10)
				functions := "sleep() { :; }\ndate() { echo " + clock + "; }\nmkdir() { if [ \"$1\" = '" + lock + "' ]; then return 1; fi; command mkdir \"$@\"; }\ncat() { if [ \"$1\" = '" + lock + "/ts' ]; then echo " + clock + "; else command cat \"$@\"; fi; }\n"
				if err := os.WriteFile(filepath.Join(fixture.bin, "functions"), []byte(functions), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			env := []string{"FAKE_GH_FAIL=1"}
			if busy {
				env = append(env, "BASH_ENV="+filepath.Join(fixture.bin, "functions"))
			}
			stdout, _, err := fixture.runEnv(t, env, "--refresh")
			if err == nil || strings.Contains(stdout, "must-not-leak") {
				t.Fatalf("mismatched cache returned: %v %s", err, stdout)
			}
		})
	}
}

func TestBoardSnapshotConfigureRequiresConfirmation(t *testing.T) {
	t.Parallel()
	fixture := newBoard(t)
	for _, env := range [][]string{nil, {"WHR_BOARD_REPOSITORY=wstein/crewbook", "WHR_BOARD_PROJECT_NUMBER=10"}} {
		if _, _, err := fixture.runEnv(t, env, "configure"); err == nil || fixture.calls(t) != 0 {
			t.Fatal("configure without explicit confirmation reached GitHub")
		}
	}
}

func newSchemaBoard(t *testing.T) board {
	t.Helper()
	fixture := newBoard(t)
	state := `{"fields":[{"id":"F_title","name":"Title","dataType":"TITLE"},{"id":"F_assignees","name":"Assignees","dataType":"ASSIGNEES"},{"id":"F_milestone","name":"Milestone","dataType":"MILESTONE"},{"id":"F_status","name":"Status","dataType":"SINGLE_SELECT","options":[{"id":"O_todo","name":"Todo","color":"GRAY","description":""},{"id":"O_ip","name":"In progress","color":"YELLOW","description":""},{"id":"O_done","name":"Done","color":"GREEN","description":""}]}],"views":[],"workflows":[{"id":"W_closed","name":"Item closed","enabled":true}]}`
	if err := os.WriteFile(filepath.Join(fixture.bin, "schema.json"), []byte(state), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := `#!/bin/sh
if [ "$1 $2" = "api rate_limit" ]; then echo '{}'; exit 0; fi
case "$*" in
*--input*)
	for arg in "$@"; do request=$arg; done
	payload=$(cat "$request")
	echo "$payload" | jq -c . >> "$SCHEMA_LOG"
	if [ -n "$FAKE_SCHEMA_FAIL" ] && echo "$payload" | jq -e --arg fail "$FAKE_SCHEMA_FAIL" '.variables.input.name == $fail' >/dev/null; then echo '{"errors":[{"message":"refused"}]}'; exit 0; fi
	query=$(echo "$payload" | jq -r '.query')
	input=$(echo "$payload" | jq -c '.variables.input')
	case "$query" in
	*createProjectV2Field*) jq --argjson input "$input" '.fields += [{id:("F_"+$input.name),name:$input.name,dataType:"SINGLE_SELECT",options:($input.singleSelectOptions | map(. + {id:("O_"+.name)}))}]' "$SCHEMA_STATE" > "$SCHEMA_STATE.tmp"; result=createProjectV2Field; node=projectV2Field; identifier=$(echo "$input" | jq -r '"F_"+.name') ;;
	*updateProjectV2Field*) jq --argjson input "$input" '.fields |= map(if .id == $input.fieldId then .options = ($input.singleSelectOptions | map(. + {id:(.id // ("O_"+.name))})) else . end)' "$SCHEMA_STATE" > "$SCHEMA_STATE.tmp"; result=updateProjectV2Field; node=projectV2Field; identifier=$(echo "$input" | jq -r '.fieldId') ;;
	*createProjectV2View*) jq --argjson input "$input" '.views += [{id:("V_"+$input.name),name:$input.name,layout:$input.layout,filter:"",configuration:{visibleFields:{nodes:($input.configuration.visibleFieldIds | map({id:.})),pageInfo:{hasNextPage:false}}}}]' "$SCHEMA_STATE" > "$SCHEMA_STATE.tmp"; result=createProjectV2View; node=projectV2View; identifier=$(echo "$input" | jq -r '"V_"+.name') ;;
	*updateProjectV2View*) jq --argjson input "$input" '.views |= map(if .id == $input.viewId then .filter=$input.filter | .layout=$input.layout | .configuration.visibleFields.nodes=($input.configuration.visibleFieldIds | map({id:.})) else . end)' "$SCHEMA_STATE" > "$SCHEMA_STATE.tmp"; result=updateProjectV2View; node=projectV2View; identifier=$(echo "$input" | jq -r '.viewId') ;;
	*) exit 1 ;;
	esac
	mv "$SCHEMA_STATE.tmp" "$SCHEMA_STATE"
	jq -n --arg result "$result" --arg node "$node" --arg id "$identifier" '{data:{($result):{($node):{id:$id}}}}'
	exit 0 ;;
*repoOwner=*) echo '{"data":{"repository":{"nameWithOwner":"wstein/crewbook"},"node":{"id":"PVT_crewbook","number":10,"url":"https://github.com/users/wstein/projects/10","owner":{"login":"wstein"}}}}'; exit 0 ;;
esac
for connection in fields views workflows; do
	case "$*" in *"$connection(first:"*)
		if [ "$connection" = fields ] && [ -n "$FAKE_LOCK_BARRIER" ] && mkdir "$FAKE_LOCK_BARRIER" 2>/dev/null; then
			while [ ! -f "$FAKE_LOCK_BARRIER/release" ]; do /bin/sleep 0.01; done
		fi
		if [ "$connection" = fields ] && [ -n "$FAKE_LOCK_SUCCESSOR" ]; then
			read -r owner_pid owner_token < "$FAKE_LOCK_SUCCESSOR/owner"
			printf '%s successor-generation\n' "$owner_pid" > "$FAKE_LOCK_SUCCESSOR/owner"
		fi
		if [ "$FAKE_SCHEMA_FAIL_PAGE" = "$connection" ]; then echo '{}'; exit 0; fi
		if [ "$FAKE_SCHEMA_PAGINATE" = "$connection" ]; then
			case "$*" in *after=next*)
				if [ -n "$FAKE_SCHEMA_FAIL_AFTER" ]; then echo '{}'; exit 0; fi
				jq --arg connection "$connection" '{data:{node:{id:"PVT_crewbook",($connection):{nodes:.[$connection][2:],pageInfo:{hasNextPage:false,endCursor:null}}}}}' "$SCHEMA_STATE"; exit 0 ;;
			esac
			jq --arg connection "$connection" '{data:{node:{id:"PVT_crewbook",($connection):{nodes:.[$connection][:2],pageInfo:{hasNextPage:true,endCursor:"next"}}}}}' "$SCHEMA_STATE"; exit 0
		fi
		jq --arg connection "$connection" '{data:{node:{id:"PVT_crewbook",($connection):{nodes:.[$connection],pageInfo:{hasNextPage:false,endCursor:null}}}}}' "$SCHEMA_STATE"; exit 0 ;;
	esac
done
echo '{}'
`
	if err := os.WriteFile(filepath.Join(fixture.bin, "gh"), []byte(fake), 0o755); err != nil { //nolint:gosec // a test fake
		t.Fatal(err)
	}
	return fixture
}

func schemaEnv(fixture board) []string {
	return []string{"WHR_BOARD_REPOSITORY=wstein/crewbook", "WHR_BOARD_OWNER=wstein", "WHR_BOARD_PROJECT_ID=PVT_crewbook", "WHR_BOARD_LANE_PREFIX=cb", "WHR_BOARD_ROLES=desk,platform,review", "SCHEMA_STATE=" + filepath.Join(fixture.bin, "schema.json"), "SCHEMA_LOG=" + fixture.log}
}

func TestBoardSnapshotConfigureLockOwner(t *testing.T) {
	for _, scenario := range []string{"live", "reused", "dead", "missing", "guard"} {
		t.Run(scenario, func(t *testing.T) {
			fixture := newSchemaBoard(t)
			lock := filepath.Join(filepath.Dir(fixture.snap), ".board-configure-PVT_crewbook.lock")
			if err := os.MkdirAll(lock, 0o700); err != nil {
				t.Fatal(err)
			}
			pid := os.Getpid()
			if scenario == "dead" {
				process := exec.CommandContext(t.Context(), "sh", "-c", "exit 0")
				if err := process.Run(); err != nil {
					t.Fatal(err)
				}
				pid = process.Process.Pid
			}
			if scenario != "missing" {
				if err := os.WriteFile(filepath.Join(lock, "owner"), []byte(strconv.Itoa(pid)+" generation-old\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(lock, "ts"), []byte("1\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if scenario == "guard" {
				if err := os.Mkdir(lock+".guard", 0o700); err != nil {
					t.Fatal(err)
				}
			}
			startup := filepath.Join(fixture.bin, "startup")
			if err := os.WriteFile(startup, []byte("sleep() { :; }\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			_, stderr, err := fixture.runEnv(t, append(schemaEnv(fixture), "BASH_ENV="+startup), "configure-fields", "PVT_crewbook")
			if scenario == "dead" {
				if err != nil || fixture.calls(t) == 0 {
					t.Fatalf("dead owner not recovered: %v %s", err, stderr)
				}
			} else if err == nil || fixture.calls(t) != 0 {
				t.Fatalf("unsafe takeover: %v %s mutations %d", err, stderr, fixture.calls(t))
			}
		})
	}
}

func TestBoardSnapshotConfigureHeldLockAndSuccessor(t *testing.T) {
	for _, successor := range []bool{false, true} {
		t.Run(strconv.FormatBool(successor), func(t *testing.T) {
			fixture := newSchemaBoard(t)
			lock := filepath.Join(filepath.Dir(fixture.snap), ".board-configure-PVT_crewbook.lock")
			barrier := filepath.Join(fixture.bin, "barrier")
			env := append(schemaEnv(fixture), "FAKE_LOCK_BARRIER="+barrier)
			if successor {
				env = append(env, "FAKE_LOCK_SUCCESSOR="+lock)
			}
			finished := make(chan error, 1)
			go func() {
				_, _, err := fixture.runEnv(t, env, "configure", "PVT_crewbook")
				finished <- err
			}()
			defer func() {
				_ = os.WriteFile(filepath.Join(barrier, "release"), nil, 0o600)
			}()
			deadline := time.Now().Add(10 * time.Second)
			for {
				if _, err := os.Stat(barrier); err == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("owner never reached schema barrier")
				}
				time.Sleep(10 * time.Millisecond)
			}
			if err := os.WriteFile(filepath.Join(lock, "ts"), []byte("1\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			startup := filepath.Join(fixture.bin, "startup")
			if err := os.WriteFile(startup, []byte("sleep() { :; }\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, _, err := fixture.runEnv(t, append(schemaEnv(fixture), "BASH_ENV="+startup), "configure", "PVT_crewbook"); err == nil || fixture.calls(t) != 0 {
				t.Fatal("second configuration stole aged active lock")
			}
			if err := os.WriteFile(filepath.Join(barrier, "release"), nil, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := <-finished; err != nil {
				t.Fatal(err)
			}
			root, err := os.OpenRoot(filepath.Dir(lock))
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			owner, err := root.ReadFile(filepath.Base(lock) + "/owner")
			if successor {
				if err != nil || !strings.HasSuffix(string(owner), " successor-generation\n") {
					t.Fatalf("successor removed: %q %v", owner, err)
				}
			} else if !os.IsNotExist(err) {
				t.Fatalf("owner did not release: %v", err)
			}
			if fixture.calls(t) != 13 {
				t.Fatalf("expected one configuration, got %d mutations", fixture.calls(t))
			}
		})
	}
}

func TestBoardSnapshotConfigureRepeatPreservesOptions(t *testing.T) {
	t.Parallel()
	fixture := newSchemaBoard(t)
	for attempt := 0; attempt < 2; attempt++ {
		stdout, stderr, err := fixture.runEnv(t, schemaEnv(fixture), "configure", "PVT_crewbook")
		if err != nil {
			t.Fatalf("configure: %v %s", err, stderr)
		}
		var metadata struct {
			Fields []struct {
				Name    string
				Options []struct{ ID, Name string }
			}
			Views []struct{ Name, Filter string }
		}
		if err := json.Unmarshal([]byte(stdout), &metadata); err != nil || len(metadata.Fields) != 6 || len(metadata.Views) != 5 {
			t.Fatalf("metadata: %v %s", err, stdout)
		}
		for _, field := range metadata.Fields {
			if field.Name == "Status" {
				if len(field.Options) != 5 || field.Options[0].ID != "O_todo" || field.Options[4].ID != "O_done" {
					t.Fatalf("status identities changed: %+v", field.Options)
				}
			}
		}
	}
	calls := fixture.lines(t)
	if len(calls) != 13 {
		t.Fatalf("mutations %d, want 3 fields and 5 create/filter view pairs", len(calls))
	}
	for _, call := range calls {
		if strings.Contains(call, "Workflow") || strings.Contains(call, "ItemFieldValue") || strings.Contains(call, "createProjectV2(input") || strings.Contains(call, "PVT_kwHNjWrOAZVCuA") {
			t.Fatalf("unexpected mutation: %s", call)
		}
	}
}

func TestBoardSnapshotConfigurePartialFailureRetry(t *testing.T) {
	t.Parallel()
	fixture := newSchemaBoard(t)
	env := schemaEnv(fixture)
	_, _, err := fixture.runEnv(t, append(env, "FAKE_SCHEMA_FAIL=Session"), "configure", "PVT_crewbook")
	if err == nil {
		t.Fatal("partial GraphQL error accepted")
	}
	if _, stderr, err := fixture.runEnv(t, env, "configure", "PVT_crewbook"); err != nil {
		t.Fatalf("retry: %v %s", err, stderr)
	}
	if _, _, err := fixture.runEnv(t, append(env, "FAKE_SCHEMA_FAIL_PAGE=views"), "metadata", "schema"); err == nil {
		t.Fatal("incomplete metadata accepted")
	}
}

func TestBoardSnapshotConfigurePaginationAndPreflight(t *testing.T) {
	t.Parallel()
	fixture := newSchemaBoard(t)
	env := append(schemaEnv(fixture), "FAKE_SCHEMA_PAGINATE=fields")
	if _, _, err := fixture.runEnv(t, append(env, "FAKE_SCHEMA_FAIL_AFTER=1"), "configure", "PVT_crewbook"); err == nil || fixture.calls(t) != 0 {
		t.Fatal("incomplete fields allowed configuration mutation")
	}
	if _, stderr, err := fixture.runEnv(t, env, "configure", "PVT_crewbook"); err != nil {
		t.Fatalf("paginated fields: %v %s", err, stderr)
	}
	before := fixture.calls(t)
	if _, _, err := fixture.runEnv(t, schemaEnv(fixture), "configure", "PVT_wrong"); err == nil || fixture.calls(t) != before {
		t.Fatal("wrong confirmation allowed mutation")
	}
}

func TestBoardSnapshotConfigureFieldsIndependentOfViews(t *testing.T) {
	t.Parallel()
	fixture := newSchemaBoard(t)
	env := append(schemaEnv(fixture), "FAKE_SCHEMA_FAIL_PAGE=views")
	stdout, stderr, err := fixture.runEnv(t, env, "configure-fields", "PVT_crewbook")
	if err != nil {
		t.Fatalf("fields-only configure: %v %s", err, stderr)
	}
	var metadata struct {
		Fields    []map[string]any
		Views     any
		Workflows any
	}
	if err := json.Unmarshal([]byte(stdout), &metadata); err != nil || len(metadata.Fields) != 6 || metadata.Views != nil || metadata.Workflows != nil {
		t.Fatalf("fields-only metadata %s: %v", stdout, err)
	}
	if len(fixture.lines(t)) != 3 {
		t.Fatalf("mutations: %v", fixture.lines(t))
	}
	for _, call := range fixture.lines(t) {
		if strings.Contains(call, "ProjectV2View") || strings.Contains(call, "ItemFieldValue") {
			t.Fatalf("fields-only mode changed views or cards: %s", call)
		}
	}
}

var defaultStatuses = []string{"Todo", "In progress", "Blocked", "In review", "Done"}

func sessionSchemaFixture(t *testing.T, options string) board {
	t.Helper()
	return sessionSchemaFixtureWithStatuses(t, options, defaultStatuses)
}

// sessionSchemaFixtureWithStatuses gives the Status field the named options as
// status-0, status-1, ... in order.
func sessionSchemaFixtureWithStatuses(t *testing.T, options string, statusNames []string) board {
	t.Helper()
	fixture := newSchemaBoard(t)
	path := filepath.Join(fixture.bin, "schema.json")
	data, err := os.ReadFile(path) //nolint:gosec // a test fixture path
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	var sessionOptions []map[string]any
	if err := json.Unmarshal([]byte(options), &sessionOptions); err != nil {
		t.Fatal(err)
	}
	fields := state["fields"].([]any)
	for _, field := range fields {
		current := field.(map[string]any)
		if current["name"] == "Status" {
			statuses := []map[string]any{}
			for index, name := range statusNames {
				statuses = append(statuses, map[string]any{"id": "status-" + strconv.Itoa(index), "name": name, "color": "GRAY", "description": ""})
			}
			current["options"] = statuses
		}
	}
	priorities := []map[string]any{}
	for _, name := range []string{"P1", "P2", "P3"} {
		priorities = append(priorities, map[string]any{"id": "priority-" + name, "name": name, "color": "GRAY", "description": ""})
	}
	state["fields"] = append(fields, map[string]any{"id": "F_session", "name": "Session", "dataType": "SINGLE_SELECT", "options": sessionOptions}, map[string]any{"id": "F_priority", "name": "Priority", "dataType": "SINGLE_SELECT", "options": priorities})
	updated, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, updated, 0o600); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func TestBoardSnapshotConfigureMigratesVerifiedSessionPrefix(t *testing.T) {
	t.Parallel()
	legacy := []map[string]any{}
	for _, role := range []string{"design", "platform", "runtime", "review", "verify", "docs", "spikes"} {
		legacy = append(legacy, map[string]any{"id": "legacy-" + role, "name": "wh/" + role, "color": "BLUE", "description": role})
	}
	legacy = append(legacy, map[string]any{"id": "legacy-human", "name": "Werner", "color": "GRAY", "description": "Human maintainer"})
	encoded, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	fixture := sessionSchemaFixture(t, string(encoded))
	env := append(schemaEnv(fixture), "WHR_BOARD_ROLES=desk,dispatch,design,review,platform,runtime,docs,verify,spikes")
	for attempt := 0; attempt < 2; attempt++ {
		stdout, stderr, err := fixture.runEnv(t, env, "configure-fields", "PVT_crewbook")
		if err != nil {
			t.Fatalf("migration: %v %s", err, stderr)
		}
		if !strings.Contains(stdout, `"cb/platform"`) || strings.Contains(stdout, `"wh/platform"`) || !strings.Contains(stdout, `"legacy-human"`) {
			t.Fatalf("migration readback: %s", stdout)
		}
	}
	mutations := 0
	for _, call := range fixture.lines(t) {
		var request struct {
			Variables struct {
				Input struct {
					FieldID string
					Options []struct{ ID, Name, Color, Description string } `json:"singleSelectOptions"`
				}
			}
		}
		if err := json.Unmarshal([]byte(call), &request); err != nil {
			t.Fatal(err)
		}
		if request.Variables.Input.FieldID != "F_session" {
			t.Fatalf("migration changed another field: %s", call)
		}
		mutations++
		options := request.Variables.Input.Options
		if len(options) != 10 || options[0].Name != "cb/desk" || options[1].Name != "cb/dispatch" || options[9].ID != "legacy-human" || options[9].Name != "Werner" {
			t.Fatalf("missing roles or human identity changed: %+v", options)
		}
		for _, option := range options[2:9] {
			role := strings.TrimPrefix(option.Name, "cb/")
			if option.ID != "legacy-"+role || option.Color != "BLUE" || option.Description != role {
				t.Fatalf("existing option changed beyond its prefix: %+v", option)
			}
		}
	}
	if mutations != 1 {
		t.Fatalf("Session mutations %d, want one on initial migration only", mutations)
	}
	before := fixture.calls(t)
	if _, _, err := fixture.runEnv(t, env, "session", "7", "Werner"); err == nil || fixture.calls(t) != before {
		t.Fatal("legacy human option weakened cb lane validation")
	}
}

func TestBoardSnapshotConfigureRejectsAmbiguousOrUnknownSession(t *testing.T) {
	t.Parallel()
	for _, options := range []string{
		`[{"id":"source","name":"wh/platform"},{"id":"target","name":"cb/platform"}]`,
		`[{"id":"custom","name":"wh/custom"}]`,
		`[{"id":"custom","name":"Some other session"}]`,
	} {
		fixture := sessionSchemaFixture(t, options)
		if _, _, err := fixture.runEnv(t, schemaEnv(fixture), "configure-fields", "PVT_crewbook"); err == nil || fixture.calls(t) != 0 {
			t.Fatalf("ambiguous/unknown options reached mutation: %s", options)
		}
	}
}

func newBoard(t *testing.T) board {
	t.Helper()
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq is not installed")
	}
	b := board{bin: t.TempDir()}
	b.snap = filepath.Join(t.TempDir(), "cache", "board.json")
	b.log = filepath.Join(b.bin, "gh.log")
	b.pages(t, map[string]string{"first": page(fakeNodes, "")})
	gh := `#!/bin/sh
if [ "$1 $2" = "api rate_limit" ]; then
	echo "rate_limit" >> "` + b.bin + `/rate.log"
	echo "${FAKE_RATE:-{\"resources\":{\"graphql\":{\"limit\":5000,\"remaining\":4900,\"reset\":1791032178}}}}"
	exit 0
fi
echo "$*" >> "` + b.log + `"
sleep 0.3
if [ -n "$FAKE_GH_FAIL" ]; then echo "GraphQL: API rate limit exceeded" >&2; exit 1; fi
case "$*" in
*"repoOwner="*) echo '{"data":{"repository":{"nameWithOwner":"wstein/crewbook"},"node":{"id":"PVT_crewbook","number":10,"url":"https://github.com/users/wstein/projects/10","owner":{"login":"wstein"}},"user":{"projectV2":{"id":"PVT_crewbook","number":10,"url":"https://github.com/users/wstein/projects/10","owner":{"login":"wstein"}}}}}'; exit 0 ;;
*addProjectV2ItemById*) echo '{"data":{}}'; exit 0 ;;
*updateProjectV2ItemFieldValue*)
	for a in "$@"; do case "$a" in i=PVTI_*) item=${a#i=PVTI_} ;; o=*) opt=${a#o=} ;; f=*) fld=${a#f=} ;; esac; done
	if [ -z "$FAKE_NOAPPLY" ] && [ "$fld" = F_status ]; then
		case "$opt" in O_todo) st=Todo ;; O_ip) st="In progress" ;; O_bl) st=Blocked ;; O_ir) st="In review" ;; esac
		printf '%s' "$st" > "` + b.bin + `/cur.$item"
	fi
	echo '{"data":{}}'; exit 0 ;;
*projectItems*)
	for a in "$@"; do case "$a" in n=*) number=${a#n=} ;; esac; done
	if [ -n "$FAKE_NOITEM" ] || [ "$number" = 99 ]; then echo '{"data":{"repository":{"issue":{"projectItems":{"nodes":[]}}}}}'; exit 0; fi
	status=null
	if [ -f "` + b.bin + `/cur.$number" ]; then status="{\"name\":\"$(cat "` + b.bin + `/cur.$number")\"}"; fi
	echo '{"data":{"repository":{"issue":{"projectItems":{"nodes":[{"id":"PVTI_other","project":{"id":"PVT_other"}},{"id":"PVTI_'$number'","project":{"id":"PVT_kwHNjWrOAZVCuA"},"status":'"$status"'}]}}}}}'; exit 0 ;;
*"fields(first"*) echo '{"data":{"node":{"id":"PVT_kwHNjWrOAZVCuA","fields":{"pageInfo":{"hasNextPage":false,"endCursor":null},"nodes":[{},{"id":"F_status","name":"Status","options":[{"id":"O_todo","name":"Todo"},{"id":"O_ip","name":"In progress"},{"id":"O_bl","name":"Blocked"},{"id":"O_ir","name":"In review"}]},{"id":"F_sess","name":"Session","options":[{"id":"O_s1","name":"wh/review"},{"id":"O_s2","name":"Werner"}]},{"id":"F_prio","name":"Priority","options":[{"id":"O_p1","name":"P1"},{"id":"O_p3","name":"P3"}]}]}}}}'; exit 0 ;;
*"items(first"*)
	cur=first
	for a in "$@"; do case "$a" in after=*) cur=${a#after=} ;; esac; done
	if [ "$FAKE_FAIL_PAGE" = "$cur" ]; then echo "GraphQL: secondary rate limit" >&2; exit 1; fi
	jq '(.data.node.items.nodes[].content | select(.repository == null)) |= (. + {repository:{nameWithOwner:"wstein/workharbor"}})' "` + b.bin + `/pages/$cur.json"; exit 0 ;;
*"issue(number"*)
	for arg in "$@"; do case "$arg" in n=*) number=${arg#n=} ;; esac; done
	if [ "$number" = 99 ]; then echo '{"data":{"repository":{"issue":null}}}'; else printf '{"data":{"repository":{"issue":{"id":"I_%s","title":"A new title"}}}}\n' "$number"; fi
	exit 0 ;;
esac
echo '{}'
`
	if err := os.WriteFile(filepath.Join(b.bin, "gh"), []byte(gh), 0o755); err != nil { //nolint:gosec // a test fake
		t.Fatal(err)
	}
	return b
}

// pages writes the fake board's pages, named by the cursor that fetches them.
func (b board) pages(t *testing.T, p map[string]string) {
	t.Helper()
	dir := filepath.Join(b.bin, "pages")
	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // a test dir
		t.Fatal(err)
	}
	for name, body := range p {
		if err := os.WriteFile(filepath.Join(dir, name+".json"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func (b board) run(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	return b.runEnv(t, nil, args...)
}

func (b board) runEnv(t *testing.T, env []string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "bash", append([]string{"board-snapshot.sh"}, args...)...) //nolint:gosec // a test script
	cmd.Env = append([]string{
		"PATH=" + b.bin + ":" + os.Getenv("PATH"),
		"HOME=" + t.TempDir(),
		"WHR_BOARD_SNAPSHOT=" + b.snap,
	}, env...)
	var so, se strings.Builder
	cmd.Stdout, cmd.Stderr = &so, &se
	err = cmd.Run()
	return so.String(), se.String(), err
}

func (b board) calls(t *testing.T) int {
	t.Helper()
	data, err := os.ReadFile(b.log) //nolint:gosec // a test path
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return len(strings.Split(strings.TrimSpace(string(data)), "\n"))
}

func (b board) age(t *testing.T, seconds int) {
	t.Helper()
	data, err := os.ReadFile(b.snap) //nolint:gosec // a test path
	if err != nil {
		t.Fatal(err)
	}
	var s map[string]any
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	s["fetched_at"] = time.Now().Unix() - int64(seconds)
	out, _ := json.Marshal(s)
	if err := os.WriteFile(b.snap, out, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestBoardSnapshotFreshMakesNoCall(t *testing.T) {
	t.Parallel()
	b := newBoard(t)
	if _, se, err := b.run(t); err != nil {
		t.Fatalf("first run: %v %s", err, se)
	}
	if b.calls(t) != 1 {
		t.Fatalf("first run made %d calls, want 1", b.calls(t))
	}
	out, _, err := b.run(t)
	if err != nil || b.calls(t) != 1 {
		t.Fatalf("second run: err %v, calls %d, want 1", err, b.calls(t))
	}
	var s struct {
		FetchedAt int64            `json:"fetched_at"`
		Items     []map[string]any `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &s); err != nil || s.FetchedAt == 0 || len(s.Items) != 5 {
		t.Fatalf("snapshot = %s (%v)", out, err)
	}
}

func TestBoardSnapshotStaleQueriesOnceWithModes(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("slow: runs in the full suite (make test)")
	}
	b := newBoard(t)
	if _, _, err := b.run(t); err != nil {
		t.Fatal(err)
	}
	b.age(t, 301)
	if _, se, err := b.run(t); err != nil || b.calls(t) != 2 {
		t.Fatalf("stale: err %v %s, calls %d, want 2", err, se, b.calls(t))
	}
	st, err := os.Stat(b.snap)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %v, %v; want 0600", st, err)
	}
	if st, err := os.Stat(filepath.Dir(b.snap)); err != nil || st.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode = %v, %v; want 0700", st, err)
	}
	// A custom max age is honoured.
	b.age(t, 20)
	if _, _, err := b.runEnv(t, []string{"WHR_BOARD_MAX_AGE=10"}); err != nil || b.calls(t) != 3 {
		t.Fatalf("max age 10: err %v, calls %d, want 3", err, b.calls(t))
	}
}

func TestBoardSnapshotConcurrentCallsQueryOnce(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("slow: runs in the full suite (make test)")
	}
	b := newBoard(t)
	if _, _, err := b.run(t); err != nil {
		t.Fatal(err)
	}
	b.age(t, 400)
	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, se, err := b.run(t); err != nil {
				t.Errorf("run: %v %s", err, se)
			}
		}()
	}
	wg.Wait()
	if b.calls(t) != 2 {
		t.Fatalf("calls = %d, want 2 (one first fill, one for three concurrent callers)", b.calls(t))
	}
}

func TestBoardSnapshotFailureKeepsOldFile(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("slow: runs in the full suite (make test)")
	}
	b := newBoard(t)
	if _, _, err := b.run(t); err != nil {
		t.Fatal(err)
	}
	b.age(t, 400)
	before, _ := os.ReadFile(b.snap) //nolint:gosec // a test path
	out, se, err := b.runEnv(t, []string{"FAKE_GH_FAIL=1"})
	if err != nil {
		t.Fatalf("a stale copy must exit 0: %v", err)
	}
	if !strings.Contains(se, "stale") || out != string(before) {
		t.Fatalf("stderr %q, out kept = %v", se, out == string(before))
	}
	after, _ := os.ReadFile(b.snap) //nolint:gosec // a test path
	if string(after) != string(before) {
		t.Fatal("the failed query changed the file")
	}
	// No file at all: exit 1.
	if err := os.Remove(b.snap); err != nil {
		t.Fatal(err)
	}
	if _, _, err := b.runEnv(t, []string{"FAKE_GH_FAIL=1"}); err == nil {
		t.Fatal("no snapshot and a failing query must fail")
	}
}

func TestBoardSnapshotRefreshForcesCall(t *testing.T) {
	t.Parallel()
	b := newBoard(t)
	if _, _, err := b.run(t); err != nil {
		t.Fatal(err)
	}
	if _, _, err := b.run(t, "--refresh"); err != nil || b.calls(t) != 2 {
		t.Fatalf("--refresh: err %v, calls %d, want 2", err, b.calls(t))
	}
}

func TestBoardSnapshotReaders(t *testing.T) {
	t.Parallel()
	b := newBoard(t)
	out, se, err := b.run(t, "queue", "wh/platform")
	if err != nil {
		t.Fatalf("queue: %v %s", err, se)
	}
	var nums []string
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		nums = append(nums, strings.Split(l, "\t")[1])
	}
	if strings.Join(nums, " ") != "#30 #10 #20" {
		t.Fatalf("queue order = %v", nums)
	}
	out, _, err = b.run(t, "card", "40")
	if err != nil || !strings.HasPrefix(out, "#40\tIn progress\twh/platform\tP1\t") {
		t.Fatalf("card = %q, %v", out, err)
	}
	if b.calls(t) != 1 {
		t.Fatalf("readers made %d calls, want 1", b.calls(t))
	}
}

func TestBoardSnapshotRejectsBadEnv(t *testing.T) {
	t.Parallel()
	b := newBoard(t)
	for _, env := range [][]string{
		{"WHR_BOARD_SNAPSHOT=relative/board.json"},
		{"WHR_BOARD_SNAPSHOT=/tmp/a\nb"},
		{"WHR_BOARD_MAX_AGE=soon"},
		{"WHR_BOARD_MAX_AGE=" + strconv.Itoa(-1)},
	} {
		if _, _, err := b.runEnv(t, env); err == nil {
			t.Errorf("%v accepted", env)
		}
	}
	if b.calls(t) != 0 {
		t.Fatal("a rejected setting must not call gh")
	}
}

func (b board) lines(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(b.log) //nolint:gosec // a test path
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

func (b board) card(t *testing.T, n string) string {
	t.Helper()
	out, se, err := b.run(t, "card", n)
	if err != nil {
		t.Fatalf("card %s: %v %s", n, err, se)
	}
	return strings.TrimSpace(out)
}

func TestBoardSnapshotMovePatchesCache(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("slow: runs in the full suite (make test)")
	}
	b := newBoard(t)
	if _, _, err := b.run(t); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(b.snap) //nolint:gosec // a test path
	if _, se, err := b.run(t, "move", "20", "In review"); err != nil {
		t.Fatalf("move: %v %s", err, se)
	}
	if _, se, err := b.run(t, "session", "20", "wh/review"); err != nil {
		t.Fatalf("session: %v %s", err, se)
	}
	if _, se, err := b.run(t, "priority", "20", "P3"); err != nil {
		t.Fatalf("priority: %v %s", err, se)
	}
	if got := b.card(t, "20"); !strings.HasPrefix(got, "#20\tIn review\twh/review\tP3\t") {
		t.Fatalf("card = %q", got)
	}
	l := b.lines(t)
	// 1 refresh, 1 field-ID query (cached), then lookup + mutation per write
	// (a move adds one read-back lookup).
	if len(l) != 1+1+3+2+2 {
		t.Fatalf("gh calls = %d, want 9: %q", len(l), l)
	}
	joined := strings.Join(l, "\n")
	for _, w := range []string{"-f f=F_status -f o=O_ir", "-f f=F_sess -f o=O_s1", "-f f=F_prio -f o=O_p3", "-f i=PVTI_20", "-F n=20"} {
		if !strings.Contains(joined, w) {
			t.Fatalf("missing %q in %q", w, l)
		}
	}
	if strings.Contains(joined, "item-edit") || strings.Contains(joined, "item-list 6 --owner wstein --url") {
		t.Fatalf("the --url route is used: %q", l)
	}
	after, _ := os.ReadFile(b.snap) //nolint:gosec // a test path
	var x, y struct {
		FetchedAt int64 `json:"fetched_at"`
	}
	_ = json.Unmarshal(before, &x)
	_ = json.Unmarshal(after, &y)
	if x.FetchedAt != y.FetchedAt {
		t.Fatal("a move changed fetched_at")
	}
}

func TestBoardSnapshotFailedMoveKeepsCache(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("slow: runs in the full suite (make test)")
	}
	b := newBoard(t)
	if _, _, err := b.run(t); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(b.snap) //nolint:gosec // a test path
	for _, args := range [][]string{{"move", "20", "Blocked"}, {"session", "20", "Werner"}, {"priority", "20", "P1"}, {"add", "99"}} {
		_, se, err := b.runEnv(t, []string{"FAKE_GH_FAIL=1"}, args...)
		if err == nil || se == "" {
			t.Fatalf("%v: err %v, stderr %q; want a failure with a message", args, err, se)
		}
	}
	after, _ := os.ReadFile(b.snap) //nolint:gosec // a test path
	if string(after) != string(before) {
		t.Fatal("a failed write changed the cache")
	}
}

func TestBoardSnapshotConcurrentMoves(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("slow: runs in the full suite (make test)")
	}
	b := newBoard(t)
	if _, _, err := b.run(t); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for _, n := range []string{"10", "20", "30", "40"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, se, err := b.run(t, "move", n, "Blocked"); err != nil {
				t.Errorf("move %s: %v %s", n, err, se)
			}
		}()
	}
	wg.Wait()
	for _, n := range []string{"10", "20", "30", "40"} {
		if got := b.card(t, n); !strings.Contains(got, "\tBlocked\t") {
			t.Errorf("card %s = %q, want Blocked", n, got)
		}
	}
}

func TestBoardSnapshotMoveWithoutCacheWritesOnly(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("slow: runs in the full suite (make test)")
	}
	b := newBoard(t)
	_, se, err := b.run(t, "move", "20", "Blocked")
	if err != nil || !strings.Contains(se, "cache") {
		t.Fatalf("err %v, stderr %q", err, se)
	}
	if _, err := os.Stat(b.snap); err == nil {
		t.Fatal("a move created a cache")
	}
	if len(b.lines(t)) != 4 {
		t.Fatalf("gh calls = %q, want fields, lookup, mutation and read-back", b.lines(t))
	}
	// A stale cache is not patched either.
	if _, _, err := b.run(t); err != nil {
		t.Fatal(err)
	}
	b.age(t, 400)
	before, _ := os.ReadFile(b.snap) //nolint:gosec // a test path
	if _, _, err := b.run(t, "move", "20", "Blocked"); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(b.snap) //nolint:gosec // a test path
	if string(after) != string(before) {
		t.Fatal("a stale cache was patched")
	}
}

func TestBoardSnapshotAdd(t *testing.T) {
	t.Parallel()
	b := newBoard(t)
	if _, _, err := b.run(t); err != nil {
		t.Fatal(err)
	}
	if _, se, err := b.run(t, "add", "77"); err != nil {
		t.Fatalf("add: %v %s", err, se)
	}
	l := b.lines(t)
	if len(l) != 3 || !strings.Contains(l[1], "issue(number") || !strings.Contains(l[2], "addProjectV2ItemById") ||
		!strings.Contains(l[2], "-f c=I_77") {
		t.Fatalf("gh calls = %q", l)
	}
	if got := b.card(t, "77"); !strings.HasPrefix(got, "#77\t") || !strings.HasSuffix(got, "A new title") {
		t.Fatalf("card = %q", got)
	}
}

func TestBoardSnapshotWriteRejectsBadValues(t *testing.T) {
	t.Parallel()
	b := newBoard(t)
	if _, _, err := b.run(t); err != nil {
		t.Fatal(err)
	}
	base := b.calls(t)
	before, _ := os.ReadFile(b.snap) //nolint:gosec // a test path
	bad := [][]string{
		{"move", "", "Done"},
		{"move", "0", "Done"},
		{"move", "1 2", "Done"},
		{"move", "12\n3", "Done"},
		{"move", "$(id)", "Done"},
		{"move", "12;id", "Done"},
		{"move", "-1", "Done"},
		{"move", "12", "Merged"},
		{"move", "12", "Done; id"},
		{"move", "12", "$(id)"},
		{"move", "12", "Done\nTodo"},
		{"move", "12", "done"},
		{"move", "12"},
		{"move"},
		{"session", "12", "wh/nobody"},
		{"session", "12", "wh/platform extra"},
		{"session", "12", "`id`"},
		{"priority", "12", "P4"},
		{"priority", "12", "P1 "},
		{"priority", "12", "P1\nP2"},
		{"priority", "x", "P1"},
		{"add", "12 13"},
		{"add", "12", "x"},
		{"move", "12", "13", "Done"},
		{"move", "12", "13", "Merged"},
		{"move", "12", "x", "Todo"},
		{"session", "12", "13", "wh/nobody"},
		{"priority", "12", "0", "P1"},
		{"add", "$(id)"},
		{"add", ""},
		{"add"},
	}
	for _, args := range bad {
		if _, _, err := b.run(t, args...); err == nil {
			t.Errorf("%q accepted", args)
		}
	}
	if b.calls(t) != base {
		t.Fatalf("a rejected value reached gh: %q", b.lines(t))
	}
	after, _ := os.ReadFile(b.snap) //nolint:gosec // a test path
	if string(after) != string(before) {
		t.Fatal("a rejected value changed the cache")
	}
}

func TestBoardSnapshotMoveRefusesDone(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("slow: runs in the full suite (make test)")
	}
	b := newBoard(t)
	if _, _, err := b.run(t); err != nil {
		t.Fatal(err)
	}
	base := b.calls(t)
	for st, want := range map[string]string{"Done": "does not set", "Ready to push": "status must be one of"} {
		_, se, err := b.run(t, "move", "20", st)
		if err == nil || !strings.Contains(se, want) {
			t.Fatalf("move %q: err %v, stderr %q; want a refusal saying %q", st, err, se, want)
		}
	}
	if b.calls(t) != base {
		t.Fatalf("a refused move made a gh call: %q", b.lines(t))
	}
	for _, st := range []string{"Todo", "In progress", "Blocked", "In review"} {
		if _, se, err := b.run(t, "move", "20", st); err != nil {
			t.Fatalf("move %q: %v %s", st, err, se)
		}
	}
}

func TestBoardSnapshotStaleLockTakeoverConcurrent(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("slow: runs in the full suite (make test)")
	}
	b := newBoard(t)
	if _, _, err := b.run(t); err != nil {
		t.Fatal(err)
	}
	lock := filepath.Join(filepath.Dir(b.snap), ".board.lock")
	if err := os.Mkdir(lock, 0o700); err != nil {
		t.Fatal(err)
	}
	old := strconv.FormatInt(time.Now().Unix()-1000, 10)
	if err := os.WriteFile(filepath.Join(lock, "ts"), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	nums := []string{"10", "20", "30", "40", "50"}
	var wg sync.WaitGroup
	for _, n := range nums {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, se, err := b.run(t, "move", n, "Blocked"); err != nil {
				t.Errorf("move %s: %v %s", n, err, se)
			}
		}()
	}
	wg.Wait()
	for _, n := range nums {
		if got := b.card(t, n); !strings.Contains(got, "\tBlocked\t") {
			t.Errorf("card %s = %q, want Blocked (a write was lost)", n, got)
		}
	}
	if got := len(b.lines(t)); got < 1+3*len(nums) || got > 1+4*len(nums) {
		t.Errorf("gh calls = %d, want %d to %d", got, 1+3*len(nums), 1+4*len(nums))
	}
}

func TestBoardSnapshotWritesNeverUseURLRouteAndCacheFields(t *testing.T) {
	t.Parallel()
	b := newBoard(t)
	for _, v := range []string{"Blocked", "Todo"} {
		if _, se, err := b.run(t, "move", "20", v); err != nil {
			t.Fatalf("move: %v %s", err, se)
		}
	}
	// Two moves: one field query (cached next to the snapshot), a lookup, a
	// mutation and a read-back each.
	if got := len(b.lines(t)); got != 7 {
		t.Fatalf("gh calls = %d, want 7: %q", got, b.lines(t))
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(b.snap), "board-fields.json")); err != nil {
		t.Fatal(err)
	}
	// A card that is not on the board fails and says so.
	if _, se, err := b.runEnv(t, []string{"FAKE_NOITEM=1"}, "move", "20", "Todo"); err == nil || !strings.Contains(se, "not on the board") {
		t.Fatalf("err %v, stderr %q; want not on the board", err, se)
	}
}

func TestBoardSnapshotWarnsUnder20Percent(t *testing.T) {
	t.Parallel()
	b := newBoard(t)
	low := `{"resources":{"graphql":{"limit":5000,"remaining":900,"reset":1791032178}}}`
	out, se, err := b.runEnv(t, []string{"FAKE_RATE=" + low})
	if err != nil || !strings.Contains(se, "900 of 5000") || !strings.Contains(se, "resets at") {
		t.Fatalf("err %v, stderr %q; want a warning with the numbers and reset", err, se)
	}
	if strings.Contains(out, "warning") {
		t.Fatal("the warning went to stdout")
	}
	// Cached run: no GitHub call, no rate check, no warning.
	if _, se, _ := b.runEnv(t, []string{"FAKE_RATE=" + low}); se != "" {
		t.Fatalf("a cache hit warned: %q", se)
	}
	// Plenty left: silent.
	b.age(t, 400)
	if _, se, err := b.run(t); err != nil || se != "" {
		t.Fatalf("err %v, stderr %q; want silence at 98%%", err, se)
	}
	// Only rate_limit calls are free: they are not in the gh log.
	for _, l := range b.lines(t) {
		if strings.Contains(l, "rate_limit") {
			t.Fatalf("rate_limit logged as a counted call: %q", l)
		}
	}
}

func TestBoardSnapshotPagination(t *testing.T) {
	t.Parallel()
	b := newBoard(t)
	one := func(n int) string {
		return `[{"content":{"__typename":"Issue","number":` + strconv.Itoa(n) + `,"title":"t","url":"u"},"status":{"name":"Todo"},"session":{"name":"wh/platform"},"priority":{"name":"P1"}}]`
	}
	b.pages(t, map[string]string{"first": page(one(1), "c1"), "c1": page(one(2), "c2"), "c2": page(one(3), "")})
	out, se, err := b.run(t)
	if err != nil || se != "" {
		t.Fatalf("err %v, stderr %q", err, se)
	}
	var s struct {
		Items []struct {
			Number int
			Type   string
			Status string
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &s); err != nil || len(s.Items) != 3 || s.Items[2].Number != 3 || s.Items[0].Type != "Issue" || s.Items[0].Status != "Todo" {
		t.Fatalf("snapshot = %s (%v)", out, err)
	}
	if b.calls(t) != 3 {
		t.Fatalf("calls = %d, want 3 pages", b.calls(t))
	}
	for _, l := range b.lines(t) {
		if strings.Contains(l, "item-list") {
			t.Fatalf("the board read still uses item-list: %q", l)
		}
	}
}

func TestBoardSnapshotFailingPageKeepsOldFile(t *testing.T) {
	t.Parallel()
	b := newBoard(t)
	if _, _, err := b.run(t); err != nil {
		t.Fatal(err)
	}
	b.age(t, 400)
	before, _ := os.ReadFile(b.snap) //nolint:gosec // a test path
	b.pages(t, map[string]string{"first": page(fakeNodes, "c1"), "c1": page(fakeNodes, "")})
	out, se, err := b.runEnv(t, []string{"FAKE_FAIL_PAGE=c1"})
	if err != nil || !strings.Contains(se, "stale") || out != string(before) {
		t.Fatalf("err %v, stderr %q, old snapshot kept = %v", err, se, out == string(before))
	}
	after, _ := os.ReadFile(b.snap) //nolint:gosec // a test path
	if string(after) != string(before) {
		t.Fatal("a failed page changed the file")
	}
}

func TestBoardSnapshotSeveralIssues(t *testing.T) {
	t.Parallel()
	b := newBoard(t)
	if _, _, err := b.run(t); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"move", "10", "20", "30", "Blocked"},
		{"session", "10", "20", "Werner"},
		{"priority", "10", "20", "P3"},
	} {
		if _, se, err := b.run(t, args...); err != nil {
			t.Fatalf("%v: %v %s", args, err, se)
		}
	}
	if got := b.card(t, "10"); !strings.HasPrefix(got, "#10\tBlocked\tWerner\tP3\t") {
		t.Fatalf("card 10 = %q", got)
	}
	if got := b.card(t, "30"); !strings.HasPrefix(got, "#30\tBlocked\t") {
		t.Fatalf("card 30 = %q", got)
	}
	if _, se, err := b.run(t, "add", "77", "78"); err != nil {
		t.Fatalf("add: %v %s", err, se)
	}
	if got := b.card(t, "78"); !strings.HasPrefix(got, "#78\t") {
		t.Fatalf("card 78 = %q", got)
	}
}

func TestBoardSnapshotSeveralIssuesPartialFailure(t *testing.T) {
	t.Parallel()
	b := newBoard(t)
	if _, _, err := b.run(t); err != nil {
		t.Fatal(err)
	}
	_, se, err := b.run(t, "move", "10", "99", "20", "Blocked")
	if err == nil || !strings.Contains(se, "#99") {
		t.Fatalf("err %v, stderr %q; want exit 1 naming #99", err, se)
	}
	for _, n := range []string{"10", "20"} {
		if got := b.card(t, n); !strings.Contains(got, "\tBlocked\t") {
			t.Errorf("card %s = %q, want Blocked despite the failure on #99", n, got)
		}
	}
}

func rateLimitPage(cost, remaining int) string {
	return `{"data":{"rateLimit":{"cost":` + strconv.Itoa(cost) + `,"remaining":` + strconv.Itoa(remaining) +
		`,"limit":5000,"resetAt":"2026-10-03T12:00:00Z"},"node":{"items":{"pageInfo":{"hasNextPage":false,"endCursor":null},"nodes":` + fakeNodes + `}}}}`
}

func TestBoardSnapshotBudgetLogAndCommand(t *testing.T) {
	t.Parallel()
	b := newBoard(t)
	if out, _, err := b.run(t, "budget"); err != nil || !strings.Contains(out, "no refresh") {
		t.Fatalf("budget with no log: %v %q", err, out)
	}
	b.pages(t, map[string]string{"first": rateLimitPage(2, 4000)})
	if _, _, err := b.run(t); err != nil {
		t.Fatal(err)
	}
	b.pages(t, map[string]string{"first": rateLimitPage(3, 3500)})
	if _, _, err := b.run(t, "--refresh"); err != nil {
		t.Fatal(err)
	}
	logf := filepath.Join(filepath.Dir(b.snap), "board-budget.log")
	st, err := os.Stat(logf)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("log mode %v, want 0600", st.Mode().Perm())
	}
	data, _ := os.ReadFile(logf) //nolint:gosec // a test path
	if n := len(strings.Split(strings.TrimSpace(string(data)), "\n")); n != 2 {
		t.Fatalf("%d log lines, want 2: %s", n, data)
	}
	// An old line outside the 24 hours does not count.
	old := `{"at":1,"cost":99,"remaining":1,"limit":5000}` + "\ngarbage{{\n"
	if err := os.WriteFile(logf, append([]byte(old), data...), 0o600); err != nil { //nolint:gosec // a test path
		t.Fatal(err)
	}
	out, _, err := b.run(t, "budget")
	if err != nil || !strings.Contains(out, "refreshes: 2") || !strings.Contains(out, "total cost: 5") ||
		!strings.Contains(out, "lowest remaining: 3500 of 5000") {
		t.Fatalf("budget: %v %q", err, out)
	}
}

func TestBoardSnapshotWarnsFromQueryRemaining(t *testing.T) {
	t.Parallel()
	b := newBoard(t)
	// rate_limit lags and says plenty; the query's own value says 10 %.
	b.pages(t, map[string]string{"first": rateLimitPage(1, 500)})
	_, se, err := b.runEnv(t, []string{"TZ=UTC"})
	if err != nil || !strings.Contains(se, "500 of 5000") || !strings.Contains(se, "resets at 12:00") {
		t.Fatalf("err %v, stderr %q; want the warning from the query's value", err, se)
	}
}

func (b board) mutations(t *testing.T) int {
	t.Helper()
	n := 0
	for _, l := range b.lines(t) {
		if strings.Contains(l, "updateProjectV2ItemFieldValue") {
			n++
		}
	}
	return n
}

func TestBoardSnapshotMoveReadsBackAndIsIdempotent(t *testing.T) {
	t.Parallel()
	b := newBoard(t)
	so, se, err := b.run(t, "move", "20", "Blocked")
	if err != nil || strings.TrimSpace(so) != "#20 -> Blocked" {
		t.Fatalf("move: %v %q %s", err, so, se)
	}
	lines := b.lines(t)
	if len(lines) != 4 || !strings.Contains(lines[3], "issue(number") {
		t.Fatalf("want a fresh single-card read after the write: %q", lines)
	}
	base := b.calls(t)
	so, _, err = b.run(t, "move", "20", "Blocked")
	if err != nil || strings.TrimSpace(so) != "#20 already Blocked" || b.mutations(t) != 1 || b.calls(t) != base+1 {
		t.Fatalf("repeat: %v %q mutations %d calls %d", err, so, b.mutations(t), b.calls(t)-base)
	}
}

func TestBoardSnapshotMoveReadBackMismatchFails(t *testing.T) {
	t.Parallel()
	b := newBoard(t)
	if _, _, err := b.run(t); err != nil {
		t.Fatal(err)
	}
	_, se, err := b.runEnv(t, []string{"FAKE_NOAPPLY=1"}, "move", "20", "Blocked")
	if err == nil || !strings.Contains(se, "read-back") || !strings.Contains(se, "#20") {
		t.Fatalf("err %v, stderr %q; want a read-back failure", err, se)
	}
	if got := b.card(t, "20"); !strings.Contains(got, "\tTodo\t") {
		t.Fatalf("the cache was patched after a mismatch: %q", got)
	}
}

const syncNodes = `[
{"content":{"__typename":"Issue","number":10,"title":"a","state":"OPEN"},"status":{"name":"Todo"}},
{"content":{"__typename":"Issue","number":20,"title":"b","state":"OPEN"},"status":{"name":"Todo"}},
{"content":{"__typename":"Issue","number":30,"title":"c","state":"OPEN"}},
{"content":{"__typename":"Issue","number":40,"title":"d","state":"OPEN"},"status":{"name":"In progress"}},
{"content":{"__typename":"Issue","number":50,"title":"e","state":"OPEN"},"status":{"name":"Todo"}},
{"content":{"__typename":"Issue","number":60,"title":"f","state":"OPEN"},"status":{"name":"In review"}},
{"content":{"__typename":"Issue","number":70,"title":"g","state":"OPEN"},"status":{"name":"Ready to push"}},
{"content":{"__typename":"Issue","number":80,"title":"h","state":"CLOSED"},"status":{"name":"Todo"}},
{"content":{"__typename":"Issue","number":90,"title":"i","state":"OPEN"},"status":{"name":"Done"}}
]`

const syncRegistry = `crewbook-registry: 1
mode: split

## 20-docs
phase: blocked

## #30
phase: Start Requested

## 60
phase: start requested

## 70
phase: blocked

## 80
phase: blocked

## 90
phase: blocked

## lane-A
phase: author started (outcome: launched, running)

Resume: log lines follow
## 50
phase: blocked
`

// syncFixture is a board whose cards, registry and worktrees give every rule
// of the sync table one case; it returns the args for `sync`.
func syncFixture(t *testing.T) (board, []string) {
	t.Helper()
	b := newBoard(t)
	b.pages(t, map[string]string{"first": page(syncNodes, "")})
	repo := t.TempDir()
	git := func(dir string, args ...string) {
		t.Helper()
		if out, err := gittest.Git(t.Context(), "", dir, gittest.Identity, args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	git(repo, "init", "-q", "-b", "main")
	git(repo, "commit", "-q", "--allow-empty", "-m", "init")
	git(repo, "worktree", "add", "-q", "-b", "feat/10-thing", filepath.Join(t.TempDir(), "wt10"))
	git(repo, "worktree", "add", "-q", "-b", "scratch", filepath.Join(t.TempDir(), "wt-scratch"))
	reg := filepath.Join(t.TempDir(), "registry.md")
	if err := os.WriteFile(reg, []byte(syncRegistry), 0o600); err != nil {
		t.Fatal(err)
	}
	return b, []string{"sync", "--root", repo, "--registry", reg}
}

func (b board) sync(t *testing.T, args ...string) (lines []string, stderr string, err error) {
	t.Helper()
	so, se, err := b.runEnv(t, []string{"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null"}, args...)
	lines = strings.Split(strings.TrimSpace(so), "\n")
	if strings.TrimSpace(so) == "" {
		lines = nil
	}
	slices.Sort(lines)
	return lines, se, err
}

func TestBoardSnapshotSyncReconcilesAndIsIdempotent(t *testing.T) {
	t.Parallel()
	b, args := syncFixture(t)
	want := []string{
		"#10 Todo -> In progress (worktree branch)",
		"#20 Todo -> Blocked (registry phase blocked)",
		"#30 (none) -> In progress (registry phase start requested)",
		"#40 In progress -> Todo (no worktree or registry signal)",
	}
	got, se, err := b.sync(t, args...)
	if err != nil || !slices.Equal(got, want) {
		t.Fatalf("sync: %v %q\n%s", err, got, se)
	}
	for n, st := range map[string]string{"10": "In progress", "20": "Blocked", "30": "In progress", "40": "Todo", "50": "Todo", "60": "In review", "70": "Ready to push", "80": "Todo", "90": "Done"} {
		if card := b.card(t, n); !strings.Contains(card, "\t"+st+"\t") {
			t.Errorf("card %s = %q, want %s", n, card, st)
		}
	}
	if b.mutations(t) != 4 {
		t.Fatalf("mutations = %d, want 4 (only where the status differs)", b.mutations(t))
	}
	// GitHub now holds the new statuses; the fake board is static, so say so.
	after := strings.NewReplacer(
		`"number":10,"title":"a","state":"OPEN"},"status":{"name":"Todo"`, `"number":10,"title":"a","state":"OPEN"},"status":{"name":"In progress"`,
		`"number":20,"title":"b","state":"OPEN"},"status":{"name":"Todo"`, `"number":20,"title":"b","state":"OPEN"},"status":{"name":"Blocked"`,
		`"number":30,"title":"c","state":"OPEN"}`, `"number":30,"title":"c","state":"OPEN"},"status":{"name":"In progress"}`,
		`"number":40,"title":"d","state":"OPEN"},"status":{"name":"In progress"`, `"number":40,"title":"d","state":"OPEN"},"status":{"name":"Todo"`,
	).Replace(syncNodes)
	b.pages(t, map[string]string{"first": page(after, "")})
	got, se, err = b.sync(t, args...)
	if err != nil || len(got) != 0 || b.mutations(t) != 4 {
		t.Fatalf("second run: %v %q mutations %d\n%s", err, got, b.mutations(t), se)
	}
}

func TestBoardSnapshotSyncDryRunWritesNothing(t *testing.T) {
	t.Parallel()
	b, args := syncFixture(t)
	got, se, err := b.sync(t, append(args, "--dry-run")...)
	if err != nil || len(got) != 4 || b.mutations(t) != 0 {
		t.Fatalf("dry run: %v %q mutations %d\n%s", err, got, b.mutations(t), se)
	}
}

func TestBoardSnapshotSyncUnreadableRegistryChangesNothing(t *testing.T) {
	t.Parallel()
	b, args := syncFixture(t)
	if err := os.WriteFile(args[len(args)-1], []byte("not a registry\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := b.sync(t, args...)
	if err == nil || b.mutations(t) != 0 {
		t.Fatalf("err %v mutations %d; want a refusal without writes", err, b.mutations(t))
	}
}

func TestBoardSnapshotSyncRefusesAStaleSnapshot(t *testing.T) {
	t.Parallel()
	b, args := syncFixture(t)
	if _, _, err := b.run(t); err != nil {
		t.Fatal(err)
	}
	fail := []string{"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null", "FAKE_GH_FAIL=1"}
	_, se, err := b.runEnv(t, fail, args...)
	if err == nil || !strings.Contains(se, "stale snapshot; nothing changed") || b.mutations(t) != 0 {
		t.Fatalf("err %v mutations %d stderr %q; want a refusal without writes", err, b.mutations(t), se)
	}
	so, _, err := b.runEnv(t, fail, append(args, "--dry-run")...)
	if err != nil || strings.TrimSpace(so) == "" {
		t.Fatalf("a dry run may read a stale snapshot: %v %q", err, so)
	}
}

func TestBoardSnapshotConfigureToleratesLegacyReadyToPush(t *testing.T) {
	t.Parallel()
	legacy := []string{"Todo", "In progress", "Blocked", "In review", "Ready to push", "Done"}
	fixture := sessionSchemaFixtureWithStatuses(t, `[{"id":"s-werner","name":"Werner","color":"GRAY","description":"Human maintainer"}]`, legacy)
	stdout, stderr, err := fixture.runEnv(t, schemaEnv(fixture), "configure", "PVT_crewbook")
	if err != nil {
		t.Fatalf("configure on a board that still has the option: %v %s", err, stderr)
	}
	if !strings.Contains(stdout, `"Ready to push"`) {
		t.Fatalf("legacy option was dropped: %s", stdout)
	}
	// The option IDs survive in order: status-0..5, so no card loses its Status.
	data, err := os.ReadFile(filepath.Join(fixture.bin, "schema.json")) //nolint:gosec // a test fixture path
	if err != nil {
		t.Fatal(err)
	}
	var state struct {
		Fields []struct {
			ID      string
			Name    string
			Options []struct{ ID, Name string }
		}
	}
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	for _, field := range state.Fields {
		if field.Name != "Status" {
			continue
		}
		if len(field.Options) != len(legacy) {
			t.Fatalf("Status options changed: %+v", field.Options)
		}
		for index, option := range field.Options {
			if option.ID != "status-"+strconv.Itoa(index) || option.Name != legacy[index] {
				t.Errorf("Status option %d is %+v; want status-%d %q", index, option, index, legacy[index])
			}
		}
	}
	logged, err := os.ReadFile(fixture.log) //nolint:gosec // a test fixture path
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(logged), "\n") {
		if strings.Contains(line, "updateProjectV2Field") && strings.Contains(line, `"fieldId":"F_status"`) {
			t.Errorf("configure rewrote the Status field: %s", line)
		}
	}
}
