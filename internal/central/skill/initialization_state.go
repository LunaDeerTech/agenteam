package skill

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

type initializationPhase string

const (
	initializationPlanned   initializationPhase = "planned"
	initializationReserved  initializationPhase = "reserved"
	initializationPublished initializationPhase = "published"
	initializationFailed    initializationPhase = "failed"
)

// This is the content accepted by the original command, not a lookup of the
// currently running binary's default bundle. It survives restart unchanged.
type frozenBundle struct {
	id                            string
	revision                      f.Revision
	manifest                      sc.Manifest
	packageDigest, manifestDigest f.Digest
	size                          f.Progress
	name, description             string
}

func freezeBundle(b BuiltinBundle) (frozenBundle, error) {
	if b.Validate() != nil {
		return frozenBundle{}, invalid()
	}
	d := b.data()
	p := d.content.data()
	md, e := p.manifest.Digest()
	if e != nil {
		return frozenBundle{}, e
	}
	out := frozenBundle{d.id, d.revision, p.manifest, p.digest, md, f.Progress(len(p.bytes)), p.entry.Name, p.entry.Description}
	if e = out.validate(); e != nil {
		return frozenBundle{}, e
	}
	return out, nil
}
func (b frozenBundle) validate() error {
	if b.id != AddSkillsBundleID || b.revision != 1 || b.name != AddSkillsName || b.packageDigest != AddSkillsPackageSHA256 || b.size != 2587 || b.manifest.Validate() != nil {
		return unavailable(nil)
	}
	md, e := b.manifest.Digest()
	if e != nil || md != b.manifestDigest {
		return unavailable(e)
	}
	details, e := b.manifest.Details()
	if e != nil {
		return unavailable(e)
	}
	files := details.Files
	if len(files) != 1 || files[0].Path != sc.EntryPath || files[0].ByteSize != 2473 || files[0].SHA256 != AddSkillsEntrySHA256 {
		return unavailable(nil)
	}
	// Metadata validation is independent of the caller's current owner grant.
	entry, e := sc.EntryMetadataFromText(addSkillsText)
	if e != nil || b.description != entry.Description {
		return unavailable(e)
	}
	return nil
}
func initializationSemantic(r pc.InitializationRequest, b frozenBundle) (f.Digest, error) {
	if r.Validate() != nil {
		return "", invalid()
	}
	if e := b.validate(); e != nil {
		return "", e
	}
	raw, e := json.Marshal(struct {
		Format   string                   `json:"format"`
		Role     string                   `json:"role"`
		Request  pc.InitializationRequest `json:"request"`
		Bundle   string                   `json:"bundle"`
		Revision f.Revision               `json:"revision"`
		Package  f.Digest                 `json:"package_sha256"`
		Manifest f.Digest                 `json:"manifest_sha256"`
	}{"skill.initialization.v1", "project-initialization", r, b.id, b.revision, b.packageDigest, b.manifestDigest})
	if e != nil {
		return "", unavailable(e)
	}
	return sum(raw), nil
}

type initializationRow struct {
	request          pc.InitializationRequest
	skill            sc.SkillID
	revision         sc.RevisionID
	bundle           frozenBundle
	semantic         f.Digest
	phase            initializationPhase
	version          f.Version
	object           oc.ObjectID
	upload           oc.UploadID
	attempt          oc.AttemptID
	reason           pc.SafeReason
	created, updated f.Instant
}

func (r initializationRow) validate() error {
	if r.request.Validate() != nil || r.skill.Validate() != nil || r.revision.Validate() != nil || r.version.Validate() != nil || r.created.Validate() != nil || r.updated.Validate() != nil || r.updated.Time().Before(r.created.Time()) {
		return unavailable(nil)
	}
	semantic, e := initializationSemantic(r.request, r.bundle)
	if e != nil || semantic != r.semantic {
		return unavailable(e)
	}
	bound := r.object.Validate() == nil && r.upload.Validate() == nil && r.attempt.Validate() == nil
	empty := r.object == (oc.ObjectID{}) && r.upload == (oc.UploadID{}) && r.attempt == (oc.AttemptID{})
	if !bound && !empty {
		return unavailable(nil)
	}
	switch r.phase {
	case initializationPlanned:
		if !empty || r.reason != "" {
			return unavailable(nil)
		}
	case initializationReserved, initializationPublished:
		if !bound || r.reason != "" {
			return unavailable(nil)
		}
	case initializationFailed:
		if r.reason.Validate() != nil || r.attempt != (oc.AttemptID{}) && r.attempt.Validate() != nil {
			return unavailable(nil)
		}
	default:
		return unavailable(nil)
	}
	return nil
}
func (r initializationRow) matches(request pc.InitializationRequest, b frozenBundle) error {
	if e := r.validate(); e != nil {
		return e
	}
	if r.request.CreationID != request.CreationID || r.request.ProjectID != request.ProjectID {
		return fault(f.Forbidden)
	}
	semantic, e := initializationSemantic(request, b)
	if e != nil {
		return e
	}
	if r.request.InitializationKey != request.InitializationKey || r.semantic != semantic {
		return fault(f.IdempotencyKeyReused)
	}
	return nil
}
func (r initializationRow) owner() (oc.ObjectOwner, error) {
	return oc.NewObjectOwner(oc.SkillRevision, r.revision.String(), r.request.ProjectID.String())
}
func (r initializationRow) locks(mode f.LockMode, object oc.ObjectID) ([]f.LockRequest, error) {
	if e := r.validate(); e != nil {
		return nil, e
	}
	cmd, e := initializationIdentity(r.request)
	if e != nil {
		return nil, e
	}
	locks := []f.LockRequest{commandLock(cmd), projectLock(r.request.ProjectID, f.Exclusive), skillLock(r.skill, mode)}
	if object != (oc.ObjectID{}) {
		if object.Validate() != nil {
			return nil, invalid()
		}
		locks = append(locks, objectLock(object, mode))
	}
	return locks, nil
}

// Command material and immutable package metadata are private database facts.
func (initializationRow) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "skill_initialization")
}
func (initializationRow) MarshalJSON() ([]byte, error) { return []byte(`"skill_initialization"`), nil }
func (initializationRow) LogValue() slog.Value         { return slog.StringValue("skill_initialization") }
