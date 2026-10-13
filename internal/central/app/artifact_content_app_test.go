//go:build integration

package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// Exercise the installed default App, current Human Session and all three HTTP
// commands. ACK controls wrap original Store returns; they do not change SQL,
// fabricate a receipt or interrupt the Object network.
func TestArtifactContentDefaultApp(t *testing.T) {
	t.Run("create-original-lookup-retry-cancel-and-owner", artifactContentApp)
}

type artifactContentIntent struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Media       string `json:"media_type"`
	Content     string `json:"content"`
}

type artifactContentLookup struct {
	State    ac.CreationState `json:"state"`
	Artifact json.RawMessage  `json:"artifact"`
}

func artifactContentCommandPath(p id.ProjectID, action string) string {
	base := artifactOwnerHTTPListPath(p)
	if action == "create" {
		return base + "/create-content"
	}
	return base + "/creation-commands/" + action
}

func artifactContentPost(t *testing.T, v *humanArtifactApp, p id.ProjectID, meta f.CommandMeta, input artifactContentIntent, action string, status int) artifactOwnerHTTPResponse {
	t.Helper()
	r, err := artifactOwnerHTTPRequest(v.ctx, v.client, v.address, v.cookie, "POST", artifactContentCommandPath(p, action), workAppJSON(t, input), v.csrf, string(meta.IdempotencyKey))
	workAppCheck(t, "artifact-content-original-post-return", err)
	if r.status != status {
		t.Fatalf("Artifact content action=%s status=%d wanted=%d", action, r.status, status)
	}
	length, err := strconv.Atoi(r.header.Get("Content-Length"))
	media := "application/json"
	if status != http.StatusOK {
		media = "application/problem+json"
	}
	if err != nil || length != len(r.body) || r.header.Get("Cache-Control") != "no-store" || r.header.Get("Content-Type") != media {
		t.Fatal("Artifact command original representation incomplete")
	}
	return r
}

// Select exactly the committed, read-only missing-command lookup that precedes
// beginCommand. The real callback and COMMIT ran, but its typed ACK is unknown.
// A missing command is not a reservation and must never trigger CancelCreation.
func artifactContentMissingACK(p id.ProjectID, key f.IdempotencyKey) *usageAppHook {
	h := &usageAppHook{mask: true, match: func(c f.TransactionCause) bool { return humanArtifactCommand(c, p, key) }}
	h.selectRow = func(ctx context.Context, x postgres.SQLExecutor, _ f.TransactionCause, before, after int64) (bool, error) {
		var missing bool
		err := x.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM agenteam_artifact.commands WHERE project_id=$1::uuid AND command_key=$2)`, p.String(), string(key)).Scan(&missing)
		return missing && before == after, err
	}
	return h
}

func artifactContentObservedACK(t *testing.T, h *usageAppHook, p id.ProjectID, key f.IdempotencyKey, readonly bool) usageAppObservation {
	t.Helper()
	select {
	case observed := <-h.observed:
		usageAppWait(t, observed.returned, "original content ACK Store return")
		if observed.result.State() != f.Committed || observed.masked.State() != f.Unknown || observed.masked.AttemptID().Validate() != nil || !humanArtifactCommand(observed.masked.Cause(), p, key) || (readonly && observed.before != observed.after) || (!readonly && observed.after <= observed.before) {
			t.Fatal("original content ACK stage/cause/write facts differ")
		}
		return observed
	default:
		t.Fatal("original content ACK was not reached")
	}
	return usageAppObservation{}
}

func artifactContentConverges(t *testing.T, v *humanArtifactApp, p id.ProjectID) int64 {
	t.Helper()
	var count int64
	workAppCheck(t, "artifact-content-converge-count", v.admin.QueryRow(v.ctx, `SELECT count(*) FROM agenteam_artifact.project_work WHERE project_id=$1::uuid AND kind='converge'`, p.String()).Scan(&count))
	return count
}

func artifactContentCanonical(t *testing.T, v *humanArtifactApp, meta f.CommandMeta, input artifactContentIntent) ac.Metadata {
	t.Helper()
	out, err := v.service.LookupCreation(v.ctx, v.invocation(t, v.a), meta, ac.Display{Name: input.Name, Description: input.Description}, input.Media, input.Content)
	workAppCheck(t, "artifact-content-formal-canonical-lookup", err)
	if out.State != ac.CreationCommitted || out.Artifact == nil {
		t.Fatal("formal lookup did not observe committed creation")
	}
	m := *out.Artifact
	d := m.Details()
	if m.Validate() != nil || d.Reference.ProjectID != v.a || d.Name != input.Name || d.Description != input.Description || d.Kind != ac.Generated || d.Object.MediaType != input.Media || d.Object.ByteSize != f.Progress(len(input.Content)) || string(d.Object.SHA256) != fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(input.Content))) || d.CreatedByUserID != v.actor.Details().UserID || d.CreatedByAgentID != "" || d.ExecutionID != "" || d.OperationID != "" {
		t.Fatal("HTTP creation changed original content/provenance")
	}
	return m
}

func artifactContentApp(t *testing.T) {
	v := newHumanArtifactApp(t)
	v.create(t, v.b, humanArtifactMeta(t), "Independent project B.\n")
	bBefore := v.snapshot(t, v.b)
	var created []ac.Metadata
	inputs := []artifactContentIntent{
		{"inline-世界.md", "Original whitespace", "text/markdown", "  # 世界🙂\n<script>literal()</script>\n"},
		{"ack-published.txt", "", "text/plain", "Committed before the ACK became unknown.\n"},
		{"ack-not-observed.txt", "", "text/plain", "One explicit retry of the same original request.\n"},
	}
	for index, input := range inputs {
		meta := humanArtifactMeta(t)
		var h *usageAppHook
		if index == 1 {
			h = humanArtifactPublishHook(v.a, meta.IdempotencyKey, true, false)
		}
		if index == 2 {
			h = artifactContentMissingACK(v.a, meta.IdempotencyKey)
		}
		if h != nil {
			v.tracked.arm(h)
			h.open()
		}
		status := http.StatusOK
		if h != nil {
			status = http.StatusServiceUnavailable
		}
		first := artifactContentPost(t, v, v.a, meta, input, "create", status)
		if h != nil {
			artifactContentObservedACK(t, h, v.a, meta.IdempotencyKey, index == 2)
			var problem struct {
				Code   f.Code        `json:"code"`
				Commit f.CommitState `json:"commit_state"`
				Retry  string        `json:"retry_hint"`
			}
			workAppCheck(t, "artifact-content-unknown-problem", json.Unmarshal(first.body, &problem))
			if problem.Code != f.CommitUnknown || problem.Commit != f.Unknown || problem.Retry != "lookup" {
				t.Fatal("HTTP lost original typed Unknown")
			}
		}
		beforeLookup := v.snapshot(t, v.a)
		var lookup artifactContentLookup
		artifactOwnerHTTPDecode(t, artifactContentPost(t, v, v.a, meta, input, "lookup", 200).body, &lookup)
		if v.snapshot(t, v.a) != beforeLookup {
			t.Fatal("explicit creation lookup changed business facts")
		}
		if index == 2 {
			if lookup.State != ac.CreationNotObserved || string(lookup.Artifact) != "null" {
				t.Fatal("missing original command falsely reported committed")
			}
			converges := artifactContentConverges(t, v, v.a)
			var cancel struct {
				State   ac.CreationState `json:"state"`
				Cleanup *string          `json:"cleanup"`
			}
			artifactOwnerHTTPDecode(t, artifactContentPost(t, v, v.a, meta, input, "cancel", 200).body, &cancel)
			if cancel.State != ac.CreationNotObserved || cancel.Cleanup != nil || artifactContentConverges(t, v, v.a) != converges || v.snapshot(t, v.a) != beforeLookup {
				t.Fatal("not_observed entered CancelCreation or wrote business facts")
			}
			first = artifactContentPost(t, v, v.a, meta, input, "create", 200)
		} else if lookup.State != ac.CreationCommitted {
			t.Fatal("explicit lookup did not recover original committed creation")
		}
		canonical := artifactContentCanonical(t, v, meta, input)
		created = append(created, canonical)
		if index != 1 {
			var response struct {
				Artifact json.RawMessage `json:"artifact"`
			}
			artifactOwnerHTTPDecode(t, first.body, &response)
			artifactOwnerHTTPMetadata(t, response.Artifact, canonical)
		} else {
			artifactOwnerHTTPMetadata(t, lookup.Artifact, canonical)
		}
		beforeReplay := v.snapshot(t, v.a)
		var replay struct {
			Artifact json.RawMessage `json:"artifact"`
		}
		artifactOwnerHTTPDecode(t, artifactContentPost(t, v, v.a, meta, input, "create", 200).body, &replay)
		artifactOwnerHTTPMetadata(t, replay.Artifact, canonical)
		if v.snapshot(t, v.a) != beforeReplay {
			t.Fatal("same original request replay duplicated creation or audit")
		}
		artifactContentPost(t, v, v.a, meta, input, "cancel", http.StatusConflict)
		if v.snapshot(t, v.a) != beforeReplay {
			t.Fatal("cancel of committed creation changed it")
		}
		if index == 0 {
			changed := input
			changed.Content += "different intent"
			converges := artifactContentConverges(t, v, v.a)
			for _, action := range []string{"lookup", "cancel"} {
				conflict := artifactContentPost(t, v, v.a, meta, changed, action, http.StatusConflict)
				var problem struct {
					Code f.Code `json:"code"`
				}
				workAppCheck(t, "artifact-content-original-intent-conflict", json.Unmarshal(conflict.body, &problem))
				if problem.Code != f.IdempotencyKeyReused {
					t.Fatal("changed original intent did not conflict")
				}
			}
			if artifactContentConverges(t, v, v.a) != converges || v.snapshot(t, v.a) != beforeReplay {
				t.Fatal("different intent reached CancelCreation or changed original")
			}
		}
	}

	meta := humanArtifactMeta(t)
	input := artifactContentIntent{"pending-cancel.txt", "", "text/plain", "Verified prospective bytes, never published.\n"}
	reject := &humanArtifactPublishRejection{project: v.a, key: meta.IdempotencyKey, err: f.NewFault(f.DependencyUnavailable, f.NotCommitted).WithCause(errors.New("controlled original final publication refusal"))}
	v.tracked.mu.Lock()
	v.tracked.reject = reject
	v.tracked.mu.Unlock()
	r := artifactContentPost(t, v, v.a, meta, input, "create", 503)
	v.tracked.mu.Lock()
	used, result := reject.used, reject.result
	v.tracked.mu.Unlock()
	var problem struct {
		Code   f.Code        `json:"code"`
		Commit f.CommitState `json:"commit_state"`
	}
	workAppCheck(t, "artifact-content-pending-problem", json.Unmarshal(r.body, &problem))
	if !used || result.State() != f.NotCommitted || problem.Code != f.DependencyUnavailable || problem.Commit != f.NotCommitted {
		t.Fatal("pending creation did not preserve actual final-publish refusal")
	}
	var pending artifactContentLookup
	artifactOwnerHTTPDecode(t, artifactContentPost(t, v, v.a, meta, input, "lookup", 200).body, &pending)
	if pending.State != ac.CreationPending || string(pending.Artifact) != "null" {
		t.Fatal("real prospective upload was not pending")
	}
	var cancellation struct {
		State   ac.CreationState `json:"state"`
		Cleanup string           `json:"cleanup"`
	}
	artifactOwnerHTTPDecode(t, artifactContentPost(t, v, v.a, meta, input, "cancel", 200).body, &cancellation)
	if cancellation.State != ac.CreationCancelled || cancellation.Cleanup != "completed" {
		t.Fatal("original cancellation did not complete")
	}
	artifactContentCancelledFacts(t, v, v.a, meta.IdempotencyKey)
	artifactOwnerHTTPDecode(t, artifactContentPost(t, v, v.a, meta, input, "lookup", 200).body, &pending)
	if pending.State != ac.CreationCancelled || string(pending.Artifact) != "null" {
		t.Fatal("cancelled creation was resurrected")
	}
	var page artifactOwnerHTTPPage
	artifactOwnerHTTPDecode(t, artifactOwnerHTTPGet(t, v, "GET", artifactOwnerHTTPListPath(v.a), 200).body, &page)
	if len(page.Items) != len(created) || page.NextCursor != "" {
		t.Fatal("actual list includes duplicate or cancelled creation")
	}
	for i, m := range created {
		artifactOwnerHTTPMetadata(t, page.Items[len(created)-1-i], m)
		var read struct {
			Artifact   json.RawMessage `json:"artifact"`
			Projection string          `json:"projection"`
			Text       string          `json:"text"`
			Offset     string          `json:"offset"`
			Next       string          `json:"next_offset"`
			Truncated  bool            `json:"truncated"`
			Invalid    bool            `json:"invalid_utf8"`
		}
		artifactOwnerHTTPDecode(t, artifactOwnerHTTPGet(t, v, "GET", artifactOwnerHTTPReadPath(m.Details().Reference), 200).body, &read)
		artifactOwnerHTTPMetadata(t, read.Artifact, m)
		if read.Projection != "text" || read.Text != inputs[i].Content || read.Offset != "0" || read.Next != strconv.Itoa(len(inputs[i].Content)) || read.Truncated || read.Invalid {
			t.Fatal("actual content read differs from original creation")
		}
	}
	foreign := artifactOwnerHTTPForeignClient(t, v)
	session, err := artifactOwnerHTTPRequest(v.ctx, foreign, v.address, "", "GET", "/api/v1/session", nil, "", "")
	workAppCheck(t, "artifact-content-foreign-session", err)
	var identity struct {
		CSRF string `json:"csrf_token"`
	}
	workAppCheck(t, "artifact-content-private-csrf", json.Unmarshal(session.body, &identity))
	if session.status != 200 || identity.CSRF == "" {
		t.Fatal("real foreign Session unavailable")
	}
	beforeDenied := v.snapshot(t, v.a)
	for _, action := range []string{"create", "lookup", "cancel"} {
		denied, err := artifactOwnerHTTPRequest(v.ctx, foreign, v.address, "", "POST", artifactContentCommandPath(v.a, action), workAppJSON(t, input), identity.CSRF, string(meta.IdempotencyKey))
		workAppCheck(t, "artifact-content-current-owner-denial", err)
		if denied.status != 404 {
			t.Fatalf("current nonowner command status=%d wanted=404", denied.status)
		}
	}
	if v.snapshot(t, v.a) != beforeDenied || v.snapshot(t, v.b) != bBefore {
		t.Fatal("denied command or another Project changed business facts")
	}
	v.audit(t, v.a, 3, 1, 3)
	v.audit(t, v.b, 1, 0, 0)
	v.joined(t)
	t.Log("ARTIFACT_CONTENT_APP create_lookup_replay=true published_ack_unknown=true missing_readonly_ack_unknown=true explicit_original_retry=true not_observed_zero_cancel=true pending_verified_cancelled=true real_list_read=true current_nonowner_denied=true original_owner_guard_db_joined=true no_sql_seed=true no_network_fault=true")
}

func artifactContentCancelledFacts(t *testing.T, v *humanArtifactApp, p id.ProjectID, key f.IdempotencyKey) {
	t.Helper()
	var complete bool
	workAppCheck(t, "artifact-content-original-cancel-facts", v.admin.QueryRow(v.ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_artifact.commands c
 JOIN agenteam_object.uploads u ON u.id=c.target_upload_id AND u.object_id=c.target_object_id
 JOIN agenteam_object.objects o ON o.id=u.object_id
 JOIN agenteam_object.upload_attempts a ON a.id=c.target_attempt_id AND a.upload_id=u.id AND a.object_id=o.id
 WHERE c.project_id=$1::uuid AND c.command_key=$2 AND c.state='cancelled' AND c.source_lease_id IS NULL
 AND u.owner_kind='artifact' AND u.owner_id=c.artifact_id AND u.project_id=c.project_id AND u.creation_cause=c.creation_cause
 AND u.disposition='revoked' AND o.state='deleted' AND a.phase='cleaned' AND a.io_closed
 AND NOT EXISTS(SELECT 1 FROM agenteam_artifact.artifacts b WHERE b.id=c.artifact_id)
 AND NOT EXISTS(SELECT 1 FROM agenteam_object.object_leases l WHERE l.object_id=o.id AND l.state='active'))`, p.String(), string(key)).Scan(&complete))
	if !complete {
		t.Fatal("cancelled creation retained original Object or published an Artifact")
	}
}
