package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func accessValues(t *testing.T) (identity.Actor, ObjectOwner, ObjectID) {
	t.Helper()
	u, _ := foundation.ParseID[identity.User](testID)
	session, _ := foundation.ParseID[identity.Session](secondID)
	actor, _ := identity.NewHuman(u, session)
	owner, _ := NewObjectOwner(Artifact, secondID, testID)
	object, _ := foundation.ParseID[StoredObject](secondID)
	return actor, owner, object
}
func TestAccessRequestsBindCompleteOperationAndCopyMutableInput(t *testing.T) {
	actor, owner, object := accessValues(t)
	payload, _ := foundation.ParseID[Payload](testID)
	requestID, _ := foundation.ParseID[foundation.Request](testID)
	prepared, _ := NewPreparedPayload(PreparedDetails{ID: payload, MediaType: "text/plain", Length: 1, SHA256: foundation.Digest("sha256:" + strings.Repeat("a", 64))})
	version := foundation.Version(1)
	meta := foundation.CommandMeta{RequestID: requestID, IdempotencyKey: "hidden-command", ExpectedVersion: &version}
	details := AccessRequestDetails{Operation: ReserveAccess, Actor: actor, Owner: owner, Intent: identity.Mutate, Command: &meta, Prepared: prepared}
	first, err := NewOwnerAccess(details)
	if err != nil {
		t.Fatal(err)
	}
	equal, err := NewOwnerAccess(details)
	if err != nil || !first.Equal(equal) {
		t.Fatal("equivalent requests differ", err)
	}
	version = 2
	changed, _ := NewOwnerAccess(details)
	if first.Equal(changed) || *first.Details().Command.ExpectedVersion != 1 {
		t.Fatal("expected-version identity/copy lost")
	}
	projected := first.Details()
	*projected.Command.ExpectedVersion = 7
	projected.Command.IdempotencyKey = "different"
	if *first.Details().Command.ExpectedVersion != 1 || first.Details().Command.IdempotencyKey != "hidden-command" {
		t.Fatal("Details changed request")
	}
	for _, mutate := range []func(*AccessRequestDetails){
		func(d *AccessRequestDetails) { d.Command.IdempotencyKey = "other" },
		func(d *AccessRequestDetails) {
			id, _ := foundation.NewID[foundation.Request]()
			d.Command.RequestID = id
		},
		func(d *AccessRequestDetails) {
			p := d.Prepared.Details()
			p.MediaType = "application/octet-stream"
			d.Prepared, _ = NewPreparedPayload(p)
		},
		func(d *AccessRequestDetails) { d.Owner, _ = NewObjectOwner(Knowledge, secondID, testID) },
	} {
		d := first.Details()
		mutate(&d)
		other, e := NewOwnerAccess(d)
		if e != nil || first.Equal(other) {
			t.Fatal("distinct complete operation collapsed", e)
		}
	}
	for _, d := range []AccessRequestDetails{
		{Operation: ReserveAccess, Actor: actor, Owner: owner, Intent: identity.Mutate, Command: &meta},
		{Operation: PrepareAccess, Actor: actor, Owner: owner, Intent: identity.Mutate, ObjectID: object},
		{Operation: ReleaseAccess, Actor: actor, Owner: owner, Intent: identity.Read, ObjectID: object},
		{Operation: "future_operation", Actor: actor, Owner: owner, Intent: identity.Mutate},
		{Kind: MaintenanceAccess, Operation: PrepareAccess, Actor: actor, Owner: owner, Intent: identity.Mutate},
	} {
		if _, e := NewOwnerAccess(d); e == nil {
			t.Fatal("open request union accepted", d.Operation)
		}
	}
	r := ByteRange{Offset: 1, Length: 2}
	read, _ := NewObjectReadAccess(AccessRequestDetails{Operation: ReadAccess, Actor: actor, Owner: owner, Intent: identity.Read, ObjectID: object, Range: &r})
	r.Offset = 9
	projected = read.Details()
	projected.Range.Length = 99
	if read.Details().Range.Offset != 1 || read.Details().Range.Length != 2 {
		t.Fatal("range aliases caller")
	}
	project, _ := foundation.ParseID[identity.Project](testID)
	operation, _ := foundation.ParseID[CleanupOperation](secondID)
	cause, _ := NewProjectCleanupCause(ProjectCleanupDetails{ProjectID: project, OperationID: operation, Version: 1})
	objects := []ObjectID{object}
	batch, e := NewProjectCleanupAccess(AccessRequestDetails{Operation: FinishProjectAccess, Actor: actor, ProjectCleanup: cause, Objects: objects})
	if e != nil {
		t.Fatal(e)
	}
	objects[0] = ObjectID{}
	batch.Details().Objects[0] = ObjectID{}
	if batch.Details().Objects[0] != object {
		t.Fatal("batch aliases caller")
	}
}
func TestAccessPlanTokenExactIssuerTxSetModesAndSafeProjection(t *testing.T) {
	actor, owner, object := accessValues(t)
	request, _ := NewOwnerAccess(AccessRequestDetails{Operation: ReleaseAccess, Actor: actor, Owner: owner, Intent: identity.Converge, ObjectID: object})
	otherRequest, _ := NewOwnerAccess(AccessRequestDetails{Operation: AttachAccess, Actor: actor, Owner: owner, Intent: identity.Mutate, ObjectID: object})
	key, _ := foundation.AggregateLock(foundation.ObjectAggregate, object.String())
	user, _ := foundation.UserLock(testID)
	original := []foundation.LockRequest{{Key: user, Mode: foundation.Shared}}
	mapping := foundation.Digest("sha256:" + strings.Repeat("b", 64))
	deps, err := NewAccessDependencies(mapping, original)
	if err != nil {
		t.Fatal(err)
	}
	original[0].Mode = foundation.Exclusive
	deps.Locks()[0].Mode = foundation.Exclusive
	if deps.Locks()[0].Mode != foundation.Shared {
		t.Fatal("dependency slice aliased")
	}
	issuer, other := NewAccessIssuer(), NewAccessIssuer()
	tx := foundation.NewTx()
	details := AccessPlanDetails{Request: request, DependencyRequest: request, Dependencies: deps, DomainBinding: mapping, Objects: []ObjectID{object}, Locks: []foundation.LockRequest{{Key: user, Mode: foundation.Shared}, {Key: key, Mode: foundation.Shared}}}
	plan, err := NewAccessLockPlan(issuer, details)
	if err != nil {
		t.Fatal(err)
	}
	details.Objects[0] = ObjectID{}
	details.Locks[1].Mode = foundation.Exclusive
	if plan.Details().Objects[0] != object || plan.Details().Locks[1].Mode != foundation.Shared {
		t.Fatal("plan input aliases")
	}
	sameFields, err := NewAccessLockPlan(issuer, plan.Details())
	if err != nil {
		t.Fatal(err)
	}
	secondDetails := plan.Details()
	secondDetails.Request = otherRequest
	secondDetails.DependencyRequest = otherRequest
	secondDetails.Locks[1].Mode = foundation.Exclusive
	second, err := NewAccessLockPlan(issuer, secondDetails)
	if err != nil {
		t.Fatal(err)
	}
	extra := []foundation.LockRequest{{Key: user, Mode: foundation.Exclusive}}
	token, err := NewLockedAccess(issuer, tx, []AccessLockPlan{plan, second}, extra)
	if err != nil {
		t.Fatal(err)
	}
	extra[0].Mode = foundation.Shared
	token.Locks()[0].Mode = foundation.Shared
	for _, r := range token.Locks() {
		if r.Mode != foundation.Exclusive {
			t.Fatal("strongest union mode lost")
		}
	}
	if !token.Matches(issuer, tx, plan, request) || !token.Matches(issuer, tx, second, otherRequest) {
		t.Fatal("included plan rejected")
	}
	for _, ok := range []bool{token.Matches(other, tx, plan, request), token.Matches(issuer, foundation.NewTx(), plan, request), token.Matches(issuer, tx, sameFields, request), token.Matches(issuer, tx, plan, otherRequest), (LockedAccess{}).Matches(issuer, tx, plan, request)} {
		if ok {
			t.Fatal("different issuer/Tx/plan/request token accepted")
		}
	}
	if _, err = NewLockedAccess(other, tx, []AccessLockPlan{plan}, nil); err == nil {
		t.Fatal("public constructor impersonated issuer")
	}
	for _, v := range []any{request, deps, issuer, plan, token} {
		var log bytes.Buffer
		slog.New(slog.NewTextHandler(&log, nil)).Info("projection", "value", v)
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range []string{fmt.Sprintf("%#v", v), fmt.Sprintf("%+v", struct{ private any }{v}), string(raw), log.String()} {
			if strings.Contains(s, testID) || strings.Contains(s, secondID) || strings.Contains(s, "hidden-command") || strings.Contains(s, string(mapping)) {
				t.Fatal("opaque access data disclosed", s)
			}
		}
	}
	for _, v := range []any{new(AccessRequest), new(AccessDependencies), new(AccessIssuer), new(AccessLockPlan), new(LockedAccess)} {
		if json.Unmarshal([]byte(`{"held":true}`), v) == nil {
			t.Fatal("JSON manufactured access")
		}
	}
}

func TestAccessClosedLeaseCleanupSourceAndMaintenanceVariants(t *testing.T) {
	actor, owner, object := accessValues(t)
	process, _ := foundation.ParseID[Process](testID)
	operation, _ := foundation.ParseID[CleanupOperation](testID)
	leaseOwner, _ := NewLeaseOwner(HistoryOwner, secondID)
	lease, err := NewLeaseAccess(AccessRequestDetails{Operation: AcquireUseAccess, Actor: actor, ObjectID: object, LeaseOwner: leaseOwner})
	if err != nil {
		t.Fatal(err)
	}
	release, _ := NewLeaseAccess(AccessRequestDetails{Operation: ReleaseUseAccess, Actor: actor, ObjectID: object, LeaseOwner: leaseOwner})
	if lease.Equal(release) {
		t.Fatal("lease action omitted from identity")
	}
	readerOwner, _ := NewLeaseOwner(ReaderOwner, secondID)
	if _, err = NewLeaseAccess(AccessRequestDetails{Operation: AcquireUseAccess, Actor: actor, ObjectID: object, LeaseOwner: readerOwner}); err == nil {
		t.Fatal("business lease forged internal reader")
	}
	cleanup, _ := NewObjectCleanupCause(CleanupDetails{OperationID: operation, Owner: owner, Reason: OwnerDeleted})
	if _, err = NewObjectCleanupAccess(AccessRequestDetails{Operation: CleanupObjectAccess, ObjectID: object, Cleanup: cleanup}); err != nil {
		t.Fatal(err)
	}
	if _, err = NewObjectCleanupAccess(AccessRequestDetails{Operation: CleanupObjectAccess, Actor: actor, ObjectID: object, Cleanup: cleanup}); err == nil {
		t.Fatal("cleanup borrowed unrelated actor grant")
	}
	maintenance, err := NewMaintenanceAccess(AccessRequestDetails{Operation: InspectAccess, InstanceID: process, ObjectID: object})
	if err != nil {
		t.Fatal(err)
	}
	if maintenance.Equal(lease) {
		t.Fatal("maintenance collapsed into a business request")
	}
	if _, err = NewMaintenanceAccess(AccessRequestDetails{Operation: InspectAccess, InstanceID: process, ObjectID: object, Owner: owner}); err == nil {
		t.Fatal("maintenance invented a caller owner")
	}
	project, _ := foundation.ParseID[identity.Project](testID)
	payloadOwner, _ := NewObjectOwner(ExecutionPayload, secondID, testID)
	ref, _ := NewBusinessFileRef(BusinessFileDetails{Kind: ExecutionFile, ProjectID: project, ExecutionID: testID, PayloadID: secondID})
	at, _ := foundation.ParseInstant("2026-10-03T00:00:00.000000Z")
	meta := ObjectMeta{ID: object, Scope: payloadOwner.Scope(), MediaType: "text/plain", ByteSize: 1, SHA256: foundation.Digest("sha256:" + strings.Repeat("c", 64)), State: Available, Version: 1, CreatedAt: at}
	source, err := NewResolvedSource(ResolvedSourceDetails{Reference: ref, Owner: payloadOwner, Meta: meta, Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	request, err := NewSourceAccess(AccessRequestDetails{Operation: ValidateSourceAccess, Actor: actor, Source: source})
	if err != nil {
		t.Fatal(err)
	}
	third, _ := foundation.NewID[identity.Execution]()
	rd := ref.Details()
	rd.ExecutionID = third.String()
	otherRef, _ := NewBusinessFileRef(rd)
	sd := source.Details()
	sd.Reference = otherRef
	otherSource, err := NewResolvedSource(sd)
	if err != nil {
		t.Fatal(err)
	}
	otherRequest, err := NewSourceAccess(AccessRequestDetails{Operation: ValidateSourceAccess, Actor: actor, Source: otherSource})
	if err != nil || request.Equal(otherRequest) {
		t.Fatal("source lost separate ExecutionID/PayloadID binding", err)
	}
	if _, err = NewSourceAccess(AccessRequestDetails{Operation: ValidateSourceAccess, Actor: actor, Source: source, ObjectID: object}); err == nil {
		t.Fatal("raw ObjectID supplemented fixed source")
	}
}
