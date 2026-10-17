package contract_test

import (
	"bytes"
	"strings"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func TestDirectTextRoundPreservesArtifactMediaReference(t *testing.T) {
	fields := inputFixture(t)
	source := ec.CapturedArtifactAttachment{ProjectID: fields.Request.Launch.ProjectID, ArtifactID: inputID[ac.ArtifactEntity](t, 810), FileID: inputID[ac.File](t, 811), ArtifactVersion: 2, ObjectID: inputID[oc.StoredObject](t, 812), ObjectVersion: 3, SHA256: f.Digest("sha256:" + strings.Repeat("a", 64)), MediaType: "image/png", ByteSize: 8, Filename: "input.png"}
	part, err := source.MessagePart()
	inputOK(t, err)
	execution, round := fields.Request.ExecutionID, inputID[ec.Round](t, 813)
	value := ec.DirectTextRoundFields{ID: round, InputBindingID: inputID[ec.InputBinding](t, 814), ExecutionID: execution, SnapshotID: inputID[ec.Snapshot](t, 815), StartID: inputID[ec.DirectTextStart](t, 816), CallID: inputID[mc.Call](t, 817), ContextDigest: source.SHA256, Input: mc.InputIdentity{ExecutionID: &execution, RoundID: round.String(), Digest: source.SHA256, SchemaVersion: 1}, Messages: []mc.Message{{Role: "system", Parts: []mc.MessagePart{{Text: &mc.TextPart{Text: "System"}}}}, {Role: "user", Parts: []mc.MessagePart{{Text: &mc.TextPart{Text: "Inspect the image"}}, part}}}, CreatedAt: fields.CapturedAt}
	original, err := ec.NewDirectTextRound(value)
	inputOK(t, err)
	stored, err := ec.DecodeDirectTextRound(original.CanonicalBytes())
	inputOK(t, err)
	if stored.Digest() != original.Digest() || !source.MatchesReference(stored.Fields().Messages[1].Parts[1].Image.Ref) || !bytes.Contains(stored.CanonicalBytes(), []byte(source.ArtifactID.String())) || bytes.Contains(stored.CanonicalBytes(), []byte(source.ObjectID.String())) {
		t.Fatal("round lost business reference or exposed raw Object identity")
	}
	for _, alter := range []func(*ec.DirectTextRoundFields){
		func(v *ec.DirectTextRoundFields) { v.Messages[0].Parts = append(v.Messages[0].Parts, part) },
		func(v *ec.DirectTextRoundFields) {
			for len(v.Messages[1].Parts) <= ec.MaxInputAttachments+1 {
				v.Messages[1].Parts = append(v.Messages[1].Parts, part)
			}
		},
		func(v *ec.DirectTextRoundFields) {
			ref, err := oc.NewBusinessFileRef(oc.BusinessFileDetails{Kind: oc.MCPFile, ProjectID: source.ProjectID, FileID: inputID[i.Execution](t, 818).String()})
			inputOK(t, err)
			v.Messages[1].Parts[1].Image.Ref = ref
		},
	} {
		changed := value.Clone()
		alter(&changed)
		if _, err := ec.NewDirectTextRound(changed); err == nil {
			t.Fatal("invalid initial attachment shape accepted")
		}
	}
}
