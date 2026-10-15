package memory

import (
	"bytes"
	"encoding/json"
	"io"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

// This is the complete selectable part of Memory's original Resolve request.
// Actor, Consumer and lease owner are reconstructed from the immutable call
// tuple, never from a new selector or the current embedding configuration.
type modelResolutionSource struct {
	Schema     f.Version           `json:"schema"`
	Source     mc.ResolutionSource `json:"source"`
	Selection  *mc.SelectionRef    `json:"selection,omitempty"`
	Snapshot   *mc.SnapshotID      `json:"serving_snapshot_id,omitempty"`
	Generation string              `json:"serving_generation_id,omitempty"`
}

func captureModelResolution(r mc.ResolveRequest) ([]byte, f.Digest, error) {
	if r.Validate() != nil || r.Consumer.Kind != mc.MemoryConsumer || r.ModelRef != nil || r.ReasoningEffort != "" {
		return nil, "", fault(f.Forbidden)
	}
	copy := r.Clone()
	value := modelResolutionSource{1, copy.Source, copy.Selection, copy.ServingSnapshotID, copy.ServingGenerationID}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, "", portError(err)
	}
	binding, err := mc.ResolveBinding(r)
	return raw, binding, portError(err)
}

func restoreModelResolution(raw []byte, binding f.Digest, original mc.ResolveRequest) (mc.ResolveRequest, error) {
	if len(raw) < 2 || len(raw) > 4096 || binding.Validate() != nil {
		return mc.ResolveRequest{}, fault(f.ResourceBusy)
	}
	var source modelResolutionSource
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&source) != nil || decoder.Decode(new(any)) != io.EOF || source.Schema != 1 {
		return mc.ResolveRequest{}, fault(f.DependencyUnavailable)
	}
	original.Source, original.Selection, original.ServingSnapshotID, original.ServingGenerationID = source.Source, source.Selection, source.Snapshot, source.Generation
	raw2, actual, err := captureModelResolution(original)
	if err != nil || actual != binding {
		return mc.ResolveRequest{}, fault(f.Forbidden)
	}
	// JSONB may reorder keys; compare parsed values, not storage byte order.
	var left, right any
	if json.Unmarshal(raw, &left) != nil || json.Unmarshal(raw2, &right) != nil {
		return mc.ResolveRequest{}, fault(f.DependencyUnavailable)
	}
	canonicalLeft, _ := json.Marshal(left)
	canonicalRight, _ := json.Marshal(right)
	if !bytes.Equal(canonicalLeft, canonicalRight) {
		return mc.ResolveRequest{}, fault(f.Forbidden)
	}
	return original.Clone(), nil
}
