package contract

import (
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

type CommandName string

const (
	Create        CommandName = "create"
	Update        CommandName = "update"
	Move          CommandName = "move"
	DeleteSubtree CommandName = "delete-subtree"
)

type LookupState string

const (
	Committed   LookupState = "committed"
	InProgress  LookupState = "in_progress"
	NotObserved LookupState = "not_observed"
)

type CreateRequest struct {
	ProjectID        id.ProjectID `json:"project_id"`
	DocumentID       DocumentID   `json:"document_id"`
	ParentDocumentID *DocumentID  `json:"parent_document_id"`
	Title            string       `json:"title"`
}

func (r CreateRequest) Validate() error {
	if r.ProjectID.Validate() != nil || r.DocumentID.Validate() != nil || !validParent(r.ParentDocumentID) || r.ParentDocumentID != nil && *r.ParentDocumentID == r.DocumentID || ValidateTitle(r.Title) != nil {
		return invalid()
	}
	return nil
}

type UpdateRequest struct {
	Title         *string `json:"title,omitempty"`
	ReplaceSource bool    `json:"replace_source"`
}

func (r UpdateRequest) Validate() error {
	if r.Title == nil && !r.ReplaceSource || r.Title != nil && ValidateTitle(*r.Title) != nil {
		return invalid()
	}
	return nil
}

type LookupRequest struct {
	ProjectID      id.ProjectID     `json:"project_id"`
	Command        CommandName      `json:"command"`
	Key            f.IdempotencyKey `json:"key"`
	SemanticDigest f.Digest         `json:"semantic_digest"`
}

func (r LookupRequest) Validate() error {
	if r.ProjectID.Validate() != nil || r.Command.Validate() != nil || r.Key.Validate() != nil || r.SemanticDigest.Validate() != nil {
		return invalid()
	}
	return nil
}

type MutationReceipt struct {
	Command        CommandName  `json:"command"`
	Document       *DocumentRef `json:"document,omitempty"`
	RootID         *DocumentID  `json:"root_id,omitempty"`
	Changed        bool         `json:"changed"`
	DeletedIDs     []DocumentID `json:"deleted_ids,omitempty"`
	CleanupPending bool         `json:"cleanup_pending,omitempty"`
}

func (r MutationReceipt) Validate() error {
	if r.Command.Validate() != nil {
		return invalid()
	}
	if r.Command == DeleteSubtree {
		if r.Document != nil || r.RootID == nil || !r.Changed {
			return invalid()
		}
		return (DeleteResult{*r.RootID, r.DeletedIDs, r.CleanupPending}).Validate()
	}
	if r.Document == nil || r.Document.Validate() != nil || r.Document.Status != Active || r.RootID != nil || len(r.DeletedIDs) != 0 || r.CleanupPending || r.Command == Create && !r.Changed {
		return invalid()
	}
	return nil
}

type CommandLookup struct {
	State   LookupState      `json:"state"`
	Receipt *MutationReceipt `json:"receipt,omitempty"`
}

func (r MutationReceipt) MarshalJSON() ([]byte, error) {
	if e := r.Validate(); e != nil {
		return nil, e
	}
	if r.Command == DeleteSubtree {
		return checked(struct {
			Command CommandName  `json:"command"`
			Root    DocumentID   `json:"root_id"`
			Changed bool         `json:"changed"`
			IDs     []DocumentID `json:"deleted_ids"`
			Pending bool         `json:"cleanup_pending"`
		}{r.Command, *r.RootID, r.Changed, r.DeletedIDs, r.CleanupPending}, nil)
	}
	return checked(struct {
		Command  CommandName  `json:"command"`
		Document *DocumentRef `json:"document"`
		Changed  bool         `json:"changed"`
	}{r.Command, r.Document, r.Changed}, nil)
}
func (r *MutationReceipt) UnmarshalJSON(b []byte) error {
	type w MutationReceipt
	v, e := decode[w](b, []string{"command", "changed"}, []string{"document", "root_id", "deleted_ids", "cleanup_pending"}, nil)
	if e != nil {
		return e
	}
	fields := []string{"command", "changed", "document"}
	if v.Command == DeleteSubtree {
		fields = []string{"command", "changed", "root_id", "deleted_ids", "cleanup_pending"}
	}
	v, e = decode[w](b, fields, nil, nil)
	if e != nil {
		return e
	}
	n := MutationReceipt(v)
	if e = n.Validate(); e == nil {
		*r = n
	}
	return e
}
func (r CommandLookup) Validate() error {
	if r.State.Validate() != nil {
		return invalid()
	}
	if r.State == Committed {
		if r.Receipt == nil {
			return invalid()
		}
		return r.Receipt.Validate()
	}
	if r.Receipt != nil {
		return invalid()
	}
	return nil
}

func CommandIdentity(project id.ProjectID, name CommandName, key f.IdempotencyKey) (f.CommandIdentity, error) {
	if project.Validate() != nil || name.Validate() != nil || key.Validate() != nil {
		return f.CommandIdentity{}, invalid()
	}
	return f.NewCommandIdentity("knowledge", []string{project.String()}, string(name), key)
}
func commandValid(actor id.Actor, meta f.CommandMeta, project id.ProjectID, target DocumentID, name CommandName) error {
	if actor.Validate() != nil || actor.Details().Kind != id.Human || meta.Validate() != nil || project.Validate() != nil || target.Validate() != nil || name.Validate() != nil {
		return invalid()
	}
	if name == Update {
		if meta.ExpectedVersion == nil {
			return invalid()
		}
	} else if meta.ExpectedVersion != nil {
		return invalid()
	}
	return nil
}

type semanticBase struct {
	Command         CommandName  `json:"command"`
	UserID          string       `json:"user_id"`
	ProjectID       id.ProjectID `json:"project_id"`
	DocumentID      DocumentID   `json:"document_id"`
	ExpectedVersion *f.Version   `json:"expected_version"`
}

func base(a id.Actor, m f.CommandMeta, p id.ProjectID, d DocumentID, c CommandName) semanticBase {
	return semanticBase{c, a.Details().UserID, p, d, clonePtr(m.ExpectedVersion)}
}

type receiptSemantic struct {
	ID            string          `json:"receipt_id"`
	UploadID      string          `json:"upload_id"`
	ObjectID      string          `json:"object_id"`
	Owner         oc.OwnerDetails `json:"owner"`
	CreationCause string          `json:"creation_cause"`
}
type businessSemantic struct {
	Kind        oc.SourceKind    `json:"kind"`
	ProjectID   string           `json:"project_id"`
	ArtifactID  string           `json:"artifact_id"`
	FileID      string           `json:"file_id"`
	DocumentID  string           `json:"document_id"`
	ExecutionID string           `json:"execution_id"`
	PayloadID   string           `json:"payload_id"`
	Revision    *f.Version       `json:"revision,omitempty"`
	Receipt     *receiptSemantic `json:"receipt,omitempty"`
}
type sourceSemantic struct {
	Kind      InputKind         `json:"kind"`
	MediaType string            `json:"media_type,omitempty"`
	Length    *f.Progress       `json:"length,omitempty"`
	SHA256    *f.Digest         `json:"sha256,omitempty"`
	Business  *businessSemantic `json:"business_file,omitempty"`
}

func sourceProjection(s SourceInput) (sourceSemantic, error) {
	d, e := s.Details()
	if e != nil {
		return sourceSemantic{}, e
	}
	v := sourceSemantic{Kind: d.Kind, MediaType: d.MediaType}
	switch d.Kind {
	case InputText:
		l := f.Progress(len(*d.Text))
		h := rawSHA([]byte(*d.Text))
		v.Length = &l
		v.SHA256 = &h
	case InputUpload:
		l := d.Upload.Length
		h := d.Upload.ExpectedSHA256
		v.Length = &l
		v.SHA256 = &h
	case InputBusinessFile:
		b := d.BusinessFile.Details()
		v.Business = &businessSemantic{Kind: b.Kind, ArtifactID: b.ArtifactID, FileID: b.FileID, DocumentID: b.DocumentID, ExecutionID: b.ExecutionID, PayloadID: b.PayloadID}
		if b.ProjectID.Validate() == nil {
			v.Business.ProjectID = b.ProjectID.String()
		}
		if b.Revision != 0 {
			v.Business.Revision = clonePtr(&b.Revision)
		}
		if b.Kind == oc.UploadedObject {
			r := b.Receipt.Details()
			v.Business.Receipt = &receiptSemantic{r.ID.String(), r.UploadID.String(), r.ObjectID.String(), r.Owner.Details(), r.CreationCause}
		}
	}
	return v, nil
}

const commandDomain = "agenteam.knowledge.command.canonical-v1"

func CreateDigest(a id.Actor, m f.CommandMeta, r CreateRequest, s SourceInput) (f.Digest, error) {
	if commandValid(a, m, r.ProjectID, r.DocumentID, Create) != nil || r.Validate() != nil {
		return "", invalid()
	}
	src, e := sourceProjection(s)
	if e != nil {
		return "", e
	}
	return digest(commandDomain, struct {
		Base   semanticBase   `json:"identity"`
		Parent *DocumentID    `json:"parent_document_id"`
		Title  string         `json:"title"`
		Source sourceSemantic `json:"source"`
	}{base(a, m, r.ProjectID, r.DocumentID, Create), r.ParentDocumentID, r.Title, src})
}
func UpdateDigest(a id.Actor, m f.CommandMeta, p id.ProjectID, d DocumentID, r UpdateRequest, s *SourceInput) (f.Digest, error) {
	if commandValid(a, m, p, d, Update) != nil || r.Validate() != nil || (s != nil) != r.ReplaceSource {
		return "", invalid()
	}
	var src *sourceSemantic
	if s != nil {
		v, e := sourceProjection(*s)
		if e != nil {
			return "", e
		}
		src = &v
	}
	return digest(commandDomain, struct {
		Base    semanticBase    `json:"identity"`
		Title   *string         `json:"title"`
		Replace bool            `json:"replace_source"`
		Source  *sourceSemantic `json:"source"`
	}{base(a, m, p, d, Update), r.Title, r.ReplaceSource, src})
}
func MoveDigest(a id.Actor, m f.CommandMeta, p id.ProjectID, d DocumentID, r MoveRequest) (f.Digest, error) {
	if commandValid(a, m, p, d, Move) != nil || r.Validate() != nil || r.TargetParentID != nil && *r.TargetParentID == d {
		return "", invalid()
	}
	return digest(commandDomain, struct {
		Base semanticBase `json:"identity"`
		Move MoveRequest  `json:"move"`
	}{base(a, m, p, d, Move), r})
}
func DeleteDigest(a id.Actor, m f.CommandMeta, p id.ProjectID, d DocumentID, t ConfirmationToken) (f.Digest, error) {
	if commandValid(a, m, p, d, DeleteSubtree) != nil || t.Validate() != nil {
		return "", invalid()
	}
	return digest(commandDomain, struct {
		Base  semanticBase `json:"identity"`
		Token f.Digest     `json:"confirmation_sha256"`
	}{base(a, m, p, d, DeleteSubtree), rawSHA([]byte(t.ForHumanResponse()))})
}

func (v CommandName) Validate() error              { return oneOf(v, Create, Update, Move, DeleteSubtree) }
func (v CommandName) MarshalJSON() ([]byte, error) { return enumJSON(v, v.Validate()) }
func (v *CommandName) UnmarshalJSON(b []byte) error {
	n, e := enumDecode(b, CommandName.Validate)
	if e == nil {
		*v = n
	}
	return e
}

func (v LookupState) Validate() error              { return oneOf(v, Committed, InProgress, NotObserved) }
func (v LookupState) MarshalJSON() ([]byte, error) { return enumJSON(v, v.Validate()) }
func (v *LookupState) UnmarshalJSON(b []byte) error {
	n, e := enumDecode(b, LookupState.Validate)
	if e == nil {
		*v = n
	}
	return e
}

func (v CreateRequest) MarshalJSON() ([]byte, error) {
	type wire CreateRequest
	return checked(wire(v), v.Validate())
}
func (v *CreateRequest) UnmarshalJSON(b []byte) error {
	type wire CreateRequest
	w, e := decode[wire](b, []string{"project_id", "document_id", "parent_document_id", "title"}, nil, []string{"parent_document_id"})
	if e != nil {
		return e
	}
	n := CreateRequest(w)
	if e = n.Validate(); e == nil {
		*v = n
	}
	return e
}

func (v UpdateRequest) MarshalJSON() ([]byte, error) {
	type wire UpdateRequest
	return checked(wire(v), v.Validate())
}
func (v *UpdateRequest) UnmarshalJSON(b []byte) error {
	type wire UpdateRequest
	w, e := decode[wire](b, []string{"replace_source"}, []string{"title"}, nil)
	if e != nil {
		return e
	}
	n := UpdateRequest(w)
	if e = n.Validate(); e == nil {
		*v = n
	}
	return e
}

func (v LookupRequest) MarshalJSON() ([]byte, error) {
	type wire LookupRequest
	return checked(wire(v), v.Validate())
}
func (v *LookupRequest) UnmarshalJSON(b []byte) error {
	type wire LookupRequest
	w, e := decode[wire](b, []string{"project_id", "command", "key", "semantic_digest"}, nil, nil)
	if e != nil {
		return e
	}
	n := LookupRequest(w)
	if e = n.Validate(); e == nil {
		*v = n
	}
	return e
}

func (v CommandLookup) MarshalJSON() ([]byte, error) {
	type wire CommandLookup
	return checked(wire(v), v.Validate())
}
func (v *CommandLookup) UnmarshalJSON(b []byte) error {
	type wire CommandLookup
	w, e := decode[wire](b, []string{"state"}, []string{"receipt"}, nil)
	if e != nil {
		return e
	}
	n := CommandLookup(w)
	if e = n.Validate(); e == nil {
		*v = n
	}
	return e
}
