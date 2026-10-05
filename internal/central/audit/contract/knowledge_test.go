package contract

import (
	"bytes"
	"encoding/json"
	"math"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func knowledgeAuditFixture(t *testing.T) (KnowledgeMetadataFields, EntryFields) {
	t.Helper()
	v := KnowledgeMetadataFields{ProjectID: "01900000-0000-7000-8000-000000000001", RootID: "01900000-0000-7000-8000-000000000002", InitiatorID: "01900000-0000-7000-8000-000000000003", ScopeDigest: f.Digest("sha256:" + strings.Repeat("1", 64)), DeletedCount: 3}
	p, e := f.ParseID[id.Project](v.ProjectID)
	if e != nil {
		t.Fatal(e)
	}
	scope, e := id.InProject(p)
	if e != nil {
		t.Fatal(e)
	}
	u, e := f.ParseID[id.User](v.InitiatorID)
	if e != nil {
		t.Fatal(e)
	}
	session, e := f.ParseID[id.Session]("01900000-0000-7000-8000-000000000004")
	if e != nil {
		t.Fatal(e)
	}
	actor, e := id.NewHuman(u, session)
	if e != nil {
		t.Fatal(e)
	}
	m, e := KnowledgeMetadata(KnowledgeDeleteSubtree, v)
	if e != nil {
		t.Fatal(e)
	}
	r, e := NewResource(KnowledgeDocumentResource, v.RootID)
	if e != nil {
		t.Fatal(e)
	}
	return v, EntryFields{Scope: scope, Actor: actor, Action: KnowledgeDeleteSubtree, Outcome: Success, Resource: r, Metadata: m}
}
func TestKnowledgeAuditClosedMetadataAndExactInteger(t *testing.T) {
	v, fields := knowledgeAuditFixture(t)
	for _, count := range []f.Progress{1, 9007199254740993, math.MaxInt64} {
		t.Run(count.String(), func(t *testing.T) {
			v.DeletedCount = count
			m, e := KnowledgeMetadata(KnowledgeDeleteSubtree, v)
			if e != nil {
				t.Fatal(e)
			}
			var wire map[string]json.RawMessage
			if e = json.Unmarshal(m.JSON(), &wire); e != nil {
				t.Fatal(e)
			}
			if len(wire) != 5 || string(wire["deleted_count"]) != `"`+count.String()+`"` {
				t.Fatal("count lost precision/metadata grew")
			}
			round, e := DecodeMetadata(KnowledgeDeleteSubtree, m.JSON())
			if e != nil || !bytes.Equal(round.JSON(), m.JSON()) {
				t.Fatal("roundtrip", e)
			}
			copy, e := round.KnowledgeFields()
			if e != nil || copy != v {
				t.Fatal("typed projection", e)
			}
			copy.RootID = "changed"
			again, _ := round.KnowledgeFields()
			if again.RootID != v.RootID {
				t.Fatal("projection aliases private data")
			}
		})
	}
	if _, e := NewEntry(fields); e != nil {
		t.Fatal(e)
	}
	if !KnowledgeAction(KnowledgeDeleteSubtree) || !KnowledgeDeleteSubtree.Valid() || ProducerFor(KnowledgeDeleteSubtree) != KnowledgeProducer || !KnowledgeProducer.Valid() || !KnowledgeDocumentResource.Valid() {
		t.Fatal("closed dispatch missing")
	}
	if KnowledgeAction("knowledge.create") || Action("knowledge.create").Valid() {
		t.Fatal("unapproved action")
	}
}
func TestKnowledgeAuditRejectsShapeInjection(t *testing.T) {
	v, _ := knowledgeAuditFixture(t)
	m, _ := KnowledgeMetadata(KnowledgeDeleteSubtree, v)
	raw := string(m.JSON())
	cases := map[string]string{
		"body":              strings.TrimSuffix(raw, "}") + `,"body":"secret"}`,
		"title":             strings.TrimSuffix(raw, "}") + `,"title":"old title"}`,
		"object":            strings.TrimSuffix(raw, "}") + `,"object_id":"` + v.RootID + `"}`,
		"token":             strings.TrimSuffix(raw, "}") + `,"confirmation":"signed"}`,
		"nodes":             strings.TrimSuffix(raw, "}") + `,"nodes":[]}`,
		"duplicate":         strings.TrimSuffix(raw, "}") + `,"root_id":"` + v.RootID + `"}`,
		"escaped_duplicate": strings.TrimSuffix(raw, "}") + `,"root\u005fid":"` + v.RootID + `"}`,
		"null":              strings.Replace(raw, `"deleted_count":"3"`, `"deleted_count":null`, 1),
		"number":            strings.Replace(raw, `"deleted_count":"3"`, `"deleted_count":3`, 1),
		"zero":              strings.Replace(raw, `"deleted_count":"3"`, `"deleted_count":"0"`, 1),
		"negative":          strings.Replace(raw, `"deleted_count":"3"`, `"deleted_count":"-1"`, 1),
		"leading_zero":      strings.Replace(raw, `"deleted_count":"3"`, `"deleted_count":"03"`, 1),
		"overflow":          strings.Replace(raw, `"deleted_count":"3"`, `"deleted_count":"9223372036854775808"`, 1),
		"missing":           strings.Replace(raw, `,"deleted_count":"3"`, "", 1),
		"trailing":          raw + ` {}`,
	}
	for name, b := range cases {
		t.Run(name, func(t *testing.T) {
			if _, e := DecodeMetadata(KnowledgeDeleteSubtree, []byte(b)); e == nil {
				t.Fatal("accepted invalid repository metadata")
			}
		})
	}
	for _, change := range []func(*KnowledgeMetadataFields){func(v *KnowledgeMetadataFields) { v.ProjectID = "" }, func(v *KnowledgeMetadataFields) { v.RootID = "01900000-0000-4000-8000-000000000002" }, func(v *KnowledgeMetadataFields) { v.InitiatorID = "not-id" }, func(v *KnowledgeMetadataFields) { v.ScopeDigest = "raw-key" }, func(v *KnowledgeMetadataFields) { v.DeletedCount = 0 }} {
		bad := v
		change(&bad)
		if _, e := KnowledgeMetadata(KnowledgeDeleteSubtree, bad); e == nil {
			t.Fatal("constructor accepted invalid metadata")
		}
	}
	if _, e := KnowledgeMetadata(ProjectDeleteAccepted, v); e == nil {
		t.Fatal("wrong action accepted")
	}
	old, _ := SecretMutationMetadata(SecretDelete, 1, nil)
	if _, e := old.KnowledgeFields(); e == nil {
		t.Fatal("other metadata projected as Knowledge")
	}
}
func TestKnowledgeAuditRequiresExactHumanProjectRoot(t *testing.T) {
	v, fields := knowledgeAuditFixture(t)
	other := "01900000-0000-7000-8000-000000000005"
	changes := map[string]func(*EntryFields){
		"system":          func(e *EntryFields) { e.Scope = id.SystemScope() },
		"foreign_project": func(e *EntryFields) { p, _ := f.ParseID[id.Project](other); e.Scope, _ = id.InProject(p) },
		"foreign_user": func(e *EntryFields) {
			u, _ := f.ParseID[id.User](other)
			s, _ := f.ParseID[id.Session](v.RootID)
			e.Actor, _ = id.NewHuman(u, s)
		},
		"foreign_root":   func(e *EntryFields) { e.Resource, _ = NewResource(KnowledgeDocumentResource, other) },
		"wrong_resource": func(e *EntryFields) { e.Resource, _ = NewResource(ObjectResource, v.RootID) },
		"failed":         func(e *EntryFields) { e.Outcome = Failed },
		"unknown":        func(e *EntryFields) { e.Outcome = Unknown },
		"service": func(e *EntryFields) {
			reg, _ := id.RegisterService(id.ObjectMaintenance)
			e.Actor, _ = reg.Actor(v.RootID, e.Scope)
		},
		"tool":      func(e *EntryFields) { e.Associations.ToolID = other },
		"execution": func(e *EntryFields) { e.Associations.ExecutionID = other },
		"approval":  func(e *EntryFields) { e.Associations.ApprovalID = other },
		"operation": func(e *EntryFields) { e.Associations.OperationID = other },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			bad := fields
			change(&bad)
			if _, e := NewEntry(bad); e == nil {
				t.Fatal("mismatched entry accepted")
			}
		})
	}
}
func TestKnowledgeAuditKeyIsDigestAndOrdinalZero(t *testing.T) {
	v, _ := knowledgeAuditFixture(t)
	if _, e := NewAppendKey(KnowledgeProducer, v.ScopeDigest.String(), 0); e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		cause   string
		ordinal int64
	}{{v.ScopeDigest.String(), 1}, {v.ScopeDigest.String(), -1}, {v.RootID, 0}, {"raw-client-key", 0}} {
		if _, e := NewAppendKey(KnowledgeProducer, tc.cause, tc.ordinal); e == nil {
			t.Fatal("noncanonical deletion append identity accepted")
		}
	}
	if _, e := NewAppendKey(ModelProducer, v.RootID, 2); e != nil {
		t.Fatal("legacy producer changed", e)
	}
}
