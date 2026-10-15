//go:build integration

package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

type artifactUserUploadJSON struct {
	Name      string `json:"name"`
	MediaType string `json:"media_type"`
	ByteSize  string `json:"byte_size"`
	SHA256    string `json:"sha256,omitempty"`
}

type artifactUserUploadView struct {
	State    ac.UserUploadState `json:"state"`
	Artifact json.RawMessage    `json:"artifact"`
}

func artifactUserUploadHTTP(t *testing.T, v *humanArtifactApp, p id.ProjectID, meta f.CommandMeta, action string, body []byte, client *http.Client, mutate func(*http.Request), status int) artifactOwnerHTTPResponse {
	t.Helper()
	method, media := "POST", "application/json"
	if action == "payload" {
		method, media = "PUT", "application/octet-stream"
	}
	req, err := http.NewRequestWithContext(v.ctx, method, v.address+artifactOwnerHTTPListPath(p)+"/upload-commands/"+action, bytes.NewReader(body))
	workAppCheck(t, "user-upload-request", err)
	req.Host = "localhost:8080"
	req.Header.Set("Origin", "http://localhost:8080")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Content-Type", media)
	req.Header.Set("X-CSRF-Token", v.csrf)
	req.Header.Set("Idempotency-Key", string(meta.IdempotencyKey))
	if client == nil {
		client = v.client
		req.AddCookie(&http.Cookie{Name: "agenteam_local_session", Value: v.cookie})
	}
	if mutate != nil {
		mutate(req)
	}
	res, err := client.Do(req)
	workAppCheck(t, "user-upload-original-http-return", err)
	raw, err := io.ReadAll(io.LimitReader(res.Body, 128<<10))
	closeErr := res.Body.Close()
	workAppCheck(t, "user-upload-original-body-read", err)
	workAppCheck(t, "user-upload-original-body-close", closeErr)
	if res.StatusCode != status {
		var problem struct {
			Code f.Code `json:"code"`
		}
		_ = json.Unmarshal(raw, &problem)
		t.Fatalf("user upload stage=%s status=%d want=%d code=%s", action, res.StatusCode, status, problem.Code)
	}
	length, err := strconv.Atoi(res.Header.Get("Content-Length"))
	if err != nil || length != len(raw) || len(raw) >= 128<<10 || res.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("incomplete upload representation")
	}
	if _, err = f.ParseID[f.Request](res.Header.Get("X-Request-ID")); err != nil {
		t.Fatal("upload lost request identity")
	}
	return artifactOwnerHTTPResponse{status: res.StatusCode, header: res.Header.Clone(), body: raw}
}

func artifactUserUploadState(t *testing.T, response artifactOwnerHTTPResponse, expected ac.UserUploadState) artifactUserUploadView {
	t.Helper()
	var out artifactUserUploadView
	artifactOwnerHTTPDecode(t, response.body, &out)
	if out.State != expected || expected != ac.UserUploadCommitted && string(out.Artifact) != "null" {
		t.Fatal("upload exposed an incomplete reference", out.State)
	}
	return out
}

// Real default App, Account Session/CSRF/current Owner, Artifact/Object/Audit
// and storage bytes. The one ACK mask preserves the real final callback/COMMIT;
// it is explicitly a typed return fault, not a PostgreSQL network fault.
func TestArtifactUserUploadHTTPDefaultApp(t *testing.T) {
	v := newHumanArtifactApp(t)
	meta := humanArtifactMeta(t)
	payload := []byte{0, 0xff, 0x80, '\r', '\n', 1, 2, 3}
	input := artifactUserUploadJSON{Name: "user-file.bin", MediaType: "application/octet-stream", ByteSize: strconv.Itoa(len(payload)), SHA256: fmt.Sprintf("sha256:%x", sha256.Sum256(payload))}
	body := workAppJSON(t, input)
	call := func(action string, data []byte, want int) artifactOwnerHTTPResponse {
		return artifactUserUploadHTTP(t, v, v.a, meta, action, data, nil, nil, want)
	}
	artifactUserUploadState(t, call("lookup", body, 200), ac.UserUploadNotObserved)
	artifactUserUploadState(t, call("begin", body, 200), ac.UserUploadPending)
	artifactUserUploadState(t, call("begin", body, 200), ac.UserUploadPending)
	artifactUserUploadState(t, call("payload", payload, 200), ac.UserUploadUploaded)
	artifactUserUploadState(t, call("lookup", body, 200), ac.UserUploadUploaded)
	var published int
	workAppCheck(t, "upload-no-artifact-before-complete", v.admin.QueryRow(v.ctx, `SELECT count(*) FROM agenteam_artifact.artifacts WHERE project_id=$1::uuid`, v.a.String()).Scan(&published))
	if published != 0 {
		t.Fatal("verified Object became an Artifact before complete")
	}
	// Known uploaded identical replay can verify its bytes, but cannot replace the
	// Object. A different payload with the same command must fail.
	artifactUserUploadState(t, call("payload", payload, 200), ac.UserUploadUploaded)
	changed := append([]byte(nil), payload...)
	changed[0] = 99
	call("payload", changed, 400) // expected SHA is verified by original Object preparation

	h := &usageAppHook{mask: true, match: func(c f.TransactionCause) bool { return humanArtifactCommand(c, v.a, meta.IdempotencyKey) }}
	h.selectRow = func(ctx context.Context, x postgres.SQLExecutor, _ f.TransactionCause, before, after int64) (bool, error) {
		var exact bool
		err := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_artifact.upload_intents u JOIN agenteam_artifact.commands c ON c.artifact_id=u.id JOIN agenteam_artifact.artifacts a ON a.id=u.id WHERE u.project_id=$1::uuid AND u.command_key=$2 AND c.command_key=$2 AND u.state='consumed' AND c.path='upload' AND c.kind='user_upload' AND c.state='completed' AND a.kind='user_upload')`, v.a.String(), string(meta.IdempotencyKey)).Scan(&exact)
		return exact && after > before, err
	}
	v.tracked.arm(h)
	h.open()
	unknown := call("complete", body, 503)
	var problem struct {
		Code f.Code `json:"code"`
	}
	workAppCheck(t, "user-upload-unknown-problem", json.Unmarshal(unknown.body, &problem))
	if problem.Code != f.CommitUnknown {
		t.Fatal("final unknown was erased", problem.Code)
	}
	select {
	case observation := <-h.observed:
		usageAppWait(t, observation.returned, "original upload publication Store return")
		if observation.result.State() != f.Committed || observation.masked.State() != f.Unknown {
			t.Fatal("wrong upload ACK boundary")
		}
	default:
		t.Fatal("final original upload COMMIT not observed")
	}
	view := artifactUserUploadState(t, call("lookup", body, 200), ac.UserUploadCommitted)
	size, _ := f.ParseProgress(input.ByteSize)
	digest, _ := f.ParseDigest(input.SHA256)
	intent := ac.UserUploadIntent{Display: ac.Display{Name: input.Name}, MediaType: input.MediaType, ByteSize: size, SHA256: &digest}
	canonical, err := v.service.LookupUserUpload(v.ctx, v.invocation(t, v.a), meta, intent)
	workAppCheck(t, "user-upload-original-domain-lookup", err)
	if canonical.State != ac.UserUploadCommitted || canonical.Artifact == nil {
		t.Fatal("canonical upload missing")
	}
	m := *canonical.Artifact
	artifactOwnerHTTPMetadata(t, view.Artifact, m)
	if m.Details().Kind != ac.UserUpload || m.Details().CreatedByUserID != v.actor.Details().UserID || m.Details().ExecutionID != "" || m.Details().Object.SHA256 != digest {
		t.Fatal("upload provenance or bytes changed")
	}
	var completed struct {
		Artifact json.RawMessage `json:"artifact"`
	}
	artifactOwnerHTTPDecode(t, call("complete", body, 200).body, &completed)
	artifactOwnerHTTPMetadata(t, completed.Artifact, m)
	artifactUserUploadState(t, call("begin", body, 200), ac.UserUploadCommitted)
	call("cancel", body, 409)
	owner, err := oc.NewObjectOwner(oc.Artifact, m.Details().Reference.ArtifactID.String(), v.a.String())
	workAppCheck(t, "user-upload-formal-owner", err)
	reader, err := v.objects.service.ReadObject(v.ctx, v.actor, owner, m.Details().Object.ID, nil)
	workAppCheck(t, "user-upload-formal-object-read", err)
	actual, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	workAppCheck(t, "user-upload-object-read-return", readErr)
	workAppCheck(t, "user-upload-object-close-return", closeErr)
	if !bytes.Equal(actual, payload) {
		t.Fatal("binary user file changed in storage")
	}
	var artifacts, creates, attempts int
	workAppCheck(t, "user-upload-exact-persistent-facts", v.admin.QueryRow(v.ctx, `SELECT (SELECT count(*) FROM agenteam_artifact.artifacts WHERE project_id=$1::uuid),(SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1::uuid AND action='artifact.create'),(SELECT count(*) FROM agenteam_object.upload_attempts WHERE object_id=$2::uuid)`, v.a.String(), m.Details().Object.ID.String()).Scan(&artifacts, &creates, &attempts))
	if artifacts != 1 || creates != 1 || attempts != 1 {
		t.Fatal("recovery duplicated upload/artifact/audit", artifacts, creates, attempts)
	}

	other := input
	other.Name = "changed.bin"
	call("lookup", workAppJSON(t, other), 409)
	// Cancel both the empty prospective intent and an uploaded-but-unconsumed
	// Object through their original cleanup path; neither can become canonical.
	for _, uploaded := range []bool{false, true} {
		cancelMeta := humanArtifactMeta(t)
		cancelCall := func(action string, data []byte, status int) artifactOwnerHTTPResponse {
			return artifactUserUploadHTTP(t, v, v.a, cancelMeta, action, data, nil, nil, status)
		}
		artifactUserUploadState(t, cancelCall("begin", body, 200), ac.UserUploadPending)
		if uploaded {
			artifactUserUploadState(t, cancelCall("payload", payload, 200), ac.UserUploadUploaded)
		}
		var cancelled struct {
			State   ac.UserUploadState `json:"state"`
			Cleanup string             `json:"cleanup"`
		}
		artifactOwnerHTTPDecode(t, cancelCall("cancel", body, 200).body, &cancelled)
		if cancelled.State != ac.UserUploadCancelled || cancelled.Cleanup != "completed" {
			t.Fatal("upload cancellation did not actually finish")
		}
		artifactUserUploadState(t, cancelCall("lookup", body, 200), ac.UserUploadCancelled)
		cancelCall("complete", body, 409)
		cancelCall("payload", payload, 409)
	}

	foreign := artifactOwnerHTTPForeignClient(t, v)
	session, err := artifactOwnerHTTPRequest(v.ctx, foreign, v.address, "", "GET", "/api/v1/session", nil, "", "")
	workAppCheck(t, "upload-foreign-current-session", err)
	var foreignSession struct {
		CSRF string `json:"csrf_token"`
	}
	workAppCheck(t, "upload-foreign-csrf", json.Unmarshal(session.body, &foreignSession))
	if session.status != 200 || foreignSession.CSRF == "" {
		t.Fatal("foreign current Session unavailable")
	}
	before := v.snapshot(t, v.a)
	for _, action := range []string{"begin", "lookup", "payload", "complete", "cancel"} {
		data := body
		if action == "payload" {
			data = payload
		}
		artifactUserUploadHTTP(t, v, v.a, meta, action, data, foreign, func(r *http.Request) { r.Header.Set("X-CSRF-Token", foreignSession.CSRF) }, 404)
	}
	artifactUserUploadHTTP(t, v, v.a, meta, "begin", body, nil, func(r *http.Request) { r.Header.Set("Origin", "https://foreign.invalid") }, 403)
	artifactUserUploadHTTP(t, v, v.a, meta, "payload", payload, nil, func(r *http.Request) { r.Header.Del("X-CSRF-Token") }, 403)
	artifactUserUploadHTTP(t, v, v.a, meta, "lookup", body, nil, func(r *http.Request) { r.Header.Del("Cookie") }, 401)
	if before != v.snapshot(t, v.a) {
		t.Fatal("denied upload changed business facts")
	}
	v.audit(t, v.a, 1, 0, 0)
	// This top only calls Artifact in A. The shared Human fixture's joined
	// helper expects calls in both Projects, which its original tops perform.
	// Keep A's exact retirement checks and require no invented B call at all.
	v.ledger(t, v.a, 0)
	var unusedCalls int
	workAppCheck(t, "user-upload-unused-project-calls", v.admin.QueryRow(v.ctx, `SELECT count(*) FROM agenteam_artifact.project_work WHERE project_id=$1::uuid`, v.b.String()).Scan(&unusedCalls))
	if unusedCalls != 0 {
		t.Fatal("upload created an unexpected call in the untouched Project", unusedCalls)
	}
	v.workAppFixture.joined(t)
	if !v.pair.Joined() {
		t.Fatal("original upload Artifact owner did not join")
	}
	var activeLeases int
	workAppCheck(t, "user-upload-final-original-leases", v.admin.QueryRow(v.ctx, `SELECT count(*) FROM agenteam_object.object_leases WHERE process_id=$1::uuid AND state='active'`, v.objects.process.String()).Scan(&activeLeases))
	if activeLeases != 0 {
		t.Fatal("original upload retained an Object lease", activeLeases)
	}
	t.Log("ARTIFACT_USER_UPLOAD_APP binary_exact=true human_user_upload=true published_ack_unknown=true readonly_recovery=true cancel_prospective_and_uploaded=true current_nonowner_session_csrf_origin_denied=true original_owner_object_guard_db_joined=true no_network_commit_fault=true")
}

// The stable Meeting reference is an ArtifactID. The original Artifact owner
// supplies its full reference before the existing content route is used.
func TestArtifactMetadataHTTPDefaultApp(t *testing.T) {
	v := newHumanArtifactApp(t)
	const text = "Original metadata reference resolves this content."
	m := v.create(t, v.a, humanArtifactMeta(t), text)
	ref := m.Details().Reference
	if ref.ArtifactID.String() == ref.FileID.String() {
		t.Fatal("fixture collapsed stable and file identities")
	}
	path := artifactOwnerHTTPListPath(v.a) + "/" + ref.ArtifactID.String()
	objectFacts := func() string {
		var raw string
		workAppCheck(t, "metadata-original-object-facts", v.admin.QueryRow(v.ctx, `SELECT jsonb_build_array(
 (SELECT to_jsonb(x) FROM agenteam_object.objects x WHERE id=$1::uuid),
 (SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY id),'[]') FROM agenteam_object.uploads x WHERE object_id=$1::uuid),
 (SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY id),'[]') FROM agenteam_object.upload_attempts x WHERE object_id=$1::uuid),
 (SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY id),'[]') FROM agenteam_object.object_leases x WHERE object_id=$1::uuid))::text`, m.Details().Object.ID.String()).Scan(&raw))
		return raw
	}
	beforeObject := objectFacts()
	get := artifactOwnerHTTPGet(t, v, "GET", path, 200)
	var view struct {
		Artifact json.RawMessage `json:"artifact"`
	}
	artifactOwnerHTTPDecode(t, get.body, &view)
	artifactOwnerHTTPMetadata(t, view.Artifact, m)
	var target struct {
		Reference ac.ArtifactRef `json:"artifact_ref"`
	}
	workAppCheck(t, "metadata-full-reference", json.Unmarshal(view.Artifact, &target))
	if target.Reference != ref {
		t.Fatal("stable lookup did not return the original complete reference")
	}
	head := artifactOwnerHTTPGet(t, v, "HEAD", path, 200)
	if head.header.Get("Content-Length") != get.header.Get("Content-Length") || objectFacts() != beforeObject {
		t.Fatal("metadata GET/HEAD changed Object or lease facts")
	}
	v.audit(t, v.a, 1, 0, 2)
	var wrongBytes int
	workAppCheck(t, "metadata-zero-content-audit", v.admin.QueryRow(v.ctx, `SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1::uuid AND action='artifact.read' AND metadata->>'sent_bytes' IS DISTINCT FROM '0'`, v.a.String()).Scan(&wrongBytes))
	if wrongBytes != 0 {
		t.Fatal("metadata read claimed Object content bytes")
	}

	before := v.snapshot(t, v.a)
	artifactOwnerHTTPGet(t, v, "GET", artifactOwnerHTTPListPath(v.b)+"/"+ref.ArtifactID.String(), 404)
	artifactOwnerHTTPGet(t, v, "GET", artifactOwnerHTTPListPath(v.a)+"/"+guardID[ac.ArtifactEntity](t).String(), 404)
	artifactOwnerHTTPGet(t, v, "GET", path+"?file_id="+ref.FileID.String(), 400)
	foreign := artifactOwnerHTTPForeignClient(t, v)
	denied, err := artifactOwnerHTTPRequest(v.ctx, foreign, v.address, "", "GET", path, nil, "", "")
	workAppCheck(t, "metadata-current-nonowner-response", err)
	if denied.status != 404 || before != v.snapshot(t, v.a) {
		t.Fatal("metadata denied scope read leaked or changed canonical facts", denied.status)
	}
	// Use only the returned formal FileID, never the stable ArtifactID as a file.
	read := artifactOwnerHTTPGet(t, v, "GET", artifactOwnerHTTPReadPath(target.Reference), 200)
	var content struct {
		Artifact    json.RawMessage `json:"artifact"`
		Projection  ac.Projection   `json:"projection"`
		Text        string          `json:"text"`
		Offset      f.Progress      `json:"offset"`
		NextOffset  f.Progress      `json:"next_offset"`
		Truncated   bool            `json:"truncated"`
		InvalidUTF8 bool            `json:"invalid_utf8"`
	}
	artifactOwnerHTTPDecode(t, read.body, &content)
	artifactOwnerHTTPMetadata(t, content.Artifact, m)
	if content.Projection != ac.TextProjection || content.Text != text || content.Offset != 0 || content.NextOffset != f.Progress(len(text)) || content.Truncated || content.InvalidUTF8 {
		t.Fatal("resolved original reference did not open its exact content")
	}
	v.audit(t, v.a, 1, 0, 3)
	v.audit(t, v.b, 0, 0, 0)
	v.joined(t)
	t.Log("ARTIFACT_METADATA_APP original_full_ref=true get_head=true current_nonowner_cross_project_missing_denied=true metadata_object_facts_unchanged=true read_audit_zero_bytes=true formal_content_read=true original_owners_joined=true")
}
