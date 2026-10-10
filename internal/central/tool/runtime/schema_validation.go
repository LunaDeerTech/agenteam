package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/registry"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/schema"
)

// RegistrySchemaValidator validates against an original immutable definition.
// It grants no current registration, execution or approval permission. Both
// input and terminal output use the caller's exact historical SpecRef, even if
// current registration has subsequently changed or been removed.
type RegistrySchemaValidator struct{ data func() schemaValidationData }

type schemaValidationData struct {
	store  Store
	reader interface {
		ReadDefinitionInTx(context.Context, f.Tx, tc.SpecRef) (tc.Definition, error)
	}
}

// NewRegistrySchemaValidator accepts the existing Registry metadata method;
// *registry.Registry implements it directly. No second metadata protocol or
// transaction owner is introduced. Composition supplies the same Store used
// by the Registry and the operation. Both independently reject a foreign Tx.
func NewRegistrySchemaValidator(store Store, reader interface {
	ReadDefinitionInTx(context.Context, f.Tx, tc.SpecRef) (tc.Definition, error)
}) (*RegistrySchemaValidator, error) {
	if nilPort(store) || nilPort(reader) {
		return nil, fail(f.DependencyUnbound)
	}
	d := schemaValidationData{store, reader}
	return &RegistrySchemaValidator{data: func() schemaValidationData { return d }}, nil
}

var _ InstallSchemaValidator = (*RegistrySchemaValidator)(nil)

func (v *RegistrySchemaValidator) ValidateInstallInputInTx(ctx context.Context, tx f.Tx, ref tc.SpecRef, raw []byte) error {
	return v.validate(ctx, tx, ref, raw, false)
}

func (v *RegistrySchemaValidator) ValidateInstallOutputInTx(ctx context.Context, tx f.Tx, ref tc.SpecRef, raw json.RawMessage) error {
	return v.validate(ctx, tx, ref, raw, true)
}

func (v *RegistrySchemaValidator) validate(ctx context.Context, tx f.Tx, ref tc.SpecRef, raw []byte, output bool) (err error) {
	if err = contextError(ctx); err != nil {
		return err
	}
	defer func() {
		if recover() != nil {
			err = fail(f.DependencyUnavailable)
		}
		if ctx.Err() != nil {
			err = ctx.Err()
		}
	}()
	if v == nil || v.data == nil {
		return fail(f.DependencyUnbound)
	}
	if ref.Validate() != nil {
		return fail(f.InvalidArgument)
	}
	if len(raw) > schema.MaxInstanceBytes {
		return fail(f.PayloadTooLarge)
	}
	d := v.data()
	x, err := d.store.InTx(tx)
	if err != nil {
		return schemaReadError(err)
	}
	if nilPort(x) {
		return fail(f.InvalidState)
	}
	tool, err := f.AggregateLock(f.ToolSpecAggregate, ref.ToolID.String())
	if err != nil {
		return fail(f.InvalidArgument)
	}
	locks := []f.LockRequest{registry.RegistryLock(f.Shared), {Key: tool, Mode: f.Shared}}
	if err = d.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return schemaReadError(err)
	}
	definition, err := d.reader.ReadDefinitionInTx(ctx, tx, ref)
	if err != nil {
		return schemaReadError(err)
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	document := definition.InputSchema
	if output {
		document = definition.OutputSchema
	}
	// Every call first crosses the original live Tx/locks/metadata boundary.
	// An absent output schema is unsupported for this fixed install profile;
	// it is never treated as successful validation of arbitrary output.
	compiled, err := schema.Compile(ctx, document)
	if err != nil {
		return err
	}
	return compiled.Validate(ctx, raw)
}

// A metadata provider can carry schema text in diagnostics. Keep only typed
// fault code/state or cancellation; neither raw errors, field paths nor their
// causes escape this boundary. The actual read has already returned.
func schemaReadError(err error) error {
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	var fault *f.Fault
	if errors.As(err, &fault) && fault != nil {
		return f.NewFault(fault.Code.Safe(), fault.CommitState.Safe())
	}
	return fail(f.DependencyUnavailable)
}

func (RegistrySchemaValidator) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "registry_schema_validator")
}
func (RegistrySchemaValidator) LogValue() slog.Value {
	return slog.StringValue("registry_schema_validator")
}
func (RegistrySchemaValidator) MarshalJSON() ([]byte, error) {
	return []byte(`"registry_schema_validator"`), nil
}
