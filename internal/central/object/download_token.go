package object

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

const downloadTokenLimit = 8 << 10

type downloadClaims struct {
	grant   oc.DownloadGrantID
	user    identity.UserID
	target  oc.DownloadTarget
	mode    oc.DownloadMode
	expires foundation.Instant
}
type downloadHeader struct {
	Version int    `json:"v"`
	Kid     string `json:"kid"`
}
type downloadRefWire struct {
	Kind      oc.SourceKind `json:"kind"`
	Project   string        `json:"project_id,omitempty"`
	Artifact  string        `json:"artifact_id,omitempty"`
	File      string        `json:"file_id,omitempty"`
	Document  string        `json:"document_id,omitempty"`
	Execution string        `json:"execution_id,omitempty"`
	Payload   string        `json:"payload_id,omitempty"`
	Revision  string        `json:"revision,omitempty"`
	Receipt   string        `json:"receipt_id,omitempty"`
	Upload    string        `json:"upload_id,omitempty"`
	Object    string        `json:"object_id,omitempty"`
	OwnerKind oc.OwnerKind  `json:"owner_kind,omitempty"`
	Owner     string        `json:"owner_id,omitempty"`
	Cause     string        `json:"creation_cause,omitempty"`
}
type downloadWire struct {
	Grant         oc.DownloadGrantID  `json:"grant_id"`
	User          identity.UserID     `json:"user_id"`
	Reference     downloadRefWire     `json:"business_ref"`
	Revision      foundation.Version  `json:"revision"`
	OwnerKind     oc.OwnerKind        `json:"owner_kind"`
	Owner         string              `json:"owner_id"`
	Project       string              `json:"project_id,omitempty"`
	Object        oc.ObjectID         `json:"object_id"`
	Length        foundation.Progress `json:"byte_size"`
	SHA256        foundation.Digest   `json:"sha256"`
	ObjectVersion foundation.Version  `json:"object_version"`
	Created       foundation.Instant  `json:"object_created_at"`
	Method        string              `json:"method"`
	Mode          oc.DownloadMode     `json:"mode"`
	Filename      string              `json:"filename"`
	MediaType     string              `json:"media_type"`
	Expires       foundation.Instant  `json:"expires_at"`
}

func encodeDownloadRef(ref oc.BusinessFileRef) downloadRefWire {
	r := ref.Details()
	w := downloadRefWire{Kind: r.Kind, Artifact: r.ArtifactID, File: r.FileID, Document: r.DocumentID, Execution: r.ExecutionID, Payload: r.PayloadID}
	if r.ProjectID.Validate() == nil {
		w.Project = r.ProjectID.String()
	}
	if r.Revision != 0 {
		w.Revision = r.Revision.String()
	}
	if r.Kind == oc.UploadedObject {
		p := r.Receipt.Details()
		o := p.Owner.Details()
		w.Receipt = p.ID.String()
		w.Upload = p.UploadID.String()
		w.Object = p.ObjectID.String()
		w.OwnerKind = o.Kind
		w.Owner = o.ID
		w.Project = o.ProjectID
		w.Cause = p.CreationCause
	}
	return w
}
func decodeDownloadRef(w downloadRefWire) (oc.BusinessFileRef, error) {
	r := oc.BusinessFileDetails{Kind: w.Kind, ArtifactID: w.Artifact, FileID: w.File, DocumentID: w.Document, ExecutionID: w.Execution, PayloadID: w.Payload}
	var err error
	if w.Kind == oc.UploadedObject {
		p := oc.ReceiptDetails{CreationCause: w.Cause}
		if p.ID, err = foundation.ParseID[oc.Receipt](w.Receipt); err != nil {
			return oc.BusinessFileRef{}, invalid()
		}
		if p.UploadID, err = foundation.ParseID[oc.Upload](w.Upload); err != nil {
			return oc.BusinessFileRef{}, invalid()
		}
		if p.ObjectID, err = foundation.ParseID[oc.StoredObject](w.Object); err != nil {
			return oc.BusinessFileRef{}, invalid()
		}
		if p.Owner, err = oc.NewObjectOwner(w.OwnerKind, w.Owner, w.Project); err != nil {
			return oc.BusinessFileRef{}, invalid()
		}
		if r.Receipt, err = oc.NewUploadReceipt(p); err != nil {
			return oc.BusinessFileRef{}, invalid()
		}
	} else {
		if w.Receipt != "" || w.Upload != "" || w.Object != "" || w.OwnerKind != "" || w.Owner != "" || w.Cause != "" {
			return oc.BusinessFileRef{}, invalid()
		}
		if r.ProjectID, err = foundation.ParseID[identity.Project](w.Project); err != nil {
			return oc.BusinessFileRef{}, invalid()
		}
	}
	if w.Revision != "" {
		if r.Revision, err = foundation.ParseVersion(w.Revision); err != nil {
			return oc.BusinessFileRef{}, invalid()
		}
	}
	return oc.NewBusinessFileRef(r)
}
func (c downloadClaims) wire() (downloadWire, error) {
	if c.grant.Validate() != nil || c.user.Validate() != nil || c.target.Validate() != nil || !c.mode.Valid() || c.expires.Validate() != nil {
		return downloadWire{}, invalid()
	}
	t := c.target.Details()
	s := t.Source.Details()
	o := s.Owner.Details()
	m := s.Meta
	return downloadWire{Grant: c.grant, User: c.user, Reference: encodeDownloadRef(s.Reference), Revision: s.Revision, OwnerKind: o.Kind, Owner: o.ID, Project: o.ProjectID, Object: m.ID, Length: m.ByteSize, SHA256: m.SHA256, ObjectVersion: m.Version, Created: m.CreatedAt, Method: "GET", Mode: c.mode, Filename: t.Filename, MediaType: m.MediaType, Expires: c.expires}, nil
}
func (w downloadWire) claims() (downloadClaims, error) {
	if w.Method != "GET" {
		return downloadClaims{}, invalid()
	}
	ref, err := decodeDownloadRef(w.Reference)
	if err != nil {
		return downloadClaims{}, err
	}
	owner, err := oc.NewObjectOwner(w.OwnerKind, w.Owner, w.Project)
	if err != nil {
		return downloadClaims{}, err
	}
	meta := oc.ObjectMeta{ID: w.Object, Scope: owner.Scope(), MediaType: w.MediaType, ByteSize: w.Length, SHA256: w.SHA256, State: oc.Available, Version: w.ObjectVersion, CreatedAt: w.Created}
	source, err := oc.NewResolvedSource(oc.ResolvedSourceDetails{Reference: ref, Owner: owner, Meta: meta, Revision: w.Revision})
	if err != nil {
		return downloadClaims{}, err
	}
	target, err := oc.NewDownloadTarget(oc.DownloadTargetDetails{Source: source, Filename: w.Filename})
	if err != nil {
		return downloadClaims{}, err
	}
	c := downloadClaims{w.Grant, w.User, target, w.Mode, w.Expires}
	if _, err = c.wire(); err != nil {
		return downloadClaims{}, err
	}
	return c, nil
}
func canonicalDownload(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, invalid()
	}
	b, err = cursor.CanonicalJSON(b)
	if err != nil {
		return nil, invalid()
	}
	return b, nil
}
func decodeCanonicalDownload(raw []byte, v any) error {
	canonical, err := cursor.CanonicalJSON(raw)
	if err != nil || !bytes.Equal(canonical, raw) {
		return invalid()
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil {
		return invalid()
	}
	// Exact re-encoding rejects nulls, wrong field casing, omitted required
	// fields and alternative scalar encodings as well as unknown fields.
	encoded, err := canonicalDownload(v)
	if err != nil || !bytes.Equal(encoded, raw) {
		return invalid()
	}
	return nil
}
func (k DownloadKeyring) signDownload(c downloadClaims) (string, error) {
	if k.Validate() != nil {
		return "", invalid()
	}
	w, err := c.wire()
	if err != nil {
		return "", err
	}
	h, err := canonicalDownload(downloadHeader{1, k.data().current})
	if err != nil {
		return "", err
	}
	p, err := canonicalDownload(w)
	if err != nil {
		return "", err
	}
	hp := base64.RawURLEncoding.EncodeToString(h) + "." + base64.RawURLEncoding.EncodeToString(p)
	key, _ := k.key(k.data().current)
	mac := hmac.New(sha256.New, key[:])
	mac.Write([]byte("agenteam.object.download.v1\x00"))
	mac.Write([]byte(hp))
	token := hp + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if len(token) > downloadTokenLimit {
		return "", invalid()
	}
	return token, nil
}
func (k DownloadKeyring) verifyDownload(token string) (downloadClaims, error) {
	bad := func() (downloadClaims, error) { return downloadClaims{}, failure(foundation.Forbidden, nil) }
	if k.Validate() != nil || len(token) == 0 || len(token) > downloadTokenLimit {
		return bad()
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return bad()
	}
	var decoded [3][]byte
	for i, p := range parts {
		b, e := base64.RawURLEncoding.Strict().DecodeString(p)
		if e != nil || base64.RawURLEncoding.EncodeToString(b) != p {
			return bad()
		}
		decoded[i] = b
	}
	var h downloadHeader
	if decodeCanonicalDownload(decoded[0], &h) != nil || h.Version != 1 || !downloadKid(h.Kid) {
		return bad()
	}
	key, ok := k.key(h.Kid)
	if !ok {
		return bad()
	}
	mac := hmac.New(sha256.New, key[:])
	mac.Write([]byte("agenteam.object.download.v1\x00"))
	mac.Write([]byte(parts[0] + "." + parts[1]))
	if !hmac.Equal(mac.Sum(nil), decoded[2]) {
		return bad()
	}
	var w downloadWire
	if decodeCanonicalDownload(decoded[1], &w) != nil {
		return bad()
	}
	c, err := w.claims()
	if err != nil {
		return bad()
	}
	return c, nil
}
