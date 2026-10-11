package contract

import (
	"encoding/json"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"testing"
)

func TestSkillAssignmentAuditClosedTypedScope(t *testing.T) {
	const rawID = "01900000-0000-7000-8000-000000000041"
	fields := SkillAssignmentMetadataFields{AgentID: rawID, AssignmentID: rawID, SkillID: rawID, ChangeID: rawID, Version: 2, Sequence: 2, Enabled: true}
	meta, err := SkillAssignmentMetadata(SkillAssign, fields)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeMetadata(SkillAssign, meta.JSON())
	if err != nil {
		t.Fatal(err)
	}
	got, err := decoded.SkillAssignmentFields()
	if err != nil || got != fields {
		t.Fatal("typed metadata lost identity")
	}
	user, _ := f.ParseID[i.User](rawID)
	session, _ := f.ParseID[i.Session](rawID)
	actor, _ := i.NewHuman(user, session)
	project, _ := f.ParseID[i.Project](rawID)
	scope, _ := i.InProject(project)
	resource, _ := NewResource(AgentResource, rawID)
	entry := EntryFields{Actor: actor, Scope: scope, Resource: resource, Action: SkillAssign, Outcome: Success, Metadata: meta}
	if _, err = NewEntry(entry); err != nil || ProducerFor(SkillAssign) != SkillProducer {
		t.Fatal("explicit closed Skill audit tuple rejected")
	}
	entry.Scope = i.SystemScope()
	if _, err = NewEntry(entry); err == nil {
		t.Fatal("assignment audit escaped project scope")
	}
	for _, name := range []string{"null-enabled", "extra", "zero-version", "removed-enabled"} {
		t.Run(name, func(t *testing.T) {
			var wire map[string]any
			if json.Unmarshal(meta.JSON(), &wire) != nil {
				t.Fatal("wire")
			}
			action := SkillAssign
			switch name {
			case "null-enabled":
				wire["enabled"] = nil
			case "extra":
				wire["root_path"] = "must-not-accept"
			case "zero-version":
				wire["version"] = "0"
			default:
				action = SkillRemove
			}
			raw, _ := json.Marshal(wire)
			if _, err := DecodeMetadata(action, raw); err == nil {
				t.Fatal("invalid closed assignment audit accepted")
			}
		})
	}
}
