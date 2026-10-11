package runtime

import (
	"context"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

// Reuse the actual historical Registry reader and standard schema engine.
// Neither method changes the existing install codec, schema or byte limits.
func (v *RegistrySchemaValidator) ValidateReadFileInputInTx(ctx context.Context, tx f.Tx, ref tc.SpecRef, raw []byte) error {
	return v.validate(ctx, tx, ref, raw, false)
}
func (v *RegistrySchemaValidator) ValidateReadFileOutputInTx(ctx context.Context, tx f.Tx, ref tc.SpecRef, raw []byte) error {
	return v.validate(ctx, tx, ref, raw, true)
}
