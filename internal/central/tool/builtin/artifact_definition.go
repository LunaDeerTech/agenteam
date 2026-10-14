package builtin

import (
	"encoding/json"

	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

const (
	ArtifactCreateName      = "create-artifact"
	ArtifactReadName        = "read-artifact"
	ArtifactListName        = "list-artifacts"
	ArtifactCreateHandlerID = "artifact.create"
	ArtifactReadHandlerID   = "artifact.read"
	ArtifactListHandlerID   = "artifact.list"
	ArtifactScopeID         = tc.ScopeResolverID("artifact.project.v1")
	ArtifactCreateRiskID    = tc.RiskClassifierID("artifact.publish.v1")
	ArtifactReadRiskID      = tc.RiskClassifierID("artifact.read.v1")
)

const artifactReferenceSchema = `{"type":"object","additionalProperties":false,"required":["project_id","artifact_id","file_id"],"properties":{"project_id":{"type":"string","format":"uuid"},"artifact_id":{"type":"string","format":"uuid"},"file_id":{"type":"string","format":"uuid"}}}`
const artifactMetadataSchema = `{"type":"object","additionalProperties":false,"required":["artifact_ref","name","media_type","size"],"properties":{"artifact_ref":` + artifactReferenceSchema + `,"name":{"type":"string"},"description":{"type":"string"},"media_type":{"type":"string"},"size":{"type":"string","pattern":"^[0-9]+$"},"source_execution_id":{"type":"string"},"source_operation_id":{"type":"string"},"created_by_agent_id":{"type":"string"}}}`

// ArtifactDefinitions describes only the bound inline publication/read/list
// surface. Core registration and current execution authority belong to Runtime.
func ArtifactDefinitions() []tc.Definition {
	return []tc.Definition{
		{StableKey: "builtin:" + ArtifactCreateName, Name: ArtifactCreateName, Description: "Publish a named text, Markdown, JSON or CSV result in the current project. Returns an Artifact reference that reviewers and the user can read.", InputSchema: json.RawMessage(`{"type":"object","additionalProperties":false,"required":["name","media_type","content"],"properties":{"name":{"type":"string","minLength":1,"maxLength":255},"media_type":{"type":"string","minLength":1},"content":{"type":"string","maxLength":1048576},"description":{"type":"string","maxLength":4096}}}`), OutputSchema: json.RawMessage(artifactMetadataSchema), Annotations: &tc.Annotations{Idempotency: tc.Keyed}},
		{StableKey: "builtin:" + ArtifactReadName, Name: ArtifactReadName, Description: "Read the actual stored content of an Artifact in the current project. Use offset and limit to continue a truncated text result; binary files return a reference.", InputSchema: json.RawMessage(`{"type":"object","additionalProperties":false,"required":["artifact_ref"],"properties":{"artifact_ref":` + artifactReferenceSchema + `,"offset":{"type":"integer","minimum":0},"limit":{"type":"integer","minimum":1,"maximum":65536}}}`), OutputSchema: json.RawMessage(`{"type":"object","additionalProperties":false,"required":["artifact","projection","offset","next_offset","truncated"],"properties":{"artifact":` + artifactMetadataSchema + `,"projection":{"type":"string","enum":["text","image_ref","file_ref"]},"text":{"type":"string"},"offset":{"type":"string"},"next_offset":{"type":"string"},"truncated":{"type":"boolean"},"invalid_utf8":{"type":"boolean"}}}`), Annotations: &tc.Annotations{ReadOnly: true, Idempotency: tc.Inherent}},
		{StableKey: "builtin:" + ArtifactListName, Name: ArtifactListName, Description: "List Artifact references in the current project, optionally filtered by the worker execution_id. Read an Artifact reference to inspect its actual content.", InputSchema: json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"execution_id":{"type":"string","format":"uuid"},"kind":{"type":"string","enum":["generated","user_upload"]},"media_type":{"type":"string"},"name_query":{"type":"string","maxLength":256},"limit":{"type":"integer","minimum":1,"maximum":200},"cursor":{"type":"string","maxLength":8192}}}`), OutputSchema: json.RawMessage(`{"type":"object","additionalProperties":false,"required":["items"],"properties":{"items":{"type":"array","items":` + artifactMetadataSchema + `},"next_cursor":{"type":"string"}}}`), Annotations: &tc.Annotations{ReadOnly: true, Idempotency: tc.Inherent}},
	}
}

func ArtifactDefinition(name string) (tc.Definition, error) {
	for _, definition := range ArtifactDefinitions() {
		if definition.Name == name {
			return definition, nil
		}
	}
	return tc.Definition{}, artifactInvalid()
}
