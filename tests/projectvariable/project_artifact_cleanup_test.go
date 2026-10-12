//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/artifact"
	art "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	au "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	boxc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	"github.com/minio/minio-go/v7"
)

// The original full Object/Skills runtime gains only two concrete providers.
// This fixture follows App's fixed MaintenanceOwnerResolver dispatch; selection
// is not a grant and errors never fall through to another owning domain.
type artifactCleanupPGAssembly struct {
	owners *artifact.OwnerProvider
	audit  *audit.Service
	stop   func(i.Actor, pc.LifecycleCause, pc.ScopeRef)
}

func (*artifactCleanupPGAssembly) manifest(t *testing.T, original pc.RequiredManifest) pc.RequiredManifest {
	t.Helper()
	entries := original.Entries()
	for n := range entries {
		if entries[n].Name == pc.ArtifactObjectParticipant {
			entries[n].ReferenceKinds = []pc.ReferenceKind{"object", "artifact"}
		}
	}
	manifest, err := pc.NewRequiredManifest(entries)
	outboxCleanupRequire(t, err)
	return manifest
}

func (*artifactCleanupPGAssembly) projectAuditFacts(t *testing.T, store *hookStore) au.ProjectFactAuthority {
	t.Helper()
	facts, err := artifact.NewProjectAuditAuthority(store)
	outboxCleanupRequire(t, err)
	return facts
}

func (a *artifactCleanupPGAssembly) bind(t *testing.T, store *hookStore, projects *project.Authority, skills *skill.Authority, auditing *audit.Service, auth object.Authorizations) object.Authorizations {
	t.Helper()
	parent, err := project.NewArtifactAuthority(projects)
	outboxCleanupRequire(t, err)
	a.owners, err = artifact.NewOwnerProvider(store, parent)
	outboxCleanupRequire(t, err)
	cleanup, err := project.NewObjectCleanupAuthority(projects)
	outboxCleanupRequire(t, err)
	resolver, err := object.NewMaintenanceOwnerResolver(store)
	outboxCleanupRequire(t, err)
	router := &artifactCleanupPGAuthorities{skills: skills, artifacts: a.owners, cleanup: cleanup, maintenance: resolver}
	a.audit = auditing
	auth.Planner, auth.Resources, auth.Read, auth.Gate, auth.Cleanup = router, router, router, router, router
	return auth
}

type artifactCleanupPGOwner interface {
	oc.AccessPlanner
	oc.ResourceAuthority
	oc.ObjectReadAuthority
	oc.ProjectGate
	oc.CleanupAuthority
}

type artifactCleanupPGAuthorities struct {
	skills      *skill.Authority
	artifacts   *artifact.OwnerProvider
	cleanup     *project.ObjectCleanupAuthority
	maintenance *object.MaintenanceOwnerResolver
}

func (a *artifactCleanupPGAuthorities) owner(owner oc.ObjectOwner) (artifactCleanupPGOwner, error) {
	if owner.Validate() != nil {
		return nil, f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	switch owner.Details().Kind {
	case oc.SkillRevision:
		return a.skills, nil
	case oc.Artifact:
		return a.artifacts, nil
	default:
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
}

func (a *artifactCleanupPGAuthorities) planner(ctx context.Context, tx f.Tx, request oc.AccessRequest) (oc.AccessPlanner, error) {
	if request.Validate() != nil {
		return nil, f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	d := request.Details()
	var owner oc.ObjectOwner
	var err error
	switch d.Kind {
	case oc.ProjectCleanupAccess:
		return a.cleanup, nil
	case oc.MaintenanceAccess:
		if tx.Valid() {
			owner, err = a.maintenance.ResolveInTx(ctx, tx, request)
		} else {
			owner, err = a.maintenance.Discover(ctx, request)
		}
	case oc.OwnerAccess, oc.ObjectReadAccess:
		owner = d.Owner
	case oc.SourceAccess:
		owner = d.Source.Details().Owner
	case oc.ObjectCleanupAccess, oc.CleanupReleaseAccess:
		owner = d.Cleanup.Details().Owner
	default:
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	if err != nil {
		return nil, err
	}
	return a.owner(owner)
}

func (a *artifactCleanupPGAuthorities) Discover(ctx context.Context, request oc.AccessRequest) (oc.AccessDependencies, error) {
	p, err := a.planner(ctx, f.Tx{}, request)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	return p.Discover(ctx, request)
}
func (a *artifactCleanupPGAuthorities) ValidateInTx(ctx context.Context, tx f.Tx, request oc.AccessRequest, expected oc.AccessDependencies) error {
	p, err := a.planner(ctx, tx, request)
	if err != nil {
		return err
	}
	return p.ValidateInTx(ctx, tx, request, expected)
}
func (a *artifactCleanupPGAuthorities) AuthorizeOwner(ctx context.Context, actor i.Actor, owner oc.ObjectOwner, intent i.AccessIntent) (oc.OwnerAuthorization, error) {
	p, err := a.owner(owner)
	if err != nil {
		return oc.OwnerAuthorization{}, err
	}
	return p.AuthorizeOwner(ctx, actor, owner, intent)
}
func (a *artifactCleanupPGAuthorities) AuthorizeOwnerInTx(ctx context.Context, tx f.Tx, actor i.Actor, owner oc.ObjectOwner, intent i.AccessIntent) (oc.OwnerAuthorization, error) {
	p, err := a.owner(owner)
	if err != nil {
		return oc.OwnerAuthorization{}, err
	}
	return p.AuthorizeOwnerInTx(ctx, tx, actor, owner, intent)
}
func (a *artifactCleanupPGAuthorities) AuthorizeObjectReadInTx(ctx context.Context, tx f.Tx, actor i.Actor, owner oc.ObjectOwner, id oc.ObjectID) (oc.OwnerAuthorization, error) {
	p, err := a.owner(owner)
	if err != nil {
		return oc.OwnerAuthorization{}, err
	}
	return p.AuthorizeObjectReadInTx(ctx, tx, actor, owner, id)
}
func (a *artifactCleanupPGAuthorities) CheckInTx(ctx context.Context, tx f.Tx, actor i.Actor, owner oc.ObjectOwner, intent i.AccessIntent) error {
	p, err := a.owner(owner)
	if err != nil {
		return err
	}
	return p.CheckInTx(ctx, tx, actor, owner, intent)
}
func (a *artifactCleanupPGAuthorities) CheckCleanupInTx(ctx context.Context, tx f.Tx, cause oc.ObjectCleanupCause, id oc.ObjectID) error {
	p, err := a.owner(cause.Details().Owner)
	if err != nil {
		return err
	}
	return p.CheckCleanupInTx(ctx, tx, cause, id)
}
func (a *artifactCleanupPGAuthorities) CheckProjectCleanupInTx(ctx context.Context, tx f.Tx, actor i.Actor, cause oc.ProjectCleanupCause) error {
	return a.cleanup.CheckProjectCleanupInTx(ctx, tx, actor, cause)
}

// Accepted Delete and Artifact Stop are controlled prerequisites: Artifact has
// no Stop implementation in this slice. Object Stop and the entire original
// Agent/Skills/Variables/Secret chain are real. Source and lease Pending gates
// have pure coverage; no source creation, source cancellation or lease proof is
// invented here. No default registry or final Project deletion is asserted.
func TestProjectArtifactCleanup(t *testing.T) {
	t.Run("inline-upload-and-original-checkpoint-return", testProjectArtifactCleanupReturn)
	t.Run("artifact-original-unknown-readonly-confirmation", func(t *testing.T) { testProjectArtifactCleanupUnknown(t, false) })
	t.Run("object-original-unknown-readonly-confirmation", func(t *testing.T) { testProjectArtifactCleanupUnknown(t, true) })
}

type artifactCleanupPG struct {
	*agentVariablesTerminalPG
	service                    *artifact.Service
	artifacts, secrets, outbox *secretCleanupPGTap
	box                        *outbox.Service
	objects                    []skillsCleanupPGObject
	other                      art.Metadata
	foreign                    string
}

func newArtifactCleanupPG(t *testing.T) *artifactCleanupPG {
	return newArtifactCleanupPGConfigured(t, nil, nil)
}

// The Stop slice supplies its real older-prefix database and replaces only the
// Artifact service with its original owned pair before accepted Delete.
func newArtifactCleanupPGConfigured(t *testing.T, base *variableHTTPFixture, prepare func(*artifactCleanupPG, *artifactCleanupPGAssembly)) *artifactCleanupPG {
	t.Helper()
	v := &artifactCleanupPG{}
	if base == nil {
		base = newVariableHTTPFixture(t)
	}
	assembly := &artifactCleanupPGAssembly{}
	base.artifactAssembly = assembly
	v.agentVariablesTerminalPG = newAgentVariablesTerminalPGConfigured(t, false, false, false, true, func(prefix *agentVariablesTerminalPG) {
		v.agentVariablesTerminalPG = prefix
		var err error
		v.service, err = artifact.New(base.tracked, assembly.owners, v.p2.objects, v.p2.objects, v.p2.objects, nil, nil, assembly.audit, base.keys)
		outboxCleanupRequire(t, err)
		v.seedArtifacts(t, assembly)
		catalog := event.NewCatalog()
		_, err = pc.RegisterProjectEvents(catalog)
		outboxCleanupRequire(t, err)
		v.box, err = outbox.New(base.tracked, catalog, outbox.Authorizations{Producers: map[event.StableName]boxc.ProducerAuthority{pc.ProjectProducer: v.projects}, Projects: v.projects, Sessions: base.accounts, System: base.accounts, Processes: captureProviderProcesses{guard: v.guard}, Audit: assembly.audit, Cursors: base.keys})
		outboxCleanupRequire(t, err)
		adapter, err := project.NewOutboxLifecycleParticipant(v.p2.lifecycle, v.box)
		outboxCleanupRequire(t, err)
		v.outbox = &secretCleanupPGTap{ProjectLifecycleParticipant: adapter}
		assembly.stop = func(actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) {
			var report pc.StopReport
			for n := 0; n < 4; n++ {
				report, err = adapter.RequestStop(ctxFor(t), actor, cause, scope)
				outboxCleanupRequire(t, err)
				if report.Details().State == pc.Stopped {
					break
				}
				if report.Details().State != pc.StopPending {
					t.Fatal("unexpected original D06 stop report")
				}
			}
			if !report.Matches(pc.OutboxParticipant, cause, scope) || report.Details().State != pc.Stopped {
				t.Fatal("actual D06 original stop did not complete")
			}
		}
		v.skillsCleanupPG.foreign = v.foreignSnapshot(t)
		if prepare != nil {
			prepare(v, assembly)
		}
	}, base)
	participant, err := project.NewArtifactLifecycleParticipant(v.p2.lifecycle, v.service)
	outboxCleanupRequire(t, err)
	v.artifacts = &secretCleanupPGTap{ProjectLifecycleParticipant: participant}
	secrets, err := project.NewSecretLifecycleParticipant(v.p2.lifecycle, v.deps.d04)
	outboxCleanupRequire(t, err)
	v.secrets = &secretCleanupPGTap{ProjectLifecycleParticipant: secrets}
	return v
}

func (v *artifactCleanupPG) invocation(t *testing.T, project i.ProjectID) art.Invocation {
	t.Helper()
	invocation, err := art.NewInvocation(art.InvocationDetails{Actor: v.p2.base.ownerBrowser.actor, ProjectID: project, AttemptID: id[art.CallAttempt](t)})
	outboxCleanupRequire(t, err)
	return invocation
}

func (v *artifactCleanupPG) seedArtifacts(t *testing.T, assembly *artifactCleanupPGAssembly) {
	t.Helper()
	require := func(stage string, err error) {
		t.Helper()
		if err != nil {
			agentRetirementDiagnostic(t, "artifact-seed-"+stage, err)
			t.Fatalf("formal Artifact seed failed at %s", stage)
		}
	}
	p := v.p2.project.ID
	inline, err := v.service.CreateFromContent(ctxFor(t), v.invocation(t, p), meta(t, "cleanup-inline", nil), art.Display{Name: "inline.txt"}, "text/plain", "原始 inline bytes\n")
	require("create-content", err)
	target, err := v.service.BeginUpload(ctxFor(t), v.invocation(t, p), meta(t, "cleanup-upload", nil), art.Display{Name: "upload.txt"}, "text/plain", 12, nil)
	require("begin-upload", err)
	stored, err := v.service.UploadPayload(ctxFor(t), v.invocation(t, p), target, io.NopCloser(strings.NewReader("upload bytes")))
	require("upload-payload", err)
	upload, err := v.service.CreateFromUpload(ctxFor(t), v.invocation(t, p), meta(t, "cleanup-upload-create", nil), art.Display{Name: "upload.txt"}, stored.Receipt)
	require("create-from-upload", err)
	if inline.Details().Kind != art.Generated || upload.Details().Kind != art.UserUpload || stored.Meta.ID != upload.Details().Object.ID {
		t.Fatal("formal Artifact publication did not preserve inline/upload identities")
	}
	v.other, err = v.service.CreateFromContent(ctxFor(t), v.invocation(t, v.p2.base.project.ID), meta(t, "foreign-artifact", nil), art.Display{Name: "other.txt"}, "text/plain", "other Project body")
	require("other-project-create", err)
	for _, value := range []art.Metadata{inline, upload} {
		var native skillsCleanupPGObject
		outboxCleanupRequire(t, v.p2.base.raw.QueryRow(ctxFor(t), `SELECT id::text,candidate_key,byte_size FROM agenteam_object.objects WHERE id=$1 AND state='available'`, value.Details().Object.ID.String()).Scan(&native.id, &native.key, &native.size))
		v.objects = append(v.objects, native)
	}
	v.native(t, true)
	preview, err := v.service.ReadArtifact(ctxFor(t), v.invocation(t, p), inline.Details().Reference, art.PreviewRequest{})
	require("read-artifact", err)
	if preview.Details().Text != "原始 inline bytes\n" {
		t.Fatal("formal Artifact reader changed original bytes")
	}
	// A real issued download grant is metadata to retire, not a fabricated row.
	sources, err := artifact.NewSources(assembly.owners, v.p2.objects, assembly.audit)
	require("sources-binding", err)
	material := func(b byte) string { return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{b}, 32)) }
	keys, err := secret.LoadKeyring(fmt.Sprintf(`{"format":1,"current_version":"1","keys":[{"version":"1","key_b64":%q}]}`, material(21)), v.p2.base.keys)
	require("secret-keyring", err)
	downloadKeys, err := object.LoadDownloadKeyring(fmt.Sprintf(`{"format":1,"current_kid":"cleanup","keys":[{"kid":"cleanup","key_b64":%q}]}`, material(22)), v.p2.base.keys, keys)
	require("download-keyring", err)
	downloads, err := object.NewDownloads(v.p2.objects, sources, downloadKeys)
	require("download-binding", err)
	ref, err := inline.Details().Reference.BusinessFile()
	require("business-file-reference", err)
	_, err = downloads.IssueDownload(ctxFor(t), v.p2.base.ownerBrowser.actor, ref, oc.DownloadAttachment, time.Minute)
	require("issue-download", err)
	if v.counts(t) != [5]int{2, 2, 1, 2, 1} {
		t.Fatal("formal Artifact/Object/download seed differs", v.counts(t))
	}
	v.foreign = v.foreignArtifacts(t)
}

func (v *artifactCleanupPG) driver(t *testing.T) *project.LifecycleCleanupDriver {
	t.Helper()
	if v.agentTerminal == nil {
		v.agentVariablesTerminalPG.driver(t)
	}
	d, err := project.NewLifecycleCleanupDriverWithArtifact(v.p2.base.tracked, v.projects, captureProviderProcesses{guard: v.guard}, v.consumer, agentRetirementSkillsDiagnostic{SkillsCleanupDomain: v.tap, t: t}, v.variables, v.agentTerminal, v.variableTerminal, v.secrets, v.artifacts, v.outbox)
	outboxCleanupRequire(t, err)
	v.drivers = append(v.drivers, d)
	return d
}

func (v *artifactCleanupPG) ready(t *testing.T, d *project.LifecycleCleanupDriver) {
	t.Helper()
	v.agentVariablesTerminalPG.ready(t, d)
	for _, name := range []string{"combined terminal", "Secret"} {
		call := v.run(t, d)
		outboxCleanupWait(t, call.done, "original "+name+" prefix return")
		outboxCleanupRequire(t, call.err)
	}
	v.state(t, "required", "domains", "terminal")
	if v.artifacts.calls.Load() != 0 || v.outbox.calls.Load() != 0 || v.secrets.calls.Load() != 1 || v.counts(t) != [5]int{2, 2, 1, 2, 1} {
		t.Fatal("Artifact prefix skipped a real predecessor or consumed Artifact early")
	}
	v.native(t, true)
}

func (v *artifactCleanupPG) counts(t *testing.T) [5]int {
	t.Helper()
	var n [5]int
	outboxCleanupRequire(t, v.p2.base.raw.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_artifact.artifacts WHERE project_id=$1),(SELECT count(*) FROM agenteam_artifact.commands WHERE project_id=$1),(SELECT count(*) FROM agenteam_artifact.upload_intents WHERE project_id=$1),(SELECT count(*) FROM agenteam_object.objects WHERE project_id=$1 AND id::text=ANY($2::text[])),(SELECT count(*) FROM agenteam_download.grants WHERE project_id=$1)`, v.p2.project.ID.String(), []string{v.objects[0].id, v.objects[1].id}).Scan(&n[0], &n[1], &n[2], &n[3], &n[4]))
	return n
}

func (v *artifactCleanupPG) native(t *testing.T, present bool) {
	t.Helper()
	for _, value := range v.objects {
		info, err := v.p2.s3.StatObject(ctxFor(t), v.p2.bucket, value.key, minio.StatObjectOptions{})
		if present {
			if err != nil || info.Size != value.size {
				t.Fatal("original Artifact payload absent or changed", err)
			}
		} else if err == nil || minio.ToErrorResponse(err).Code != "NoSuchKey" {
			t.Fatal("original Artifact payload not physically absent", err)
		}
	}
}

func (v *artifactCleanupPG) foreignArtifacts(t *testing.T) string {
	t.Helper()
	var value string
	outboxCleanupRequire(t, v.p2.base.raw.QueryRow(ctxFor(t), `SELECT jsonb_build_array((SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM agenteam_artifact.artifacts x WHERE project_id=$1),(SELECT jsonb_agg(to_jsonb(x) ORDER BY command_hash) FROM agenteam_artifact.commands x WHERE project_id=$1),(SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM agenteam_object.objects x WHERE project_id=$1),(SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM agenteam_download.grants x WHERE project_id=$1))::text`, v.p2.base.project.ID.String()).Scan(&value))
	return value
}

func (v *artifactCleanupPG) state(t *testing.T, cleanup, stage, phase string) {
	t.Helper()
	var lifecycle, actualStage, actual, claim, combined, secretState, stop string
	outboxCleanupRequire(t, v.p2.base.raw.QueryRow(ctxFor(t), `SELECT p.lifecycle,o.cleanup_stage,r.cleanup_state,w.phase,a.cleanup_state,s.cleanup_state,r.stop_state FROM agenteam_project.projects p JOIN agenteam_project.lifecycle_operations o ON o.id=p.current_lifecycle_operation_id JOIN agenteam_project.lifecycle_participants r ON r.operation_id=o.id AND r.participant_name='artifact-object' JOIN agenteam_project.lifecycle_participants a ON a.operation_id=o.id AND a.participant_name='agent-skills-variables' JOIN agenteam_project.lifecycle_participants s ON s.operation_id=o.id AND s.participant_name='secret' JOIN agenteam_project.work_claims w ON w.work_id=o.id AND w.work_kind='lifecycle' WHERE p.id=$1 AND o.id=$2`, v.p2.project.ID.String(), v.cause.OperationID.String()).Scan(&lifecycle, &actualStage, &actual, &claim, &combined, &secretState, &stop))
	if lifecycle != "deleting" || actualStage != stage || actual != cleanup || claim != phase || combined != "completed" || secretState != "completed" || stop != "stopped" {
		t.Fatal("Artifact original domain/claim differs", actual, actualStage, claim)
	}
	var schema *int64
	var checkpoint []byte
	outboxCleanupRequire(t, v.p2.base.raw.QueryRow(ctxFor(t), `SELECT checkpoint_schema,checkpoint FROM agenteam_project.lifecycle_participants WHERE operation_id=$1 AND participant_name='artifact-object'`, v.cause.OperationID.String()).Scan(&schema, &checkpoint))
	if cleanup == "required" {
		if schema != nil || checkpoint != nil {
			t.Fatal("unreturned Artifact call published a checkpoint")
		}
	} else {
		var cp map[string]string
		if schema == nil || *schema != 1 || json.Unmarshal(checkpoint, &cp) != nil || len(cp) != 2 || cp["project_id"] != v.p2.project.ID.String() || cp["operation_id"] != v.cause.OperationID.String() {
			t.Fatal("Artifact checkpoint lost its exact original scope")
		}
	}

	if v.foreignArtifacts(t) != v.foreign {
		t.Fatal("Artifact cleanup changed Project B's original metadata")
	}
	process, err := v.guard.CurrentProcess()
	outboxCleanupRequire(t, err)
	if process != v.process {
		t.Fatal("original Object process guard returned prematurely")
	}
}

func (v *artifactCleanupPG) snapshot(t *testing.T) string {
	t.Helper()
	var snapshot string
	outboxCleanupRequire(t, v.p2.base.raw.QueryRow(ctxFor(t), `SELECT jsonb_build_array(
 (SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM agenteam_artifact.artifacts x WHERE project_id=$1),
 (SELECT jsonb_agg(to_jsonb(x) ORDER BY command_hash) FROM agenteam_artifact.commands x WHERE project_id=$1),
 (SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM agenteam_artifact.upload_intents x WHERE project_id=$1),
 (SELECT to_jsonb(x) FROM agenteam_artifact.cleanup x WHERE project_id=$1),
 (SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM agenteam_object.objects x WHERE project_id=$1),
 (SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM agenteam_object.uploads x WHERE object_id::text=ANY($2::text[])),
 (SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM agenteam_object.upload_attempts x WHERE object_id::text=ANY($2::text[])),
 (SELECT jsonb_agg(to_jsonb(x) ORDER BY to_jsonb(x)::text) FROM agenteam_object.object_references x WHERE partition_id=$1),
 (SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM agenteam_object.object_leases x WHERE object_id::text=ANY($2::text[])),
 (SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM agenteam_object.cleanup_operations x WHERE object_id::text=ANY($2::text[])),
 (SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM agenteam_object.project_work x WHERE project_id=$1),
 (SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM agenteam_download.grants x WHERE project_id=$1))::text`, v.p2.project.ID.String(), []string{v.objects[0].id, v.objects[1].id}).Scan(&snapshot))
	return snapshot
}

func (v *artifactCleanupPG) empty(t *testing.T) {
	t.Helper()
	if v.counts(t) != [5]int{} {
		t.Fatal("Artifact/Object/download metadata was not actually removed", v.counts(t))
	}
	var remaining int
	var operation, state string
	outboxCleanupRequire(t, v.p2.base.raw.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_object.objects WHERE project_id=$1),operation_id::text,state FROM agenteam_artifact.cleanup WHERE project_id=$1`, v.p2.project.ID.String()).Scan(&remaining, &operation, &state))
	if remaining != 0 || state != "completed" || operation != v.cause.OperationID.String() {
		t.Fatal("original Artifact receipt or all-Project Object remainder differs")
	}
	v.native(t, false)
}

func (v *artifactCleanupPG) otherReadable(t *testing.T) {
	t.Helper()
	read, err := v.service.ReadArtifact(ctxFor(t), v.invocation(t, v.p2.base.project.ID), v.other.Details().Reference, art.PreviewRequest{})
	outboxCleanupRequire(t, err)
	if read.Details().Text != "other Project body" || v.foreignArtifacts(t) != v.foreign {
		t.Fatal("Project A cleanup blocked or changed Project B's actual read")
	}
}

func (v *artifactCleanupPG) waitHeld(t *testing.T, call *skillsCleanupPGCall) {
	t.Helper()
	select {
	case <-v.barrier.entered:
	case <-call.done:
		agentRetirementDiagnostic(t, "artifact-before-original-return", call.err)
		t.Fatal("original Artifact/Object Store return barrier was not reached")
	case <-time.After(5 * time.Second):
		t.Fatal("original Artifact/Object Store return barrier was not reached")
	}
}

func (v *artifactCleanupPG) secondDriverBusy(t *testing.T, original *skillsCleanupPGCall) {
	t.Helper()
	other := v.driver(t)
	before := v.outbox.calls.Load()
	if err := other.Run(ctxFor(t), v.p2.project.ID, v.cause.OperationID); !outboxCleanupFault(err, f.ResourceBusy) {
		t.Fatal("second same-Authority driver overtook the original Run return", err)
	}
	if v.outbox.calls.Load() != before {
		t.Fatal("second driver entered Outbox before the original return")
	}
	select {
	case <-original.done:
		t.Fatal("original held Run returned prematurely")
	default:
	}
}

func (v *artifactCleanupPG) finishArtifact(t *testing.T, d *project.LifecycleCleanupDriver) {
	t.Helper()
	// Each explicit continuation requires the preceding original Pending report.
	// Two objects fit Object's fixed batch; allow its original join-recovery tail.
	for n := 0; n < 4; n++ {
		call := v.run(t, d)
		outboxCleanupWait(t, call.done, "original Artifact cleanup continuation")
		agentRetirementDiagnostic(t, "artifact-continuation", call.err)
		outboxCleanupRequire(t, call.err)
		v.artifacts.mu.Lock()
		r := v.artifacts.report
		v.artifacts.mu.Unlock()
		if !r.Matches(pc.ArtifactObjectParticipant, v.cause, pc.ScopeRef{Kind: pc.ProjectScope, ProjectID: v.p2.project.ID}) {
			t.Fatal("original Artifact report changed scope")
		}
		if r.Details().State == pc.CleanupCompleted {
			v.state(t, "completed", "outbox", "terminal")
			v.empty(t)
			return
		}
		if r.Details().State != pc.CleanupPending {
			t.Fatal("Artifact continuation was not an explicit Pending")
		}
		v.state(t, "pending", "domains", "terminal")
	}
	t.Fatal("source-free two-object cleanup did not finish its original bounded rounds")
}

func testProjectArtifactCleanupReturn(t *testing.T) {
	v := newArtifactCleanupPG(t)
	d := v.driver(t)
	v.ready(t, d)
	store := v.p2.base.tracked
	var finish atomic.Bool
	store.setAfter(func(ctx context.Context, tx f.Tx, c f.TransactionCause) error {
		if c.Kind() != f.JobCause || c.Details().JobType != "project-lifecycle" || c.Details().JobID != v.cause.OperationID.String() {
			return nil
		}
		x, err := store.InTx(tx)
		if err != nil {
			return err
		}
		var completed bool
		err = x.QueryRow(ctx, `SELECT o.cleanup_stage='outbox' AND p.cleanup_state='completed' AND w.phase='terminal' FROM agenteam_project.lifecycle_operations o JOIN agenteam_project.lifecycle_participants p ON p.operation_id=o.id AND p.participant_name='artifact-object' JOIN agenteam_project.work_claims w ON w.work_id=o.id AND w.work_kind='lifecycle' WHERE o.id=$1`, v.cause.OperationID.String()).Scan(&completed)
		if err == nil && completed {
			finish.Store(true)
		}
		return err
	})
	v.barrier = newSchedulerStopBarrier()
	defer v.barrier.open()
	store.mu.Lock()
	store.afterResult = func(c f.TransactionCause, r f.CommitResult) {
		if c.Kind() == f.JobCause && c.Details().JobType == "project-lifecycle" && c.Details().JobID == v.cause.OperationID.String() && finish.Load() && r.State() == f.Committed {
			v.barrier.hold(r)
		}
	}
	store.mu.Unlock()
	call := v.run(t, d)
	v.waitHeld(t, call)
	v.state(t, "completed", "outbox", "terminal")
	v.empty(t)
	if v.outbox.calls.Load() != 0 {
		t.Fatal("Artifact finish called Outbox before returning")
	}
	v.secondDriverBusy(t, call)
	v.otherReadable(t)
	v.barrier.open()
	outboxCleanupWait(t, call.done, "actual Artifact completed checkpoint original Store return")
	outboxCleanupRequire(t, call.err)
	v.clearHooks()
	call = v.run(t, d)
	outboxCleanupWait(t, call.done, "explicit next original Outbox call")
	outboxCleanupRequire(t, call.err)
	if v.outbox.calls.Load() != 1 {
		t.Fatal("the next explicit Run did not consume the real Outbox participant")
	}
	v.empty(t)
	v.otherReadable(t)
	v.close(t)
	t.Log("PROJECT_ARTIFACT_CLEANUP_RETURN formal_inline_upload_download=true physical_minio_removed=true original_project_finish_return_held=true same_authority_second_driver_busy=true other_project_read=true next_explicit_outbox=true artifact_stop_controlled_object_stop_real=true no_full_delete=true")
}

func (v *artifactCleanupPG) providerCause(c f.TransactionCause, owner string) bool {
	d := c.Details()
	return c.Kind() == f.RecoveryCause && d.Owner == owner && d.RecoveryRunID == v.cause.OperationID.String() && d.CheckpointRef == v.p2.project.ID.String()
}

func testProjectArtifactCleanupUnknown(t *testing.T, child bool) {
	v := newArtifactCleanupPG(t)
	d := v.driver(t)
	v.ready(t, d)
	store := v.p2.base.tracked
	attempt := id[f.TransactionAttempt](t)
	var selected f.TransactionCause
	var captured, masked atomic.Bool
	store.setAfter(func(ctx context.Context, tx f.Tx, c f.TransactionCause) error {
		if captured.Load() {
			return nil
		}
		if !child {
			if v.providerCause(c, "artifact-cleanup") {
				selected = c
				captured.Store(true)
			}
			return nil
		}
		if c.Kind() != f.RecoveryCause || c.Details().Owner != "object" {
			return nil
		}
		x, err := store.InTx(tx)
		if err != nil {
			return err
		}
		var gated bool
		err = x.QueryRow(ctx, `SELECT count(*)=2 AND bool_and(cleaning AND project_cleanup_pass=1) FROM agenteam_object.objects WHERE project_id=$1`, v.p2.project.ID.String()).Scan(&gated)
		if err == nil && gated {
			selected = c
			captured.Store(true)
		}
		return err
	})
	var original f.CommitResult
	store.mu.Lock()
	store.transformResult = func(c f.TransactionCause, r f.CommitResult) f.CommitResult {
		if captured.Load() && c.Kind() == selected.Kind() && c.Details().Owner == selected.Details().Owner && c.Details().RecoveryRunID == selected.Details().RecoveryRunID && c.Details().CheckpointRef == selected.Details().CheckpointRef && r.State() == f.Committed && masked.CompareAndSwap(false, true) {
			original = f.UnknownResult(attempt, c)
			return original
		}
		return r
	}
	store.mu.Unlock()
	call := v.run(t, d)
	outboxCleanupWait(t, call.done, "original Artifact or Object Unknown return")
	var fault, providerFault *f.Fault
	v.artifacts.mu.Lock()
	providerErr, report := v.artifacts.err, v.artifacts.report
	v.artifacts.mu.Unlock()
	if !masked.Load() || original.State() != f.Unknown || original.AttemptID() != attempt || !errors.As(call.err, &fault) || !errors.As(providerErr, &providerFault) || fault != providerFault || fault.Code != f.CommitUnknown || fault.CommitState != f.Unknown || fault.CauseID != attempt.String() || report.Details().State != "" {
		agentRetirementDiagnostic(t, "artifact-original-unknown", call.err)
		t.Fatal("original provider Unknown did not retain its original Fault/attempt and zero progress")
	}
	v.state(t, "required", "domains", "running")
	want := [5]int{2, 2, 1, 2, 1}
	if child {
		want = [5]int{0, 2, 1, 2, 1}
	}
	if v.counts(t) != want {
		t.Fatal("controlled original COMMIT did not reach the selected real provider", v.counts(t))
	}
	v.native(t, true)
	v.clearHooks()
	before := v.snapshot(t)
	var deletes, observations atomic.Int64
	var mu sync.Mutex
	baselines := make(map[f.Tx]int64)
	match := func(c f.TransactionCause) bool {
		return v.providerCause(c, "artifact-cleanup") || child && v.providerCause(c, "object-project-cleanup-confirm")
	}
	read := func(ctx context.Context, tx f.Tx) (int64, error) {
		x, err := store.InTx(tx)
		if err != nil {
			return 0, err
		}
		var n int64
		err = x.QueryRow(ctx, `SELECT COALESCE(sum(n_tup_del),0)::bigint FROM pg_stat_xact_user_tables WHERE schemaname IN ('agenteam_artifact','agenteam_object','agenteam_download')`).Scan(&n)
		return n, err
	}
	// This nil-default observation seam is inherited from the reviewed Secret
	// correction: PG17 pending statistics require same-original-Tx deltas.
	store.mu.Lock()
	store.before = func(ctx context.Context, tx f.Tx, c f.TransactionCause) error {
		if !match(c) {
			return nil
		}
		n, err := read(ctx, tx)
		if err != nil {
			return err
		}
		mu.Lock()
		baselines[tx] = n
		mu.Unlock()
		return nil
	}
	store.mu.Unlock()
	t.Cleanup(func() { store.mu.Lock(); store.before = nil; store.mu.Unlock() })
	store.setAfter(func(ctx context.Context, tx f.Tx, c f.TransactionCause) error {
		if !match(c) {
			return nil
		}
		n, err := read(ctx, tx)
		if err != nil {
			return err
		}
		mu.Lock()
		old, ok := baselines[tx]
		delete(baselines, tx)
		mu.Unlock()
		if !ok || n < old {
			return errors.New("original Artifact confirmation statistics lost their same-Tx baseline")
		}
		deletes.Add(n - old)
		observations.Add(1)
		return nil
	})
	v.barrier = newSchedulerStopBarrier()
	defer v.barrier.open()
	holdOwner := "artifact-cleanup"
	if child {
		holdOwner = "object-project-cleanup-confirm"
	}
	store.mu.Lock()
	store.afterResult = func(c f.TransactionCause, r f.CommitResult) {
		if v.providerCause(c, holdOwner) && r.State() == f.Committed {
			v.barrier.hold(r)
		}
	}
	store.mu.Unlock()
	call = v.run(t, d)
	v.waitHeld(t, call)
	wantObservations := int64(1)
	if child {
		wantObservations = 2
	}
	if observations.Load() != wantObservations || deletes.Load() != 0 || v.snapshot(t) != before {
		t.Fatal("original provider confirmation mutated metadata or did not traverse its exact read-only transactions", observations.Load(), deletes.Load())
	}
	v.state(t, "required", "domains", "running")
	v.secondDriverBusy(t, call)
	v.barrier.open()
	outboxCleanupWait(t, call.done, "actual provider read-only confirmation original return")
	outboxCleanupRequire(t, call.err)
	store.mu.Lock()
	store.before = nil
	store.mu.Unlock()
	v.clearHooks()
	v.state(t, "pending", "domains", "terminal")
	if v.snapshot(t) != before {
		t.Fatal("original read-only confirmation changed its recovery metadata")
	}
	v.finishArtifact(t, d)
	v.otherReadable(t)
	v.close(t)
	if child {
		t.Log("PROJECT_OBJECT_CLEANUP_UNKNOWN actual_commit_local_ackmask=true original_provider_attempt=true artifact_and_object_confirmation_zero_delete_delta=true exact_metadata_unchanged=true original_confirmation_return_held=true next_explicit_mutation=true no_network_fault=true")
	} else {
		t.Log("PROJECT_ARTIFACT_CLEANUP_UNKNOWN actual_commit_local_ackmask=true original_provider_attempt=true artifact_confirmation_zero_delete_delta=true exact_metadata_unchanged=true original_confirmation_return_held=true next_explicit_mutation=true no_network_fault=true")
	}
}
