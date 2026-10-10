package contract

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type CommandName string

const (
	CreateCommand         CommandName = "create"
	UpdateCommand         CommandName = "update"
	ArchiveCommand        CommandName = "archive"
	RestoreCommand        CommandName = "restore"
	DeleteCommand         CommandName = "delete"
	RetryLifecycleCommand CommandName = "retry-lifecycle"
)

func (c CommandName) Validate() error {
	return oneOf(c, CreateCommand, UpdateCommand, ArchiveCommand, RestoreCommand, DeleteCommand, RetryLifecycleCommand)
}
func (c CommandName) MarshalJSON() ([]byte, error) { return enumJSON(c, c.Validate()) }
func (c *CommandName) UnmarshalJSON(raw []byte) error {
	v, err := decodeEnum(raw, CommandName.Validate)
	if err == nil {
		*c = v
	}
	return err
}

// CommandIdentity always uses the target Project, including its first Create.
// Changing target is a new intent; it is never recovery of an unknown outcome.
func CommandIdentity(target ProjectID, command CommandName, key foundation.IdempotencyKey) (foundation.CommandIdentity, error) {
	if target.Validate() != nil || command.Validate() != nil || key.Validate() != nil {
		return foundation.CommandIdentity{}, invalid("", "INVALID_COMMAND")
	}
	return foundation.NewCommandIdentity("project", []string{target.String()}, string(command), key)
}

func ValidateCommandMeta(command CommandName, meta foundation.CommandMeta) error {
	if command.Validate() != nil || meta.Validate() != nil {
		return invalid("", "INVALID_COMMAND_META")
	}
	if (command == CreateCommand) != (meta.ExpectedVersion == nil) {
		return invalid("/expected_version", "INVALID_PRESENCE")
	}
	return nil
}

type CreateProjectRequest struct {
	ProjectID   ProjectID `json:"project_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
}

func (r CreateProjectRequest) Validate() error {
	if r.ProjectID.Validate() != nil {
		return invalid("/project_id", "INVALID_ID")
	}
	if _, err := NormalizeName(r.Name); err != nil {
		return err
	}
	return ValidateDescription(r.Description)
}
func (r CreateProjectRequest) MarshalJSON() ([]byte, error) {
	type wire CreateProjectRequest
	return checkedJSON(wire(r), r.Validate())
}
func (r *CreateProjectRequest) UnmarshalJSON(raw []byte) error {
	type wire CreateProjectRequest
	v, err := decodeFields[wire](raw, []string{"project_id", "name"}, []string{"description"}, nil)
	if err != nil {
		return err
	}
	value := CreateProjectRequest(v)
	if err = value.Validate(); err == nil {
		*r = value
	}
	return err
}

// Pointers record patch presence. Nil means absent; wire null is rejected.
type UpdateProjectRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	// Scheduler is an internal Owner command capability. Existing HTTP input
	// remains its independent name/description-only wire type.
	Scheduler *ProjectSchedulerConfig `json:"scheduler,omitempty"`
}

func (r UpdateProjectRequest) Validate() error {
	if r.Name == nil && r.Description == nil && r.Scheduler == nil {
		return invalid("", "EMPTY_PATCH")
	}
	if r.Name != nil {
		if _, err := NormalizeName(*r.Name); err != nil {
			return err
		}
	}
	if r.Description != nil {
		if err := ValidateDescription(*r.Description); err != nil {
			return err
		}
	}
	if r.Scheduler != nil {
		return r.Scheduler.Validate()
	}
	return nil
}
func (r UpdateProjectRequest) MarshalJSON() ([]byte, error) {
	type wire UpdateProjectRequest
	return checkedJSON(wire(r), r.Validate())
}
func (r *UpdateProjectRequest) UnmarshalJSON(raw []byte) error {
	type wire UpdateProjectRequest
	v, err := decodeFields[wire](raw, nil, []string{"name", "description", "scheduler"}, nil)
	if err != nil {
		return err
	}
	value := UpdateProjectRequest(v)
	if err = value.Validate(); err == nil {
		*r = value
	}
	return err
}

type DeleteProjectRequest struct {
	NormalizedCurrentPath string `json:"normalized_current_path"`
	Permanent             bool   `json:"permanent"`
}

func (r DeleteProjectRequest) Validate() error {
	if !r.Permanent {
		return invalid("/permanent", "CONFIRMATION_REQUIRED")
	}
	_, err := NormalizeConfirmationPath(r.NormalizedCurrentPath)
	return err
}
func (r DeleteProjectRequest) MarshalJSON() ([]byte, error) {
	type wire DeleteProjectRequest
	return checkedJSON(wire(r), r.Validate())
}
func (r *DeleteProjectRequest) UnmarshalJSON(raw []byte) error {
	type wire DeleteProjectRequest
	v, err := decodeFields[wire](raw, []string{"normalized_current_path", "permanent"}, nil, nil)
	if err != nil {
		return err
	}
	value := DeleteProjectRequest(v)
	if err = value.Validate(); err == nil {
		*r = value
	}
	return err
}

type CommandLookupRequest struct {
	ProjectID ProjectID                 `json:"project_id"`
	Command   CommandName               `json:"command"`
	Key       foundation.IdempotencyKey `json:"key"`
}

func (r CommandLookupRequest) Validate() error {
	_, err := CommandIdentity(r.ProjectID, r.Command, r.Key)
	return err
}
func (r CommandLookupRequest) MarshalJSON() ([]byte, error) {
	type wire CommandLookupRequest
	return checkedJSON(wire(r), r.Validate())
}
func (r *CommandLookupRequest) UnmarshalJSON(raw []byte) error {
	type wire CommandLookupRequest
	v, err := decodeFields[wire](raw, []string{"project_id", "command", "key"}, nil, nil)
	if err != nil {
		return err
	}
	value := CommandLookupRequest(v)
	if err = value.Validate(); err == nil {
		*r = value
	}
	return err
}

type LookupState string

const (
	LookupCommitted   LookupState = "committed"
	LookupInProgress  LookupState = "in_progress"
	LookupNotObserved LookupState = "not_observed"
)

func (s LookupState) Validate() error {
	return oneOf(s, LookupCommitted, LookupInProgress, LookupNotObserved)
}
func (s LookupState) MarshalJSON() ([]byte, error) { return enumJSON(s, s.Validate()) }
func (s *LookupState) UnmarshalJSON(raw []byte) error {
	v, err := decodeEnum(raw, LookupState.Validate)
	if err == nil {
		*s = v
	}
	return err
}

// CommandResult is a closed result union. Receipt replay must still check the
// current Session and visibility before constructing one of these values.
type CommandResult struct {
	Command   CommandName      `json:"command"`
	Creation  *CreationResult  `json:"creation,omitempty"`
	Project   *ProjectRef      `json:"project,omitempty"`
	Lifecycle *LifecycleResult `json:"lifecycle,omitempty"`
}

func (r CommandResult) Validate() error {
	switch r.Command {
	case CreateCommand:
		if r.Creation != nil && r.Project == nil && r.Lifecycle == nil {
			return r.Creation.Validate()
		}
	case UpdateCommand, RestoreCommand:
		if r.Creation == nil && r.Project != nil && r.Lifecycle == nil && r.Project.Lifecycle != Deleting {
			return r.Project.Validate()
		}
	case ArchiveCommand, DeleteCommand, RetryLifecycleCommand:
		if r.Creation == nil && r.Project == nil && r.Lifecycle != nil {
			if err := r.Lifecycle.Validate(); err != nil {
				return err
			}
			if r.Command == ArchiveCommand && (r.Lifecycle.Operation == nil || r.Lifecycle.Operation.Action != Archive) {
				break
			}
			if r.Command == DeleteCommand && r.Lifecycle.Operation != nil && r.Lifecycle.Operation.Action != Delete {
				break
			}
			return nil
		}
	}
	return invalid("", "INVALID_RESULT")
}
func (r CommandResult) MarshalJSON() ([]byte, error) {
	type wire CommandResult
	return checkedJSON(wire(r), r.Validate())
}
func (r *CommandResult) UnmarshalJSON(raw []byte) error {
	type wire CommandResult
	v, err := decodeFields[wire](raw, []string{"command"}, []string{"creation", "project", "lifecycle"}, nil)
	if err != nil {
		return err
	}
	value := CommandResult(v)
	if err = value.Validate(); err == nil {
		*r = value
	}
	return err
}

type CommandLookupResult struct {
	State  LookupState    `json:"state"`
	Result *CommandResult `json:"result,omitempty"`
}

func (r CommandLookupResult) Validate() error {
	if r.State.Validate() != nil {
		return invalid("", "INVALID_RESULT")
	}
	if r.State == LookupCommitted && r.Result != nil {
		return r.Result.Validate()
	}
	if r.State != LookupCommitted && r.Result == nil {
		return nil
	}
	return invalid("", "INVALID_RESULT")
}
func (r CommandLookupResult) MarshalJSON() ([]byte, error) {
	type wire CommandLookupResult
	return checkedJSON(wire(r), r.Validate())
}
func (r *CommandLookupResult) UnmarshalJSON(raw []byte) error {
	type wire CommandLookupResult
	v, err := decodeFields[wire](raw, []string{"state"}, []string{"result"}, nil)
	if err != nil {
		return err
	}
	value := CommandLookupResult(v)
	if err = value.Validate(); err == nil {
		*r = value
	}
	return err
}

type ListOwnedProjectsRequest struct {
	Lifecycle []Lifecycle `json:"lifecycle,omitempty"`
}

// NormalizedFilter is a sorted set. Only an omitted filter defaults to all.
func (r ListOwnedProjectsRequest) NormalizedFilter() ([]Lifecycle, error) {
	if r.Lifecycle == nil {
		return []Lifecycle{Active, Archived, Archiving, Deleting}, nil
	}
	if len(r.Lifecycle) == 0 || len(r.Lifecycle) > 4 {
		return nil, invalid("/lifecycle", "INVALID_FILTER")
	}
	filter := append([]Lifecycle(nil), r.Lifecycle...)
	sort.Slice(filter, func(i, j int) bool { return filter[i] < filter[j] })
	for i, value := range filter {
		if value.Validate() != nil || i > 0 && value == filter[i-1] {
			return nil, invalid("/lifecycle", "INVALID_FILTER")
		}
	}
	return filter, nil
}
func (r ListOwnedProjectsRequest) Validate() error { _, err := r.NormalizedFilter(); return err }
func (r ListOwnedProjectsRequest) MarshalJSON() ([]byte, error) {
	type wire ListOwnedProjectsRequest
	return checkedJSON(wire(r), r.Validate())
}
func (r *ListOwnedProjectsRequest) UnmarshalJSON(raw []byte) error {
	type wire ListOwnedProjectsRequest
	v, err := decodeFields[wire](raw, nil, []string{"lifecycle"}, nil)
	if err != nil {
		return err
	}
	value := ListOwnedProjectsRequest(v)
	if err = value.Validate(); err == nil {
		*r = value
	}
	return err
}
func ValidateProjectPage(page foundation.PageRequest) error {
	if page.Validate() != nil || page.Limit > 100 {
		return invalid("/limit", "INVALID_PAGE")
	}
	return nil
}

type ProjectListItem struct {
	ID          ProjectID          `json:"id"`
	Name        string             `json:"name"`
	Lifecycle   Lifecycle          `json:"lifecycle"`
	Version     foundation.Version `json:"version"`
	Description *string            `json:"description,omitempty"`
	OperationID *OperationID       `json:"operation_id,omitempty"`
}

func (p ProjectListItem) Validate() error {
	if p.ID.Validate() != nil || p.Version.Validate() != nil || p.Lifecycle.Validate() != nil {
		return invalid("", "INVALID_PROJECT")
	}
	if _, err := NormalizeName(p.Name); err != nil {
		return err
	}
	if p.OperationID != nil && p.OperationID.Validate() != nil {
		return invalid("/operation_id", "INVALID_ID")
	}
	if p.Lifecycle == Deleting {
		if p.Description != nil || p.OperationID == nil {
			return invalid("", "INVALID_RESULT")
		}
		return nil
	}
	if p.Description == nil || p.Lifecycle == Archiving && p.OperationID == nil {
		return invalid("", "INVALID_RESULT")
	}
	return ValidateDescription(*p.Description)
}
func (p ProjectListItem) MarshalJSON() ([]byte, error) {
	type wire ProjectListItem
	return checkedJSON(wire(p), p.Validate())
}
func (p *ProjectListItem) UnmarshalJSON(raw []byte) error {
	type wire ProjectListItem
	v, err := decodeFields[wire](raw, []string{"id", "name", "lifecycle", "version"}, []string{"description", "operation_id"}, nil)
	if err != nil {
		return err
	}
	value := ProjectListItem(v)
	if err = value.Validate(); err == nil {
		*p = value
	}
	return err
}

// Fields are in canonical-v1 object-key order; semantic integers use foundation
// decimal strings. These private, fixed structs are not arbitrary JSON patches.
type semanticParameters struct {
	Description           *string                 `json:"description,omitempty"`
	Name                  *string                 `json:"name,omitempty"`
	NormalizedCurrentPath *string                 `json:"normalized_current_path,omitempty"`
	NormalizedName        *string                 `json:"normalized_name,omitempty"`
	OperationID           *OperationID            `json:"operation_id,omitempty"`
	Permanent             *bool                   `json:"permanent,omitempty"`
	Scheduler             *ProjectSchedulerConfig `json:"scheduler,omitempty"`
}
type semanticCommand struct {
	ActorUserID     string              `json:"actor_user_id"`
	Command         CommandName         `json:"command"`
	ExpectedVersion *foundation.Version `json:"expected_version"`
	Parameters      semanticParameters  `json:"parameters"`
	ProjectID       ProjectID           `json:"project_id"`
}

func digestBytes(domain string, raw []byte) foundation.Digest {
	sum := sha256.Sum256(append([]byte(domain+"\x00"), raw...))
	return foundation.Digest("sha256:" + hex.EncodeToString(sum[:]))
}
func semanticDigest(human identity.Actor, meta foundation.CommandMeta, command CommandName, project ProjectID, parameters semanticParameters) (foundation.Digest, error) {
	if human.Validate() != nil || human.Details().Kind != identity.Human {
		return "", fault(foundation.Unauthenticated)
	}
	if err := ValidateCommandMeta(command, meta); err != nil {
		return "", err
	}
	if project.Validate() != nil {
		return "", invalid("/project_id", "INVALID_ID")
	}
	raw, err := json.Marshal(semanticCommand{human.Details().UserID, command, meta.ExpectedVersion, parameters, project})
	if err != nil {
		return "", invalid("", "INVALID_ENCODING")
	}
	return digestBytes("agenteam.project.command.canonical-v1", raw), nil
}
func CreateDigest(human identity.Actor, meta foundation.CommandMeta, r CreateProjectRequest) (foundation.Digest, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	normalized, _ := NormalizeName(r.Name)
	return semanticDigest(human, meta, CreateCommand, r.ProjectID, semanticParameters{Name: &r.Name, NormalizedName: &normalized, Description: &r.Description})
}
func UpdateDigest(human identity.Actor, meta foundation.CommandMeta, project ProjectID, r UpdateProjectRequest) (foundation.Digest, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	parameters := semanticParameters{Name: r.Name, Description: r.Description, Scheduler: r.Scheduler}
	if r.Name != nil {
		normalized, _ := NormalizeName(*r.Name)
		parameters.NormalizedName = &normalized
	}
	return semanticDigest(human, meta, UpdateCommand, project, parameters)
}
func ArchiveDigest(human identity.Actor, meta foundation.CommandMeta, project ProjectID) (foundation.Digest, error) {
	return semanticDigest(human, meta, ArchiveCommand, project, semanticParameters{})
}
func RestoreDigest(human identity.Actor, meta foundation.CommandMeta, project ProjectID) (foundation.Digest, error) {
	return semanticDigest(human, meta, RestoreCommand, project, semanticParameters{})
}
func DeleteDigest(human identity.Actor, meta foundation.CommandMeta, project ProjectID, r DeleteProjectRequest) (foundation.Digest, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	path, _ := NormalizeConfirmationPath(r.NormalizedCurrentPath)
	return semanticDigest(human, meta, DeleteCommand, project, semanticParameters{NormalizedCurrentPath: &path, Permanent: &r.Permanent})
}
func RetryLifecycleDigest(human identity.Actor, meta foundation.CommandMeta, project ProjectID, operation OperationID) (foundation.Digest, error) {
	if operation.Validate() != nil {
		return "", invalid("/operation_id", "INVALID_ID")
	}
	return semanticDigest(human, meta, RetryLifecycleCommand, project, semanticParameters{OperationID: &operation})
}

func DeletionCommandKeyHash(project ProjectID, owner identity.UserID, key foundation.IdempotencyKey) (foundation.Digest, error) {
	if project.Validate() != nil || owner.Validate() != nil || key.Validate() != nil {
		return "", invalid("", "INVALID_COMMAND")
	}
	raw, err := json.Marshal(struct {
		Key     foundation.IdempotencyKey `json:"key"`
		Owner   identity.UserID           `json:"original_owner_user_id"`
		Project ProjectID                 `json:"project_id"`
	}{key, owner, project})
	if err != nil {
		return "", invalid("", "INVALID_ENCODING")
	}
	return digestBytes("agenteam.project.delete-key.v1", raw), nil
}

// OwnedProjectsQueryDigest binds the cursor to owner/filter/order. Neither the
// Session nor page limit belongs to the digest. It does not authorize a page.
func OwnedProjectsQueryDigest(owner identity.UserID, r ListOwnedProjectsRequest) (foundation.Digest, error) {
	if owner.Validate() != nil {
		return "", invalid("/user_id", "INVALID_ID")
	}
	filter, err := r.NormalizedFilter()
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(struct {
		Lifecycle []Lifecycle     `json:"lifecycle_filter"`
		Order     string          `json:"order"`
		Query     string          `json:"query"`
		Owner     identity.UserID `json:"user_id"`
	}{filter, "created_at-desc,id-desc", "owned-projects-v1", owner})
	if err != nil {
		return "", invalid("", "INVALID_ENCODING")
	}
	return digestBytes("agenteam.project.query.canonical-v1", raw), nil
}

// ProjectService is a consumer port, with no default or successful unbound
// implementation. Every Human call rechecks the current Session and Owner.
type ProjectService interface {
	CreateProject(context.Context, identity.Actor, foundation.CommandMeta, CreateProjectRequest) (CreationResult, error)
	GetCreation(context.Context, identity.Actor, CreationID) (CreationResult, error)
	GetProject(context.Context, identity.Actor, ProjectID) (ProjectRef, error)
	ListOwnedProjects(context.Context, identity.Actor, ListOwnedProjectsRequest, foundation.PageRequest) (foundation.Page[ProjectListItem], error)
	UpdateProject(context.Context, identity.Actor, foundation.CommandMeta, ProjectID, UpdateProjectRequest) (ProjectRef, error)
	ResolveProjectPath(context.Context, identity.Actor, string, string) (ProjectRef, error)
	BeginArchive(context.Context, identity.Actor, foundation.CommandMeta, ProjectID) (LifecycleOperation, error)
	RestoreProject(context.Context, identity.Actor, foundation.CommandMeta, ProjectID) (ProjectRef, error)
	BeginDeleteProject(context.Context, identity.Actor, foundation.CommandMeta, ProjectID, DeleteProjectRequest) (LifecycleResult, error)
	GetLifecycle(context.Context, identity.Actor, ProjectID, OperationID) (LifecycleResult, error)
	RetryLifecycle(context.Context, identity.Actor, foundation.CommandMeta, ProjectID, OperationID) (LifecycleResult, error)
	LookupCommand(context.Context, identity.Actor, CommandLookupRequest) (CommandLookupResult, error)
}
