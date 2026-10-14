package config

import (
	"encoding/json"
	"net/netip"

	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
)

const outboundProxyOriginsSetting = "OUTBOUND_PROXY_ORIGINS"

// OutboundProxyOrigins identifies the exact HTTPS origins whose destination
// resolution may be delegated to the deployment's trusted environment proxy.
// Absence preserves direct-only routing. The returned slice is a copy.
func (c Config) OutboundProxyOrigins() []outbound.Origin {
	return append([]outbound.Origin(nil), c.outboundProxyOrigins...)
}

func loadOutboundProxyOrigins(lookup LookupEnv) ([]outbound.Origin, error) {
	raw, present := lookup(Prefix + outboundProxyOriginsSetting)
	if !present {
		return nil, nil
	}
	var values []string
	if len(raw) == 0 || len(raw) > 16384 || json.Unmarshal([]byte(raw), &values) != nil || len(values) == 0 || len(values) > 32 {
		return nil, invalid(outboundProxyOriginsSetting)
	}
	origins := make([]outbound.Origin, len(values))
	for n, value := range values {
		origin, err := outbound.ParseOrigin(value)
		if err != nil {
			return nil, invalid(outboundProxyOriginsSetting)
		}
		origins[n] = origin
	}
	if err := validateOutboundProxyOrigins(origins); err != nil {
		return nil, err
	}
	return origins, nil
}

func validateOutboundProxyOrigins(origins []outbound.Origin) error {
	if len(origins) > 32 {
		return invalid(outboundProxyOriginsSetting)
	}
	seen := make(map[string]bool, len(origins))
	for _, origin := range origins {
		_, numeric := netip.ParseAddr(origin.Host())
		if !origin.Valid() || origin.Scheme() != "https" || numeric == nil || seen[origin.String()] {
			return invalid(outboundProxyOriginsSetting)
		}
		seen[origin.String()] = true
	}
	return nil
}
