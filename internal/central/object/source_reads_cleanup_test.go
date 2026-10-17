package object

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/minio/minio-go/v7"
)

// Control SQL and storage transport only; exercise the real SourceReads,
// integrity reader, access planner and registered cleanup lifetime without I/O.
type sourceCleanupStore struct{ *sourceStopStore }

func (s *sourceCleanupStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.sourceStopStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *sourceCleanupStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	return s.sourceStopStore.WithinTx(ctx, cause, func(callback context.Context, tx f.Tx) error {
		return fn(context.WithValue(callback, cleanupCallbackOnlyKey{}, tx), tx)
	})
}
func (s *sourceCleanupStore) QueryRow(ctx context.Context, q string, args ...any) postgres.Row {
	h := s.fixture.h
	d := h.source.Details()
	switch {
	case strings.Contains(q, "FROM agenteam_object.objects WHERE id=$1"):
		if len(args) != 1 || args[0] != d.Meta.ID.String() {
			return auditTestRow(func(...any) error { return errors.New("wrong source object") })
		}
		digest := sha256.Sum256(sourceCleanupPayload())
		return auditValues(d.Meta.ID.String(), "project", d.Owner.Partition(), d.Owner.Details().ProjectID, d.Meta.MediaType, int64(d.Meta.ByteSize), digest[:], d.Meta.State, int64(d.Meta.Version), "source-body", false, d.Meta.CreatedAt.Time())
	case strings.Contains(q, "SELECT EXISTS(SELECT 1 FROM agenteam_object.object_references"):
		return auditValues(true)
	case strings.Contains(q, "SELECT EXISTS(SELECT 1 FROM agenteam_object.object_leases"):
		return auditValues(len(args) == 3 && args[0] == h.id.String() && args[1] == d.Meta.ID.String() && args[2] == h.process.String() && s.fixture.leaseState == "active")
	default:
		return s.sourceStopStore.QueryRow(ctx, q, args...)
	}
}

type sourceCleanupTransport struct{ body *countedSource }

func (s sourceCleanupTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method != http.MethodGet || r.URL.Host != "source-cleanup.invalid" || r.URL.Path != "/owned/source-body" {
		return nil, errors.New("unexpected controlled storage request")
	}
	size := len(sourceCleanupPayload())
	return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Request: r, Body: s.body, ContentLength: int64(size), Header: http.Header{
		"Content-Length": {strconv.Itoa(size)}, "Content-Type": {"text/plain"}, "Last-Modified": {time.Unix(0, 0).UTC().Format(http.TimeFormat)}, "Etag": {`"owned-source"`},
	}}, nil
}

type sourceCleanupAuthority struct {
	witness     *int
	fixture     *sourceStopFixture
	denyRelease bool
	tailChecks  int
}

func (a *sourceCleanupAuthority) Discover(ctx context.Context, req oc.AccessRequest) (oc.AccessDependencies, error) {
	if ctx.Err() != nil || ctx.Value(cleanupOriginalValueKey{}) != a.witness {
		return oc.AccessDependencies{}, failure(f.Forbidden, nil)
	}
	if req.Details().Operation == oc.ReleaseReaderAccess {
		deadline, ok := ctx.Deadline()
		if a.denyRelease || !ok || deadline.After(time.Now().Add(2*time.Second)) || ctx.Value(cleanupCallbackOnlyKey{}) != nil {
			return oc.AccessDependencies{}, failure(f.Forbidden, nil)
		}
		a.tailChecks++
	} else if req.Details().Operation != oc.AcquireSourceAccess {
		return oc.AccessDependencies{}, failure(f.Forbidden, nil)
	}
	key, _ := f.ProjectLock(a.fixture.h.source.Details().Owner.Details().ProjectID)
	return oc.NewAccessDependencies(newDigest(make([]byte, 32)), []f.LockRequest{{Key: key, Mode: f.Shared}})
}
func (a *sourceCleanupAuthority) ValidateInTx(ctx context.Context, tx f.Tx, req oc.AccessRequest, deps oc.AccessDependencies) error {
	if ctx.Err() != nil || ctx.Value(cleanupOriginalValueKey{}) != a.witness || a.denyRelease && req.Details().Operation == oc.ReleaseReaderAccess {
		return failure(f.Forbidden, nil)
	}
	return a.fixture.store.RequireHeldLocks(ctx, tx, deps.Locks())
}

type sourceCleanupResolver struct {
	oc.SourceResolver
	authority *sourceCleanupAuthority
}

func (r sourceCleanupResolver) ValidateInTx(ctx context.Context, _ f.Tx, actor id.Actor, source oc.ResolvedSource, _ oc.AccessLockPlan, _ oc.LockedAccess) error {
	h := r.authority.fixture.h
	if ctx.Value(cleanupOriginalValueKey{}) != r.authority.witness || !actor.Equal(h.actor) || source.Details().Meta.ID != h.source.Details().Meta.ID {
		return failure(f.Forbidden, nil)
	}
	return ctx.Err()
}

func sourceCleanupPayload() []byte { return bytes.Repeat([]byte("source-tail"), oc.StreamBufferSize) }

func awaitSourceCleanupReturn(t *testing.T, s *Service) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for {
		st := s.state()
		st.mu.Lock()
		pending, changed := len(st.cleanupRequests), st.changed
		st.mu.Unlock()
		if pending == 0 {
			return
		}
		select {
		case <-changed:
		case <-ctx.Done():
			t.Fatal("original cleanup registration did not return")
		}
	}
}

func newSourceCleanupFixture(t *testing.T) (*sourceStopFixture, *sourceCleanupAuthority, oc.SourceLease, *countedSource) {
	t.Helper()
	v := &sourceStopFixture{leaseState: "active"}
	v.store = &sourceStopStore{joinControlStore: &joinControlStore{active: map[f.Tx]bool{}}, fixture: v, locks: map[f.Tx][]f.LockRequest{}}
	a := &sourceCleanupAuthority{fixture: v, witness: new(int)}
	st := &serviceState{store: &sourceCleanupStore{v.store}, initialized: true, process: auditID[oc.Process](t), accessIssuer: oc.NewAccessIssuer(), accessTransactions: map[f.Tx]bool{}, metadataTransactions: map[f.Tx]bool{}, projectWork: map[string]*projectWorkHandle{}, projectWriters: map[*projectWorkWriter]bool{}, projectJoins: map[string]*projectWorkHandle{}, closedLeases: map[oc.LeaseID]oc.ObjectID{}, operations: map[*operation]bool{}, cleanupRequests: map[*cleanupRequest]bool{}, changed: make(chan struct{}), auth: Authorizations{Planner: a}}
	v.service = &Service{data: func() *serviceState { return st }}
	var err error
	v.reads, err = NewSourceReads(v.service, sourceCleanupResolver{authority: a})
	if err != nil {
		t.Fatal(err)
	}
	d := downloadVectorClaims(t).target.Details().Source.Details()
	payload := sourceCleanupPayload()
	d.Meta.ByteSize, d.Meta.SHA256 = f.Progress(len(payload)), digestOf(payload)
	source, err := oc.NewResolvedSource(d)
	if err != nil {
		t.Fatal(err)
	}
	actor, _ := id.NewHuman(auditID[id.User](t), auditID[id.Session](t))
	v.h = &sourceLeaseState{owner: v.reads.data(), id: auditID[oc.Lease](t), origin: f.NewTx(), actor: actor, source: source, process: st.process}
	v.reads.data().leases[v.h.id] = v.h
	lease, err := oc.NewSourceLease(v.reads.data().issuer, v.h.id)
	if err != nil {
		t.Fatal(err)
	}
	grant, err := oc.NewOwnerAuthorization(oc.OwnerAuthorizationDetails{Actor: actor, Owner: d.Owner, Intent: id.Read, Existence: oc.ExistingOwner, Version: 1, ReadObjectID: d.Meta.ID})
	if err != nil {
		t.Fatal(err)
	}
	auth := &fixedIndexReadGrant{grant: grant}
	st.auth.Resources, st.auth.Gate = auth, auth
	body := &countedSource{reader: bytes.NewReader(payload)}
	client, err := minio.New("source-cleanup.invalid", &minio.Options{Region: "us-east-1", BucketLookup: minio.BucketLookupPath, Transport: sourceCleanupTransport{body}, MaxRetries: 1})
	if err != nil {
		t.Fatal(err)
	}
	backend := &backendState{client: client, config: StorageConfig{data: func() storageConfig { return storageConfig{bucket: "owned"} }}}
	st.backend = &Backend{data: func() *backendState { return backend }}
	return v, a, lease, body
}

func TestSourceReadsCleanupPreservesOriginalOpenValues(t *testing.T) {
	for _, mode := range []string{"normal", "cancelled-parent", "failed-tail-then-cancel"} {
		t.Run(mode, func(t *testing.T) {
			v, a, lease, body := newSourceCleanupFixture(t)
			original, cancel := context.WithCancel(context.WithValue(context.Background(), cleanupOriginalValueKey{}, a.witness))
			defer cancel()
			reader, err := v.reads.OpenLeasedSource(original, v.h.actor, lease)
			if err != nil {
				t.Fatal("original open", err)
			}
			t.Cleanup(func() { _ = reader.Close() })
			if v.h.originalValues == nil || v.h.originalValues.Value(cleanupOriginalValueKey{}) != a.witness || v.h.originalValues.Value(cleanupCallbackOnlyKey{}) != nil {
				t.Fatal("Open retained a transaction callback or lost original values")
			}
			if mode == "cancelled-parent" {
				cancel()
			}
			if mode == "failed-tail-then-cancel" {
				a.denyRelease = true
			}
			closeErr := reader.Close()
			if mode == "failed-tail-then-cancel" {
				if !hasCode(closeErr, f.Forbidden) || v.releases != 0 {
					t.Fatal("current tail denial was bypassed", closeErr)
				}
				a.denyRelease = false
				cancel()
				// A later stop caller lacks the witness; only the already admitted
				// reader's saved outer context can authorize its own cleanup.
				if err := v.reads.CancelSourceLease(context.Background(), lease); err != nil {
					t.Fatal("original tail did not converge", err)
				}
			} else if closeErr != nil && !(mode == "cancelled-parent" && errors.Is(closeErr, context.Canceled)) {
				t.Fatal("original reader close", closeErr)
			}
			awaitSourceCleanupReturn(t, v.service)
			if v.releases != 1 || a.tailChecks != 1 || !body.closed || !v.h.joined || !v.h.released || cleanupOriginalRegistered(v.service) != 0 || len(v.service.state().operations) != 0 || len(v.reads.data().leases) != 0 {
				t.Fatal("original reader/release/cleanup did not actually join")
			}
		})
	}
}

func TestSourceReadsUnopenedCancelUsesCurrentOuterValues(t *testing.T) {
	v, a, lease, body := newSourceCleanupFixture(t)
	if err := v.reads.CancelSourceLease(context.Background(), lease); !hasCode(err, f.Forbidden) || v.releases != 0 || a.tailChecks != 0 {
		t.Fatal("bare cancellation context manufactured source authority", err)
	}
	original := context.WithValue(context.Background(), cleanupOriginalValueKey{}, a.witness)
	if err := v.reads.CancelSourceLease(original, lease); err != nil {
		t.Fatal("unopened lease lost the actual cancellation's values", err)
	}
	awaitSourceCleanupReturn(t, v.service)
	if v.releases != 1 || a.tailChecks != 1 || body.reads != 0 || body.closed || !v.h.joined || !v.h.released || cleanupOriginalRegistered(v.service) != 0 || len(v.reads.data().leases) != 0 || v.h.originalValues != nil {
		t.Fatal("unopened cancel fabricated a reader or retained a cleanup")
	}
}
