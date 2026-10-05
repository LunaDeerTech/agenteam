package model

import (
	"context"
	"errors"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
)

type resolutionPreparation struct {
	ID, Identity, Project, OwnerKind, OwnerID string
	Semantic                                  f.Digest
	Request                                   resolutionRequestDTO
	SnapshotID                                mc.SnapshotID
	Phase                                     string
	Version                                   f.Version
	Draft                                     resolutionDraft
}

func loadResolutionPreparation(ctx context.Context, x postgres.SQLExecutor, identity string) (*resolutionPreparation, error) {
	r := &resolutionPreparation{}
	var request, draft []byte
	var snapshot string
	var created, updated time.Time
	var committed *time.Time
	err := x.QueryRow(ctx, `SELECT id::text,resolution_identity,project_id::text,owner_kind,owner_id::text,semantic_digest,request_data,snapshot_id::text,phase,plan_version,draft_plan,created_at,updated_at,committed_at FROM agenteam_model.resolution_preparations WHERE resolution_identity=$1`, identity).Scan(&r.ID, &r.Identity, &r.Project, &r.OwnerKind, &r.OwnerID, &r.Semantic, &request, &snapshot, &r.Phase, &r.Version, &draft, &created, &updated, &committed)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, unavailable(err)
	}
	r.SnapshotID, err = f.ParseID[mc.Snapshot](snapshot)
	if err != nil {
		return nil, unavailable(err)
	}
	if err = resolutionDecode(request, 16<<10, &r.Request); err != nil {
		return nil, err
	}
	if err = resolutionDecode(draft, 256<<10, &r.Draft); err != nil {
		return nil, err
	}
	_, idErr := f.ParseID[struct{}](r.ID)
	raw, _ := encoded(r.Request)
	unit, unitErr := resolutionUnitIdentity(r.Request.Consumer, r.Request.Purpose, r.Request.Owner)
	if idErr != nil || unitErr != nil || unit.Canonical() != r.Identity || r.Request.validate() != nil || r.Identity != identity || r.Version.Validate() != nil || r.Request.Format != 1 || r.Request.Consumer.Validate() != nil || r.Request.Purpose != r.Request.Consumer.Purpose || r.Request.Source != mc.CurrentSelectionSource || r.Request.Selection == nil || (mc.SelectionRequest{Consumer: r.Request.Consumer, ModelRef: r.Request.ModelRef, Selection: *r.Request.Selection}).Validate() != nil || r.Request.ReasoningEffort != "" || r.Request.Consumer.ProjectID.String() != r.Project || string(r.Request.Owner.Kind) != r.OwnerKind || r.Request.Owner.ID != r.OwnerID || hash(raw) != r.Semantic || r.Draft.validate() != nil || r.Draft.Snapshot.ID != r.SnapshotID || r.Draft.Snapshot.ProjectID.String() != r.Project || updated.Before(created) || r.Phase != "prepared" && r.Phase != "committed" || (r.Phase == "committed") != (committed != nil) || committed != nil && committed.Before(created) {
		return nil, unavailable(nil)
	}
	owner, e := sc.NewCredentialLeaseOwner(r.Request.Owner.Kind, r.Request.Owner.ID)
	if e != nil || owner.Details().Kind == sc.ExecutionOwner && (r.Request.Consumer.ExecutionID == nil || r.Request.Consumer.ExecutionID.String() != owner.Details().ID) {
		return nil, unavailable(e)
	}
	return r, nil
}
func canonicalResolutionLease(ctx context.Context, x postgres.SQLExecutor, r mc.ResolveRequest, d resolutionDraft) (string, error) {
	if d.Snapshot.CredentialID == "" {
		return "", nil
	}
	scope := "system"
	if d.Snapshot.CredentialProject != "" {
		scope = "project"
	}
	o := r.LeaseOwner.Details()
	rows, e := x.Query(ctx, `SELECT preparation_id::text,resolution_identity,snapshot_id::text,lease_id::text,project_id::text FROM agenteam_model.snapshot_bindings WHERE credential_id=$1 AND credential_scope=$2 AND credential_project_id IS NOT DISTINCT FROM $3::uuid AND owner_kind=$4 AND owner_id=$5`, d.Snapshot.CredentialID, scope, null(d.Snapshot.CredentialProject), string(o.Kind), o.ID)
	if e != nil {
		return "", unavailable(e)
	}
	type candidate struct{ preparation, identity, snapshot, lease, project string }
	candidates := []candidate{}
	for rows.Next() {
		var c candidate
		if e = rows.Scan(&c.preparation, &c.identity, &c.snapshot, &c.lease, &c.project); e != nil {
			rows.Close()
			return "", unavailable(e)
		}
		candidates = append(candidates, c)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return "", unavailable(e)
	}
	canonical := ""
	// Close the query before loading the complete canonical facts on the same Tx.
	// A matching owner/ref index entry alone is never lease authority.
	for _, c := range candidates {
		p, err := loadResolutionPreparation(ctx, x, c.identity)
		if err != nil {
			return "", err
		}
		if p == nil || p.Phase != "committed" || p.ID != c.preparation || p.SnapshotID.String() != c.snapshot || p.Project != c.project || p.Request.Owner != o || p.Draft.LeaseID != c.lease || p.Draft.Snapshot.CredentialID != d.Snapshot.CredentialID || p.Draft.Snapshot.CredentialProject != d.Snapshot.CredentialProject {
			return "", unavailable(nil)
		}
		if err = loadCommittedResolution(ctx, x, p); err != nil {
			return "", err
		}
		if p.Project != r.Consumer.ProjectID.String() || !resolutionEqual(p.Request.Consumer.AgentID, r.Consumer.AgentID) || !resolutionEqual(p.Request.Consumer.ExecutionID, r.Consumer.ExecutionID) {
			return "", fault(f.Forbidden)
		}
		if canonical != "" && canonical != c.lease {
			return "", unavailable(nil)
		}
		canonical = c.lease
	}
	return canonical, nil
}
func loadCommittedResolution(ctx context.Context, x postgres.SQLExecutor, p *resolutionPreparation) error {
	if p == nil || p.Phase != "committed" {
		return unavailable(nil)
	}
	var snap, request []byte
	var digest f.Digest
	var pv, mv f.Version
	var identity, project, kind, owner string
	var liveProvider, liveModel, credential, scope, credentialProject, lease *string
	err := x.QueryRow(ctx, `SELECT s.snapshot_data,s.snapshot_digest,s.provider_version,s.model_version,s.live_provider_id::text,s.live_model_id::text,b.resolution_identity,b.project_id::text,b.owner_kind,b.owner_id::text,b.request_data,b.credential_id::text,b.credential_scope,b.credential_project_id::text,b.lease_id::text FROM agenteam_model.snapshot_bindings b JOIN agenteam_model.snapshots s ON s.id=b.snapshot_id AND s.project_id=b.project_id WHERE b.preparation_id=$1 AND b.snapshot_id=$2`, p.ID, p.SnapshotID.String()).Scan(&snap, &digest, &pv, &mv, &liveProvider, &liveModel, &identity, &project, &kind, &owner, &request, &credential, &scope, &credentialProject, &lease)
	if err != nil {
		return unavailable(err)
	}
	var snapshot resolutionSnapshotDTO
	var req resolutionRequestDTO
	if resolutionDecode(snap, 128<<10, &snapshot) != nil || resolutionDecode(request, 16<<10, &req) != nil {
		return unavailable(nil)
	}
	raw, _ := encoded(snapshot)
	if hash(raw) != digest || !resolutionEqual(snapshot, p.Draft.Snapshot) || !resolutionEqual(req, p.Request) || pv != p.Draft.ProviderVersion || mv != p.Draft.ModelVersion || identity != p.Identity || project != p.Project || kind != p.OwnerKind || owner != p.OwnerID || liveProvider != nil && *liveProvider != snapshot.Identity.ProviderID.String() || liveModel != nil && *liveModel != snapshot.Identity.ModelID.String() {
		return unavailable(nil)
	}
	value := func(v *string) string {
		if v == nil {
			return ""
		}
		return *v
	}
	wantScope := ""
	if snapshot.CredentialID != "" {
		wantScope = "system"
		if snapshot.CredentialProject != "" {
			wantScope = "project"
		}
	}
	if value(credential) != snapshot.CredentialID || value(scope) != wantScope || value(credentialProject) != snapshot.CredentialProject || value(lease) != p.Draft.LeaseID {
		return unavailable(nil)
	}
	return nil
}
func saveResolutionPreparation(ctx context.Context, x postgres.SQLExecutor, c *resolutionCandidate, old *resolutionPreparation) error {
	if c.draft.validate() != nil {
		return unavailable(nil)
	}
	request, _ := encoded(resolutionRequest(c.request))
	draft, _ := encoded(c.draft)
	if len(request) > 16<<10 || len(draft) > 256<<10 {
		return fault(f.PayloadTooLarge)
	}
	at, e := dbNow(ctx, x)
	if e != nil {
		return e
	}
	if old == nil {
		_, e = x.Exec(ctx, `INSERT INTO agenteam_model.resolution_preparations(id,resolution_identity,project_id,owner_kind,owner_id,semantic_digest,request_data,snapshot_id,phase,plan_version,draft_plan,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'prepared',$9,$10,$11,$11)`, c.row.ID, c.identity.Canonical(), c.request.Consumer.ProjectID.String(), string(c.request.LeaseOwner.Details().Kind), c.request.LeaseOwner.Details().ID, resolutionSemantic(c.request), request, c.draft.Snapshot.ID.String(), c.version, draft, at.Time())
	} else {
		if old.Phase == "committed" {
			return nil
		}
		tag, err := x.Exec(ctx, `UPDATE agenteam_model.resolution_preparations SET plan_version=$2,draft_plan=$3,updated_at=$4 WHERE id=$1 AND phase='prepared' AND plan_version=$5`, old.ID, c.version, draft, at.Time(), old.Version)
		e = err
		if e == nil && tag.RowsAffected() != 1 {
			return fault(f.ResourceBusy)
		}
	}
	return portError(e)
}
func completeResolution(ctx context.Context, x postgres.SQLExecutor, c *resolutionCandidate) error {
	if c.row.Phase == "committed" {
		return loadCommittedResolution(ctx, x, c.row)
	}
	at, e := dbNow(ctx, x)
	if e != nil {
		return e
	}
	snapshot, _ := encoded(c.draft.Snapshot)
	request, _ := encoded(resolutionRequest(c.request))
	if len(snapshot) > 128<<10 {
		return fault(f.PayloadTooLarge)
	}
	_, e = x.Exec(ctx, `INSERT INTO agenteam_model.snapshots(id,project_id,live_provider_id,live_model_id,snapshot_data,snapshot_digest,provider_version,model_version,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, c.draft.Snapshot.ID.String(), c.request.Consumer.ProjectID.String(), c.draft.Snapshot.Identity.ProviderID.String(), c.draft.Snapshot.Identity.ModelID.String(), snapshot, hash(snapshot), c.draft.ProviderVersion, c.draft.ModelVersion, at.Time())
	if e != nil {
		return unavailable(e)
	}
	scope := ""
	if c.draft.Snapshot.CredentialID != "" {
		scope = "system"
		if c.draft.Snapshot.CredentialProject != "" {
			scope = "project"
		}
	}
	_, e = x.Exec(ctx, `INSERT INTO agenteam_model.snapshot_bindings(preparation_id,snapshot_id,resolution_identity,project_id,owner_kind,owner_id,request_data,credential_id,credential_scope,credential_project_id,lease_id,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, c.row.ID, c.draft.Snapshot.ID.String(), c.identity.Canonical(), c.request.Consumer.ProjectID.String(), string(c.request.LeaseOwner.Details().Kind), c.request.LeaseOwner.Details().ID, request, null(c.draft.Snapshot.CredentialID), null(scope), null(c.draft.Snapshot.CredentialProject), null(c.draft.LeaseID), at.Time())
	if e != nil {
		return unavailable(e)
	}
	tag, e := x.Exec(ctx, `UPDATE agenteam_model.resolution_preparations SET phase='committed',committed_at=$2,updated_at=$2 WHERE id=$1 AND phase='prepared' AND plan_version=$3`, c.row.ID, at.Time(), c.version)
	if e != nil {
		return unavailable(e)
	}
	if tag.RowsAffected() != 1 {
		return fault(f.ResourceBusy)
	}
	return nil
}
