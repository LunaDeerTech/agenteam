package model

import (
	"context"
	"encoding/json"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

type meetingSummaryRecord struct {
	ID        string
	Version   f.Version
	Model     string
	UpdatedAt f.Instant
}

func (r *meetingSummaryRecord) view() (mc.MeetingSummarySelection, error) {
	if r == nil {
		return mc.MeetingSummarySelection{}, fault(f.DependencyUnbound)
	}
	v := mc.MeetingSummarySelection{ID: r.ID, Version: r.Version}
	if r.Model != "" {
		m, e := f.ParseID[mc.Model](r.Model)
		if e != nil || m.String() != r.Model {
			return mc.MeetingSummarySelection{}, unavailable(e)
		}
		v.Model = &m
	}
	if v.Validate() != nil || r.UpdatedAt.Validate() != nil || r.UpdatedAt.Time().IsZero() {
		return mc.MeetingSummarySelection{}, unavailable(nil)
	}
	return v, nil
}

// This loader distinguishes a missing table, a missing technical singleton,
// and an initialized nullable choice. LIMIT 2 detects a corrupted singleton.
func loadMeetingSummary(ctx context.Context, x postgres.SQLExecutor) (*meetingSummaryRecord, error) {
	var raw []byte
	e := x.QueryRow(ctx, `SELECT coalesce(jsonb_agg(jsonb_build_object('ID',id::text,'Version',version::text,'Model',coalesce(model_id::text,''),'UpdatedAt',to_char(updated_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),'Singleton',singleton,'Platform',(SELECT id::text FROM agenteam_model.platform_selection WHERE singleton)) ORDER BY id),'[]'::jsonb) FROM (SELECT * FROM agenteam_model.meeting_summary_selection ORDER BY id LIMIT 2) summary`).Scan(&raw)
	if e != nil {
		return nil, unavailable(e)
	}
	var records []struct {
		meetingSummaryRecord
		Singleton bool
		Platform  *string
	}
	if json.Unmarshal(raw, &records) != nil || records == nil || len(records) > 1 {
		return nil, unavailable(nil)
	}
	var out *meetingSummaryRecord
	for _, v := range records {
		if !v.Singleton || v.Platform == nil || *v.Platform == v.ID {
			return nil, unavailable(nil)
		}
		if _, e = v.view(); e != nil {
			return nil, e
		}
		r := v.meetingSummaryRecord
		out = &r
	}
	return out, nil
}
func meetingSummaryReferences(r *meetingSummaryRecord) []referenceRecord {
	refs := []referenceRecord{}
	if r != nil && r.Model != "" {
		refs = append(refs, referenceRecord{Kind: "platform_selector", Owner: r.ID, Role: "meeting_summary", Model: r.Model, Version: r.Version})
	}
	return refs
}

// Include every Summary-role edge, even when its owner is unknown; include all
// roles of the canonical owner, so an incorrect extra role cannot be hidden.
func meetingSummaryConsistent(ctx context.Context, x postgres.SQLExecutor, r *meetingSummaryRecord) error {
	owner := ""
	if r != nil {
		owner = r.ID
	}
	var raw []byte
	e := x.QueryRow(ctx, `SELECT coalesce(jsonb_agg(jsonb_build_object('Kind',owner_kind,'Owner',owner_id::text,'Role',role,'Project',coalesce(project_id::text,''),'Model',model_id::text,'Version',owner_version::text,'Effort',reasoning_effort) ORDER BY owner_id,role),'[]'::jsonb) FROM (SELECT owner_kind,owner_id,role,project_id,model_id,owner_version,reasoning_effort FROM agenteam_model.references WHERE owner_kind='platform_selector' AND (role='meeting_summary' OR owner_id::text=$1) ORDER BY owner_id,role LIMIT 2) summary_references`, owner).Scan(&raw)
	if e != nil {
		return unavailable(e)
	}
	var refs []referenceRecord
	if json.Unmarshal(raw, &refs) != nil || refs == nil || len(refs) > 1 || !sameValue(refs, meetingSummaryReferences(r)) {
		return unavailable(nil)
	}
	return nil
}
func loadMeetingSummaryState(ctx context.Context, x postgres.SQLExecutor) (*meetingSummaryRecord, error) {
	r, e := loadMeetingSummary(ctx, x)
	if e != nil {
		return nil, e
	}
	if e = meetingSummaryConsistent(ctx, x, r); e != nil {
		return nil, e
	}
	return r, nil
}
func applyMeetingSummary(ctx context.Context, x postgres.SQLExecutor, r *meetingSummaryRecord) error {
	if _, e := r.view(); e != nil {
		return e
	}
	tag, e := x.Exec(ctx, `UPDATE agenteam_model.meeting_summary_selection SET version=$2,model_id=$3,updated_at=$4 WHERE id=$1`, r.ID, int64(r.Version), null(r.Model), r.UpdatedAt.Time())
	if e != nil {
		return unavailable(e)
	}
	if tag.RowsAffected() != 1 {
		return unavailable(nil)
	}
	if _, e = x.Exec(ctx, `DELETE FROM agenteam_model.references WHERE owner_kind='platform_selector' AND owner_id=$1`, r.ID); e != nil {
		return unavailable(e)
	}
	for _, ref := range meetingSummaryReferences(r) {
		if _, e = x.Exec(ctx, `INSERT INTO agenteam_model.references(owner_kind,owner_id,role,model_id,owner_version) VALUES('platform_selector',$1,'meeting_summary',$2,$3)`, ref.Owner, ref.Model, int64(ref.Version)); e != nil {
			return unavailable(e)
		}
	}
	return nil
}
