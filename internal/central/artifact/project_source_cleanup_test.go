package artifact

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5/pgconn"
)

// These tests consume the real technical-tail predicate with the original
// private cleanup owner/live Tx. SQL projections are controlled: they do not
// prove PostgreSQL rollback, a Project grant or preceding physical Object work.
type sourceCleanupStore struct {
	*cleanupTechnicalStore
	fixture *sourceCleanupFixture
}

func (s *sourceCleanupStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.cleanupTechnicalStore.InTx(tx); err != nil {
		return nil, err
	}
	return sourceCleanupExecutor{s, tx}, nil
}

type sourceCleanupSelection struct {
	ID      string `json:"id"`
	Command string `json:"command"`
	Lease   string `json:"lease"`
}
type sourceCleanupExecutor struct {
	store *sourceCleanupStore
	tx    f.Tx
}

func (e sourceCleanupExecutor) Query(ctx context.Context, q string, args ...any) (*postgres.Rows, error) {
	return (cleanupTechnicalExecutor{e.store.cleanupTechnicalStore, e.tx}).Query(ctx, q, args...)
}
func (e sourceCleanupExecutor) QueryRow(ctx context.Context, q string, args ...any) postgres.Row {
	return (cleanupTechnicalExecutor{e.store.cleanupTechnicalStore, e.tx}).QueryRow(ctx, q, args...)
}
func (e sourceCleanupExecutor) Exec(ctx context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	v := e.store.fixture
	if _, err := e.store.InTx(e.tx); err != nil {
		return pgconn.CommandTag{}, err
	}
	if !strings.Contains(q, "USING jsonb_to_recordset($2::jsonb)") {
		if strings.HasPrefix(q, "DELETE FROM agenteam_artifact.project_work ") {
			v.sequence = append(v.sequence, "plain-work")
		} else if strings.HasPrefix(q, "DELETE FROM agenteam_artifact.project_stops ") {
			v.sequence = append(v.sequence, "archive")
		}
		return (cleanupTechnicalExecutor{e.store.cleanupTechnicalStore, e.tx}).Exec(ctx, q, args...)
	}
	v.mu.Lock()
	_, err := v.fact(e.tx, args)
	v.mu.Unlock()
	if err != nil {
		return pgconn.CommandTag{}, err
	}
	if len(args) != 2 {
		return pgconn.CommandTag{}, errors.New("source retirement lost exact selection")
	}
	raw, ok := args[1].([]byte)
	if !ok {
		return pgconn.CommandTag{}, errors.New("source selection is not original JSON bytes")
	}
	var selected []sourceCleanupSelection
	var fields []map[string]json.RawMessage
	if json.Unmarshal(raw, &selected) != nil || json.Unmarshal(raw, &fields) != nil || len(selected) == 0 || len(selected) > 100 {
		return pgconn.CommandTag{}, errors.New("invalid source selection")
	}
	for _, item := range fields {
		if len(item) != 3 || item["id"] == nil || item["command"] == nil || item["lease"] == nil {
			return pgconn.CommandTag{}, errors.New("source selection changed its closed fields")
		}
	}
	kind := "source-work"
	if strings.HasPrefix(q, "DELETE FROM agenteam_artifact.commands c ") {
		kind = "source-commands"
	} else if !strings.HasPrefix(q, "DELETE FROM agenteam_artifact.project_work w ") {
		return pgconn.CommandTag{}, errors.New("unexpected source mutation")
	}
	for _, predicate := range []string{"command_hash=decode(selected.command,'hex')", "source_lease_id=selected.lease", "c.path='source'", "c.state='deleted'", "c.source->>'project'="} {
		if !strings.Contains(q, predicate) {
			return pgconn.CommandTag{}, fmt.Errorf("missing source predicate: %s", predicate)
		}
	}
	if kind == "source-work" {
		for _, predicate := range []string{"w.project_id=$1", "w.id=selected.id", "w.source_project_id=$1", "w.kind='creation'", "w.joined_at IS NOT NULL", "w.source_joined_at IS NOT NULL"} {
			if !strings.Contains(q, predicate) {
				return pgconn.CommandTag{}, errors.New("source work lost its returned same-Project predicate")
			}
		}
		want := []sourceCleanupSelection{}
		facts := v.technical[v.gate[e.tx]]
		for _, row := range facts.Work[:min(100, len(facts.Work))] {
			if row.SourceUse || row.Source != "" {
				want = append(want, sourceCleanupSelection{row.ID, row.Command, row.Lease})
			}
		}
		if !reflect.DeepEqual(selected, want) {
			return pgconn.CommandTag{}, errors.New("source retirement changed its first bounded row set")
		}
		v.selected, v.selectionTx = append([]sourceCleanupSelection(nil), selected...), e.tx
	} else if !strings.Contains(q, "c.project_id=$1::text::uuid") || v.selectionTx != e.tx || !reflect.DeepEqual(v.selected, selected) {
		return pgconn.CommandTag{}, errors.New("source command retirement left the original transaction/selection")
	}
	for _, row := range selected {
		command := v.commands[row.Command]
		if command == nil || command.Project != args[0] || command.State != "deleted" || command.Lease != row.Lease {
			return pgconn.CommandTag{}, errors.New("source command did not match the selected before-image")
		}
		if decoded, err := hex.DecodeString(row.Command); err != nil || len(decoded) != sha256.Size {
			return pgconn.CommandTag{}, errors.New("SQL command_hash must be raw hex, not a Digest projection")
		}
	}
	v.sequence = append(v.sequence, kind)
	if v.beforeSourceExec != nil {
		if err := v.beforeSourceExec(ctx, kind); err != nil {
			return pgconn.CommandTag{}, err
		}
	}
	if v.shortSource == kind {
		return pgconn.NewCommandTag(fmt.Sprintf("DELETE %d", len(selected)-1)), nil
	}
	if kind == "source-work" {
		ids := map[string]bool{}
		for _, row := range selected {
			ids[row.ID] = true
		}
		p := v.gate[e.tx]
		facts := v.technical[p]
		kept := make([]artifactCleanupWork, 0, len(facts.Work)-len(selected))
		for _, row := range facts.Work {
			if !ids[row.ID] {
				kept = append(kept, row)
			}
		}
		facts.Work = kept
		v.technical[p] = facts
	} else {
		for _, row := range selected {
			delete(v.commands, row.Command)
		}
	}
	return pgconn.NewCommandTag(fmt.Sprintf("DELETE %d", len(selected))), ctx.Err()
}

type sourceCleanupFixture struct {
	*cleanupTechnicalFixture
	store            *sourceCleanupStore
	commands         map[string]*artifactCleanupSourceCommand
	selected         []sourceCleanupSelection
	selectionTx      f.Tx
	sequence         []string
	shortSource      string
	beforeSourceExec func(context.Context, string) error
}

func newSourceCleanupFixture(t *testing.T) (*sourceCleanupFixture, i.ProjectID) {
	t.Helper()
	base, p := newCleanupTechnicalFixture(t)
	v := &sourceCleanupFixture{cleanupTechnicalFixture: base, commands: map[string]*artifactCleanupSourceCommand{}}
	v.store = &sourceCleanupStore{base.store, v}
	owners, err := NewOwnerProvider(v.store, base)
	if err != nil {
		t.Fatal(err)
	}
	base.state.store, base.state.owners = v.store, owners
	return v, p
}
func (v *sourceCleanupFixture) addSources(t *testing.T, p i.ProjectID, n int) {
	t.Helper()
	d := newProjectAuditSourceFixture(t).command.source.Details()
	ref := d.Reference.Details()
	ref.ProjectID = p
	var err error
	d.Reference, err = oc.NewBusinessFileRef(ref)
	if err != nil {
		t.Fatal(err)
	}
	d.Owner, err = oc.NewObjectOwner(oc.Artifact, ref.ArtifactID, p.String())
	if err != nil {
		t.Fatal(err)
	}
	d.Meta.Scope, err = i.InProject(p)
	if err != nil {
		t.Fatal(err)
	}
	source, err := oc.NewResolvedSource(d)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := encodeSource(source)
	if err != nil {
		t.Fatal(err)
	}
	facts := v.technical[p]
	for range n {
		key := f.IdempotencyKey(newTestID[struct{}](t).String())
		identity, err := f.NewCommandIdentity("artifact", []string{p.String()}, "create", key)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256([]byte(identity.Canonical()))
		hash := hex.EncodeToString(sum[:]) // Match encode(command_hash,'hex') exactly.
		lease := newTestID[oc.Lease](t).String()
		command := &artifactCleanupSourceCommand{Key: string(key), Project: p.String(), Path: "source", State: "deleted", Lease: lease, Source: append([]byte(nil), raw...)}
		v.commands[hash] = command
		facts.Work = append(facts.Work, artifactCleanupWork{ID: newTestID[artifactCallID](t).String(), Project: p.String(), Process: v.process.String(), Instance: v.pair.state.instance.String(), Source: p.String(), Joined: true, SourceUse: true, Kind: "creation", Command: hash, Lease: lease, SourceJoined: true, SourceCommand: command})
	}
	sort.Slice(facts.Work, func(a, b int) bool { return facts.Work[a].ID < facts.Work[b].ID })
	v.technical[p] = facts
}

func TestArtifactSourceCleanupFacts(t *testing.T) {
	for _, mode := range []string{"exact", "command-hash", "command-key", "command-lease", "missing-command", "command-path", "command-project", "source-metadata", "source-project", "source-kind", "source-side", "source-unjoined", "unreturned-owner", "replacement-instance", "duplicate-command"} {
		t.Run(mode, func(t *testing.T) {
			v, p := newSourceCleanupFixture(t)
			v.addSources(t, p, 1)
			facts := v.technical[p]
			row := &facts.Work[0]
			switch mode {
			case "command-hash":
				row.Command = strings.Repeat("0", 64)
			case "command-key":
				row.SourceCommand.Key = "different-original-key"
			case "command-lease":
				row.SourceCommand.Lease = newTestID[oc.Lease](t).String()
			case "missing-command":
				row.SourceCommand = nil
			case "command-path":
				row.SourceCommand.Path = "content"
			case "command-project":
				row.SourceCommand.Project = newTestID[i.Project](t).String()
			case "source-metadata", "source-project", "source-kind":
				var wire sourceWire
				if err := json.Unmarshal(row.SourceCommand.Source, &wire); err != nil {
					t.Fatal(err)
				}
				if mode == "source-metadata" {
					wire.Object = oc.ObjectID{}
				} else if mode == "source-project" {
					wire.Project = newTestID[i.Project](t).String()
					wire.Reference.Project = wire.Project
				} else {
					wire.OwnerKind, wire.Owner = oc.Knowledge, newTestID[struct{}](t).String()
					wire.Reference = referenceWire{Kind: oc.KnowledgeFile, Project: p.String(), Document: wire.Owner, Revision: wire.Revision}
				}
				var err error
				row.SourceCommand.Source, err = json.Marshal(wire)
				if err != nil {
					t.Fatal(err)
				}
			case "source-unjoined":
				row.SourceJoined = false
			case "source-side":
				row.Project = newTestID[i.Project](t).String()
			case "unreturned-owner":
				id := newTestID[artifactCallID](t)
				v.pair.state.owners[id] = &artifactCall{record: artifactCallRecord{ID: id, Project: p}}
				t.Cleanup(func() { delete(v.pair.state.owners, id) })
			case "replacement-instance":
				row.Instance = newTestID[artifactInstanceID](t).String()
			case "duplicate-command":
				duplicate := *row
				duplicate.ID = newTestID[artifactCallID](t).String()
				facts.Work = append(facts.Work, duplicate)
				sort.Slice(facts.Work, func(a, b int) bool { return facts.Work[a].ID < facts.Work[b].ID })
			}
			v.technical[p] = facts
			ready, clean, err := v.retire(context.Background(), p)
			if mode == "exact" {
				if err != nil || !ready || !clean || !reflect.DeepEqual(v.sequence, []string{"source-work", "source-commands"}) || len(v.commands) != 0 {
					t.Fatal("exact source history was not retired in its original Tx", ready, clean, err, v.sequence)
				}
			} else if err != nil || ready || clean || len(v.sequence) != 0 {
				t.Fatal("unconfirmed source history reached mutation", mode, ready, clean, err, v.sequence)
			}
		})
	}
}

func TestArtifactSourceCleanupBatchAndErrors(t *testing.T) {
	for _, mode := range []string{"mixed", "partial", "work-count", "command-count", "sql-error", "not-deleted"} {
		t.Run(mode, func(t *testing.T) {
			v, p := newSourceCleanupFixture(t)
			n := 1
			if mode == "partial" {
				n = 101
			}
			v.addSources(t, p, n)
			v.addArchive(t, p)
			if mode == "mixed" {
				v.addWork(t, p, 1)
			}
			originalErr := errors.New("original source delete failed")
			switch mode {
			case "work-count":
				v.shortSource = "source-work"
			case "command-count":
				v.shortSource = "source-commands"
			case "sql-error":
				v.beforeSourceExec = func(context.Context, string) error { return originalErr }
			case "not-deleted":
				v.technical[p].Work[0].SourceCommand.State = "completed"
			}
			ready, clean, err := v.retire(context.Background(), p)
			switch mode {
			case "mixed":
				if err != nil || !ready || !clean || !reflect.DeepEqual(v.sequence, []string{"source-work", "source-commands", "plain-work", "archive"}) || len(v.commands) != 0 || len(v.technical[p].Work) != 0 || len(v.technical[p].Stops) != 1 {
					t.Fatal("mixed source/plain tail order changed", ready, clean, err, v.sequence)
				}
			case "partial":
				if err != nil || !ready || clean || len(v.commands) != 1 || len(v.technical[p].Work) != 1 || len(v.technical[p].Stops) != 2 || len(v.selected) != 100 || !reflect.DeepEqual(v.sequence, []string{"source-work", "source-commands"}) {
					t.Fatal("bounded source tail lost its remaining complete command", ready, clean, err)
				}
				ready, clean, err = v.retire(context.Background(), p)
				if err != nil || !ready || !clean || len(v.commands) != 0 || len(v.technical[p].Work) != 0 || len(v.technical[p].Stops) != 1 {
					t.Fatal("next explicit source batch failed", ready, clean, err)
				}
			default:
				if err == nil || clean || len(v.technical[p].Stops) != 2 {
					t.Fatal("failed source retirement advanced archive cleanup", err, v.sequence)
				}
				if mode == "sql-error" && !errors.Is(err, originalErr) {
					t.Fatal("original SQL error lost", err)
				}
				if mode == "work-count" && !reflect.DeepEqual(v.sequence, []string{"source-work"}) || mode == "command-count" && !reflect.DeepEqual(v.sequence, []string{"source-work", "source-commands"}) || mode == "not-deleted" && len(v.sequence) != 0 {
					t.Fatal("inexact source result allowed a later deletion", v.sequence)
				}
			}
		})
	}
}

func TestArtifactSourceCleanupUnknownReadonlyReturn(t *testing.T) {
	v, p := newSourceCleanupFixture(t)
	v.addSources(t, p, 101)
	attempt := newTestID[f.TransactionAttempt](t)
	var original f.CommitResult
	v.store.after = func(_ context.Context, cause f.TransactionCause, result f.CommitResult) f.CommitResult {
		if len(v.sequence) > 0 && result.State() == f.Committed {
			original = f.UnknownResult(attempt, cause)
			return original
		}
		return result
	}
	_, _, err := v.retire(context.Background(), p)
	requireCleanupOwnerUnknown(t, oc.ProjectCleanupResult{}, err, original)
	if len(v.commands) != 1 || len(v.technical[p].Work) != 1 {
		t.Fatal("Unknown did not follow the actual first batch")
	}
	writes := append([]string(nil), v.sequence...)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	open := func() { once.Do(func() { close(release) }) }
	type output struct {
		out oc.ProjectCleanupResult
		err error
	}
	done := make(chan output, 1)
	returned := false
	t.Cleanup(func() {
		open()
		if !returned {
			cleanupOwnerWait(t, done)
		}
	})
	v.store.after = func(_ context.Context, _ f.TransactionCause, result f.CommitResult) f.CommitResult {
		close(entered)
		<-release
		return result
	}
	go func() { out, err := v.run(context.Background(), v.service, p); done <- output{out, err} }()
	cleanupOwnerWait(t, entered)
	v.state.cleanupMu.Lock()
	owner := v.state.cleanupCalls[p]
	retained := owner != nil && owner.active && owner.retained && owner.original.AttemptID() == attempt
	v.state.cleanupMu.Unlock()
	if !retained || !reflect.DeepEqual(v.sequence, writes) || len(v.commands) != 1 {
		t.Fatal("readonly confirmation released the original Unknown or repeated a batch")
	}
	open()
	got := cleanupOwnerWait(t, done)
	returned = true
	v.store.after = nil
	if got.err != nil || got.out.State != oc.CleanupPending || !reflect.DeepEqual(v.sequence, writes) || len(v.state.cleanupCalls) != 0 {
		t.Fatal("remaining source confirmation was not readonly Pending", got.out, got.err)
	}
	ready, clean, err := v.retire(context.Background(), p)
	if err != nil || !ready || !clean || len(v.commands) != 0 {
		t.Fatal("explicit batch after original confirmation failed", ready, clean, err)
	}
}

func TestArtifactSourceCleanupDeleteReturn(t *testing.T) {
	v, p := newSourceCleanupFixture(t)
	v.addSources(t, p, 1)
	v.addArchive(t, p)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	open := func() { once.Do(func() { close(release) }) }
	originalErr := errors.New("held source command delete failed")
	v.beforeSourceExec = func(_ context.Context, kind string) error {
		if kind == "source-commands" {
			close(entered)
			<-release
			return originalErr
		}
		return nil
	}
	done := make(chan error, 1)
	returned := false
	t.Cleanup(func() {
		open()
		if !returned {
			cleanupOwnerWait(t, done)
		}
	})
	go func() { _, _, err := v.retire(ctx, p); done <- err }()
	cleanupOwnerWait(t, entered)
	cancel()
	v.state.cleanupMu.Lock()
	owner := v.state.cleanupCalls[p]
	active := owner != nil && owner.active
	v.state.cleanupMu.Unlock()
	if !active || len(v.commands) != 1 || len(v.technical[p].Stops) != 2 {
		t.Fatal("cancel retired original source transaction ownership")
	}
	select {
	case <-done:
		returned = true
		t.Fatal("cancel substituted for the original SQL return")
	default:
	}
	open()
	err := cleanupOwnerWait(t, done)
	returned = true
	if !errors.Is(err, originalErr) || !reflect.DeepEqual(v.sequence, []string{"source-work", "source-commands"}) || len(v.state.cleanupCalls) != 0 {
		t.Fatal("original delete failure or return ownership changed", err, v.sequence)
	}
}
