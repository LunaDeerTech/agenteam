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

// Work on a value copy: a failed or uncommitted publication must leave the
// original complete Runner result available for explicit recovery.
func readFileOutputCandidate(ctx context.Context, tx f.Tx, schemas *RegistrySchemaValidator, ref tc.SpecRef, terminal readFileTerminal, payload []byte) (readFileTerminal, error) {
	if terminal.Outcome != "success" {
		return terminal, nil
	}
	var mismatch bool
	err := schemas.validateInstance(ctx, tx, ref, payload, true, &mismatch)
	if err != nil {
		if !mismatch {
			return readFileTerminal{}, err
		}
		terminal.Outcome, terminal.Code, terminal.Result = "unknown", "backend_contract_violation", nil
	}
	return terminal, nil
}
