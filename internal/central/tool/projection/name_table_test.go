package projection_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/projection"
)

const firstName = "tool_01902e15100070008000000000000001"

func reference(t *testing.T, ordinal int, revision foundation.Version) contract.SpecRef {
	t.Helper()
	id, err := foundation.ParseID[identity.Tool](fmt.Sprintf("01902e15-1000-7000-8000-%012x", ordinal))
	if err != nil {
		t.Fatal(err)
	}
	return contract.SpecRef{ToolID: id, SpecRevision: revision}
}

func table(t *testing.T, refs []contract.SpecRef) *projection.NameTable {
	t.Helper()
	got, err := projection.BuildNameTable(context.Background(), refs)
	if err != nil || got == nil {
		t.Fatalf("build: %v", err)
	}
	return got
}

func requireFault(t *testing.T, err error, code foundation.Code) {
	t.Helper()
	var fault *foundation.Fault
	if !errors.As(err, &fault) || fault.Code != code || fault.CommitState != foundation.NotStarted {
		t.Fatalf("wanted %s/not_started, got %v", code, err)
	}
	if fault.SafeMessage != "" || len(fault.FieldErrors) != 0 || fault.CauseID != "" || fault.RetryHint != "" || errors.Unwrap(fault) != nil {
		t.Fatal("unexpected error input or diagnostic metadata")
	}
}

func TestNameTableNamesCopiesAndSnapshotRevisions(t *testing.T) {
	first := reference(t, 1, math.MaxInt64)
	second := reference(t, 2, 7)
	refs := []contract.SpecRef{second, first}
	names := table(t, refs)
	refs[1] = second
	got, err := names.Entries(context.Background())
	want := []projection.Entry{
		{Reference: first, ModelVisibleName: firstName},
		{Reference: second, ModelVisibleName: "tool_01902e15100070008000000000000002"},
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("manual name/order/value expectation: %v", err)
	}
	for _, entry := range got {
		if len(entry.ModelVisibleName) != 37 {
			t.Fatal("name is not 37 bytes")
		}
		resolved, err := names.Resolve(context.Background(), entry.ModelVisibleName)
		if err != nil || resolved != entry.Reference {
			t.Fatalf("exact lookup: %v", err)
		}
	}
	got[0] = projection.Entry{}
	again, err := names.Entries(context.Background())
	if err != nil || !reflect.DeepEqual(again, want) {
		t.Fatal("Entries or input mutation changed immutable table")
	}
	ordered, err := table(t, []contract.SpecRef{first, second}).Entries(context.Background())
	if err != nil || !reflect.DeepEqual(ordered, want) {
		t.Fatal("input order changed projection")
	}
	old := table(t, []contract.SpecRef{reference(t, 1, 1)})
	oldRef, err := old.Resolve(context.Background(), firstName)
	newRef, newErr := names.Resolve(context.Background(), firstName)
	if err != nil || newErr != nil || oldRef.SpecRevision != 1 || newRef != first {
		t.Fatal("table did not retain its own snapshot revision")
	}
}

func TestNameTableBoundsAndInvalidReferences(t *testing.T) {
	refs := make([]contract.SpecRef, projection.MaxTools)
	for i := range refs {
		refs[i] = reference(t, i+1, 1)
	}
	entries, err := table(t, refs).Entries(context.Background())
	if err != nil || len(entries) != 128 {
		t.Fatal("128 references were not preserved")
	}
	for _, tt := range []struct {
		name string
		refs []contract.SpecRef
		code foundation.Code
	}{
		{"over_limit_before_invalid", append(refs, contract.SpecRef{}), foundation.PayloadTooLarge},
		{"same_revision", []contract.SpecRef{refs[0], refs[0]}, foundation.InvalidArgument},
		{"different_revision", []contract.SpecRef{refs[0], reference(t, 1, 2)}, foundation.InvalidArgument},
		{"invalid_id_after_valid", []contract.SpecRef{refs[0], {SpecRevision: 1}}, foundation.InvalidArgument},
		{"invalid_revision_after_valid", []contract.SpecRef{refs[0], reference(t, 2, 0)}, foundation.InvalidArgument},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := projection.BuildNameTable(context.Background(), tt.refs)
			requireFault(t, err, tt.code)
			if got != nil {
				t.Fatal("partial table returned")
			}
		})
	}
}

func TestNameTableEmptyAndUnconstructed(t *testing.T) {
	for _, refs := range [][]contract.SpecRef{nil, {}} {
		names := table(t, refs)
		entries, err := names.Entries(context.Background())
		if err != nil || entries == nil || len(entries) != 0 {
			t.Fatal("constructed empty table must return non-nil empty entries")
		}
		ref, err := names.Resolve(context.Background(), firstName)
		requireFault(t, err, foundation.NotFound)
		if ref != (contract.SpecRef{}) {
			t.Fatal("empty table resolved a reference")
		}
	}
	for _, names := range []*projection.NameTable{nil, {}} {
		entries, err := names.Entries(context.Background())
		requireFault(t, err, foundation.DependencyUnbound)
		ref, resolveErr := names.Resolve(context.Background(), "")
		requireFault(t, resolveErr, foundation.DependencyUnbound)
		if entries != nil || ref != (contract.SpecRef{}) {
			t.Fatal("unconstructed table returned data")
		}
	}
}

func TestNameTableExactResolutionAndSafeUnknownName(t *testing.T) {
	names := table(t, []contract.SpecRef{reference(t, 1, 1)})
	canary := "UNKNOWN_TOOL_NAME_CANARY\n\x00\xff"
	for _, name := range []string{
		"", strings.ToUpper(firstName), " " + firstName, firstName + " ",
		firstName[:36], firstName + "x", "tool_01902e15100070008000000000000002",
		"tool_01902e15-1000-7000-8000-000000000001", "%74" + firstName[1:],
		strings.Repeat("\xff", 37), canary, strings.Repeat(canary, 4096),
	} {
		got, err := names.Resolve(context.Background(), name)
		requireFault(t, err, foundation.NotFound)
		if got != (contract.SpecRef{}) {
			t.Fatal("unknown name resolved")
		}
		assertSafeOutput(t, err, []string{"UNKNOWN_TOOL_NAME_CANARY", firstName})
	}
}

// Cancel on an observable context check, without timers, sleeps or test-only
// hooks in production. The embedded real context supplies the original Err.
type checkedContext struct {
	context.Context
	cancel context.CancelFunc
	after  int
	checks int
}

func (c *checkedContext) Err() error {
	c.checks++
	if c.after > 0 && c.checks == c.after {
		c.cancel()
	}
	return c.Context.Err()
}

func checked(t *testing.T, after int) *checkedContext {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return &checkedContext{Context: ctx, cancel: cancel, after: after}
}

func TestNameTableCancellationReturnsNoPartialResults(t *testing.T) {
	refs := []contract.SpecRef{reference(t, 3, 1), reference(t, 1, 1), reference(t, 2, 1)}
	names := table(t, refs)
	operations := []struct {
		name string
		call func(context.Context) (bool, error)
	}{
		{"build", func(ctx context.Context) (bool, error) {
			got, err := projection.BuildNameTable(ctx, refs)
			return got == nil, err
		}},
		{"entries", func(ctx context.Context) (bool, error) {
			got, err := names.Entries(ctx)
			return got == nil, err
		}},
		{"resolve", func(ctx context.Context) (bool, error) {
			got, err := names.Resolve(ctx, firstName)
			return got == (contract.SpecRef{}), err
		}},
	}
	for _, op := range operations {
		t.Run(op.name, func(t *testing.T) {
			observer := checked(t, 0)
			if zero, err := op.call(observer); zero || err != nil || observer.checks < 2 {
				t.Fatalf("successful call/checks: %v", err)
			}
			// This includes cancellation after partial construction/copying and
			// at the last check immediately before returning a successful value.
			for at := 1; at <= observer.checks; at++ {
				ctx := checked(t, at)
				zero, err := op.call(ctx)
				if !zero || !errors.Is(err, context.Canceled) {
					t.Fatalf("cancellation at check %d returned data or lost cancellation", at)
				}
			}
			zero, err := op.call(nil)
			requireFault(t, err, foundation.InvalidArgument)
			if !zero {
				t.Fatal("nil context returned data")
			}
			deadline, cancel := context.WithDeadline(context.Background(), time.Unix(0, 0))
			defer cancel()
			if zero, err := op.call(deadline); !zero || !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("original deadline error not preserved")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := projection.BuildNameTable(ctx, make([]contract.SpecRef, 129)); got != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation did not precede input validation")
	}
	var unbound *projection.NameTable
	if got, err := unbound.Entries(ctx); got != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation did not precede table validation")
	}
}

func TestNameTableConcurrentReaders(t *testing.T) {
	ref := reference(t, 1, 17)
	names := table(t, []contract.SpecRef{ref})
	var joined sync.WaitGroup
	for range 16 {
		joined.Go(func() {
			for range 32 {
				got, err := names.Resolve(context.Background(), firstName)
				entries, entriesErr := names.Entries(context.Background())
				if err != nil || got != ref || entriesErr != nil || len(entries) != 1 || entries[0].Reference != ref {
					t.Error("concurrent reader changed immutable mapping")
					return
				}
				entries[0] = projection.Entry{}
			}
		})
	}
	joined.Wait()
}

func TestNameTableDefaultNestedLoggingHidesMapping(t *testing.T) {
	ref := reference(t, 1, 987654321)
	names := table(t, []contract.SpecRef{ref})
	assertSafeOutput(t, names, []string{firstName, ref.ToolID.String(), "987654321"})
	assertSafeOutput(t, *names, []string{firstName, ref.ToolID.String(), "987654321"})
	// fmt can traverse unexported enclosing fields without invoking their
	// Formatter methods; private captured storage must remain hidden too.
	private := struct{ table projection.NameTable }{table: *names}
	for _, format := range []string{"%v", "%+v", "%#v"} {
		text := fmt.Sprintf(format, private)
		if strings.Contains(text, firstName) || strings.Contains(text, "987654321") {
			t.Fatal("private enclosing field exposed captured mappings")
		}
	}
}

func assertSafeOutput(t *testing.T, value any, forbidden []string) {
	t.Helper()
	for _, nested := range []any{value, []any{value}, map[string]any{"value": value}, struct{ Value any }{value}} {
		var outputs []string
		for _, format := range []string{"%v", "%+v", "%#v"} {
			outputs = append(outputs, fmt.Sprintf(format, nested))
		}
		encoded, err := json.Marshal(nested)
		if err != nil {
			t.Fatal(err)
		}
		outputs = append(outputs, string(encoded))
		var log bytes.Buffer
		slog.New(slog.NewJSONHandler(&log, nil)).Info("projection", "value", nested)
		outputs = append(outputs, log.String())
		for _, output := range outputs {
			for _, canary := range forbidden {
				if strings.Contains(output, canary) {
					t.Fatal("default output leaked a mapping or unknown input")
				}
			}
		}
	}
}
