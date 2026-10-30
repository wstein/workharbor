// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/doctor"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/workspacecfg"
)

// Home clone/backup (specs/006-whr-cli/contracts/home-clone-backup-amendment-20261029.md):
// copy a workspace home volume into a new volume with tar in a digest-pinned
// base image. The source is mounted read-only in every helper run and nothing
// is ever deleted automatically.

const (
	maxHomeExcludes   = 32
	maxHomeExcludeLen = 256
	maxHomeVolumeLen  = 128
	homeCopyName      = "whr-home-copy"
	homeVerifyName    = "whr-home-verify"
	homeOperationKey  = "workharbor.home-copy.operation"

	// homeCopyTemplate is constant; excludes arrive only as positional "$@".
	homeCopyTemplate = `set -eu -o pipefail; cd /from; tar -cpf - --numeric-owner "$@" . | tar -xpf - --numeric-owner -C /to`
	// homeVerifyTemplate counts both read-only sides with the same excludes
	// pruned; each exclude is used only as a quoted variable.
	// homeRsyncTemplate runs in the whr tools image; excludes arrive as anchored --exclude=/x args.
	homeRsyncTemplate  = `set -eu; cd /from; exec rsync -aH --numeric-ids --info=progress2 "$@" ./ /to/`
	homeVerifyTemplate = `set -eu
count() {
	r=$1
	shift
	for p do set -- "$@" -path "$r/$p" -prune -o; shift; done
	for t in f d l; do printf '%s %s %s\n' "$r" "$t" "$(find "$r" -xdev "$@" -type "$t" -print | wc -l)"; done
	for s in .claude/projects .codex/sessions; do
		n=0
		if [ -d "$r/$s" ]; then n=$(find "$r/$s" -xdev -type f -name '*.jsonl' | wc -l); fi
		printf '%s %s %s\n' "$r" "$s" "$n"
	done
}
count /from "$@"
count /to "$@"`
)

// credentialExcludes are excluded from a clone unless explicitly included.
var credentialExcludes = []string{"./.claude.json", "./.claude/.credentials.json", "./.codex/auth.json", "./.config/gh", "./.docker/config.json", "./.git-credentials", "./.gnupg", "./.netrc", "./.ssh"}

var protectedVolumePrefixes = []string{"anchorage-home-", "whr-stage-home-", "bundledock-"}

// HomeCopyOptions selects one workspace home and the copy mode.
type HomeCopyOptions struct {
	Mode               string // "clone" or "backup"
	Target             string // exact container name or full ID
	To                 string // optional new volume name
	Excludes           []string
	IncludeCredentials bool
	Progress           io.Writer // live rsync progress; nil discards it
}

// HomeCopySource identifies the source container and its home volume.
type HomeCopySource struct {
	ContainerID   string `json:"container_id"`
	ContainerName string `json:"container_name"`
	Role          string `json:"role"`
	Volume        string `json:"volume"`
}

// HomeCopyCounts are find counts of one side; they are not a content attestation.
type HomeCopyCounts struct {
	RegularFiles       int64 `json:"regular_files"`
	Directories        int64 `json:"directories"`
	SymbolicLinks      int64 `json:"symbolic_links"`
	ClaudeSessionFiles int64 `json:"claude_session_files"`
	CodexSessionFiles  int64 `json:"codex_session_files"`
}

// HomeCopyVerification compares the source (excludes pruned) with the target.
type HomeCopyVerification struct {
	Status string         `json:"status"`
	Source HomeCopyCounts `json:"source"`
	Target HomeCopyCounts `json:"target"`
}

// HomeCopyAction is one effect; Attempted without Applied is uncertain.
type HomeCopyAction struct {
	Kind      string `json:"kind"`
	Target    string `json:"target"`
	Attempted bool   `json:"attempted"`
	Applied   bool   `json:"applied"`
}

// HomeCopyResult is the closed localHomeCopyResult of the CLI contract.
type HomeCopyResult struct {
	Mode                string                `json:"mode"`
	Status              string                `json:"status"`
	DryRun              bool                  `json:"dry_run"`
	Source              HomeCopySource        `json:"source"`
	TargetVolume        string                `json:"target_volume"`
	HelperImage         string                `json:"helper_image"`
	Excludes            []string              `json:"excludes"`
	CredentialsIncluded bool                  `json:"credentials_included"`
	EstimatedSize       *string               `json:"estimated_size"`
	Fingerprint         string                `json:"fingerprint"`
	Verification        *HomeCopyVerification `json:"verification"`
	Actions             []HomeCopyAction      `json:"actions"`
	Residuals           []string              `json:"residuals"`
}

type homeCopyPlan struct {
	options  HomeCopyOptions
	source   HomeCopySource
	target   string
	image    string
	pull     bool     // pinned image is pulled; a workspace image is already local
	build    bool     // the tools image is missing and is built on apply
	fallback string   // workspace image whose tar runs the copy if the build fails
	excludes []string // canonical ./x form, sorted
	size     *string
	print    string
}

// HomeCopy binds one previewed plan to one admitted daemon; Apply consumes it once.
type HomeCopy struct {
	mu       sync.Mutex
	used     bool
	client   Client
	endpoint string
	plan     homeCopyPlan
	newOp    func() (string, error)
}

// canonicalHomeExclude turns x/y, ./x/y or /x/y into ./x/y.
func canonicalHomeExclude(value string) (string, bool) {
	rel := value
	switch {
	case strings.HasPrefix(rel, "./"):
		rel = rel[2:]
	case strings.HasPrefix(rel, "/"):
		rel = rel[1:]
	}
	if !homeRelative(rel) {
		return "", false
	}
	return "./" + rel, true
}

func homeRelative(rel string) bool {
	if rel == "" || len(rel) > maxHomeExcludeLen {
		return false
	}
	for _, part := range strings.Split(rel, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
		for _, c := range part {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("._-+@", c)) {
				return false
			}
		}
	}
	return true
}

func protectedVolume(name string) bool {
	for _, p := range protectedVolumePrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

func validHomeVolume(name string) bool { return volumeName(name) && len(name) <= maxHomeVolumeLen }

// validateHomeCopyOptions refuses bad input (64) before any I/O and returns
// the sorted canonical excludes.
func validateHomeCopyOptions(o HomeCopyOptions) ([]string, error) {
	usage := func(format string, args ...any) ([]string, error) {
		return nil, exitcode.Errorf(exitcode.PathBadArguments, format, args...)
	}
	if o.Mode != "clone" && o.Mode != "backup" {
		return usage("home copy mode must be clone or backup")
	}
	if strings.TrimSpace(o.Target) == "" {
		return usage("home %s needs an exact container name or full ID", o.Mode)
	}
	if o.To != "" && !validHomeVolume(o.To) {
		return usage("--to needs a volume name of [A-Za-z0-9][A-Za-z0-9_.-]* with at most %d bytes", maxHomeVolumeLen)
	}
	if o.Mode == "backup" && (len(o.Excludes) > 0 || o.IncludeCredentials) {
		return usage("home backup takes no --exclude or --include-credentials")
	}
	if len(o.Excludes) > maxHomeExcludes {
		return usage("at most %d --exclude paths", maxHomeExcludes)
	}
	set := map[string]bool{"./.cache": true}
	if o.Mode == "clone" && !o.IncludeCredentials {
		for _, c := range credentialExcludes {
			set[c] = true
		}
	}
	for _, e := range o.Excludes {
		c, ok := canonicalHomeExclude(e)
		if !ok {
			return usage("--exclude needs a home-relative path of letters, digits and ._-+@ components without wildcards")
		}
		set[c] = true
	}
	out := make([]string, 0, len(set))
	for e := range set {
		out = append(out, e)
	}
	sort.Strings(out)
	return out, nil
}

// PrepareHomeCopy validates, resolves and previews without any mutation.
func (c Client) PrepareHomeCopy(parent context.Context, ports doctor.Ports, o HomeCopyOptions) (*HomeCopy, error) {
	if _, err := validateHomeCopyOptions(o); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(parent, doctor.Budget)
	defer cancel()
	endpoint, err := ResolveEndpoint(ctx, ports)
	if err != nil {
		return nil, err
	}
	plan, err := c.planHomeCopy(ctx, endpoint, o)
	if err != nil {
		return nil, err
	}
	if o.Progress != nil {
		c.Progress = o.Progress
	}
	return &HomeCopy{client: c, endpoint: endpoint, plan: plan, newOp: randomOperation}, nil
}

func randomOperation() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// homeSourceOf selects one verified workspace container by exact name or full ID and its home volume.
func (c Client) homeSourceOf(ctx context.Context, endpoint, target string) (inspection, HomeCopySource, error) {
	conflict := func(format string, args ...any) (inspection, HomeCopySource, error) {
		return inspection{}, HomeCopySource{}, exitcode.Errorf(exitcode.PathConflict, format, args...)
	}
	all, _, err := c.inspectAllAt(ctx, endpoint)
	if err != nil {
		return inspection{}, HomeCopySource{}, err
	}
	var selected []inspection
	for _, item := range all {
		if item.ID == target || strings.TrimPrefix(item.Name, "/") == target {
			selected = append(selected, item)
		}
	}
	switch {
	case len(selected) == 0:
		return inspection{}, HomeCopySource{}, exitcode.Errorf(exitcode.PathTargetAbsent, "no local container matches %q", target)
	case len(selected) > 1:
		return inspection{}, HomeCopySource{}, exitcode.Errorf(exitcode.PathBadArguments, "local selector %q is ambiguous; use the full container ID", target)
	}
	item := selected[0]
	role, recognized := workspaceRole(item.Labels)
	if !recognized || !fullID(item.ID) {
		return conflict("refusing container without verified workspace ownership")
	}
	source := HomeCopySource{ContainerID: item.ID, ContainerName: strings.TrimPrefix(item.Name, "/"), Role: role}
	for _, m := range item.Mounts {
		if homeMount(m) && (source.Volume == "" || m.Destination == "/home/captain") {
			source.Volume = m.Name
		}
	}
	if source.Volume == "" || !validHomeVolume(source.Volume) {
		return conflict("the selected workspace has no usable home volume")
	}
	return item, source, nil
}

func (c Client) planHomeCopy(ctx context.Context, endpoint string, o HomeCopyOptions) (homeCopyPlan, error) {
	excludes, err := validateHomeCopyOptions(o)
	if err != nil {
		return homeCopyPlan{}, err
	}
	conflict := func(format string, args ...any) (homeCopyPlan, error) {
		return homeCopyPlan{}, exitcode.Errorf(exitcode.PathConflict, format, args...)
	}
	item, source, err := c.homeSourceOf(ctx, endpoint, o.Target)
	if err != nil {
		return homeCopyPlan{}, err
	}
	plan := homeCopyPlan{options: o, excludes: excludes, source: source}
	if pinned, perr := helperBaseImage(ctx, item.Labels["devcontainer.local_folder"]); perr == nil {
		plan.image, plan.pull = pinned, true
	} else if ctx.Err() != nil {
		return homeCopyPlan{}, ctx.Err()
	} else if c.toolsImagePresent(ctx, endpoint) {
		plan.image = ToolsImage()
	} else if ctx.Err() != nil {
		return homeCopyPlan{}, ctx.Err()
	} else if validLocalImage(item.Image) {
		// No pin and no tools image yet: build it on apply; the workspace image's tar is the fallback.
		plan.image, plan.build, plan.fallback = ToolsImage(), true, item.Image
	} else {
		return homeCopyPlan{}, perr
	}
	existing, err := c.homeVolumes(ctx, endpoint)
	if err != nil {
		return homeCopyPlan{}, err
	}
	plan.target = o.To
	if plan.target == "" {
		base := "whr-home-" + o.Mode + "-" + plan.source.ContainerName + "-"
		if !validHomeVolume(base + "1") {
			return conflict("the container name cannot form a default volume name; give --to")
		}
		for n := 1; plan.target == ""; n++ {
			if candidate := base + strconv.Itoa(n); !existing[candidate] {
				plan.target = candidate
			}
		}
	}
	switch {
	case !validHomeVolume(plan.target):
		return conflict("target volume name is too long; give --to")
	case protectedVolume(plan.target):
		return conflict("target volume %s uses a protected prefix", plan.target)
	case existing[plan.target] || plan.target == plan.source.Volume:
		return conflict("target volume %s already exists; home copies only create new volumes", plan.target)
	}
	plan.size = c.homeVolumeSize(ctx, endpoint, plan.source.Volume)
	plan.print = homeFingerprint(endpoint, plan)
	return plan, nil
}

var localImageRef = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@-]*$`)

// validLocalImage accepts an image reference that is safe as one docker argument.
func validLocalImage(ref string) bool { return len(ref) <= 256 && localImageRef.MatchString(ref) }

// helperBaseImage reads only workharbor.json of the label folder through the
// bounded no-follow reader. Nothing is pulled here.
func helperBaseImage(ctx context.Context, folder string) (string, error) {
	refuse := func(why string) (string, error) {
		return "", exitcode.Errorf(exitcode.PathConflict, "no digest-pinned helper base image: %s", why).
			WithRemediation("pin one with whr workspace init <dir> --helper-base-image <registry/repository@sha256:...> --overwrite workharbor.json")
	}
	if folder == "" || !filepath.IsAbs(folder) {
		return refuse("the workspace has no local folder label")
	}
	resolved, err := filepath.EvalSymlinks(folder)
	if err != nil {
		return refuse("the workspace folder is unavailable")
	}
	checked, err := workspacecfg.ReadManifest(ctx, resolved)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return refuse("workharbor.json is unavailable")
	}
	if checked.Manifest == nil || len(checked.Findings) != 0 {
		return refuse("workharbor.json fails validation")
	}
	if !workspacecfg.DigestPinnedImage(checked.Manifest.HelperBaseImage) {
		return refuse("helper_images.base is not set")
	}
	return checked.Manifest.HelperBaseImage, nil
}

func (c Client) toolsImagePresent(ctx context.Context, endpoint string) bool {
	_, err := c.homeRead(ctx, endpoint, "image", "inspect", "--format", "{{.Id}}", "--", ToolsImage())
	return err == nil
}

func localBuildTag(ref string) bool { return ref == ToolsImage() || ref == RecoverImage() }

func (c Client) homeVolumes(ctx context.Context, endpoint string) (map[string]bool, error) {
	raw, err := c.homeRead(ctx, endpoint, "volume", "ls", "--quiet")
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, exitcode.Errorf(exitcode.PathConflict, "local Docker volume listing failed")
	}
	out := map[string]bool{}
	for _, line := range strings.Split(string(raw), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out[line] = true
		}
	}
	return out, nil
}

// homeVolumeSize is a best-effort estimate; any failure yields nil.
func (c Client) homeVolumeSize(ctx context.Context, endpoint, volume string) *string {
	raw, err := c.homeRead(ctx, endpoint, "system", "df", "--verbose", "--format", "{{json .Volumes}}")
	if err != nil {
		return nil
	}
	var rows []struct{ Name, Size string }
	if json.Unmarshal(bytes.TrimSpace(raw), &rows) != nil {
		return nil
	}
	for _, r := range rows {
		if r.Name == volume && r.Size != "" && len(r.Size) <= 32 {
			size := r.Size
			return &size
		}
	}
	return nil
}

func homeFingerprint(endpoint string, p homeCopyPlan) string {
	doc, _ := json.Marshal(struct {
		Endpoint, Mode, ContainerID, Source, Target, Image string
		Excludes                                           []string
		Credentials                                        bool
		Template                                           string
	}{endpoint, p.options.Mode, p.source.ContainerID, p.source.Volume, p.target, p.image, p.excludes, p.options.IncludeCredentials || p.options.Mode == "backup", homeCopyTemplate + homeRsyncTemplate})
	sum := sha256.Sum256(doc)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Preview returns a detached copy of the planned operation.
func (h *HomeCopy) Preview() HomeCopyResult {
	p := h.plan
	r := HomeCopyResult{Mode: p.options.Mode, Status: "preview", DryRun: true, Source: p.source, TargetVolume: p.target, HelperImage: p.image,
		Excludes: append([]string{}, p.excludes...), CredentialsIncluded: p.options.Mode == "backup" || p.options.IncludeCredentials,
		Fingerprint: p.print, Actions: []HomeCopyAction{}, Residuals: []string{}}
	if p.size != nil {
		size := *p.size
		r.EstimatedSize = &size
	}
	return r
}

// Apply rechecks the plan and trust, then pulls, creates, copies and verifies.
func (h *HomeCopy) Apply(parent context.Context, trust string) (HomeCopyResult, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	result := h.Preview()
	result.DryRun = false
	result.Status = "failed"
	if h.used {
		return result, exitcode.Errorf(exitcode.PathConflict, "home copy transaction already consumed; prepare a new preview")
	}
	h.used = true
	if err := parent.Err(); err != nil {
		return result, err
	}
	if trust != h.plan.print {
		return result, exitcode.Errorf(exitcode.PathConflict, "trusted fingerprint does not match the home copy preview")
	}
	readCtx, cancel := context.WithTimeout(parent, doctor.Budget)
	fresh, err := h.client.planHomeCopy(readCtx, h.endpoint, h.plan.options)
	cancel()
	if err != nil {
		if parent.Err() != nil {
			return result, parent.Err()
		}
		return result, err
	}
	if fresh.print != h.plan.print {
		return result, exitcode.Errorf(exitcode.PathConflict, "home copy plan changed since preview; request a new preview")
	}
	op, err := h.newOp()
	if err != nil || !homeOperation(op) {
		return result, exitcode.Errorf(exitcode.PathUnclassified, "cannot allocate a home copy operation ID")
	}
	if err := parent.Err(); err != nil {
		return result, err
	}
	p := h.plan
	act := func(kind, target string) int {
		result.Actions = append(result.Actions, HomeCopyAction{Kind: kind, Target: target, Attempted: true})
		return len(result.Actions) - 1
	}
	fail := func(path exitcode.Path, format string, args ...any) (HomeCopyResult, error) {
		e := exitcode.Errorf(path, format, args...)
		if len(result.Residuals) > 0 {
			e = e.WithRemediation("Inspect and remove the reported residuals yourself; whr deletes nothing automatically. The source home is unchanged.")
		}
		e.Result = result
		return result, e
	}

	var i int
	if p.pull {
		i = act("image_pull", p.image)
		if reply, err := h.client.homeMutate(parent, h.endpoint, "image", "pull", "--quiet", "--", p.image); err != nil || reply.status != 0 {
			if parent.Err() != nil {
				return result, parent.Err()
			}
			return fail(exitcode.PathConflict, "helper base image pull failed; no volume was created")
		}
		result.Actions[i].Applied = true
	}

	if p.build {
		i = act("tools_image_build", p.image)
		if reply, err := h.client.homeMutateIO(parent, h.endpoint, bytes.NewReader(toolsDockerfile), nil, "build", "--quiet", "--tag", p.image, "-"); err != nil || reply.status != 0 {
			if parent.Err() != nil {
				return result, parent.Err()
			}
			if p.fallback == "" {
				return fail(exitcode.PathConflict, "tools image build failed; no volume was created")
			}
			p.image = p.fallback // offline or no network: the workspace image's tar does the copy
			result.HelperImage = p.image
		} else {
			result.Actions[i].Applied = true
		}
	}

	i = act("volume_create", p.target)
	reply, createErr := h.client.homeMutate(parent, h.endpoint, "volume", "create", "--label", "workharbor.home-copy="+p.options.Mode, "--label", homeOperationKey+"="+op, "--label", "workharbor.home-copy.source="+p.source.Volume, "--", p.target)
	owned, inspectErr := h.client.homeVolumeOwned(parent, h.endpoint, p.target, op)
	switch {
	case owned:
		result.Actions[i].Applied = true
		result.Residuals = append(result.Residuals, "volume:"+p.target)
	case inspectErr != nil && (createErr == nil && reply.status == 0):
		result.Residuals = append(result.Residuals, "volume:"+p.target)
		return fail(exitcode.PathPartialFailure, "target volume %s ownership could not be confirmed", p.target)
	default:
		if parent.Err() != nil {
			return result, parent.Err()
		}
		return fail(exitcode.PathConflict, "target volume %s was not created by this operation; it was left untouched", p.target)
	}

	i = act("copy", p.target)
	var live io.Writer = io.Discard
	if h.client.Progress != nil {
		live = h.client.Progress
	}
	reply, err = h.client.homeMutateIO(parent, h.endpoint, nil, live, homeRunArgv(false, p.source.Volume, p.target, op, p.image, homeCopyExcludeArgs(p.image, p.excludes))...)
	if err != nil || reply.status != 0 {
		if parent.Err() != nil {
			result.Residuals = append(result.Residuals, "container:"+homeCopyName+"-"+op)
			return fail(exitcode.PathPartialFailure, "home copy interrupted; the helper container may still be running")
		}
		return fail(exitcode.PathPartialFailure, "home copy failed; the new target volume %s is incomplete", p.target)
	}
	result.Actions[i].Applied = true

	i = act("verify", p.target)
	reply, err = h.client.homeMutate(parent, h.endpoint, homeRunArgv(true, p.source.Volume, p.target, op, p.image, homeRelativeExcludes(p.excludes))...)
	if err != nil || reply.status != 0 {
		if parent.Err() != nil {
			result.Residuals = append(result.Residuals, "container:"+homeVerifyName+"-"+op)
		}
		return fail(exitcode.PathPartialFailure, "home copy verification did not complete; the target %s is kept", p.target)
	}
	verification, ok := parseHomeVerification(reply.stdout)
	if !ok {
		return fail(exitcode.PathPartialFailure, "home copy verification output is not understood; the target %s is kept", p.target)
	}
	result.Actions[i].Applied = true
	result.Verification = &verification
	if verification.Status != "match" {
		return fail(exitcode.PathPartialFailure, "home copy verification differs; the source may have changed during the copy; the target %s is kept", p.target)
	}
	result.Status = "copied"
	result.Residuals = []string{}
	return result, nil
}

// homeCopyExcludeArgs spells excludes for the engine the image selects: tar
// matches ./x, rsync matches the transfer-root anchored /x.
func homeCopyExcludeArgs(image string, excludes []string) []string {
	out := make([]string, len(excludes))
	for i, e := range excludes {
		if image == ToolsImage() {
			e = strings.TrimPrefix(e, ".")
		}
		out[i] = "--exclude=" + e
	}
	return out
}

func homeRelativeExcludes(excludes []string) []string {
	out := make([]string, len(excludes))
	for i, e := range excludes {
		out[i] = strings.TrimPrefix(e, "./")
	}
	return out
}

func homeOperation(op string) bool {
	if len(op) != 16 {
		return false
	}
	_, err := hex.DecodeString(op)
	return err == nil && strings.ToLower(op) == op
}

func (c Client) homeVolumeOwned(parent context.Context, endpoint, volume, op string) (bool, error) {
	ctx, cancel := context.WithTimeout(parent, doctor.Budget)
	defer cancel()
	raw, err := c.homeRead(ctx, endpoint, "volume", "inspect", "--format", "{{json .Labels}}", "--", volume)
	if err != nil {
		return false, err
	}
	var labels map[string]string
	if json.Unmarshal(bytes.TrimSpace(raw), &labels) != nil {
		return false, errors.New("invalid volume labels")
	}
	return labels[homeOperationKey] == op, nil
}

func parseHomeVerification(raw []byte) (HomeCopyVerification, bool) {
	v := HomeCopyVerification{}
	sides := map[string]*HomeCopyCounts{"/from": &v.Source, "/to": &v.Target}
	seen := map[string]bool{}
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 3 {
			return v, false
		}
		side, ok := sides[fields[0]]
		n, err := strconv.ParseInt(fields[2], 10, 64)
		if !ok || err != nil || n < 0 || seen[fields[0]+" "+fields[1]] {
			return v, false
		}
		seen[fields[0]+" "+fields[1]] = true
		switch fields[1] {
		case "f":
			side.RegularFiles = n
		case "d":
			side.Directories = n
		case "l":
			side.SymbolicLinks = n
		case ".claude/projects":
			side.ClaudeSessionFiles = n
		case ".codex/sessions":
			side.CodexSessionFiles = n
		default:
			return v, false
		}
	}
	if len(seen) != 10 {
		return v, false
	}
	v.Status = "differs"
	if v.Source == v.Target {
		v.Status = "match"
	}
	return v, true
}

// homeRunArgv builds the only two admitted helper runs.
func homeRunArgv(verify bool, source, target, op, image string, excludes []string) []string {
	name, template, mount := homeCopyName, homeCopyTemplate, target+":/to"
	if image == ToolsImage() {
		template = homeRsyncTemplate
	}
	if verify {
		name, template, mount = homeVerifyName, homeVerifyTemplate, target+":/to:ro"
	}
	args := []string{"run", "--rm", "--pull", "never", "--network", "none", "--read-only",
		"--security-opt", "no-new-privileges", "--cap-drop", "ALL",
		"--cap-add", "CHOWN", "--cap-add", "DAC_OVERRIDE", "--cap-add", "FOWNER", "--cap-add", "FSETID",
		"--user", "0:0", "--name", name + "-" + op, "--label", homeOperationKey + "=" + op,
		"-v", source + ":/from:ro", "-v", mount, "--entrypoint", "/bin/sh", image, "-c", template, name}
	return append(args, excludes...)
}

const homeRunFixed = 35 // argv elements before the positional excludes

// admitHomeMutation accepts only the fixed pull, create and helper-run vectors.
func admitHomeMutation(args []string) bool {
	switch {
	case len(args) == 5 && args[0] == "image" && args[1] == "pull" && args[2] == "--quiet" && args[3] == "--":
		return workspacecfg.DigestPinnedImage(args[4]) || toolboxImageRE.MatchString(args[4])
	case len(args) == 5 && args[0] == "build" && args[1] == "--quiet" && args[2] == "--tag" && localBuildTag(args[3]) && args[4] == "-":
		return true
	case len(args) == 6 && args[0] == "volume" && args[1] == "create" && args[2] == "--label" && args[3] == "workharbor.recover=toolbox" && args[4] == "--":
		return toolboxVolumeRE.MatchString(args[5])
	case len(args) == 9 && args[0] == "run" && args[1] == "--rm" && args[2] == "--pull" && args[3] == "never" && args[4] == "--network" && args[5] == "none" && args[6] == "--mount":
		id := args[8]
		return imageIDRE.MatchString("sha256:"+id) && args[7] == "type=volume,src=bundledock-toolbox-"+id+",dst=/bundle,volume-nocopy"
	case len(args) == 10 && args[0] == "volume" && args[1] == "create":
		mode := strings.TrimPrefix(args[3], "workharbor.home-copy=")
		op := strings.TrimPrefix(args[5], homeOperationKey+"=")
		source := strings.TrimPrefix(args[7], "workharbor.home-copy.source=")
		return (mode == "clone" || mode == "backup") && homeOperation(op) && validHomeVolume(source) && validHomeVolume(args[9]) && !protectedVolume(args[9]) && source != args[9] &&
			reflect.DeepEqual(args, []string{"volume", "create", "--label", "workharbor.home-copy=" + mode, "--label", homeOperationKey + "=" + op, "--label", "workharbor.home-copy.source=" + source, "--", args[9]})
	case len(args) >= homeRunFixed && args[0] == "run":
		verify := args[homeRunFixed-1] == homeVerifyName
		name := homeCopyName
		if verify {
			name = homeVerifyName
		}
		op := strings.TrimPrefix(args[22], name+"-")
		source, ok := strings.CutSuffix(args[26], ":/from:ro")
		if !ok {
			return false
		}
		suffix := ":/to"
		if verify {
			suffix = ":/to:ro"
		}
		target, ok := strings.CutSuffix(args[28], suffix)
		if !ok || !homeOperation(op) || !validHomeVolume(source) || !validHomeVolume(target) || protectedVolume(target) || source == target || !validLocalImage(args[31]) {
			return false
		}
		excludes := args[homeRunFixed:]
		if len(excludes) > maxHomeExcludes+len(credentialExcludes)+1 {
			return false
		}
		for _, e := range excludes {
			rel := e
			if !verify {
				prefix := "--exclude=./"
				if args[31] == ToolsImage() {
					prefix = "--exclude=/"
				}
				var cut bool
				if rel, cut = strings.CutPrefix(e, prefix); !cut {
					return false
				}
			}
			if !homeRelative(rel) {
				return false
			}
		}
		return reflect.DeepEqual(args, homeRunArgv(verify, source, target, op, args[31], excludes))
	}
	return false
}

// admitHomeRead accepts discovery plus the fixed volume/size/label reads.
func admitHomeRead(args []string) bool {
	if discoveryArgs(args) {
		return true
	}
	switch {
	case len(args) == 6 && args[0] == "image" && args[1] == "inspect" && args[2] == "--format" && args[3] == "{{.Id}}" && args[4] == "--" && (localBuildTag(args[5]) || toolboxImageRE.MatchString(args[5])):
		return true
	case reflect.DeepEqual(args, []string{"volume", "ls", "--quiet"}):
		return true
	case reflect.DeepEqual(args, []string{"system", "df", "--verbose", "--format", "{{json .Volumes}}"}):
		return true
	case len(args) == 6 && args[0] == "volume" && args[1] == "inspect" && args[2] == "--format" && args[3] == "{{json .Labels}}" && args[4] == "--":
		return validHomeVolume(args[5])
	}
	return false
}

func homeEndpoint(endpoint string) bool {
	return strings.HasPrefix(endpoint, "unix://") && filepath.IsAbs(strings.TrimPrefix(endpoint, "unix://"))
}

func (c Client) homeRead(ctx context.Context, endpoint string, args ...string) ([]byte, error) {
	if !homeEndpoint(endpoint) || !admitHomeRead(args) {
		return nil, errors.New("unsupported home copy read")
	}
	r, err := c.runFixed(ctx, endpoint, args...)
	if err != nil {
		return nil, err
	}
	if r.status != 0 {
		return nil, errors.New("Docker home copy read failed")
	}
	return r.stdout, nil
}

func (c Client) homeMutate(ctx context.Context, endpoint string, args ...string) (readResult, error) {
	return c.homeMutateIO(ctx, endpoint, nil, nil, args...)
}

// homeMutateIO is homeMutate with optional stdin (the tools Dockerfile) and a
// live stdout writer (rsync progress).
func (c Client) homeMutateIO(ctx context.Context, endpoint string, stdin io.Reader, stdout io.Writer, args ...string) (readResult, error) {
	if !homeEndpoint(endpoint) || !admitHomeMutation(args) {
		return readResult{}, fmt.Errorf("unsupported home copy operation")
	}
	return c.run(ctx, endpoint, stdin, stdout, args...)
}
