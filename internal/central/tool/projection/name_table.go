// Package projection constructs bounded, immutable model-visible Tool names.
// It does not resolve Registry facts, authorize calls or invoke a backend.
package projection

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

const MaxTools = 128

// Entry is an explicitly requested mapping, not a Tool capability.
type Entry struct {
	Reference        contract.SpecRef
	ModelVisibleName string
}

type tableData struct {
	entries []Entry
	byName  map[string]contract.SpecRef
}

// NameTable retains the exact revisions supplied by its caller. A future
// executor must select the original Snapshot's table for each ToolCall: another
// table can bind the same name to a different revision. The zero value is unbound.
type NameTable struct {
	// A closure also prevents fmt traversal through an enclosing private field
	// from exposing the mappings. No method returns this captured storage.
	data func() tableData
}

func (NameTable) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "tool.NameTable") }
func (NameTable) LogValue() slog.Value       { return slog.StringValue("tool.NameTable") }
func (NameTable) MarshalJSON() ([]byte, error) {
	return []byte(`"tool.NameTable"`), nil
}

// BuildNameTable copies refs and rejects any repeated ToolID, including repeats
// with a different revision. A nil or empty slice constructs a valid empty table.
func BuildNameTable(ctx context.Context, refs []contract.SpecRef) (*NameTable, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if len(refs) > MaxTools {
		return nil, fault(foundation.PayloadTooLarge)
	}
	data := tableData{
		entries: make([]Entry, 0, len(refs)),
		byName:  make(map[string]contract.SpecRef, len(refs)),
	}
	for _, ref := range refs {
		if err := contextError(ctx); err != nil {
			return nil, err
		}
		if err := ref.Validate(); err != nil {
			return nil, err
		}
		name := "tool_" + strings.ReplaceAll(ref.ToolID.String(), "-", "")
		if _, exists := data.byName[name]; exists {
			return nil, fault(foundation.InvalidArgument)
		}
		data.byName[name] = ref
		data.entries = append(data.entries, Entry{Reference: ref, ModelVisibleName: name})
	}
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	slices.SortFunc(data.entries, func(a, b Entry) int {
		return strings.Compare(a.ModelVisibleName, b.ModelVisibleName)
	})
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	return &NameTable{data: func() tableData { return data }}, nil
}

// Entries returns a new slice sorted by ASCII model-visible name. A built empty
// table returns a non-nil empty slice; an unconstructed table returns an error.
func (t *NameTable) Entries(ctx context.Context) ([]Entry, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if t == nil || t.data == nil {
		return nil, fault(foundation.DependencyUnbound)
	}
	data := t.data()
	entries := make([]Entry, len(data.entries))
	for i, entry := range data.entries {
		if err := contextError(ctx); err != nil {
			return nil, err
		}
		entries[i] = entry
	}
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	return entries, nil
}

// Resolve performs only an exact table lookup. It never normalizes a name,
// falls back to a Registry, or echoes an unknown name in an error.
func (t *NameTable) Resolve(ctx context.Context, name string) (contract.SpecRef, error) {
	if err := contextError(ctx); err != nil {
		return contract.SpecRef{}, err
	}
	if t == nil || t.data == nil {
		return contract.SpecRef{}, fault(foundation.DependencyUnbound)
	}
	if len(name) != 37 {
		return contract.SpecRef{}, fault(foundation.NotFound)
	}
	ref, found := t.data().byName[name]
	if !found {
		return contract.SpecRef{}, fault(foundation.NotFound)
	}
	if err := contextError(ctx); err != nil {
		return contract.SpecRef{}, err
	}
	return ref, nil
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return fault(foundation.InvalidArgument)
	}
	return ctx.Err()
}

func fault(code foundation.Code) error {
	return foundation.NewFault(code, foundation.NotStarted)
}
