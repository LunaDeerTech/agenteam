package object

import (
	"context"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

// sourceRow runs only after full access-plan and real business-source
// validation. A newly acquired source lease is never proof of read permission.
func (s *Service) sourceRow(ctx context.Context, tx foundation.Tx, actor identity.Actor, source oc.ResolvedSource) (objectRow, error) {
	d := source.Details()
	var obj objectRow
	var err error
	if d.Reference.Details().Kind != oc.UploadedObject {
		obj, err = s.readRow(ctx, tx, actor, d.Owner, d.Meta.ID)
	} else {
		grant, err := s.authorize(ctx, tx, actor, d.Owner, identity.Read)
		if err != nil {
			return objectRow{}, err
		}
		if err = s.gate(ctx, tx, actor, d.Owner, identity.Read); err != nil {
			return objectRow{}, err
		}
		e, err := executor(s, tx)
		if err != nil {
			return objectRow{}, err
		}
		receipt := d.Reference.Details().Receipt.Details()
		u, found, err := loadUpload(ctx, e, receipt.UploadID)
		if err != nil {
			return objectRow{}, err
		}
		if !found {
			return objectRow{}, failure(foundation.NotFound, nil)
		}
		if err = checkOriginal(u, actor, d.Owner); err != nil {
			return objectRow{}, err
		}
		if u.receipt != receipt.ID || u.object != d.Meta.ID || !receipt.Owner.Equal(d.Owner) || u.creation != receipt.CreationCause || grant.Details().CreationCause != u.creation {
			return objectRow{}, failure(foundation.Forbidden, nil)
		}
		if u.disposition != "reserved" || u.state != "committed" {
			return objectRow{}, failure(foundation.InvalidState, nil)
		}
		obj, found, err = loadObject(ctx, e, d.Meta.ID)
		if err != nil {
			return objectRow{}, err
		}
		if !found || !objectPartition(obj, d.Owner) {
			return objectRow{}, failure(foundation.Forbidden, nil)
		}
		if obj.cleaning || obj.meta.State != oc.Available {
			return objectRow{}, deleted(false)
		}
		var reserved bool
		err = e.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_object.object_references WHERE object_id=$1 AND upload_id=$2 AND owner_kind=$3 AND owner_id=$4 AND partition_id=$5 AND kind='reserved')`, d.Meta.ID.String(), u.id.String(), string(d.Owner.Details().Kind), d.Owner.Details().ID, d.Owner.Partition()).Scan(&reserved)
		if err != nil {
			return objectRow{}, unavailable(err)
		}
		if !reserved {
			return objectRow{}, failure(foundation.Forbidden, nil)
		}
	}
	if err != nil {
		return objectRow{}, err
	}
	m := obj.meta
	if m.ID != d.Meta.ID || !m.Scope.Equal(d.Meta.Scope) || m.MediaType != d.Meta.MediaType || m.ByteSize != d.Meta.ByteSize || m.SHA256 != d.Meta.SHA256 || m.Version != d.Meta.Version || m.State != d.Meta.State || !m.CreatedAt.Time().Equal(d.Meta.CreatedAt.Time()) {
		return objectRow{}, failure(foundation.VersionConflict, nil)
	}
	return obj, nil
}
