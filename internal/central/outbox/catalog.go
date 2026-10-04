package outbox

import (
	"encoding/json"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"sort"
)

func normalizeDefinition(d oc.HandlerDefinition) (oc.HandlerDefinition, foundation.Digest, error) {
	if d.ID.Validate() != nil || !d.Effect.Valid() || !d.Ordering.Valid() || nilPort(d.Handler) || len(d.Subscriptions) == 0 || len(d.Subscriptions) > 128 {
		return d, "", invalid()
	}
	d.Subscriptions = append([]oc.Subscription(nil), d.Subscriptions...)
	seen := map[event.StableName]bool{}
	for i, s := range d.Subscriptions {
		if s.EventType.Validate() != nil || seen[s.EventType] || len(s.Versions) == 0 || len(s.Versions) > 128 {
			return d, "", invalid()
		}
		seen[s.EventType] = true
		s.Versions = append([]uint32(nil), s.Versions...)
		sort.Slice(s.Versions, func(i, j int) bool { return s.Versions[i] < s.Versions[j] })
		for j, v := range s.Versions {
			if v == 0 || j > 0 && v == s.Versions[j-1] {
				return d, "", invalid()
			}
		}
		d.Subscriptions[i] = s
	}
	sort.Slice(d.Subscriptions, func(i, j int) bool { return d.Subscriptions[i].EventType < d.Subscriptions[j].EventType })
	b, err := json.Marshal(struct {
		ID            event.StableName
		Effect        oc.HandlerEffect
		Ordering      oc.OrderingPolicy
		Subscriptions []oc.Subscription
	}{d.ID, d.Effect, d.Ordering, d.Subscriptions})
	if err != nil {
		return d, "", invalid()
	}
	return d, oc.DigestBytes(b), nil
}
func supports(d oc.HandlerDefinition, h event.Header) bool {
	for _, s := range d.Subscriptions {
		if s.EventType == h.EventType {
			for _, v := range s.Versions {
				if v == h.SchemaVersion {
					return true
				}
			}
		}
	}
	return false
}
