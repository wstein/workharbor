// SPDX-License-Identifier: EUPL-1.2
package config

import (
	"reflect"
	"strings"
	"testing"
)

func fixtureDescriptor() Descriptor {
	return Descriptor{RepositoryIdentity: "https://example.test/workspace.git", Commit: strings.Repeat("a", 40), Submodules: []Submodule{{Path: "projects/app", Commit: strings.Repeat("b", 40)}}}
}
func descriptorMap(d Descriptor) map[string]any {
	entries := []any{}
	for _, s := range d.Submodules {
		entries = append(entries, map[string]any{"path": s.Path, "commit": s.Commit})
	}
	return map[string]any{"repository_identity": d.RepositoryIdentity, "commit": d.Commit, "submodules": entries}
}
func code(t *testing.T, err error, want string) {
	t.Helper()
	e, ok := err.(*Error)
	if !ok || e.Code != want {
		t.Fatalf("code=%v, want %s", err, want)
	}
}
func TestDescriptorExactNormalizedRevisions(t *testing.T) {
	want := fixtureDescriptor()
	got, err := DecodeDescriptor(descriptorMap(want))
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("descriptor=%#v error=%v", got, err)
	}
	for _, mutate := range []func(map[string]any){func(m map[string]any) { m["commit"] = "main" }, func(m map[string]any) { m["extra"] = "SECRET-CANARY" }, func(m map[string]any) { m["submodules"] = nil }, func(m map[string]any) {
		m["submodules"] = []any{map[string]any{"path": "../app", "commit": strings.Repeat("b", 40)}}
	}, func(m map[string]any) {
		m["submodules"] = []any{map[string]any{"path": "projects/app", "commit": strings.Repeat("b", 40)}, map[string]any{"path": "projects/app", "commit": strings.Repeat("b", 40)}}
	}} {
		m := descriptorMap(want)
		mutate(m)
		_, err := DecodeDescriptor(m)
		code(t, err, "invalid_configuration")
	}
}
func TestDescriptorOriginAliasesNeverNormalize(t *testing.T) {
	for _, origin := range []string{"https://Host/Repo.git", "https://host/Repo.git", "ssh://git@host/Repo.git", "git@host:Repo.git"} {
		d := fixtureDescriptor()
		d.RepositoryIdentity = origin
		got, err := DecodeDescriptor(descriptorMap(d))
		if err != nil || got.RepositoryIdentity != origin {
			t.Fatalf("literal origin %q changed: %#v %v", origin, got, err)
		}
	}
	for _, origin := range []string{"https://user:secret@host/repo", "file:///tmp/repo", "https://host/repo?secret=x", "https://host/repo\n"} {
		d := fixtureDescriptor()
		d.RepositoryIdentity = origin
		_, err := DecodeDescriptor(descriptorMap(d))
		code(t, err, "invalid_configuration")
	}
}
func TestApprovalRequiresExactOwnerReferenceAndCompleteSet(t *testing.T) {
	d := fixtureDescriptor()
	a := Approval{ID: "00000000-0000-4000-8000-000000000005", OwnerID: "00000000-0000-4000-8000-000000000001", Descriptor: d}
	if err := MatchApproval(a, a.OwnerID, a.ID, d); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Approval){func(a *Approval) { a.OwnerID = "00000000-0000-4000-8000-000000000009" }, func(a *Approval) { a.ID = "00000000-0000-4000-8000-000000000009" }, func(a *Approval) { a.Descriptor.Commit = strings.Repeat("c", 40) }, func(a *Approval) { a.Descriptor.Submodules = nil }, func(a *Approval) { a.Descriptor.RepositoryIdentity = "https://EXAMPLE.test/workspace.git" }} {
		b := a
		b.Descriptor.Submodules = append([]Submodule{}, d.Submodules...)
		change(&b)
		code(t, MatchApproval(b, a.OwnerID, a.ID, d), "trust_required")
	}
}
func TestSourceImageAndDockerfileClosedMatrix(t *testing.T) {
	for _, m := range []map[string]any{{"image": "ubuntu@sha256:" + strings.Repeat("a", 64)}, {"build": map[string]any{"dockerfile": "Dockerfile", "context": "..", "args": map[string]any{"APP_VERSION": "1"}}, "postCreateCommand": []any{"sh", "./prepare"}, "postStartCommand": map[string]any{"one": "true", "two": []any{"true"}}}} {
		if err := ValidateSource(m); err != nil {
			t.Fatal(err)
		}
	}
	for _, m := range []map[string]any{{}, {"image": "ubuntu:latest"}, {"image": "ubuntu@sha256:" + strings.Repeat("a", 64), "build": map[string]any{"dockerfile": "Dockerfile"}}, {"image": false}, {"build": map[string]any{"dockerfile": "Dockerfile", "extra": "CANARY"}}, {"image": "ubuntu@sha256:" + strings.Repeat("a", 64), "remoteEnv": map[string]any{"X": nil}}, {"image": "ubuntu@sha256:" + strings.Repeat("a", 64), "postCreateCommand": []any{1}}} {
		code(t, ValidateSource(m), "unsupported_configuration")
	}
}
func TestUnsupportedSourceRefusesEveryEffectClass(t *testing.T) {
	for _, field := range []string{"initializeCommand", "postAttachCommand", "features", "dockerComposeFile", "customizations", "runArgs", "mounts", "workspaceMount", "workspaceFolder", "privileged", "capAdd", "securityOpt", "hostRequirements", "appPort", "forwardPorts", "unknown"} {
		t.Run(field, func(t *testing.T) {
			m := map[string]any{"image": "ubuntu@sha256:" + strings.Repeat("a", 64), field: "SECRET-CANARY"}
			code(t, ValidateSource(m), "unsupported_configuration")
		})
	}
	for _, field := range []string{"userEnvProbe", "updateRemoteUserUID", "waitFor", "shutdownAction"} {
		m := map[string]any{"image": "ubuntu@sha256:" + strings.Repeat("a", 64), field: true}
		code(t, ValidateSource(m), "unsupported_configuration")
	}
}
func TestReservedFrontendArgumentsAndSubstitutionRefuse(t *testing.T) {
	for _, name := range []string{"BUILDKIT_SYNTAX", "BUILDKIT_INLINE_CACHE", "_DEV_CONTAINERS_BASE_IMAGE"} {
		m := map[string]any{"build": map[string]any{"dockerfile": "Dockerfile", "args": map[string]any{name: "evil/frontend"}}}
		code(t, ValidateSource(m), "unsupported_configuration")
	}
	code(t, ValidateSource(map[string]any{"image": "ubuntu@sha256:" + strings.Repeat("a", 64), "containerEnv": map[string]any{"TOKEN": "${localEnv:SECRET}"}}), "unsupported_configuration")
}
func TestDockerfileASTRefusesBeforeBuild(t *testing.T) {
	base := "ubuntu@sha256:" + strings.Repeat("a", 64)
	data := "FROM " + base + " AS base\nARG APP_VERSION=1\nRUN printf ok\nFROM base AS final\nCOPY --from=base /etc/os-release /release\n"
	refs, err := ValidateDockerfile([]byte(data))
	if err != nil || !reflect.DeepEqual(refs, []string{base}) {
		t.Fatalf("refs=%v err=%v", refs, err)
	}
	for _, data := range []string{"# syntax=evil/frontend\nFROM " + base, "\xef\xbb\xbf# SyNtAx=evil/frontend\r\nFROM " + base, "FROM " + base + "\nVOLUME [\"/workspace/subdir\"]", "FROM " + base + "\nONBUILD RUN true", "ARG BASE=ubuntu\nFROM ${BASE}", "FROM --platform=linux/amd64 " + base, "FROM " + base + "\nARG BUILDKIT_SYNTAX=evil/frontend", "FROM " + base + "\nARG _DEV_CONTAINERS_BASE_IMAGE=evil", "FROM ubuntu:latest", ""} {
		_, err := ValidateDockerfile([]byte(data))
		code(t, err, "unsupported_configuration")
	}
}

func TestDockerfileExternalStageSourcesRefuseBeforeBuild(t *testing.T) {
	base := "ubuntu@sha256:" + strings.Repeat("a", 64)
	head := "FROM " + base + " AS tool\nFROM " + base + "\n"
	for _, data := range []string{
		head + "COPY --from=tool /bin/x /x\n",
		head + "COPY --from=0 /bin/x /x\n",
		head + "RUN --mount=type=bind,from=tool,target=/t true\n",
		head + "RUN --mount=type=cache,target=/c true\n",
		head + "ADD local.tar /dst\n",
		head + "ARG U=vscode\nCOPY --chown=$U:$U a /a\n",
		head + "ARG X=y\nRUN --mount=type=cache,target=/root/${X} true\n",
	} {
		if _, err := ValidateDockerfile([]byte(data)); err != nil {
			t.Fatalf("prior-stage source refused: %q %v", data, err)
		}
	}
	for _, data := range []string{
		head + "COPY --from=evil/tool:latest / /\n",
		head + "COPY --from=" + base + " / /\n",
		head + "COPY --from=1 /x /x\n",
		head + "COPY --from=$TOOL /x /x\n",
		head + "ADD --from=evil/tool:latest / /\n",
		head + "RUN --mount=type=bind,from=evil/tool:latest,target=/t true\n",
		head + "RUN --mount=from=evil/tool:latest,target=/t true\n",
		head + "ADD https://example.test/x.tar /dst\n",
		head + "ADD git@example.test:repo.git /dst\n",
		head + "ADD user@example.test:repo.git /dst\n",
		head + "ADD ssh://example.test/repo.git /dst\n",
		head + "ADD git://example.test/repo.git /dst\n",
		head + "ADD HTTP://example.test/repo.git /dst\n",
		head + "ADD http://example.test/repo.git /dst\n",
		head + "ADD https://example.test/repo.git#main /dst\n",
		head + "ADD a-b_c@host-1.example:path/x.git?ref=1#frag /dst\n",
		head + "RUN --mount='type=bind,\"from=evil/tool:latest\",target=/t' true\n",
		head + "RUN --mount=type=bind,\" From =evil/tool:latest\",target=/t true\n",
		head + "RUN --mount=type=bind,from=$TOOL,target=/t true\n",
		"FROM " + base + "\nCOPY --from=later /x /x\nFROM " + base + " AS later\n",
	} {
		_, err := ValidateDockerfile([]byte(data))
		code(t, err, "unsupported_configuration")
	}
}

func TestSourceNULAndUnrepresentableEnvironmentNamesRefuse(t *testing.T) {
	for _, field := range []string{"name", "postCreateCommand"} {
		m := map[string]any{"image": "ubuntu@sha256:" + strings.Repeat("a", 64), field: "bad\x00value"}
		code(t, ValidateSource(m), "unsupported_configuration")
	}
	for _, key := range []string{"", "A=B", "A\x00B"} {
		m := map[string]any{"image": "ubuntu@sha256:" + strings.Repeat("a", 64), "containerEnv": map[string]any{key: "literal"}}
		code(t, ValidateSource(m), "unsupported_configuration")
	}
}

func TestDockerfileMalformedInstructionsRefuseBeforeBuild(t *testing.T) {
	base := "ubuntu@sha256:" + strings.Repeat("a", 64)
	for _, raw := range []string{"FROM " + base + "\nBOGUS effect\n", "FROM " + base + "\nCOPY only-one-operand\n", "RUN before-first-stage\nFROM " + base + "\n", "FROM " + base + "\nENV only-key\n"} {
		_, e := ValidateDockerfile([]byte(raw))
		code(t, e, "unsupported_configuration")
	}
}

func TestDockerfileMaintainedGrammarCheckCommentsNeverPanic(t *testing.T) {
	base := "ubuntu@sha256:" + strings.Repeat("a", 64)
	_, e := ValidateDockerfile([]byte("FROM " + base + " AS app\n#check=skip=JSONArgsRecommended\nRUN true\n"))
	if e != nil {
		t.Fatal(e)
	}
}

func TestDockerfileEveryReservedARGDeclarationRefuses(t *testing.T) {
	base := "ubuntu@sha256:" + strings.Repeat("a", 64)
	for _, arg := range []string{"ARG OK BUILDKIT_SYNTAX=evil/frontend", "ARG FIRST SECOND _DEV_CONTAINERS_BASE_IMAGE=evil", "ARG OK=1 BUILDKIT_INLINE_CACHE=1 LAST=2"} {
		for _, prefix := range []string{"FROM " + base + "\n", ""} {
			raw := prefix + arg + "\n"
			if prefix == "" {
				raw += "FROM " + base + "\n"
			}
			_, e := ValidateDockerfile([]byte(raw))
			code(t, e, "unsupported_configuration")
		}
	}
}
