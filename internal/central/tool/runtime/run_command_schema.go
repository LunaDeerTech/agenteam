package runtime

import (
	"context"
	"encoding/json"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

// Reuse the actual historical Registry reader and standard schema engine.
// Neither method changes the existing install codec, schema or byte limits.
func (v *RegistrySchemaValidator) ValidateRunCommandInputInTx(ctx context.Context, tx f.Tx, ref tc.SpecRef, raw []byte) error {
	return v.validate(ctx, tx, ref, raw, false)
}
func (v *RegistrySchemaValidator) ValidateRunCommandOutputInTx(ctx context.Context, tx f.Tx, ref tc.SpecRef, raw []byte) error {
	return v.validate(ctx, tx, ref, raw, true)
}

// Work on a value copy: a failed or uncommitted publication must leave the
// original complete Runner result available for explicit recovery.
func runCommandOutputCandidate(ctx context.Context, tx f.Tx, schemas *RegistrySchemaValidator, ref tc.SpecRef, terminal runCommandTerminal, payload, stdout, stderr []byte) (runCommandTerminal, error) {
	if !terminal.ReportAvailable {
		return terminal, nil
	}
	if terminal.Result == nil {
		return runCommandTerminal{}, fail(f.InvalidState)
	}
	if _, err := decodeRunCommandStoredResult(*terminal.Result, payload, stdout, stderr, terminal.Operation, terminal.Attempt, terminal.Request); err != nil {
		return runCommandTerminal{}, err
	}
	// Definition validation covers the full explicit envelope. The envelope is
	// temporary; metadata and the two streams remain independent SQL columns.
	raw, err := json.Marshal(struct {
		Report json.RawMessage `json:"report"`
		Stdout string          `json:"stdout"`
		Stderr string          `json:"stderr"`
	}{payload, string(stdout), string(stderr)})
	if err != nil {
		return runCommandTerminal{}, fail(f.InvalidState)
	}
	defer clear(raw)
	var mismatch bool
	err = schemas.validateInstance(ctx, tx, ref, raw, true, &mismatch)
	if err != nil {
		if !mismatch {
			return runCommandTerminal{}, err
		}
		terminal.Outcome, terminal.Code, terminal.Result = "unknown", "backend_contract_violation", nil
		terminal.ReportAvailable = false
	}
	return terminal, nil
}
