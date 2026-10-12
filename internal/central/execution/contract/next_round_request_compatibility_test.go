package contract_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

// These expected bytes were captured from the unchanged, delivered 448f6ade
// codec before the shared Model implementation existed. Never regenerate them
// with the implementation under test. The capture method lives in task evidence.
func TestNextRoundRequestPreservesCapturedLegacyGolden(t *testing.T) {
	const root = "testdata/successor_legacy"
	read := func(name string) []byte {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(root, name))
		inputOK(t, err)
		return raw
	}
	manifestRaw := read("manifest.json")
	if fmt.Sprintf("%x", sha256.Sum256(manifestRaw)) != "6f559fe198c72b86e05aba51bca77ab5495cd93f63385df8766f5f82357b0781" {
		t.Fatal("independent legacy capture manifest changed")
	}
	var manifest struct {
		SourceRef string `json:"source_ref"`
		Vectors   []struct {
			Name          string                         `json:"name"`
			RequestFile   string                         `json:"request_file"`
			SemanticFile  string                         `json:"semantic_file"`
			RequestBytes  int                            `json:"request_bytes"`
			SemanticBytes int                            `json:"semantic_bytes"`
			RequestDigest string                         `json:"request_digest"`
			InputDigest   string                         `json:"input_digest"`
			Fields        ec.NextRoundModelRequestFields `json:"fields"`
		} `json:"vectors"`
	}
	inputOK(t, json.Unmarshal(manifestRaw, &manifest))
	if manifest.SourceRef != "448f6adea22d62dcb71255a3c834dbb8f1b95d84" || len(manifest.Vectors) != 3 {
		t.Fatal("legacy fixture source or exact vector set changed")
	}
	for _, vector := range manifest.Vectors {
		t.Run(vector.Name, func(t *testing.T) {
			raw, semanticRaw := read(vector.RequestFile), read(vector.SemanticFile)
			if len(raw) != vector.RequestBytes || len(semanticRaw) != vector.SemanticBytes || fmt.Sprintf("sha256:%x", sha256.Sum256(raw)) != vector.RequestDigest || fmt.Sprintf("sha256:%x", sha256.Sum256(semanticRaw)) != vector.InputDigest {
				t.Fatal("original captured bytes or digests changed")
			}
			semantic, err := mc.DecodeSuccessorSemantic(context.Background(), semanticRaw)
			inputOK(t, err)
			parts, f := semantic.Parts(), vector.Fields
			input := ec.NextRoundModelRequestInput{
				ProjectID: f.ProjectID, AgentID: f.AgentID, ExecutionID: f.ExecutionID, RoundInputID: f.RoundInputID,
				RoundInputDigest: f.RoundInputDigest, SnapshotID: f.SnapshotID, SnapshotDigest: f.SnapshotDigest,
				ProjectionDigest: f.ProjectionDigest, CallID: f.CallID,
				Actor: parts.Actor, Model: parts.Model, Messages: parts.Messages, Tools: parts.Tools,
			}
			fromParts, err := ec.NewNextRoundModelRequest(context.Background(), input)
			inputOK(t, err)
			decoded, err := ec.DecodeNextRoundModelRequest(context.Background(), raw)
			inputOK(t, err)
			validated, err := mc.ValidateSuccessorRequest(context.Background(), decoded.Request())
			inputOK(t, err)
			for _, got := range []ec.NextRoundModelRequest{fromParts, decoded} {
				if got.Fields() != f || string(got.Digest()) != vector.RequestDigest || string(got.InputDigest()) != vector.InputDigest || !bytes.Equal(got.CanonicalBytes(), raw) {
					t.Fatal("delegation changed the original wrapper, identity, or two digests")
				}
			}
			if string(semantic.Digest()) != vector.InputDigest || !bytes.Equal(semantic.CanonicalBytes(), semanticRaw) || !bytes.Equal(validated.CanonicalBytes(), semanticRaw) {
				t.Fatal("shared codec changed the original semantic bytes")
			}
			input.CallID = inputID[mc.Call](t, 2402)
			otherCall, err := ec.NewNextRoundModelRequest(context.Background(), input)
			inputOK(t, err)
			if otherCall.InputDigest() != fromParts.InputDigest() || otherCall.Digest() == fromParts.Digest() {
				t.Fatal("logical call and semantic digest layers were conflated")
			}
		})
	}
}
