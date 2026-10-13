import { createHash, webcrypto } from 'node:crypto'
import { vi } from 'vitest'
import { createArtifactCreationAPI, type ArtifactContentInput } from '../api/artifact-creation'
import { createKnowledgeCommandsAPI } from '../api/knowledge-commands'
import { createKnowledgeTextAPI } from '../api/knowledge-text'
import { artifactRecord, artifactSessionFixture } from './project-artifacts.fixture'
export * from './project-artifacts.fixture'
export const creationInput: ArtifactContentInput = {
  name: '  文本产物🙂.md  ',
  media_type: 'text/markdown',
  content: '  # 中文\r\n<script>literal</script>\n  ',
  description: ' 保留描述 ',
}
export function createdRecord(input = creationInput) {
  return {
    ...artifactRecord(),
    name: input.name,
    description: input.description,
    object: {
      media_type: input.media_type,
      byte_size: String(Buffer.byteLength(input.content)),
      sha256: 'sha256:' + createHash('sha256').update(input.content).digest('hex'),
    },
  }
}
export function creationSessionFixture() {
  vi.stubGlobal('crypto', webcrypto)
  return artifactSessionFixture((fetch) => ({
    artifactCreation: createArtifactCreationAPI(fetch),
    knowledgeCommands: createKnowledgeCommandsAPI(fetch),
    knowledgeText: createKnowledgeTextAPI(fetch),
  }))
}
