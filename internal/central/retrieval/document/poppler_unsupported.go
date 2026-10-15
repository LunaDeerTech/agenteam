//go:build !linux

package document

import (
	"context"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"time"
)

func runPopplerProcess(context.Context, PopplerConfig, string, []string, []byte, int, time.Duration) ([]byte, []byte, error) {
	return nil, nil, f.NewFault(f.CapabilityUnsupported, f.NotStarted)
}
