package meeting

import (
	"bytes"
	"context"
	"encoding/json"
	"io"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// Only the source is serialized. Actor, consumer and lease owner are rebuilt
// from the immutable domain call, then checked against the full original
// ResolveBinding. In particular Selection.Version=nil stays nil.
type summaryResolveSource struct {
	Source     mc.ResolutionSource `json:"source"`
	Selection  *mc.SelectionRef    `json:"selection,omitempty"`
	Snapshot   *mc.SnapshotID      `json:"snapshot,omitempty"`
	Generation string              `json:"generation,omitempty"`
}

func summaryResolveRequest(actor i.Actor, row summaryCallRecord, source summaryResolveSource) (mc.ResolveRequest, error) {
	owner, err := sc.NewCredentialLeaseOwner(sc.ModelCallOwner, row.call.String())
	if err != nil {
		return mc.ResolveRequest{}, err
	}
	request := mc.ResolveRequest{Actor: actor,
		Consumer: mc.Consumer{Kind: mc.MeetingConsumer, ProjectID: row.input.ProjectID, MeetingID: row.input.MeetingID.String(), OperationID: row.input.GenerationID.String(), Purpose: row.purpose},
		Purpose:  row.purpose, LeaseOwner: owner, Source: source.Source, Selection: source.Selection,
		ServingSnapshotID: source.Snapshot, ServingGenerationID: source.Generation}
	if request.Validate() != nil {
		return mc.ResolveRequest{}, fault(f.ConfirmationStale)
	}
	if source.Source == mc.CurrentSelectionSource && (source.Selection.Kind != "platform" || source.Selection.Selector != mc.MeetingSummarySelector) || source.Source == mc.ServingSnapshotSource && source.Generation != row.input.GenerationID.String() {
		return mc.ResolveRequest{}, fault(f.ConfirmationStale)
	}
	return request.Clone(), nil
}

func decodeSummaryResolve(actor i.Actor, row summaryCallRecord) (mc.ResolveRequest, error) {
	if len(row.resolveSource) == 0 || row.resolveBinding == nil {
		return mc.ResolveRequest{}, unknownSummary()
	}
	if len(row.resolveSource) > 8192 {
		return mc.ResolveRequest{}, fault(f.InternalError)
	}
	var source summaryResolveSource
	decoder := json.NewDecoder(bytes.NewReader(row.resolveSource))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&source) != nil || decoder.Decode(new(any)) != io.EOF {
		return mc.ResolveRequest{}, fault(f.InternalError)
	}
	request, err := summaryResolveRequest(actor, row, source)
	if err != nil {
		return request, err
	}
	binding, err := mc.ResolveBinding(request)
	if err != nil || binding.String() != *row.resolveBinding {
		return mc.ResolveRequest{}, fault(f.ConfirmationStale)
	}
	return request, nil
}

func (m *SummaryModel) captureSummaryResolve(ctx context.Context, x postgres.SQLExecutor, row *summaryCallRecord) error {
	var snapshot *string
	var released bool
	if err := x.QueryRow(ctx, `SELECT snapshot_id,released_at IS NOT NULL FROM agenteam_meeting.summary_generations WHERE id=$1 AND meeting_id=$2 AND status='generating'`, row.input.GenerationID.String(), row.input.MeetingID.String()).Scan(&snapshot, &released); err != nil {
		return portError(err)
	}
	if released || snapshot == nil && row.attempt != 1 {
		return fault(f.DependencyUnavailable)
	}
	source := summaryResolveSource{Source: mc.CurrentSelectionSource, Selection: &mc.SelectionRef{Kind: "platform", Selector: mc.MeetingSummarySelector}}
	if snapshot != nil {
		id, err := f.ParseID[mc.Snapshot](*snapshot)
		if err != nil {
			return fault(f.InternalError)
		}
		source = summaryResolveSource{Source: mc.ServingSnapshotSource, Snapshot: &id, Generation: row.input.GenerationID.String()}
	}
	scope, err := i.InProject(row.input.ProjectID)
	if err != nil {
		return err
	}
	actor, err := m.deps.Actor.Actor(row.input.GenerationID.String(), scope)
	if err != nil {
		return err
	}
	request, err := summaryResolveRequest(actor, *row, source)
	if err != nil {
		return err
	}
	binding, err := mc.ResolveBinding(request)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(source)
	if err != nil {
		return portError(err)
	}
	value := binding.String()
	row.resolveSource, row.resolveBinding = raw, &value
	return nil
}
