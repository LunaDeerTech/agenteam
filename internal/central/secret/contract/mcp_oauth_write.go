package contract

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strconv"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// MCPOAuthOwnerKind is separate from Account's closed material owner set.
type MCPOAuthOwnerKind string

const (
	MCPOAuthFlow  MCPOAuthOwnerKind = "oauth_flow"
	MCPOAuthToken MCPOAuthOwnerKind = "oauth_token"
)

type MCPOAuthWriteFields struct {
	Actor           id.Actor
	Identity        f.CommandIdentity
	ProjectID       id.ProjectID
	ConnectionID    string
	ConfigRevision  f.Version
	Kind            MutationKind
	Ref             CredentialRef
	ExpectedVersion f.Version
	OwnerKind       MCPOAuthOwnerKind
	OwnerID         string
	Value           SecretMaterial
}
type mcpOAuthWriteData struct {
	fields      MCPOAuthWriteFields
	valueDigest string
}

// MCPOAuthWriteRequest identifies an intent; construction grants no authority.
type MCPOAuthWriteRequest struct{ data func() mcpOAuthWriteData }

func NewMCPOAuthWriteRequest(v MCPOAuthWriteFields) (MCPOAuthWriteRequest, error) {
	a := v.Actor.Details()
	if v.Actor.Validate() != nil || a.Kind != id.Service || a.ServiceName != id.SecretService || v.ProjectID.Validate() != nil || a.ProjectID != v.ProjectID.String() || !validID(a.CauseRef) || !validID(v.ConnectionID) || !validID(v.OwnerID) || v.ConfigRevision.Validate() != nil || v.Identity.Validate() != nil || v.Identity.Namespace() != "secret.mcp" || v.Identity.Command() != string(v.Kind) || !slices.Equal(v.Identity.OwnerIDs(), []string{v.ProjectID.String(), v.ConnectionID, v.OwnerID}) {
		return MCPOAuthWriteRequest{}, bad()
	}
	if v.OwnerKind != MCPOAuthFlow && v.OwnerKind != MCPOAuthToken || v.OwnerKind == MCPOAuthToken && v.OwnerID != v.ConnectionID {
		return MCPOAuthWriteRequest{}, bad()
	}
	if v.Kind != Create && v.Kind != Update && v.Kind != Delete {
		return MCPOAuthWriteRequest{}, bad()
	}
	if v.Kind == Create {
		if v.Ref.Validate() == nil || v.ExpectedVersion != 0 {
			return MCPOAuthWriteRequest{}, bad()
		}
	} else if v.Ref.Validate() != nil || v.Ref.Details().Scope.Details().Kind != id.ProjectScope || v.Ref.Details().Scope.Details().ProjectID != v.ProjectID.String() || v.ExpectedVersion.Validate() != nil {
		return MCPOAuthWriteRequest{}, bad()
	}
	digest := ""
	if v.Kind != Delete {
		if err := v.Value.Use(func(b []byte) error { h := sha256.Sum256(b); digest = hex.EncodeToString(h[:]); return nil }); err != nil {
			return MCPOAuthWriteRequest{}, bad()
		}
	} else {
		v.Value = SecretMaterial{}
	}
	d := mcpOAuthWriteData{v, digest}
	return MCPOAuthWriteRequest{func() mcpOAuthWriteData { return d }}, nil
}
func (r MCPOAuthWriteRequest) Validate() error {
	if r.data == nil {
		return bad()
	}
	return nil
}
func (r MCPOAuthWriteRequest) Fields() MCPOAuthWriteFields {
	if r.data == nil {
		return MCPOAuthWriteFields{}
	}
	return r.data().fields
}
func (r MCPOAuthWriteRequest) WithoutMaterial() MCPOAuthWriteRequest {
	if r.data == nil {
		return MCPOAuthWriteRequest{}
	}
	d := r.data()
	d.fields.Value = SecretMaterial{}
	return MCPOAuthWriteRequest{func() mcpOAuthWriteData { return d }}
}
func MCPOAuthWriteBinding(r MCPOAuthWriteRequest) (f.Digest, error) {
	if r.Validate() != nil {
		return "", bad()
	}
	d := r.data()
	v := d.fields
	ref := ""
	if v.Ref.Validate() == nil {
		ref = v.Ref.Details().ID.String()
	}
	b, e := json.Marshal(struct {
		Format                                             int
		Project, Connection, Revision, ActorCause, Command string
		Kind                                               MutationKind
		Ref, Expected                                      string
		OwnerKind                                          MCPOAuthOwnerKind
		OwnerID, ValueDigest                               string
	}{1, v.ProjectID.String(), v.ConnectionID, strconv.FormatInt(int64(v.ConfigRevision), 10), v.Actor.Details().CauseRef, v.Identity.Canonical(), v.Kind, ref, strconv.FormatInt(int64(v.ExpectedVersion), 10), v.OwnerKind, v.OwnerID, d.valueDigest})
	if e != nil {
		return "", bad()
	}
	defer clear(b)
	h := sha256.Sum256(b)
	return f.Digest("sha256:" + hex.EncodeToString(h[:])), nil
}
func (MCPOAuthWriteRequest) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "mcp_oauth_secret_write")
}
func (MCPOAuthWriteRequest) MarshalJSON() ([]byte, error) {
	return []byte(`"mcp_oauth_secret_write"`), nil
}
func (*MCPOAuthWriteRequest) UnmarshalJSON([]byte) error { return bad() }
func (MCPOAuthWriteRequest) LogValue() slog.Value        { return slog.StringValue("mcp_oauth_secret_write") }
func (MCPOAuthWriteFields) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "mcp_oauth_secret_write")
}
func (MCPOAuthWriteFields) MarshalJSON() ([]byte, error) {
	return []byte(`"mcp_oauth_secret_write"`), nil
}
func (MCPOAuthWriteFields) LogValue() slog.Value { return slog.StringValue("mcp_oauth_secret_write") }

// Each check requires the MCP owner's private original flow/refresh witness,
// exact durable binding and complete lock plan. A Service actor or DTO alone is
// never a grant. Observation may validate either before or after the original
// transaction, including cancellation; it must not authorize a fresh mutation.
type MCPOAuthWriteAuthority interface {
	DiscoverMCPOAuthWrite(context.Context, MCPOAuthWriteRequest) (WriteDependencies, error)
	ValidateMCPOAuthWriteInTx(context.Context, f.Tx, MCPOAuthWriteRequest, WriteDependencies) error
	ValidateMCPOAuthWriteObservationInTx(context.Context, f.Tx, MCPOAuthWriteRequest, WriteDependencies) error
}

// PreparedMCPOAuthWrite contains only sealed state. Implementations reject
// foreign handles, including caller implementations of this interface.
type PreparedMCPOAuthWrite interface {
	Ref() CredentialRef
	RequiredLocks() []f.LockRequest
}
type MCPOAuthWriteObservation struct {
	Found  bool
	Result MutationResult
}
type MCPOAuthWrites interface {
	DiscoverMCPOAuthWrite(context.Context, MCPOAuthWriteRequest) (WriteDependencies, error)
	PrepareMCPOAuthWrite(context.Context, MCPOAuthWriteRequest, WriteDependencies) (PreparedMCPOAuthWrite, error)
	ApplyMCPOAuthWriteInTx(context.Context, f.Tx, PreparedMCPOAuthWrite) (MCPOAuthWriteObservation, error)
	ObserveMCPOAuthWriteInTx(context.Context, f.Tx, PreparedMCPOAuthWrite) (MCPOAuthWriteObservation, error)
}
