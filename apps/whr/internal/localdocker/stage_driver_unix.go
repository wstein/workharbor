//go:build unix

// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"context"
	"encoding/csv"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func stageShellLiteral(v string) string { return "'" + strings.ReplaceAll(v, "'", "'\\''") + "'" }

type stageDriverPolicy struct {
	PrimaryID, PrimaryName, PrimaryHome, OperationID, WorkspaceSource, WorkspaceDestination, Home, HomeDestination string
	AllowedMounts                                                                                                  []string
	PullReferences                                                                                                 []string
	InitializerVolumes                                                                                             map[string]string
}

// DockerDriver routes only phase-specific fixed protocols. It is cooperative
// routing, not a sandbox for trusted host code or another Docker-owner client.
func (r *stageRunner) DockerDriver(mode string) (string, error) {
	docker, ok := r.Tools["docker"]
	if !ok {
		return "", stageConflict("Docker driver tool missing")
	}
	shell, ok := r.Tools["sh"]
	if !ok {
		return "", stageConflict("Docker driver shell missing")
	}
	if mode != "preview" && mode != "stage" {
		return "", stageConflict("Docker driver phase unsupported")
	}
	p := r.Policy
	if mode != "preview" && (p == nil || !fullID(p.PrimaryID) || p.PrimaryName == "" || !lowerHex32(p.OperationID) || !filepath.IsAbs(p.WorkspaceSource) || !filepath.IsAbs(p.WorkspaceDestination) || !volumeName(p.PrimaryHome) || !volumeName(p.Home) || (p.HomeDestination != "/home/vscode" && p.HomeDestination != "/home/captain")) {
		return "", stageConflict("Docker driver declared resource intent missing")
	}
	if p == nil {
		p = &stageDriverPolicy{}
	}
	if mode == "stage" && p.Home == p.PrimaryHome {
		return "", stageConflict("candidate primary home exposure")
	}
	for _, mount := range p.AllowedMounts {
		parts, e := stageNativeMount(mount)
		if e != nil {
			return "", e
		}
		if mode == "stage" && parts["type"] == "volume" && parts["source"] == p.PrimaryHome {
			return "", stageConflict("candidate primary home exposure")
		}
	}

	expectedName := "whr-anchorage-staging"
	script := "#!" + shell.Path + "\nset -eu\n" + "native=" + stageShellLiteral(docker.Path) + "\nendpoint=" + stageShellLiteral(r.Endpoint) + "\nmode=" + stageShellLiteral(mode) + "\nprimary_id=" + stageShellLiteral(p.PrimaryID) + "\nexpected_name=" + stageShellLiteral(expectedName) + "\noperation=" + stageShellLiteral(p.OperationID) + "\nworkspace_mount=" + stageShellLiteral(renderStageMount("type=bind", "source="+p.WorkspaceSource, "target="+p.WorkspaceDestination)) + "\nhome_mount=" + stageShellLiteral(renderStageMount("type=volume", "source="+p.Home, "target="+p.HomeDestination)) + "\n"
	script += `refuse() { printf 'whr-driver:%s\n' "$1" >&2; exit 68; }
[ "$#" -gt 0 ] || refuse empty_vector
command=$1
case "$command" in --host|--host=*|-H|-H?*|--context|--context=*|--config|--config=*) refuse routing_override ;; stop|rm|kill|restart|prune) refuse protected_resource_mutation ;; esac
case "$command" in --version|-v|version|info|ps|inspect|events|exec|start|run|build|buildx|image|volume|container|network|context|pull) ;; *) refuse unsupported_native_command ;; esac
# Docker global routing/config overrides never enter a managed vector. Native
# run/exec parsers below stop at their image/command boundary.
if [ "$command" != run ] && [ "$command" != exec ]; then
 for argument do
  case "$argument" in --host|--host=*|-H|-H?*|--context|--context=*|--config|--config=*) refuse routing_override ;; esac
 done
fi
case "$command" in
 image) [ "$#" -ge 2 ] && [ "$2" = inspect ] || refuse image_mutation ;;
 container|network) [ "$#" -ge 2 ] || refuse metadata_vector; case "$2" in inspect|ls) ;; *) refuse protected_resource_mutation ;; esac ;;
 context) [ "$#" -eq 2 ] && [ "$2" = show ] || refuse context_mutation ;;
 volume) [ "$#" -ge 2 ] || refuse metadata_vector; case "$2" in inspect|ls) ;; create) [ "$mode" = stage ] && [ "$#" -eq 3 ] || refuse volume_mutation; case "$3" in
`
	volumes := make([]string, 0, len(p.InitializerVolumes))
	for volume := range p.InitializerVolumes {
		volumes = append(volumes, volume)
	}
	sort.Strings(volumes)
	if len(volumes) > 0 {
		for _, volume := range volumes {
			script += stageShellLiteral(volume) + ") ;;\n"
		}
	}
	script += `*) refuse undeclared_initializer_volume ;; esac ;; *) refuse volume_mutation ;; esac ;;
 pull) [ "$mode" = stage ] && [ "$#" -eq 2 ] || refuse pull_phase; case "$2" in
`
	refs := append([]string(nil), p.PullReferences...)
	for _, v := range volumes {
		refs = append(refs, p.InitializerVolumes[v])
	}
	sort.Strings(refs)
	last := ""
	for _, ref := range refs {
		if ref != "" && ref != last {
			script += stageShellLiteral(ref) + ") ;;\n"
			last = ref
		}
	}
	script += `*) refuse undeclared_pull_reference ;; esac ;;
 build) [ "$mode" = stage ] || refuse build_phase ;;
 buildx) [ "$#" -ge 2 ] || refuse metadata_vector; case "$2" in version|inspect|ls) ;; build) [ "$mode" = stage ] || refuse build_phase ;; *) refuse builder_mutation ;; esac ;;
 esac
if [ "$mode" = preview ]; then
 case "$command" in --version|-v|version|info|ps|inspect|image|container|network|context) ;; volume) case "$2" in inspect|ls) ;; *) refuse preview_mutation ;; esac ;; buildx) [ "$#" -eq 2 ] && [ "$2" = version ] || refuse preview_mutation ;; *) refuse preview_mutation ;; esac
fi
check_bound_container() {
 target=$1
 [ "$target" != "$primary_id" ] || refuse old_primary_target
 [ "${#target}" -eq 64 ] || refuse container_identity
 case "$target" in *[!0-9a-f]*) refuse container_identity ;; esac
 observed=$("$native" --host "$endpoint" container inspect --format '{{.Name}} {{index .Config.Labels "workharbor.stage.operation"}}' "$target") || refuse container_identity
 [ "$observed" = "/$expected_name $operation" ] || refuse foreign_container_target
}
check_literal_env() {
 case "$1" in =*|'') refuse unbound_env_copy ;; *=*) ;; *) refuse unbound_env_copy ;; esac
}
if [ "$command" = exec ]; then
 check_exec() {
  shift
  while [ "$#" -gt 0 ]; do
   case "$1" in -i|-t|-it|--interactive|--tty|-d|--detach|--privileged) shift ;; --env|-e) [ "$#" -ge 2 ] || refuse exec_option; check_literal_env "$2"; shift 2 ;; --env=*) check_literal_env "${1#--env=}"; shift ;; -e?*) check_literal_env "${1#-e}"; shift ;; --user|-u|--workdir|-w|--detach-keys) [ "$#" -ge 2 ] || refuse exec_option; shift 2 ;; --user=*|--workdir=*|--detach-keys=*) shift ;; --) shift; break ;; -*) refuse exec_option ;; *) break ;; esac
  done
  [ "$#" -gt 1 ] || refuse exec_vector
  check_bound_container "$1"
 }
 check_exec "$@"
fi
if [ "$command" = start ]; then
 [ "$#" -eq 2 ] || refuse start_vector
 check_bound_container "$2"
fi
check_mount() {
 case "$1" in
 "$workspace_mount") workspace_seen=$((workspace_seen+1)) ;;
 "$home_mount") home_seen=$((home_seen+1)) ;;
`
	for _, mount := range p.AllowedMounts {
		script += stageShellLiteral(mount) + ") ;;\n"
	}
	script += `*) refuse undeclared_native_mount ;; esac
}
if [ "$command" = run ]; then
 check_bundle_run() {
  [ "$mode" = stage ] && [ "$#" -eq 7 ] && [ "$2" = --rm ] && [ "$3" = --network ] && [ "$4" = none ] && [ "$5" = --mount ] || return 1
  reference=
  case "$6" in
`
	for _, volume := range volumes {
		mount := "type=volume,src=" + volume + ",dst=/bundle,volume-nocopy"
		script += stageShellLiteral(mount) + ") reference=" + stageShellLiteral(p.InitializerVolumes[volume]) + " ;;\n"
	}
	script += `*) return 1 ;; esac
  observed=$("$native" --host "$endpoint" image inspect --format '{{.Id}}' -- "$reference") || refuse initializer_image_binding
  [ "${#observed}" -eq 71 ] || refuse initializer_image_binding
  case "$observed" in sha256:*) ;; *) refuse initializer_image_binding ;; esac
  hex=${observed#sha256:}; case "$hex" in *[!0-9a-f]*) refuse initializer_image_binding ;; esac
  [ "$7" = "$observed" ] || refuse initializer_image_binding
  return 0
 }
 if check_bundle_run "$@"; then exec "$native" --host "$endpoint" "$@"; fi
 shift
 validate_run() {
  name=;workspace_seen=0;home_seen=0
  while [ "$#" -gt 0 ]; do
   case "$1" in
    --) shift; break ;;
    --pull|--pull=*) refuse native_pull_override ;;
    --name) [ "$#" -ge 2 ] && [ -z "$name" ] || refuse native_name; name=$2; shift 2 ;;
    --name=*) [ -z "$name" ] || refuse native_name; name=${1#--name=}; shift ;;
    --mount) [ "$#" -ge 2 ] || refuse native_mount; check_mount "$2"; shift 2 ;;
    --mount=*) mount=${1#--mount=}; check_mount "$mount"; shift ;;
    --env|-e) [ "$#" -ge 2 ] || refuse native_run_option; check_literal_env "$2"; shift 2 ;;
    --env=*) check_literal_env "${1#--env=}"; shift ;;
    -e?*) check_literal_env "${1#-e}"; shift ;;
    --env-file|--env-file=*) refuse native_env_file ;;
    -d|-i|-t|-it|--detach|--interactive|--tty|--rm|--init|--privileged|--sig-proxy=false|--sig-proxy=true) shift ;;
    --hostname|-h|--user|-u|--workdir|-w|--entrypoint|--label|-l|--network|--security-opt|--cap-add|--cap-drop|--device|--gpus|--publish|-p|--expose|--cpus|--memory|--shm-size|-a) [ "$#" -ge 2 ] || refuse native_run_option; shift 2 ;;
    --hostname=*|--user=*|--workdir=*|--entrypoint=*|--label=*|--network=*|--security-opt=*|--cap-add=*|--cap-drop=*|--device=*|--gpus=*|--publish=*|--expose=*|--cpus=*|--memory=*|--shm-size=*) shift ;;
    -*) refuse unsupported_native_run_option ;;
    *) break ;;
   esac
  done
  [ "$name" = "$expected_name" ] || refuse native_name
  [ "$workspace_seen" -eq 1 ] && [ "$home_seen" -eq 1 ] || refuse native_mount_binding
  [ "$#" -gt 0 ] || refuse native_image
 }
 validate_run "$@"
 exec "$native" --host "$endpoint" run "$@"
fi
exec "$native" --host "$endpoint" "$@"
`
	path := filepath.Join(r.Root, "driver", "docker")
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0700)
	if e != nil {
		return "", stageConflict("Docker driver publication unavailable")
	}
	_, e = f.WriteString(script)
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return "", stageConflict("Docker driver publication failed")
	}
	dir, e := os.Open(filepath.Dir(path))
	if e != nil {
		return "", stageConflict("Docker driver directory unavailable")
	}
	e = dir.Sync()
	dir.Close()
	if e != nil {
		return "", stageConflict("Docker driver durability uncertain")
	}
	digest, e := stageFileDigest(context.Background(), path)
	if e != nil {
		return "", e
	}
	r.Tools["docker_driver"] = stageToolIdentity{Path: path, Version: "generated-v1", Digest: digest}
	for i, value := range r.Environment {
		if strings.HasPrefix(value, "PATH=") {
			r.Environment[i] = "PATH=" + filepath.Dir(path) + string(os.PathListSeparator) + strings.TrimPrefix(value, "PATH=")
		}
	}
	return path, nil
}

// stageNativeMount parses the Docker phase's CSV mount value only. It is not
// bundle pin inference and does not change that reader's settled alias rules.
func stageNativeMount(raw string) (map[string]string, error) {
	reader := csv.NewReader(strings.NewReader(raw))
	reader.FieldsPerRecord = -1
	reader.TrimLeadingSpace = true
	fields, e := reader.Read()
	if e != nil {
		return nil, stageConflict("native mount syntax unsupported")
	}
	if _, e = reader.Read(); e != io.EOF {
		return nil, stageConflict("native mount syntax unsupported")
	}
	out := map[string]string{}
	for _, field := range fields {
		k, v, ok := strings.Cut(field, "=")
		if !ok {
			v = "true"
		}
		switch k {
		case "src":
			k = "source"
		case "dst", "destination":
			k = "target"
		case "ro":
			k = "readonly"
		}
		if k == "" {
			return nil, stageConflict("native mount empty field")
		}
		if _, exists := out[k]; exists {
			return nil, stageConflict("native mount duplicate field")
		}
		out[k] = v
	}
	if out["type"] == "" {
		out["type"] = "volume"
	}
	if out["target"] == "" {
		return nil, stageConflict("native mount destination missing")
	}
	return out, nil
}
