package work

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

func TestTaskRelaunchFailureOriginsKeepLegacyEncoding(t *testing.T) {
	old, _ := failureControlRecord(t)
	oldRequest, err := json.Marshal(struct {
		Claim           c.TaskClaimRequest
		DispatchVersion f.Version
		LaunchAttempt   int64
	}{old.Request.Claim, old.Request.DispatchVersion, old.Request.LaunchAttempt})
	if err != nil {
		t.Fatal(err)
	}
	oldFacts, err := json.Marshal(struct {
		Guard      c.TaskClaimGuard
		Reason     c.TaskLaunchFailureReason
		OccurredAt f.Instant
	}{old.Facts.Guard, old.Facts.Reason, old.Facts.OccurredAt})
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range []struct {
		value any
		old   []byte
	}{{old.Request, oldRequest}, {old.Facts, oldFacts}} {
		raw, e := json.Marshal(pair.value)
		if e != nil || !bytes.Equal(raw, pair.old) {
			t.Fatal("historical claim bytes changed", e)
		}
	}
	r, _ := relaunchControlRequest(t)
	source := c.TaskRelaunchSource{Request: r, MilestoneID: old.Before.MilestoneID, ReferenceDigest: digest([]byte("controlled relaunch source"))}
	request := c.TaskLaunchFailureRequest{Relaunch: &r, DispatchVersion: 3, LaunchAttempt: 1}
	facts := c.TaskLaunchFailureFacts{Relaunch: &source, Reason: c.TaskLaunchFailureUnsupportedResourceConstraints, OccurredAt: old.CreatedAt}
	if request.Validate() != nil || facts.ValidateFor(request) != nil || old.Facts.ValidateFor(old.Request) != nil {
		t.Fatal("coherent origins rejected")
	}
	raw, err := json.Marshal(request)
	var decoded c.TaskLaunchFailureRequest
	if err != nil || json.Unmarshal(raw, &decoded) != nil || !request.Equal(decoded) || bytes.Contains(raw, []byte(`"Claim"`)) {
		t.Fatal("relaunch request roundtrip", err)
	}
	factRaw, err := json.Marshal(facts)
	var decodedFacts c.TaskLaunchFailureFacts
	if err != nil || json.Unmarshal(factRaw, &decodedFacts) != nil || decodedFacts.ValidateFor(decoded) != nil || !sameValue(decodedFacts, facts) || bytes.Contains(factRaw, []byte(`"Guard"`)) {
		t.Fatal("relaunch facts roundtrip", err)
	}
	requestCopy, factsCopy := request.Clone(), facts.Clone()
	requestCopy.Relaunch.RequestID = pureID[f.Request](t, 140)
	factsCopy.Relaunch.ReferenceDigest = digest([]byte("another source"))
	if request.Equal(requestCopy) || request.Relaunch.RequestID != r.RequestID || facts.Relaunch.ReferenceDigest != source.ReferenceDigest {
		t.Fatal("mutable request/facts alias")
	}
	if facts.ValidateFor(old.Request) == nil || old.Facts.ValidateFor(request) == nil {
		t.Fatal("claim and relaunch facts were interchangeable")
	}
	for _, sample := range []struct {
		raw  []byte
		key  string
		old  any
		read func([]byte) error
	}{
		{raw, "Claim", old.Request.Claim, func(b []byte) error { var v c.TaskLaunchFailureRequest; return json.Unmarshal(b, &v) }},
		{factRaw, "Guard", old.Facts.Guard, func(b []byte) error { var v c.TaskLaunchFailureFacts; return json.Unmarshal(b, &v) }},
	} {
		for _, mode := range []string{"both", "null", "extra"} {
			var fields map[string]json.RawMessage
			if json.Unmarshal(sample.raw, &fields) != nil {
				t.Fatal("control JSON")
			}
			switch mode {
			case "both":
				fields[sample.key], err = json.Marshal(sample.old)
			case "null":
				fields["Relaunch"] = json.RawMessage(`null`)
			case "extra":
				fields["grant"] = json.RawMessage(`true`)
			}
			changed, e := json.Marshal(fields)
			if err != nil || e != nil || sample.read(changed) == nil {
				t.Fatal("ambiguous origin accepted", mode, err, e)
			}
		}
	}
}

func TestTaskRelaunchFailureSchemaDoesNotRewriteClaimHistory(t *testing.T) {
	old, _ := failureControlRecord(t)
	r, actor := relaunchControlRequest(t)
	_, current, _, _, _, _, _ := newTaskLaunchControl(t)
	r.ExpectedTaskVersion = old.Before.Version
	origin := taskRelaunchRecord{Request: r, Task: old.Before.Clone(), Sprint: old.Sprint.Clone(), Milestone: current.milestone, CreatedAt: old.CreatedAt}
	source := origin.source()
	request := c.TaskLaunchFailureRequest{Relaunch: &r, DispatchVersion: 3, LaunchAttempt: 1}
	facts := c.TaskLaunchFailureFacts{Relaunch: &source, Reason: old.Facts.Reason, OccurredAt: old.Facts.OccurredAt}
	record, err := buildTaskFailureRecord(request, facts, schedulerClaimRecord{}, old.Before, old.Sprint, old.CurrentSprintID, old.Groups, old.QueryGeneration, old.Blocker.ID, old.Event.TaskEventIDs, old.Header.EventID, old.CreatedAt, &origin)
	if err != nil || !record.Changed || record.Relaunch == nil || record.RelaunchEvent == nil || record.Event != nil || record.Header == nil || record.Header.SchemaVersion != c.TaskRelaunchFailureSchemaVersion {
		t.Fatal("actual relaunch failure record construction", err)
	}
	recordRaw, err := canonical(record)
	var decoded taskFailureRecord
	var recordFields map[string]json.RawMessage
	if err != nil || json.Unmarshal(recordRaw, &decoded) != nil || validateTaskFailureRecord(&decoded) != nil || !sameValue(record, decoded) || json.Unmarshal(recordRaw, &recordFields) != nil || recordFields["claim"] != nil || recordFields["relaunch"] == nil || recordFields["relaunch_event"] == nil {
		t.Fatal("actual schema5 record roundtrip or origin exclusivity", err)
	}
	decoded.Facts.Relaunch.ReferenceDigest = digest([]byte("different persisted origin"))
	if validateTaskFailureRecord(&decoded) == nil {
		t.Fatal("failure record lost its exact relaunch parent")
	}
	payload, header := *record.RelaunchEvent, *record.Header
	catalog := event.NewCatalog()
	factory, err := c.RegisterTaskLaunchFailureEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := c.RegisterSchedulerClaimEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	value, err := factory.NewTaskRelaunchFailed(header, payload)
	if err != nil {
		t.Fatal("schema5 event construction", err)
	}
	raw := value.PayloadBytes()
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != 14 || fields["dispatch_id"] == nil || fields["origin"] == nil || fields["source"] == nil || fields["claim_id"] != nil {
		t.Fatal("relaunch serialized a fictitious claim")
	}
	restored, err := factory.Restore(header, raw)
	if err != nil || !bytes.Equal(raw, restored.PayloadBytes()) {
		t.Fatal("schema5 strict roundtrip", err)
	}
	legacy, err := factory.NewTaskLaunchFailed(*old.Header, *old.Event)
	if err != nil {
		t.Fatal("schema4 stopped constructing", err)
	}
	legacyRaw, err := json.Marshal(old.Event)
	if err != nil || !bytes.Equal(legacyRaw, legacy.PayloadBytes()) {
		t.Fatal("schema4 payload changed", err)
	}
	if _, err = factory.Restore(*old.Header, legacy.PayloadBytes()); err != nil {
		t.Fatal("schema4 history stopped restoring", err)
	}
	claim, _ := claimControlRecord(t)
	claimEvent, err := claims.NewTaskClaimed(claim.Header, claim.Event)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = claims.Restore(claim.Header, claimEvent.PayloadBytes()); err != nil {
		t.Fatal("schema2 history stopped restoring", err)
	}
	if _, err = factory.Restore(*old.Header, raw); err == nil {
		t.Fatal("schema5 relaunch entered schema4 claim arm")
	}
	if _, err = factory.Restore(header, legacy.PayloadBytes()); err == nil {
		t.Fatal("schema4 claim entered schema5 relaunch arm")
	}
	for _, mutate := range []func(*c.TaskRelaunchFailed){
		func(v *c.TaskRelaunchFailed) { v.Origin = c.TaskDispatchTodoClaim },
		func(v *c.TaskRelaunchFailed) { v.Source.Request.DispatchID = claim.Request.DispatchID },
		func(v *c.TaskRelaunchFailed) { v.SourcePosition.PreviousID = &r.TaskID },
	} {
		bad := payload.Clone()
		mutate(&bad)
		if _, err = factory.NewTaskRelaunchFailed(header, bad); err == nil {
			t.Fatal("incoherent relaunch event accepted")
		}
	}
	wrongProject := header
	wrongProject.Scope.ProjectID = pureID[i.Project](t, 141)
	if _, err = factory.NewTaskRelaunchFailed(wrongProject, payload); err == nil {
		t.Fatal("event crossed Project scope")
	}
	store, deps := failureControlPorts(t)
	_, err = deps.Authority.DiscoverAppend(context.Background(), actor, value.Summary())
	pureCode(t, err, f.Forbidden)
	if store.touches.Load() != 0 {
		t.Fatal("typed schema5 payload replaced actual Work applied witness")
	}
}
