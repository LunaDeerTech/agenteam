package runtimeinfohttp

import (
	"encoding/json"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/runtimeinfo"
)

const successBytes = 16 << 10

type centralDTO struct {
	Version    *string `json:"version"`
	SafeReason string  `json:"safe_reason"`
}

type databaseSuccessDTO struct {
	CheckedAt         foundation.Instant `json:"checked_at"`
	ReceivedAt        foundation.Instant `json:"received_at"`
	PostgreSQLVersion string             `json:"postgresql_version"`
	PGVectorVersion   string             `json:"pgvector_version"`
}

type databaseDTO struct {
	Status      runtimeinfo.Status `json:"status"`
	SafeReason  *string            `json:"safe_reason"`
	LastSuccess databaseSuccessDTO `json:"last_success"`
}

type objectStorageDTO struct {
	Backend               string             `json:"backend"`
	Assessment            string             `json:"assessment"`
	Status                runtimeinfo.Status `json:"status"`
	SafeReason            *string            `json:"safe_reason"`
	LastSuccessReceivedAt foundation.Instant `json:"last_success_received_at"`
	Details               string             `json:"details"`
}

type readinessDTO struct {
	Ready      bool            `json:"ready"`
	SafeReason foundation.Code `json:"safe_reason"`
}

type snapshotDTO struct {
	ObservedAt    foundation.Instant `json:"observed_at"`
	Central       centralDTO         `json:"central"`
	Database      databaseDTO        `json:"database"`
	ObjectStorage objectStorageDTO   `json:"object_storage"`
	Readiness     readinessDTO       `json:"readiness"`
}

func projectSnapshot(value runtimeinfo.Snapshot) (snapshotDTO, error) {
	// Validate again at the wire boundary, including the cross-domain readiness
	// implication; an invalid reader value must never become a partial 200.
	if err := value.Validate(); err != nil {
		return snapshotDTO{}, fault(foundation.DependencyUnavailable)
	}
	last := value.Database.LastSuccess
	return snapshotDTO{
		ObservedAt: value.ObservedAt,
		Central:    centralDTO{Version: nil, SafeReason: "build_version_not_recorded"},
		Database: databaseDTO{
			Status: value.Database.Status, SafeReason: statusReason(value.Database.Status),
			LastSuccess: databaseSuccessDTO{last.CheckedAt, last.ReceivedAt, last.PostgreSQLVersion, last.PGVectorVersion},
		},
		ObjectStorage: objectStorageDTO{
			Backend: "minio", Assessment: "object_storage_aggregate",
			Status: value.ObjectStorage.Status, SafeReason: statusReason(value.ObjectStorage.Status),
			LastSuccessReceivedAt: value.ObjectStorage.LastSuccessReceivedAt, Details: "not_reported",
		},
		Readiness: readinessDTO{Ready: false, SafeReason: value.Readiness},
	}, nil
}

func statusReason(status runtimeinfo.Status) *string {
	var reason string
	switch status {
	case runtimeinfo.Unavailable:
		reason = "check_unavailable"
	case runtimeinfo.Stale:
		reason = "sample_stale"
	default:
		return nil
	}
	return &reason
}

func encodeSnapshot(value runtimeinfo.Snapshot) ([]byte, error) {
	dto, err := projectSnapshot(value)
	if err != nil {
		return nil, err
	}
	return encode(dto)
}

func encode(value any) ([]byte, error) {
	body, err := json.Marshal(value)
	if err != nil || len(body) > successBytes {
		return nil, fault(foundation.DependencyUnavailable)
	}
	return body, nil
}
