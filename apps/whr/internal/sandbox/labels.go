// SPDX-License-Identifier: EUPL-1.2

package sandbox

import (
	"path"
	"regexp"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/runtime"
)

// Label keys of the ownership labels (data model). Every container, volume and
// directory of an instance carries all of them.
const (
	LabelMarker            = "io.workharbor.sandbox"
	LabelID                = LabelMarker + ".id"
	LabelWorkspace         = LabelMarker + ".workspace"
	LabelProfile           = LabelMarker + ".profile"
	LabelProvider          = LabelMarker + ".provider"
	LabelRetention         = LabelMarker + ".retention"
	LabelCreated           = LabelMarker + ".created"
	LabelSchema            = LabelMarker + ".schema"
	LabelGitProfile        = LabelMarker + ".gitprofile"
	LabelSharedGitMetadata = LabelMarker + ".shared_git_metadata"
)

// SchemaVersion is the value of the schema label.
const SchemaVersion = "1"

// SharedGitMetadata states whether the instance can write the primary
// repository metadata (FR-011).
type SharedGitMetadata string

// The two values of the shared_git_metadata label.
const (
	SharedGitWritable SharedGitMetadata = "writable"
	SharedGitNone     SharedGitMetadata = "none"
)

// OwnershipLabels is the typed form of the label set.
type OwnershipLabels struct {
	ID        ID
	Workspace string
	Profile   runtime.TrustProfile
	Provider  string
	Retention Retention
	// Created is written as RFC 3339 UTC with second precision.
	Created time.Time
	// GitProfile is the opaque version and hash of the git profile, for
	// example "1:<sha256>".
	GitProfile        string
	SharedGitMetadata SharedGitMetadata
}

var (
	labelValue = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
	gitProfile = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:+-]{0,127}$`)
)

// LabelKeys lists every key an instance object must carry.
func LabelKeys() []string {
	return []string{LabelMarker, LabelID, LabelWorkspace, LabelProfile, LabelProvider, LabelRetention,
		LabelCreated, LabelSchema, LabelGitProfile, LabelSharedGitMetadata}
}

// Map renders the labels for a provider.
func (l OwnershipLabels) Map() map[string]string {
	return map[string]string{
		LabelMarker:            "1",
		LabelID:                string(l.ID),
		LabelWorkspace:         l.Workspace,
		LabelProfile:           string(l.Profile),
		LabelProvider:          l.Provider,
		LabelRetention:         string(l.Retention),
		LabelCreated:           l.Created.UTC().Format(time.RFC3339),
		LabelSchema:            SchemaVersion,
		LabelGitProfile:        l.GitProfile,
		LabelSharedGitMetadata: string(l.SharedGitMetadata),
	}
}

// ParseLabels reads the labels of an object. Anything that is not exactly an
// instance object of schema 1 is a foreign object (FR-005): the result is a
// runtime.Refusal with refused.foreign_object. Labels of other owners beside
// ours are ignored.
func ParseLabels(m map[string]string) (OwnershipLabels, error) {
	foreign := runtime.NewRefusal(runtime.RefusedForeignObject)
	if m[LabelMarker] != "1" || m[LabelSchema] != SchemaVersion {
		return OwnershipLabels{}, foreign
	}
	id, err := ParseID(m[LabelID])
	if err != nil {
		return OwnershipLabels{}, foreign
	}
	created, err := time.Parse(time.RFC3339, m[LabelCreated])
	if err != nil || m[LabelCreated] != created.UTC().Format(time.RFC3339) {
		return OwnershipLabels{}, foreign
	}
	l := OwnershipLabels{
		ID:                id,
		Workspace:         m[LabelWorkspace],
		Profile:           runtime.TrustProfile(m[LabelProfile]),
		Provider:          m[LabelProvider],
		Retention:         Retention(m[LabelRetention]),
		Created:           created.UTC(),
		GitProfile:        m[LabelGitProfile],
		SharedGitMetadata: SharedGitMetadata(m[LabelSharedGitMetadata]),
	}
	if l.Validate() != nil {
		return OwnershipLabels{}, foreign
	}
	return l, nil
}

// Validate checks every value and the profile rules: an untrusted job never
// has shared writable metadata (FR-010) and a cooperative job always does
// (FR-011); a developer sandbox records either.
func (l OwnershipLabels) Validate() error {
	bad := runtime.NewRefusal(runtime.RefusedForeignObject)
	switch {
	case !l.ID.Valid(),
		!labelValue.MatchString(l.Workspace),
		!l.Profile.Valid(),
		!labelValue.MatchString(l.Provider),
		!l.Retention.Valid(),
		l.Created.IsZero(),
		!gitProfile.MatchString(l.GitProfile),
		l.SharedGitMetadata != SharedGitWritable && l.SharedGitMetadata != SharedGitNone,
		l.Profile == runtime.ProfileUntrustedJob && l.SharedGitMetadata != SharedGitNone,
		l.Profile == runtime.ProfileCooperativeJob && l.SharedGitMetadata != SharedGitWritable:
		return bad
	}
	return nil
}

// ObjectKind is what an instance owns: containers, volumes and directories.
type ObjectKind string

// The object kinds.
const (
	KindContainer ObjectKind = "container"
	KindVolume    ObjectKind = "volume"
	KindDirectory ObjectKind = "directory"
)

// Object is a provider object a mutating operation would act on.
type Object struct {
	Kind   ObjectKind
	Name   string
	Labels map[string]string
}

// ObjectRef names an object without its labels.
type ObjectRef struct {
	Kind ObjectKind
	Name string
}

// PrimaryEnvironment lists the workspace's own objects (primary container,
// home and tool volumes, checkout). They carry no instance labels and are
// never a sandbox target (FR-001).
type PrimaryEnvironment struct {
	Objects []ObjectRef
}

func (p PrimaryEnvironment) contains(o Object) bool {
	name := o.Name
	if o.Kind == KindDirectory {
		name = path.Clean(name)
	}
	for _, ref := range p.Objects {
		refName := ref.Name
		if ref.Kind == KindDirectory {
			refName = path.Clean(refName)
		}
		if ref.Kind == o.Kind && refName == name {
			return true
		}
	}
	return false
}

// CheckOwned is the ownership gate that every mutating operation passes first
// (FR-005). It returns the labels when obj belongs to instance id. The
// workspace's primary environment is refused as refused.primary_target, even
// if it carried labels; an unlabelled, mislabelled or other instance's object
// is refused as refused.foreign_object. The caller must not touch obj unless
// the error is nil, so a refusal means zero mutations.
func CheckOwned(obj Object, id ID, primary PrimaryEnvironment) (OwnershipLabels, error) {
	if !id.Valid() {
		return OwnershipLabels{}, ErrInvalidID
	}
	if primary.contains(obj) {
		return OwnershipLabels{}, runtime.NewRefusal(runtime.RefusedPrimaryTarget)
	}
	l, err := ParseLabels(obj.Labels)
	if err != nil {
		return OwnershipLabels{}, err
	}
	if l.ID != id {
		return OwnershipLabels{}, runtime.NewRefusal(runtime.RefusedForeignObject)
	}
	return l, nil
}
