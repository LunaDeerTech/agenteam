package secret

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestIndependentStorageIncrementLegacyPurpose(t *testing.T) {
	s, prepared, store, _, _, intent := projectVariableApplyFixture(t, sc.Update, false)
	projection, _ := prepared.Preparation()
	meta, _ := projection.Fields()
	r := intent.Fields().Request.Fields()
	// Use a genuinely valid legacy identity, so the negative cannot pass just
	// because the dedicated command's namespace was rejected first.
	command, err := f.NewCommandIdentity("secret", []string{r.ProjectID.String(), r.Actor.Details().UserID}, "update", "independent-legacy-command")
	if err != nil {
		t.Fatal(err)
	}
	request := sc.WriteRequest{Actor: r.Actor, Scope: meta.Ref.Details().Scope, Identity: command, Kind: sc.Update, Ref: meta.Ref, ExpectedVersion: 3, Purpose: sc.Model}
	if err := validateWrite(request); err != nil {
		t.Fatal("legacy positive is not structurally valid", err)
	}
	request.Purpose = sc.ProjectVariable
	before, nonce := store.queries, s.state().nonces[2].next
	got, err := s.PrepareWrite(context.Background(), request)
	if err == nil || got.data != nil || store.queries != before || s.state().nonces[2].next != nonce {
		t.Fatal("dedicated purpose entered the old public prepare path")
	}
	// An old valid request does not change the purpose of its existing target.
	store.purpose = sc.Model
	if metadata, _, err := loadMetadata(context.Background(), store, meta.Ref); err != nil || metadata.Purpose != sc.Model {
		t.Fatal("legacy current-row positive rejected", err)
	}
	store.purpose = sc.ProjectVariable
	if _, _, err := loadMetadata(context.Background(), store, meta.Ref); err == nil {
		t.Fatal("old current-row loader admitted dedicated ownership")
	}
	store.purpose = sc.Model
	if _, _, err := loadProjectVariableMetadata(context.Background(), store, meta.Ref); err == nil {
		t.Fatal("dedicated loader converted legacy ownership")
	}
	if len(store.execs) != 0 {
		t.Fatal("purpose boundary changed persistent facts")
	}
}

type incrementStageAuthority struct {
	*projectVariableApplyAuthority
	afterRead func()
	newError  error
}

func (a *incrementStageAuthority) CheckInTx(ctx context.Context, tx f.Tx, request sc.ProjectVariableWriteRequest, plan sc.ProjectVariableWritePlan, stage sc.ProjectVariableWriteStage) error {
	if err := a.projectVariableApplyAuthority.CheckInTx(ctx, tx, request, plan, stage); err != nil {
		return err
	}
	if stage == sc.ProjectVariableReceiptRead && a.afterRead != nil {
		a.afterRead()
	}
	if stage == sc.ProjectVariableNewWrite {
		return a.newError
	}
	return nil
}

func TestIndependentStorageIncrementApplyRechecksBetweenStages(t *testing.T) {
	for _, loseLock := range []bool{false, true} {
		name := "current-new-write-unknown"
		if loseLock {
			name = "full-lock-no-longer-held"
		}
		t.Run(name, func(t *testing.T) {
			s, prepared, store, authority, audit, _ := projectVariableApplyFixture(t, sc.Update, false)
			cause := errors.New("original dedicated current authority")
			unknown := f.NewFault(f.CommitUnknown, f.Unknown).WithCause(cause)
			gate := &incrementStageAuthority{projectVariableApplyAuthority: authority, newError: unknown}
			if loseLock {
				gate.afterRead = func() { store.locks = nil }
			}
			s.state().auth.ProjectVariables = gate
			got, err := s.ApplyProjectVariableWriteInTx(context.Background(), store.tx, prepared)
			if err == nil || got.Validate() == nil || len(store.execs) != 0 || audit.calls != 0 {
				t.Fatal("Read stage granted a stale new write")
			}
			if loseLock {
				if len(authority.stages) != 1 || authority.stages[0] != sc.ProjectVariableReceiptRead {
					t.Fatal("new authority called before second full held-lock check")
				}
			} else if err != unknown || !errors.Is(err, cause) || len(authority.stages) != 2 || authority.stages[1] != sc.ProjectVariableNewWrite {
				t.Fatal("new stage original Unknown/cause changed")
			}
		})
	}
}

// Raw pages are controlled, but scanCandidates and actual postgres.Rows perform
// Next/Scan/Close. The existing independent overlay bridge opens no connection.
type incrementRotationRows struct {
	pgx.Rows
	items  []envelope
	next   int
	closed int
}

func (r *incrementRotationRows) Next() bool {
	if r.next >= len(r.items) {
		return false
	}
	r.next++
	return true
}
func (r *incrementRotationRows) Scan(dest ...any) error {
	p := r.items[r.next-1]
	return projectVariablePayloadRow{p, int16(p.ownerKind)}.Scan(dest...)
}
func (r *incrementRotationRows) Close()     { r.closed++ }
func (r *incrementRotationRows) Err() error { return nil }

type incrementRotationStore struct {
	*projectVariableMaintenanceStore
	t     *testing.T
	items []envelope
	pages []*incrementRotationRows
	last  string
}

func (s *incrementRotationStore) Query(_ context.Context, query string, args ...any) (*postgres.Rows, error) {
	if !strings.Contains(query, "WHERE master_version<>$1") || !strings.Contains(query, "ORDER BY payload_id LIMIT 100") || len(args) != 2 || args[0] != int64(2) {
		s.t.Fatal("rotation changed bounded candidate query")
	}
	rows := &incrementRotationRows{}
	switch len(s.pages) {
	case 0:
		if args[1] != s.last {
			s.t.Fatal("rotation lost original last-ID checkpoint")
		}
	case 1:
		if args[1] != nil || s.pages[0].closed != 1 {
			s.t.Fatal("head rescan preceded original Rows retirement")
		}
		rows.items = s.items
	default:
		s.t.Fatal("extra candidate query")
	}
	s.pages = append(s.pages, rows)
	return postgres.ReviewB02Rows(rows), nil
}

func TestIndependentStorageIncrementActualRotationScanAndCAS(t *testing.T) {
	for _, name := range []string{"all-three-kinds", "last-cas-miss", "last-owner-changed"} {
		t.Run(name, func(t *testing.T) {
			keys := testKeys(t)
			project := projectAuditID[i.Project](t)
			scope, _ := i.InProject(project)
			credentials := []string{projectAuditID[sc.Credential](t).String(), projectAuditID[sc.Credential](t).String(), projectAuditID[sc.Credential](t).String()}
			changed := projectAuditID[sc.Credential](t).String()
			old := make([]envelope, 3)
			for n, kind := range []ownerKind{valueOwner, receiptOwner, projectVariableReceiptOwner} {
				owner := credentials[n]
				if n > 0 {
					owner = projectAuditID[sc.ProjectVariableReceipt](t).String()
				}
				nonce, _ := masterNonce(uint64(30 + n))
				var err error
				old[n], err = seal(keys, 1, nonce, scope, kind, owner, projectAuditID[payloadMarker](t), bytes.Repeat([]byte{byte(65 + n)}, 32))
				if err != nil {
					t.Fatal(err)
				}
			}
			store := &incrementRotationStore{projectVariableMaintenanceStore: &projectVariableMaintenanceStore{projectAuditUnitStore: &projectAuditUnitStore{tx: f.NewTx()}}, t: t, items: old, last: projectAuditID[payloadMarker](t).String()}
			run := projectAuditID[struct{}](t).String()
			store.read = func(query string, args []any) postgres.Row {
				switch {
				case strings.Contains(query, "FROM agenteam_secret.secret_control"):
					return projectAuditUnitRow{values: []any{int64(2), int64(1)}}
				case strings.Contains(query, "FROM agenteam_secret.secret_rotation_runs"):
					if strings.HasPrefix(query, "SELECT run_id") {
						return projectAuditUnitRow{values: []any{run, store.last, "running"}}
					}
					return projectAuditUnitRow{values: []any{"running"}}
				case strings.Contains(query, "FROM agenteam_secret.secret_command_receipts"):
					if args[0] != old[1].ownerID || args[1] != old[1].id.String() {
						t.Fatal("kind2 reverse ownership changed")
					}
					return projectAuditUnitRow{values: []any{credentials[1]}}
				case strings.Contains(query, "FROM agenteam_secret.project_variable_receipts"):
					if len(store.pages) != 2 || store.pages[1].closed != 1 || len(args) != 3 || args[0] != old[2].ownerID || args[1] != old[2].id.String() || args[2] != project.String() {
						t.Fatal("kind3 reverse lookup omitted exact identity or raced Rows")
					}
					owner := credentials[2]
					if store.acquires != 0 && name == "last-owner-changed" {
						owner = changed
					}
					return projectAuditUnitRow{values: []any{owner}}
				case strings.HasPrefix(query, "SELECT scope,"):
					if args[0] != old[2].id.String() {
						t.Fatal("wrong current kind3 payload")
					}
					return projectAuditUnitRow{values: []any{"project", project.String(), int16(3), old[2].ownerID}}
				default:
					t.Fatal("rotation consulted live canonical or unexpected SQL")
					return nil
				}
			}
			cas, checkpoint, total := 0, 0, int64(0)
			store.exec = func(query string, args []any) (pgconn.CommandTag, error) {
				if strings.HasPrefix(query, "UPDATE agenteam_secret.secret_payloads") {
					p := old[cas]
					cas++
					if args[0] != p.id.String() || args[3] != int64(2) || args[4] != int64(2) || args[5] != int64(1) || args[6] != int64(1) || !strings.Contains(query, "master_version=$6 AND wrap_revision=$7") {
						t.Fatal("CAS used another candidate's identity/versions")
					}
					if cas == 3 && name == "last-cas-miss" {
						return pgconn.NewCommandTag("UPDATE 0"), nil
					}
					total++
					return pgconn.NewCommandTag("UPDATE 1"), nil
				}
				if !strings.HasPrefix(query, "UPDATE agenteam_secret.secret_rotation_runs") || args[0] != run || args[1] != old[2].id.String() || args[2] != total {
					t.Fatal("rotation checkpoint did not use actual affected count")
				}
				checkpoint++
				return pgconn.NewCommandTag("UPDATE 1"), nil
			}
			state := &serviceState{store: store, initialized: true, keys: keys, nonces: map[f.Version]*nonceRange{2: {next: 200, end: 299}}}
			s := &Service{data: func() *serviceState { return state }}
			prepared, err := s.PrepareRewrap(context.Background())
			if err != nil || prepared.data == nil {
				t.Fatal("actual PrepareRewrap failed", err)
			}
			batch := prepared.data()
			if len(batch.items) != 3 || len(store.pages) != 2 || store.pages[0].closed != 1 || store.pages[1].closed != 1 {
				t.Fatal("bounded scan/head-rescan/Rows close incomplete")
			}
			for n, item := range batch.items {
				plain, err := openEnvelope(keys, item.next)
				if err != nil || !bytes.Equal(plain, bytes.Repeat([]byte{byte(65 + n)}, 32)) || !bytes.Equal(item.next.ciphertext, old[n].ciphertext) || item.credential != credentials[n] {
					t.Fatal("actual mixed-kind rewrap lost content or historical owner")
				}
				clear(plain)
			}
			got, err := s.ApplyPreparedRewrapInTx(context.Background(), store.tx, prepared)
			if name == "last-owner-changed" {
				projectAuditCode(t, err, f.ResourceBusy)
				if got.Applied != 0 || cas != 2 || checkpoint != 0 {
					t.Fatal("late changed owner published progress or wrong-group CAS")
				}
			} else if err != nil || got.Applied != f.Progress(total) || cas != 3 || checkpoint != 1 {
				t.Fatal("CAS result/report mismatch", err)
			}
			if store.acquires != 1 || store.transactions != 0 {
				t.Fatal("rotation opened another Tx or acquired late locks")
			}
			for _, lock := range store.locks {
				late, _ := f.AggregateLock(f.CredentialRefAggregate, changed)
				if f.CompareLockKeys(lock.Key, late) == 0 {
					t.Fatal("changed mapping silently acquired another aggregate")
				}
			}
		})
	}
}
