package builtin

import (
	"encoding/json"

	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

const skillInstallInputSchema = `{
  "$schema":"https://json-schema.org/draft/2020-12/schema",
  "type":"object","additionalProperties":false,"required":["source","mode"],
  "properties":{
    "source":{
      "type":"object","additionalProperties":false,"required":["kind","files"],
      "properties":{
        "kind":{"type":"string","const":"text_files"},
        "files":{"type":"array","minItems":1,"maxItems":128,"items":{
          "type":"object","additionalProperties":false,"required":["path","utf8_text"],
          "properties":{
            "path":{"type":"string","minLength":1,"maxLength":1024},
            "utf8_text":{"type":"string","maxLength":1048576}
          }
        }}
      }
    },
    "mode":{"type":"string","const":"create"}
  }
}`

const skillInstallOutputSchema = `{
  "$schema":"https://json-schema.org/draft/2020-12/schema",
  "type":"object","additionalProperties":false,"required":["skill_id","revision","version"],
  "properties":{
    "skill_id":{"type":"string","pattern":"^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"},
    "revision":{"type":"string","const":"1"},
    "version":{"type":"string","const":"1"}
  }
}`

// SkillInstallDefinition is metadata only, not registry.BuiltinSource. There is
// no Active registration, invented runtime authority or Backend success here.
// Every call owns its schema and annotation copies.
func SkillInstallDefinition() tc.Definition {
	return tc.Definition{
		StableKey:    SkillInstallStableKey,
		Name:         "install-skill",
		Description:  "Create a new skill in the current project from a validated text-file package. Does not assign the skill or execute its contents.",
		InputSchema:  json.RawMessage(skillInstallInputSchema),
		OutputSchema: json.RawMessage(skillInstallOutputSchema),
		Annotations:  &tc.Annotations{ReadOnly: false, Destructive: false, Idempotency: tc.Keyed},
	}
}
