package artifact

import (
	"bytes"
	"encoding/json"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

// This repository-only representation is not a Tool/HTTP DTO. IDs and captured
// content facts are durable; URLs, locators, arbitrary payload and grants are not.
type referenceWire struct {
	Kind         oc.SourceKind      `json:"kind"`
	Project      string             `json:"project,omitempty"`
	Artifact     string             `json:"artifact,omitempty"`
	File         string             `json:"file,omitempty"`
	Document     string             `json:"document,omitempty"`
	Execution    string             `json:"execution,omitempty"`
	Payload      string             `json:"payload,omitempty"`
	Revision     foundation.Version `json:"revision,omitempty"`
	Receipt      string             `json:"receipt,omitempty"`
	Upload       string             `json:"upload,omitempty"`
	Object       string             `json:"object,omitempty"`
	OwnerKind    oc.OwnerKind       `json:"owner_kind,omitempty"`
	Owner        string             `json:"owner,omitempty"`
	OwnerProject string             `json:"owner_project,omitempty"`
	Cause        string             `json:"cause,omitempty"`
}

func referenceStorage(r oc.BusinessFileRef) referenceWire {
	d := r.Details()
	w := referenceWire{Kind: d.Kind, Artifact: d.ArtifactID, File: d.FileID, Document: d.DocumentID, Execution: d.ExecutionID, Payload: d.PayloadID, Revision: d.Revision}
	if d.Kind == oc.UploadedObject {
		u := d.Receipt.Details()
		o := u.Owner.Details()
		w.Receipt = u.ID.String()
		w.Upload = u.UploadID.String()
		w.Object = u.ObjectID.String()
		w.OwnerKind = o.Kind
		w.Owner = o.ID
		w.OwnerProject = o.ProjectID
		w.Cause = u.CreationCause
	} else {
		w.Project = d.ProjectID.String()
	}
	return w
}
func (w referenceWire) value() (oc.BusinessFileRef, error) {
	d := oc.BusinessFileDetails{Kind: w.Kind, ArtifactID: w.Artifact, FileID: w.File, DocumentID: w.Document, ExecutionID: w.Execution, PayloadID: w.Payload, Revision: w.Revision}
	if w.Kind == oc.UploadedObject {
		rid, e := foundation.ParseID[oc.Receipt](w.Receipt)
		if e != nil {
			return oc.BusinessFileRef{}, invalid()
		}
		uid, e := foundation.ParseID[oc.Upload](w.Upload)
		if e != nil {
			return oc.BusinessFileRef{}, invalid()
		}
		oid, e := foundation.ParseID[oc.StoredObject](w.Object)
		if e != nil {
			return oc.BusinessFileRef{}, invalid()
		}
		o, e := oc.NewObjectOwner(w.OwnerKind, w.Owner, w.OwnerProject)
		if e != nil {
			return oc.BusinessFileRef{}, invalid()
		}
		d.Receipt, e = oc.NewUploadReceipt(oc.ReceiptDetails{ID: rid, UploadID: uid, ObjectID: oid, Owner: o, CreationCause: w.Cause})
		if e != nil {
			return oc.BusinessFileRef{}, invalid()
		}
	} else {
		var e error
		d.ProjectID, e = foundation.ParseID[identity.Project](w.Project)
		if e != nil {
			return oc.BusinessFileRef{}, invalid()
		}
	}
	r, err := oc.NewBusinessFileRef(d)
	if err != nil {
		return oc.BusinessFileRef{}, err
	}
	if referenceStorage(r) != w {
		return oc.BusinessFileRef{}, invalid()
	}
	return r, nil
}

type sourceWire struct {
	Reference     referenceWire       `json:"reference"`
	OwnerKind     oc.OwnerKind        `json:"owner_kind"`
	Owner         string              `json:"owner"`
	Project       string              `json:"project"`
	Object        oc.ObjectID         `json:"object"`
	Media         string              `json:"media"`
	Size          foundation.Progress `json:"size"`
	SHA           foundation.Digest   `json:"sha"`
	ObjectVersion foundation.Version  `json:"object_version"`
	Created       foundation.Instant  `json:"created"`
	Revision      foundation.Version  `json:"revision"`
}

func sourceStorage(s oc.ResolvedSource) sourceWire {
	d := s.Details()
	o := d.Owner.Details()
	m := d.Meta
	return sourceWire{referenceStorage(d.Reference), o.Kind, o.ID, o.ProjectID, m.ID, m.MediaType, m.ByteSize, m.SHA256, m.Version, m.CreatedAt, d.Revision}
}
func (w sourceWire) value() (oc.ResolvedSource, error) {
	r, err := w.Reference.value()
	if err != nil {
		return oc.ResolvedSource{}, err
	}
	o, err := oc.NewObjectOwner(w.OwnerKind, w.Owner, w.Project)
	if err != nil {
		return oc.ResolvedSource{}, err
	}
	return oc.NewResolvedSource(oc.ResolvedSourceDetails{Reference: r, Owner: o, Meta: oc.ObjectMeta{ID: w.Object, Scope: o.Scope(), MediaType: w.Media, ByteSize: w.Size, SHA256: w.SHA, State: oc.Available, Version: w.ObjectVersion, CreatedAt: w.Created}, Revision: w.Revision})
}
func encodeSource(s oc.ResolvedSource) ([]byte, error) {
	if s.Validate() != nil {
		return nil, invalid()
	}
	return json.Marshal(sourceStorage(s))
}
func decodeSource(raw []byte) (oc.ResolvedSource, error) {
	var w sourceWire
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&w) != nil {
		return oc.ResolvedSource{}, unavailable(nil)
	}
	s, err := w.value()
	if err != nil {
		return oc.ResolvedSource{}, unavailable(err)
	}
	return s, nil
}
func sameSource(a, b oc.ResolvedSource) bool {
	return a.Validate() == nil && b.Validate() == nil && sourceStorage(a) == sourceStorage(b)
}
