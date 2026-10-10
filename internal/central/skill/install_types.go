package skill

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

// Installation identifies the one durable install command. It is not an
// assignment, an initialization receipt, a ToolCall, or authority to publish.
type Installation struct{}
type InstallationID = f.ID[Installation]

// InstallRequest accepts only the existing validated, canonical text package.
// It cannot name a host path, URL, Object key, Runner, or credentials. Ordinary
// creation publishes revision one; update has no entry point in this slice.
type InstallRequest struct{ data func() installInput }
type installInput struct {
	skill                         sc.SkillID
	pkg                           Package
	name, normalized, description string
	packageDigest, manifestDigest f.Digest
	size                          f.Progress
	manifest                      sc.Manifest
}

func NewInstallRequest(ctx context.Context, skill sc.SkillID, pkg Package) (InstallRequest, error) {
	if ctx == nil || skill.Validate() != nil || pkg.Validate() != nil {
		return InstallRequest{}, invalid()
	}
	if err := ctx.Err(); err != nil {
		return InstallRequest{}, portError(err)
	}
	// Package's private closure was created by BuildPackage or the strict
	// canonical parser. It exposes no mutable backing bytes to its callers.
	data := pkg.data()
	normalized, err := sc.NormalizeName(data.entry.Name)
	if err != nil {
		return InstallRequest{}, err
	}
	if normalized == AddSkillsNormalizedName {
		return InstallRequest{}, fault(f.Forbidden)
	}
	manifestDigest, err := data.manifest.Digest()
	if err != nil {
		return InstallRequest{}, err
	}
	input := installInput{skill: skill, pkg: pkg, name: data.entry.Name, normalized: normalized, description: data.entry.Description,
		packageDigest: data.digest, manifestDigest: manifestDigest, size: f.Progress(len(data.bytes)), manifest: data.manifest}
	if err = input.validate(); err != nil {
		return InstallRequest{}, err
	}
	if err = ctx.Err(); err != nil {
		return InstallRequest{}, portError(err)
	}
	return InstallRequest{data: func() installInput { return input }}, nil
}

func (v installInput) validate() error {
	key, err := sc.NormalizeName(v.name)
	if err != nil || key != v.normalized || key == AddSkillsNormalizedName || v.skill.Validate() != nil || v.pkg.Validate() != nil || v.manifest.Validate() != nil || v.packageDigest.Validate() != nil || v.manifestDigest.Validate() != nil || v.size <= 0 || v.size > sc.MaxArchiveBytes {
		return invalid()
	}
	// Metadata comes from the existing strict SKILL.md parser. Do not introduce
	// a second, relaxed description/name schema at the installation boundary.
	d := v.pkg.data()
	md, err := d.manifest.Digest()
	if err != nil || d.entry.Name != v.name || d.entry.Description != v.description || d.digest != v.packageDigest || md != v.manifestDigest || len(d.bytes) != int(v.size) {
		return invalid()
	}
	return nil
}

func (r InstallRequest) Validate() error {
	if r.data == nil {
		return invalid()
	}
	return r.data().validate()
}
func (r InstallRequest) SkillID() sc.SkillID {
	if r.data == nil {
		return sc.SkillID{}
	}
	return r.data().skill
}
func (InstallRequest) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "skill_install_request") }
func (InstallRequest) LogValue() slog.Value         { return slog.StringValue("skill_install_request") }
func (InstallRequest) MarshalJSON() ([]byte, error) { return []byte(`"skill_install_request"`), nil }
func (*InstallRequest) UnmarshalJSON([]byte) error  { return invalid() }

func installIdentity(project id.ProjectID, key f.IdempotencyKey) (f.CommandIdentity, error) {
	if project.Validate() != nil || key.Validate() != nil {
		return f.CommandIdentity{}, invalid()
	}
	return f.NewCommandIdentity("project", []string{project.String()}, "skill.install", key)
}

// Bind the canonical content and caller identity, not request IDs or a current
// default package. Retries under a new Session of the same User still require
// fresh authorization; another User cannot claim this command's receipt.
func installSemantic(project id.ProjectID, user id.UserID, input installInput) (f.Digest, error) {
	if project.Validate() != nil || user.Validate() != nil || input.validate() != nil {
		return "", invalid()
	}
	raw, err := json.Marshal(struct {
		Format, Project, User, Skill, Name, Normalized, Description string
		Package, Manifest                                           f.Digest
		Size                                                        f.Progress
	}{"skill.install.v1", project.String(), user.String(), input.skill.String(), input.name, input.normalized, input.description, input.packageDigest, input.manifestDigest, input.size})
	if err != nil {
		return "", unavailable(err)
	}
	return sum(raw), nil
}

// InstallReceipt is emitted only after the original publication transaction is
// known committed. It intentionally contains no package bytes, filename list,
// command key, storage key, or implied Agent assignment.
type InstallReceipt struct {
	InstallationID InstallationID
	SkillID        sc.SkillID
	ProjectID      id.ProjectID
	RevisionID     sc.RevisionID
	Revision       f.Revision
	Version        f.Version
	ObjectID       oc.ObjectID
	PackageSHA256  f.Digest
}

func (r InstallReceipt) Validate() error {
	if r.InstallationID.Validate() != nil || r.SkillID.Validate() != nil || r.ProjectID.Validate() != nil || r.RevisionID.Validate() != nil || r.Revision != 1 || r.Version != 1 || r.ObjectID.Validate() != nil || r.PackageSHA256.Validate() != nil {
		return invalid()
	}
	return nil
}
func (InstallReceipt) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "skill_install_receipt") }
func (InstallReceipt) LogValue() slog.Value         { return slog.StringValue("skill_install_receipt") }
func (InstallReceipt) MarshalJSON() ([]byte, error) { return []byte(`"skill_install_receipt"`), nil }
func (*InstallReceipt) UnmarshalJSON([]byte) error  { return invalid() }
