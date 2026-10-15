package meeting

import (
	"context"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
)

// LookupSend confirms only the original semantic request under current Owner
// read authority. A missing receipt never authorizes replaying an unknown write.
func (s *Service) LookupSend(ctx context.Context, a i.Actor, r c.SendRequest) (c.Receipt, bool, error) {
	if err := human(a); err != nil {
		return c.Receipt{}, false, err
	}
	if err := r.Validate(); err != nil {
		return c.Receipt{}, false, err
	}
	d, err := commandDigest(a, "send", r)
	if err != nil {
		return c.Receipt{}, false, err
	}
	return s.lookup(ctx, a, r.ProjectID, r.Meta.IdempotencyKey, d)
}
