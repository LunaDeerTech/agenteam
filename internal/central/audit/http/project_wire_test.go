package audithttp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

const projectAuditWireID = "01900000-0000-7000-8000-000000000003"
const projectAuditMaxScalar = "9223372036854775807"

func projectAuditTestCases() []typedCase {
	cases := []typedCase{}
	for _, v := range typedCases() {
		if _, ok := projectAuditSummary(c.Action(v.action)); ok {
			cases = append(cases, v)
		}
	}
	id := `"` + wireID + `"`
	p := `"` + projectAuditWireID + `"`
	for _, v := range []struct{ action, phase string }{{"issue", "issued"}, {"complete", "sent"}, {"revoke", "revoked"}} {
		reason := ""
		if v.action == "revoke" {
			reason = `,"reason":"private_not_allowed"`
		}
		cases = append(cases, typedCase{"object.transfer." + v.action, "object_transfer", `{"object_id":` + id + `,"transfer_id":` + id + `,"initiator_kind":"agent_run","initiator_id":` + id + `,"initiator_execution_id":` + id + `,"media_type":"text/plain","byte_size":"9","sent_bytes":"9","phase":"` + v.phase + `"` + reason + `}`, identity.ObjectMaintenance, c.Success})
	}
	for _, v := range []struct{ action, phase string }{{"create", "published"}, {"read", "read"}, {"download", "sent"}} {
		sent := "9"
		if v.action == "create" {
			sent = "0"
		}
		cases = append(cases, typedCase{"artifact." + v.action, "artifact", `{"artifact_id":` + id + `,"object_id":` + id + `,"source_kind":"execution_file","source_id":` + id + `,"source_revision":"1","media_type":"text/plain","byte_size":"9","sent_bytes":"` + sent + `","phase":"` + v.phase + `"}`, "", c.Success})
	}
	cases = append(cases, typedCase{"artifact.list", "artifact_collection", `{"phase":"listed","count":"9"}`, "", c.Success})
	for _, suffix := range []string{"create.accepted", "create.completed", "update", "archive.accepted", "archive.completed", "restore", "delete.accepted", "lifecycle.retry"} {
		m := `{"project_id":` + p + `,"initiator_id":` + id + `,"project_version":"2"`
		kind := "project"
		service := identity.ServiceName("")
		switch suffix {
		case "create.accepted", "create.completed":
			m = `{"project_id":` + p + `,"initiator_id":` + id + `,"project_version":"1","creation_id":` + id + `,"creation_version":"1"`
			kind = "project_creation"
			if suffix == "create.completed" {
				service = identity.ProjectInitialization
			}
		case "update":
			m += `,"changed_fields":["description","name"]`
		case "archive.accepted":
			m += `,"from":"active","to":"archiving","action":"archive"`
		case "archive.completed":
			m += `,"from":"archiving","to":"archived","action":"archive"`
			service = identity.ProjectLifecycle
		case "restore":
			m += `,"from":"archived","to":"active","action":"restore"`
		case "delete.accepted":
			m += `,"from":"archived","to":"deleting","action":"delete"`
		case "lifecycle.retry":
			m += `,"action":"archive"`
		}
		if strings.HasPrefix(suffix, "archive.") || suffix == "delete.accepted" || suffix == "lifecycle.retry" {
			m += `,"operation_id":` + id + `,"operation_version":"1"`
			kind = "project_operation"
		}
		cases = append(cases, typedCase{"project." + suffix, kind, m + `}`, service, c.Success})
	}
	cases = append(cases, typedCase{"knowledge.delete_subtree", "knowledge_document", `{"project_id":` + p + `,"root_id":` + id + `,"initiator_id":` + id + `,"scope_digest":"sha256:` + strings.Repeat("a", 64) + `","deleted_count":"1"}`, "", c.Success})
	return cases
}
func projectAuditTestRecord(t *testing.T, tc typedCase, kind identity.ActorKind, extreme bool) c.SafeRecord {
	t.Helper()
	if extreme && (strings.HasPrefix(tc.action, "secret.") || tc.action == "outbound.access.deny") {
		tc.service = identity.ProjectInitialization
	}
	if extreme && strings.HasPrefix(tc.action, "object.") {
		tc.service = identity.ObjectMaintenance
	}
	if extreme && tc.action == "outbound.access.deny" {
		tc.resource = "secret"
	}
	if extreme && strings.HasPrefix(tc.action, "secret.") {
		tc.outcome = c.Unknown
	}
	p, _ := foundation.ParseID[identity.Project](projectAuditWireID)
	scope, _ := identity.InProject(p)
	user, _ := foundation.ParseID[identity.User](wireID)
	session, _ := foundation.ParseID[identity.Session]("01900000-0000-7000-8000-000000000002")
	actor, _ := identity.NewHuman(user, session)
	cause := "sha256:" + strings.Repeat("f", 64)
	if tc.action == "project.create.completed" || tc.action == "project.archive.completed" {
		cause = wireID
	}
	if tc.service != "" {
		registration, err := identity.RegisterService(tc.service)
		if err != nil {
			t.Fatal(err)
		}
		actor, err = registration.Actor(cause, scope)
		if err != nil {
			t.Fatal(err)
		}
	} else if kind == identity.AgentRun {
		a, _ := foundation.ParseID[identity.Agent](wireID)
		x, _ := foundation.ParseID[identity.Execution](wireID)
		actor, _ = identity.NewAgentRun(p, a, x)
	} else if kind == identity.Service {
		reg, _ := identity.RegisterService(identity.ProjectInitialization)
		actor, _ = reg.Actor(cause, scope)
	}
	raw := []byte(tc.metadata)
	if extreme {
		var m map[string]any
		if json.Unmarshal(raw, &m) != nil {
			t.Fatal("metadata fixture")
		}
		for _, k := range []string{"version", "creation_version", "operation_version", "source_revision", "byte_size", "sent_bytes", "count", "redrive_cycle", "affected_count", "deleted_count"} {
			if _, ok := m[k]; ok {
				m[k] = projectAuditMaxScalar
			}
		}
		if _, ok := m["project_version"]; ok && !strings.HasPrefix(tc.action, "project.create.") {
			m["project_version"] = projectAuditMaxScalar
		}
		if _, ok := m["media_type"]; ok {
			m["media_type"] = strings.Repeat("&", 256)
		}
		if _, ok := m["consumer"]; ok {
			m["consumer"] = "object_storage"
		}
		if _, ok := m["reason"]; ok {
			m["reason"] = "private_not_allowed"
		}
		if tc.action == "secret.resolve" {
			m["reason"] = "private_not_allowed"
		}
		if tc.action == "secret.update" {
			m["changed_fields"] = []string{"purpose", "value"}
		}
		if strings.HasPrefix(tc.action, "object.") {
			m["initiator_kind"] = "agent_run"
			m["initiator_execution_id"] = wireID
		}
		if strings.HasPrefix(tc.action, "artifact.") && tc.action != "artifact.list" {
			m["source_kind"] = "uploaded_object"
		}
		if tc.action == "artifact.download" {
			m["phase"] = "failed"
			m["reason"] = "private_not_allowed"
			tc.outcome = c.Failed
		}
		if tc.action == "artifact.create" {
			m["sent_bytes"] = "0"
		}
		if tc.action == "outbox.delivery.requeue" {
			m["handler_id"] = strings.Repeat("a", 128)
			m["reason_code"] = "dependency_restored"
		}
		raw, _ = json.Marshal(m)
	}
	metadata, err := c.DecodeMetadata(c.Action(tc.action), raw)
	if err != nil {
		t.Fatal("formal metadata", tc.action, err)
	}
	rid := wireID
	if tc.resource == "project" || tc.resource == "artifact_collection" {
		rid = p.String()
	}
	if tc.resource == "outbound_policy" {
		rid = ""
	}
	resource, err := c.NewResource(c.ResourceKind(tc.resource), rid)
	if err != nil {
		t.Fatal(err)
	}
	links := c.Associations{}
	if extreme {
		links = c.Associations{ToolID: wireID, ExecutionID: wireID, ToolCallID: wireID, OperationID: wireID, RequestID: wireID, ApprovalID: wireID, RunnerID: wireID, CorrelationID: wireID, HTTPTraceID: wireID}
		if c.ProjectAction(c.Action(tc.action)) {
			links = c.Associations{RequestID: wireID, RunnerID: wireID, CorrelationID: wireID, HTTPTraceID: wireID}
			if strings.Contains(tc.metadata, "operation_id") {
				links.OperationID = wireID
			}
		}
		if c.ModelAction(c.Action(tc.action)) {
			links = c.Associations{CorrelationID: wireID, HTTPTraceID: wireID}
		}
		if c.KnowledgeAction(c.Action(tc.action)) {
			links = c.Associations{}
		}
	}
	entry, err := c.NewEntry(c.EntryFields{Scope: scope, Actor: actor, Action: c.Action(tc.action), Outcome: tc.outcome, Resource: resource, Metadata: metadata, Associations: links})
	if err != nil {
		t.Fatal("formal entry", tc.action, err)
	}
	f := entry.Fields()
	d := actor.Details()
	summary := c.ActorSummary{Kind: d.Kind, ID: d.UserID}
	if d.Kind == identity.AgentRun {
		summary = c.ActorSummary{Kind: d.Kind, ID: d.AgentID, ProjectID: d.ProjectID, ExecutionID: d.ExecutionID}
	}
	if d.Kind == identity.Service {
		summary = c.ActorSummary{Kind: d.Kind, ProjectID: d.ProjectID, Service: d.ServiceName, CauseRef: d.CauseRef}
	}
	id, _ := foundation.ParseID[c.Record](wireID)
	at, _ := foundation.ParseInstant("9999-12-31T23:59:59.999999Z")
	safeSummary, _ := projectAuditSummary(f.Action)
	return c.SafeRecord{AppendReceipt: c.AppendReceipt{AuditID: id, CreatedAt: at}, Scope: scope, Actor: summary, Action: f.Action, Outcome: f.Outcome, Resource: f.Resource, Metadata: f.Metadata, Associations: f.Associations, Summary: safeSummary}
}

type projectAuditSchemaVector struct {
	Name, Schema string
	Body         json.RawMessage
	Valid        bool
}

func TestProjectAuditVariableWire(t *testing.T) {
	p, _ := foundation.ParseID[identity.Project](projectAuditWireID)
	var vectors []projectAuditSchemaVector
	for _, change := range []string{"create", "update", "delete"} {
		version, fields := "2", `["description","value"]`
		if change == "create" {
			version, fields = "1", `["created"]`
		}
		if change == "delete" {
			fields = `["deleted"]`
		}
		tc := typedCase{"project.variable." + change, "project_variable", `{"variable_id":"` + wireID + `","version":"` + version + `","changed_fields":` + fields + `}`, "", c.Success}
		r := projectAuditTestRecord(t, tc, identity.Human, false)
		body, err := projectAuditEncodeRecord(context.Background(), p, r.AuditID, r)
		if err != nil {
			t.Fatal(err)
		}
		vectors = append(vectors, projectAuditSchemaVector{change, "ProjectAuditRecord", body, true})
		for name, mutate := range map[string]func(*c.SafeRecord){
			"actor": func(v *c.SafeRecord) {
				v.Actor.Kind = identity.Service
				v.Actor.Service = identity.ProjectInitialization
			},
			"association": func(v *c.SafeRecord) { v.Associations.RequestID = wireID },
			"outcome":     func(v *c.SafeRecord) { v.Outcome = c.Denied },
			"resource":    func(v *c.SafeRecord) { v.Resource, _ = c.NewResource(c.ProjectVariableResource, projectAuditWireID) },
		} {
			bad := r
			mutate(&bad)
			if b, e := projectAuditEncodeRecord(context.Background(), p, bad.AuditID, bad); e == nil || len(b) != 0 {
				t.Fatal(change, name, "accepted")
			}
		}
		var wire map[string]any
		if json.Unmarshal(body, &wire) != nil {
			t.Fatal("wire")
		}
		wire["metadata"].(map[string]any)["value"] = "private-canary"
		bad, _ := json.Marshal(wire)
		vectors = append(vectors, projectAuditSchemaVector{change + "/value", "ProjectAuditRecord", bad, false})
	}
	projectAuditStandardSchema(t, vectors)
}

func projectAuditStandardSchema(t *testing.T, vectors []projectAuditSchemaVector) {
	t.Helper()
	python := os.Getenv("AGENTEAM_PROJECT_AUDIT_SCHEMA_PYTHON")
	if python == "" {
		t.Fatal("fixed standard schema interpreter required")
	}
	root, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "api", "openapi"))
	if err != nil {
		t.Fatal(err)
	}
	input, _ := json.Marshal(vectors)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, "-c", projectAuditSchemaProgram, root)
	cmd.Stdin = bytes.NewReader(input)
	started := time.Now()
	output, err := cmd.CombinedOutput()
	t.Logf("SCHEMA_PROCESS wait_returned=true elapsed=%s context_error=%v vectors=%d", time.Since(started), ctx.Err(), len(vectors))
	if err != nil {
		t.Fatalf("standard schema validation failed: %v %s", err, output)
	}
	t.Log(strings.TrimSpace(string(output)))
}

const projectAuditSchemaProgram = `
import sys,json,pathlib,re,calendar,time
started=time.monotonic()
def diagnostic(stage,*,name=None,index=None,duration=None):
 # Only fixed stages and bounded fixture/schema labels, never Body, errors,
 # schema contents, argv or environment. Flush preserves the last stage if
 # the unchanged parent deadline kills this process during validation.
 fields={'stage':stage,'elapsed_seconds':round(time.monotonic()-started,6)}
 if name is not None:fields['name']=name if isinstance(name,str) and re.fullmatch(r'[A-Za-z0-9_.:/+\-]{1,160}',name) else 'non-label'
 if index is not None:fields['index']=index
 if duration is not None:fields['duration_seconds']=round(duration,6)
 print('SCHEMA_DIAGNOSTIC '+json.dumps(fields,sort_keys=True),flush=True)
diagnostic('imports.begin')
from jsonschema import Draft202012Validator,FormatChecker
from referencing import Registry,Resource
from referencing.jsonschema import DRAFT202012
diagnostic('imports.end')
diagnostic('documents.begin')
root=pathlib.Path(sys.argv[1]); doc=json.loads((root/'project-audit.json').read_bytes()); common=json.loads((root/'common.json').read_bytes())
base=(root/'project-audit.json').as_uri(); registry=Registry().with_resource(base,Resource.from_contents(doc,default_specification=DRAFT202012)).with_resource((root/'common.json').as_uri(),Resource.from_contents(common,default_specification=DRAFT202012)).crawl()
diagnostic('documents.end')
checker=FormatChecker()
@checker.checks('date-time')
def instant(s):
 if not isinstance(s,str):return True
 m=re.fullmatch(r'(\d{4})-(\d\d)-(\d\d)T(\d\d):(\d\d):(\d\d)(?:\.\d+)?Z',s)
 if not m:return False
 y,mo,d,h,mi,se=map(int,m.groups());return 1<=mo<=12 and 1<=d<=calendar.monthrange(y,mo)[1] and h<24 and mi<60 and se<60
for name,schema in doc['components']['schemas'].items():
 diagnostic('check_schema.begin',name=name); phase=time.monotonic()
 Draft202012Validator.check_schema(schema)
 diagnostic('check_schema.end',name=name,duration=time.monotonic()-phase)
diagnostic('vectors.read.begin')
vectors=json.load(sys.stdin)
diagnostic('vectors.read.end')
validators={}
for index,v in enumerate(vectors):
 diagnostic('vector.begin',name=v['Name'],index=index); phase=time.monotonic()
 if v['Schema'] not in validators:
  schema={'$ref':base+'#/components/schemas/'+v['Schema']}
  validators[v['Schema']]=Draft202012Validator(schema,registry=registry,format_checker=checker)
 valid=validators[v['Schema']].is_valid(v['Body'])
 diagnostic('vector.end',name=v['Name'],index=index,duration=time.monotonic()-phase)
 if valid!=v['Valid']:raise SystemExit('vector disagrees: '+v['Name'])
print('standard Draft2020-12 accepted '+str(len(vectors))+' vectors with fixed local common refs')
`

func TestProjectAuditHTTPPureWireAndMaximumPage(t *testing.T) {
	p, _ := foundation.ParseID[identity.Project](projectAuditWireID)
	cases := projectAuditTestCases()
	if len(cases) != 31 {
		t.Fatal("action closure", len(cases))
	}
	vectors := []projectAuditSchemaVector{}
	var largest c.SafeRecord
	maxBytes := 0
	for _, tc := range cases {
		t.Run(tc.action, func(t *testing.T) {
			actor := identity.Human
			if strings.HasPrefix(tc.action, "artifact.") {
				actor = identity.AgentRun
			}
			r := projectAuditTestRecord(t, tc, actor, true)
			body, err := projectAuditEncodeRecord(context.Background(), p, r.AuditID, r)
			if err != nil {
				t.Fatal(err)
			}
			if len(body) > maxBytes {
				maxBytes = len(body)
				largest = r
			}
			t.Logf("maximum representative metadata=%d record=%d", len(r.Metadata.JSON()), len(body))
			vectors = append(vectors, projectAuditSchemaVector{tc.action, "ProjectAuditRecord", body, true})
			var value map[string]any
			if json.Unmarshal(body, &value) != nil || len(value) != 11 || value["project_id"] != p.String() {
				t.Fatal("unsafe/lossy projection")
			}
			value["session_id"] = wireID
			bad, _ := json.Marshal(value)
			vectors = append(vectors, projectAuditSchemaVector{tc.action + "/extra", "ProjectAuditRecord", bad, false})
			r.Summary = "unsafe-source-canary"
			if b, e := projectAuditEncodeRecord(context.Background(), p, r.AuditID, r); e == nil || len(b) != 0 {
				t.Fatal("unsafe summary accepted")
			}
		})
	}
	for _, tc := range cases {
		if strings.HasPrefix(tc.action, "secret.") || tc.action == "outbound.access.deny" || strings.HasPrefix(tc.action, "artifact.") {
			for _, actor := range []identity.ActorKind{identity.Human, identity.AgentRun, identity.Service} {
				if strings.HasPrefix(tc.action, "artifact.") && actor == identity.Service {
					continue
				}
				tc.service = ""
				r := projectAuditTestRecord(t, tc, actor, false)
				body, err := projectAuditEncodeRecord(context.Background(), p, r.AuditID, r)
				if err != nil {
					t.Fatal(tc.action, actor, err)
				}
				vectors = append(vectors, projectAuditSchemaVector{tc.action + "/" + string(actor), "ProjectAuditRecord", body, true})
			}
		}
	}
	page := foundation.Page[c.SafeRecord]{NextCursor: strings.Repeat("x", 8192)}
	for i := 0; i < 200; i++ {
		r := largest
		r.AuditID, _ = foundation.ParseID[c.Record](fmt.Sprintf("01900000-0000-7000-8000-%012x", 1000-i))
		page.Items = append(page.Items, r)
	}
	body, err := projectAuditEncodePage(context.Background(), p, c.Filter{}, foundation.PageRequest{Limit: 200}, page)
	if err != nil || len(body) > 1<<20 {
		t.Fatal("legal maximal page", len(body), err)
	}
	var decoded struct {
		Items      []projectAuditRecordDTO
		NextCursor string `json:"next_cursor"`
	}
	if json.Unmarshal(body, &decoded) != nil || len(decoded.Items) != 200 || decoded.NextCursor != page.NextCursor {
		t.Fatal("page clipped")
	}
	for i, r := range decoded.Items {
		if r.AuditID != page.Items[i].AuditID || !bytes.Equal(r.Metadata, page.Items[i].Metadata.JSON()) {
			t.Fatal("page metadata/identity truncated")
		}
	}
	t.Logf("MAXIMUM_PAGE actual_bytes=%d records=200 largest_action=%s largest_record=%d cursor_bytes=8192 budget=1048576", len(body), largest.Action, maxBytes)
	vectors = append(vectors, projectAuditSchemaVector{"maximum-page", "ProjectAuditPage", body, true})
	for _, change := range []func(*foundation.Page[c.SafeRecord]){func(v *foundation.Page[c.SafeRecord]) { v.Items = append(v.Items, v.Items[0]) }, func(v *foundation.Page[c.SafeRecord]) { v.NextCursor += "x" }, func(v *foundation.Page[c.SafeRecord]) { v.Items[199] = v.Items[0] }} {
		bad := page
		bad.Items = append([]c.SafeRecord(nil), page.Items...)
		change(&bad)
		if b, e := projectAuditEncodePage(context.Background(), p, c.Filter{}, foundation.PageRequest{Limit: 200}, bad); e == nil || len(b) != 0 {
			t.Fatal("bad page candidate")
		}
	}
	for _, stamp := range []string{"0000-02-29T00:00:00Z", "9999-12-31T23:59:59.999999Z"} {
		r := largest
		r.CreatedAt, _ = foundation.ParseInstant(stamp)
		b, e := projectAuditEncodeRecord(context.Background(), p, r.AuditID, r)
		if e != nil {
			t.Fatal(e)
		}
		vectors = append(vectors, projectAuditSchemaVector{stamp, "ProjectAuditRecord", b, true})
	}
	vectors = append(vectors, projectAuditOptionalBranches(t, cases)...)
	// Capacity uses an 8192-byte safe shape, not a claimed authentic cursor.
	// Also encode/verify the real signer representation of this exact last item.
	keys, e := cursor.LoadKeyring(`{"format":1,"current_kid":"test","keys":[{"kid":"test","key_b64":"AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="}]}`)
	if e != nil {
		t.Fatal(e)
	}
	scope, _ := identity.InProject(p)
	digest, e := cursor.Digest([]byte(`{"from":null,"to":null,"actor_kind":"","actor_id":"","action":"","outcome":"","resource_kind":"","resource_id":"","tool_id":"","execution_id":"","operation_id":"","approval_id":"","runner_id":"","agent_id":""}`))
	if e != nil {
		t.Fatal(e)
	}
	last := page.Items[199]
	at, e := cursor.Instant(last.CreatedAt)
	if e != nil {
		t.Fatal(e)
	}
	id, e := cursor.UUID(last.AuditID.String())
	if e != nil {
		t.Fatal(e)
	}
	binding := cursor.Binding{Scope: scope, QueryDigest: digest, Order: cursor.AuditOrder}
	token, e := keys.Sign(binding, cursor.Position{Scalars: []cursor.Scalar{at, id}})
	if e != nil {
		t.Fatal(e)
	}
	position, e := keys.Verify(token, binding)
	if e != nil || position.Scalars[1].Value() != last.AuditID.String() {
		t.Fatal("real signed last retained position")
	}
	page.NextCursor = token
	signed, e := projectAuditEncodePage(context.Background(), p, c.Filter{}, foundation.PageRequest{Limit: 200}, page)
	if e != nil {
		t.Fatal(e)
	}
	vectors = append(vectors, projectAuditSchemaVector{"real-signer-page", "ProjectAuditPage", signed, true})
	t.Logf("REAL_SIGNER_PAGE bytes=%d token_bytes=%d; SQL 201 sentinel/sign-last-row is a separate query/PG acceptance item", len(signed), len(token))
	projectAuditStandardSchema(t, vectors)
}

// A bounded branch map, not an assertion that a finite sample exhausts the
// cross product. Optional fields are exercised present at their legal maximum
// and absent; closed actor/phase/source alternatives are independently varied.
func projectAuditOptionalBranches(t *testing.T, cases []typedCase) []projectAuditSchemaVector {
	t.Helper()
	p, _ := foundation.ParseID[identity.Project](projectAuditWireID)
	vectors := []projectAuditSchemaVector{}
	byAction := map[string]typedCase{}
	for _, tc := range cases {
		byAction[tc.action] = tc
	}
	add := func(name string, tc typedCase, actor identity.ActorKind) {
		t.Helper()
		r := projectAuditTestRecord(t, tc, actor, false)
		b, e := projectAuditEncodeRecord(context.Background(), p, r.AuditID, r)
		if e != nil {
			t.Fatal(name, e)
		}
		vectors = append(vectors, projectAuditSchemaVector{name, "ProjectAuditRecord", b, true})
	}
	edit := func(tc typedCase, change func(map[string]any)) typedCase {
		var m map[string]any
		if json.Unmarshal([]byte(tc.metadata), &m) != nil {
			t.Fatal("branch metadata")
		}
		change(m)
		raw, _ := json.Marshal(m)
		tc.metadata = string(raw)
		return tc
	}
	for _, tc := range cases {
		if strings.HasPrefix(tc.action, "object.") {
			for _, actor := range []string{"human", "agent_run", "service"} {
				for _, service := range []identity.ServiceName{identity.ObjectService, identity.ObjectMaintenance} {
					v := edit(tc, func(m map[string]any) {
						m["initiator_kind"] = actor
						delete(m, "initiator_execution_id")
						if actor == "agent_run" {
							m["initiator_execution_id"] = wireID
						}
						if tc.action == "object.transfer.revoke" {
							delete(m, "reason")
						}
					})
					v.service = service
					add(tc.action+"/initiator-"+actor+"/"+string(service), v, identity.Human)
				}
			}
		}
		if strings.HasPrefix(tc.action, "secret.") {
			for _, o := range []c.Outcome{c.Success, c.Denied, c.Failed, c.Unknown} {
				v := tc
				v.outcome = o
				add(tc.action+"/"+string(o), v, identity.Human)
			}
		}
		if tc.action == "project.delete.accepted" {
			add("delete-active", edit(tc, func(m map[string]any) { m["from"] = "active" }), identity.Human)
		}
		if tc.action == "project.lifecycle.retry" {
			add("retry-delete", edit(tc, func(m map[string]any) { m["action"] = "delete" }), identity.Human)
		}
	}
	for _, action := range []string{"artifact.create", "artifact.read", "artifact.download"} {
		for _, source := range []string{"", "inline", "uploaded_object", "artifact_file", "knowledge_file", "execution_file"} {
			if action == "artifact.create" && source == "" {
				continue
			}
			v := edit(byAction[action], func(m map[string]any) {
				delete(m, "source_kind")
				delete(m, "source_id")
				delete(m, "source_revision")
				if source != "" {
					m["source_kind"] = source
				}
				if source != "" && source != "inline" {
					m["source_id"] = wireID
				}
			})
			add(action+"/source-"+source, v, identity.AgentRun)
		}
	}
	for _, phase := range []string{"issued", "started", "sent", "failed"} {
		v := edit(byAction["artifact.download"], func(m map[string]any) {
			m["phase"] = phase
			if phase == "failed" {
				m["reason"] = "private_not_allowed"
			}
		})
		if phase == "failed" {
			v.outcome = c.Failed
		}
		add("download-phase-"+phase, v, identity.Human)
	}
	for _, resource := range []string{"outbound_policy", "agent", "secret"} {
		v := byAction["outbound.access.deny"]
		v.resource = resource
		add("access-resource-"+resource, v, identity.AgentRun)
	}
	add("model-delete-no-replacement", edit(byAction["model.delete"], func(m map[string]any) {
		delete(m, "replacement_id")
		m["affected_count"] = "0"
		m["changed_fields"] = []string{"deleted"}
	}), identity.Human)
	// The portable schema does not encode cross-field UUID equality. Prove those
	// semantic guards using the production projection, separately from schema.
	for _, name := range []string{"project_id", "resource_id", "actor_project", "operation_equality"} {
		r := projectAuditTestRecord(t, byAction["project.archive.completed"], identity.Human, true)
		other := "01900000-0000-7000-8000-000000000004"
		switch name {
		case "project_id":
			foreign, _ := foundation.ParseID[identity.Project](other)
			r.Scope, _ = identity.InProject(foreign)
		case "resource_id":
			r.Resource, _ = c.NewResource(c.ProjectOperationResource, other)
		case "actor_project":
			r.Actor.ProjectID = other
		case "operation_equality":
			r.Associations.OperationID = other
		}
		if b, e := projectAuditEncodeRecord(context.Background(), p, r.AuditID, r); e == nil || len(b) != 0 {
			t.Fatal("cross-field guard", name)
		}
	}
	negative := func(name string, tc typedCase, change func(map[string]any)) {
		t.Helper()
		r := projectAuditTestRecord(t, tc, identity.Human, true)
		body, e := projectAuditEncodeRecord(context.Background(), p, r.AuditID, r)
		if e != nil {
			t.Fatal(e)
		}
		var value map[string]any
		json.Unmarshal(body, &value)
		change(value)
		body, e = json.Marshal(value)
		if e != nil {
			t.Fatal(e)
		}
		vectors = append(vectors, projectAuditSchemaVector{name, "ProjectAuditRecord", body, false})
	}
	for _, action := range []string{"secret.create", "object.transfer.revoke", "project.update", "knowledge.delete_subtree"} {
		for _, field := range []string{"audit_id", "created_at", "scope", "project_id", "actor", "action", "outcome", "resource", "metadata", "associations", "summary"} {
			negative(action+"/missing-"+field, byAction[action], func(v map[string]any) { delete(v, field) })
			negative(action+"/null-"+field, byAction[action], func(v map[string]any) { v[field] = nil })
		}
	}
	for _, tc := range cases {
		negative(tc.action+"/system-action", tc, func(v map[string]any) { v["action"] = "account.login" })
		negative(tc.action+"/foreign-resource", tc, func(v map[string]any) { v["resource"].(map[string]any)["kind"] = "secret_master" })
	}
	for _, action := range []string{"project.create.accepted", "project.create.completed", "project.update", "project.restore"} {
		negative(action+"/forbidden-operation", byAction[action], func(v map[string]any) { v["associations"].(map[string]any)["operation_id"] = wireID })
	}
	for _, ending := range []string{"\n", "\r", "\r\n", "\u2028", "\u2029"} {
		negative("handler-ending-"+fmt.Sprintf("%x", ending), byAction["outbox.delivery.requeue"], func(v map[string]any) { v["metadata"].(map[string]any)["handler_id"] = "a" + ending })
	}
	for _, value := range []string{"0", "01", "9223372036854775808", "1e3"} {
		negative("version-"+value, byAction["secret.create"], func(v map[string]any) { v["metadata"].(map[string]any)["version"] = value })
	}
	for _, value := range []string{strings.Repeat("a", 257), "text/plain; filename=secret", "TEXT/plain", "text/plain\n"} {
		negative("mime-"+fmt.Sprint(len(value))+value[:1], byAction["object.transfer.revoke"], func(v map[string]any) { v["metadata"].(map[string]any)["media_type"] = value })
	}
	negative("duplicate-fields", byAction["secret.create"], func(v map[string]any) { v["metadata"].(map[string]any)["changed_fields"] = []string{"value", "value"} })
	negative("numeric-not-string", byAction["secret.create"], func(v map[string]any) { v["metadata"].(map[string]any)["version"] = 1 })
	negative("download-phase-outcome", byAction["artifact.download"], func(v map[string]any) { v["outcome"] = "success" })
	negative("object-human-actor", byAction["object.transfer.revoke"], func(v map[string]any) { v["actor"] = map[string]any{"kind": "human", "id": wireID} })
	negative("project-wrong-service", byAction["project.archive.completed"], func(v map[string]any) { v["actor"].(map[string]any)["service"] = "secret" })
	negative("secret-unknown-actor", byAction["secret.create"], func(v map[string]any) { v["actor"].(map[string]any)["kind"] = "system" })
	return vectors
}
