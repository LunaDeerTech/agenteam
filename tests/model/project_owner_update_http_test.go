//go:build integration

package model_test

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"strings"
	"sync/atomic"
	"testing"
)

func TestModelProjectOwnerUpdateHTTPStrictAndReplay(t *testing.T) {
	projectOwnerReadTop(t)
	v := newProjectUpdateFixture(t)
	path := projectOwnerReadPath(v.project.ID)
	lookup := projectUpdateLookupPath(v.project.ID)
	before := projectUpdateSnapshot(t, v.projectUsageHTTPFixture)
	name := "Updated-Owner"
	desc := strings.Repeat("&", 8192)
	body := projectUpdateBody(t, 1, &name, &desc)
	first := v.write(t, v.ownerBrowser, "PATCH", path, "original", body).want(t, 200)
	if len(first.object(t)) != 11 || first.object(t)["version"] != "2" || first.object(t)["description"] != desc {
		t.Fatal("incomplete response")
	}
	after := projectUpdateSnapshot(t, v.projectUsageHTTPFixture)
	if after.Completed != before.Completed+1 || after.Audits != before.Audits+1 || after.Events != before.Events+1 {
		t.Fatal("exact atomic facts")
	}
	replay := v.write(t, v.ownerBrowser, "PATCH", path, "original", body).want(t, 200)
	if !bytes.Equal(replay.body, first.body) || projectUpdateSnapshot(t, v.projectUsageHTTPFixture) != after {
		t.Fatal("replay changed facts")
	}
	observed := v.write(t, v.ownerBrowser, "POST", lookup, "original", projectUpdateLookupBody).want(t, 200)
	if observed.object(t)["state"] != "committed" {
		t.Fatal("lookup missing receipt")
	}
	v.write(t, v.ownerBrowser, "PATCH", path, "original", projectUpdateBody(t, 1, &name, nil)).problem(t, 409, f.IdempotencyKeyReused)
	empty := ""
	second := v.write(t, v.ownerBrowser, "PATCH", path, "later", projectUpdateBody(t, 2, nil, &empty)).want(t, 200)
	if second.object(t)["version"] != "3" {
		t.Fatal("later version")
	}
	history := v.write(t, v.ownerBrowser, "POST", lookup, "original", projectUpdateLookupBody).want(t, 200)
	if !bytes.Equal(observed.body, history.body) {
		t.Fatal("lookup overwritten with current")
	}
	current := v.request(t, v.ownerBrowser, "GET", path).want(t, 200)
	if current.object(t)["description"] != "" {
		t.Fatal("current GET did not advance")
	}
	v.request(t, v.ownerBrowser, "GET", projectUsageResolve(v.ownerName, v.project.Name)).problem(t, 404, f.NotFound)
	v.request(t, v.ownerBrowser, "GET", projectUsageResolve(v.ownerName, name)).want(t, 200)
	noopBefore := projectUpdateSnapshot(t, v.projectUsageHTTPFixture)
	v.write(t, v.ownerBrowser, "PATCH", path, "noop", projectUpdateBody(t, 3, nil, &empty)).want(t, 200)
	noopAfter := projectUpdateSnapshot(t, v.projectUsageHTTPFixture)
	if noopAfter.Project != noopBefore.Project || noopAfter.Completed != noopBefore.Completed+1 || noopAfter.Audits != noopBefore.Audits || noopAfter.Events != noopBefore.Events {
		t.Fatal("no-op fabricated mutation")
	}
	v.write(t, v.ownerBrowser, "POST", lookup, "unseen", projectUpdateLookupBody).want(t, 200)
	fixed := projectUpdateSnapshot(t, v.projectUsageHTTPFixture)
	for _, raw := range []string{`{"expected_version":"3","description":null}`, `{"expected_version":"3"}`, `{"expected_version":"3","Name":"bad"}`, `{"expected_version":"3","description":"","description":"x"}`, `{"expected_version":3,"description":""}`, `{"expected_version":"3","name":".."}`} {
		v.write(t, v.ownerBrowser, "PATCH", path, "invalid", []byte(raw)).problem(t, 400, f.InvalidArgument)
	}
	for _, command := range []string{"create", "archive", "restore", "delete", "retry-lifecycle"} {
		v.write(t, v.ownerBrowser, "POST", lookup, "original", projectUpdateJSON(t, map[string]string{"command": command})).problem(t, 400, f.InvalidArgument)
	}
	if projectUpdateSnapshot(t, v.projectUsageHTTPFixture) != fixed {
		t.Fatal("invalid changed facts")
	}
	renewed := v.login(t, v.ownerBrowser.email)
	if !bytes.Equal(v.write(t, renewed, "PATCH", path, "original", body).want(t, 200).body, first.body) {
		t.Fatal("new Session lost original receipt")
	}
	projectUpdateExport(t, "patch", "PATCH", path, first)
	projectUpdateExport(t, "lookup", "POST", lookup, observed)
	t.Log("formal current Owner; full 8192-byte description; atomic facts, no-op and historical replay; presence remains part of intent")
}
func TestModelProjectOwnerUpdateHTTPAuthorityAndAtomicity(t *testing.T) {
	projectOwnerReadTop(t)
	v := newProjectUpdateFixture(t)
	path := projectOwnerReadPath(v.project.ID)
	lookup := projectUpdateLookupPath(v.project.ID)
	name := "atomic-owner"
	body := projectUpdateBody(t, 1, &name, nil)
	for _, b := range []systemHTTPBrowser{v.adminBrowser, v.otherBrowser} {
		v.write(t, b, "PATCH", path, "private", body).problem(t, 404, f.NotFound)
		v.write(t, b, "POST", lookup, "private", projectUpdateLookupBody).problem(t, 404, f.NotFound)
	}
	v.write(t, systemHTTPBrowser{}, "PATCH", path, "private", body).problem(t, 401, f.Unauthenticated)
	before := projectUpdateSnapshot(t, v.projectUsageHTTPFixture)
	var reached atomic.Bool
	v.tracked.hooks(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
		x, e := v.tracked.InTx(tx)
		if e != nil {
			return e
		}
		var state string
		e = x.QueryRow(ctx, `SELECT state FROM agenteam_project.commands WHERE project_id=$1 AND command_name='update' AND key='atomic'`, v.project.ID.String()).Scan(&state)
		if e == nil && state == "completed" {
			var audits, events int
			if e = x.QueryRow(ctx, `SELECT (SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1 AND action='project.update'),(SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1 AND event_type='project.updated')`, v.project.ID.String()).Scan(&audits, &events); e != nil {
				return e
			}
			if audits < 1 || events < 1 {
				return f.NewFault(f.InternalError, f.NotStarted)
			}
			reached.Store(true)
			return f.NewFault(f.DependencyUnavailable, f.NotCommitted)
		}
		return nil
	}, nil)
	v.write(t, v.ownerBrowser, "PATCH", path, "atomic", body).want(t, 503)
	v.tracked.hooks(nil, nil)
	if !reached.Load() || projectUpdateSnapshot(t, v.projectUsageHTTPFixture) != before {
		t.Fatal("Append then rollback did not preserve all facts")
	}
	var state string
	if e := v.raw.QueryRow(testContext(t), `SELECT state FROM agenteam_project.commands WHERE project_id=$1 AND key='atomic'`, v.project.ID.String()).Scan(&state); e != nil || state != "planned" {
		t.Fatal("durable planning vanished", e)
	}
	v.write(t, v.ownerBrowser, "POST", lookup, "atomic", projectUpdateLookupBody).want(t, 200)
	for _, gate := range []string{"producer", "project"} {
		v.gates.reject = gate
		v.write(t, v.ownerBrowser, "PATCH", path, "atomic", body).want(t, 403)
		if projectUpdateSnapshot(t, v.projectUsageHTTPFixture) != before {
			t.Fatal("real gate rejection changed facts")
		}
	}
	for _, gate := range []string{"audit-cause", "audit-fields", "producer-summary", "project-summary"} {
		v.gates.reject = gate
		count := v.gates.realRejected.Load()
		response := v.write(t, v.ownerBrowser, "PATCH", path, "atomic", body)
		if response.status == 200 || v.gates.realRejected.Load() != count+1 || projectUpdateSnapshot(t, v.projectUsageHTTPFixture) != before {
			t.Fatal("bad fact did not reach actual validator", gate, response.status)
		}
	}
	v.gates.reject = ""
	v.write(t, v.ownerBrowser, "PATCH", path, "atomic", body).want(t, 200)
	if v.gates.audits.Load() == 0 || v.gates.producerCurrent.Load() == 0 || v.gates.producerNew.Load() == 0 || v.gates.projectCurrent.Load() == 0 || v.gates.projectNew.Load() == 0 {
		t.Fatal("real Audit/producer/Project current+new gates not reached")
	}
	revoked := v.login(t, v.ownerBrowser.email)
	if e := v.core.Logout(testContext(t), account.LogoutRequest{Actor: revoked.actor, Key: f.IdempotencyKey(newID[struct{}](t).String())}); e != nil {
		t.Fatal(e)
	}
	v.write(t, revoked, "PATCH", path, "atomic", body).problem(t, 401, f.SessionRevoked)
	v.write(t, revoked, "POST", lookup, "atomic", projectUpdateLookupBody).problem(t, 401, f.SessionRevoked)
	// Existing receipt remains visible after a legitimate lifecycle transition;
	// new mutations use the original gate. This test-only lifecycle registry does
	// not enter the default root or open a lifecycle HTTP operation.
	archived, e := v.projectService.BeginArchive(testContext(t), v.owner, f.CommandMeta{RequestID: newID[f.Request](t), IdempotencyKey: "archive-for-replay", ExpectedVersion: projectUpdatePtr(f.Version(2))}, v.project.ID)
	if e != nil {
		t.Fatal(e)
	}
	_ = archived
	v.write(t, v.ownerBrowser, "PATCH", path, "atomic", body).want(t, 200)
	v.write(t, v.ownerBrowser, "PATCH", path, "new-after-archive", projectUpdateBody(t, 3, nil, projectUpdatePtr("new"))).want(t, 409)
	var p pc.ProjectRef
	if json.Unmarshal(v.write(t, v.ownerBrowser, "PATCH", path, "atomic", body).body, &p) != nil || p.Lifecycle != pc.Active {
		t.Fatal("receipt replaced by lifecycle state")
	}
}
