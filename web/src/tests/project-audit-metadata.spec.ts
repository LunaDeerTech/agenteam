import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import {
  parseProjectAuditMetadata,
  projectAuditActions,
  projectAuditAssociationRows,
  projectAuditMetadataFields,
  projectAuditResourceKinds,
} from '../api/project-audit-metadata'
import { parseProjectAuditRecord } from '../api/project-audit'
import { auditMetadataJSONBytes, auditServices } from '../api/system-audit-metadata'

const id = (n: number) => `01970000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const project = id(1),
  human = { kind: 'human', id: id(2) }
const service = (name = 'object', cause = id(3)) => ({
  kind: 'service',
  service: name,
  cause_ref: cause,
  project_id: project,
})
const agent = { kind: 'agent_run', id: id(4), execution_id: id(5), project_id: project }
const max = '9223372036854775807'
type Vector = {
  metadata: Record<string, unknown>
  resource: Record<string, unknown>
  actor?: Record<string, unknown>
  outcome?: string
  associations?: Record<string, unknown>
}
const resource = (kind: string, n = 6) => ({ kind, id: id(n) })
const objectBase = {
  object_id: id(6),
  initiator_kind: 'human',
  initiator_id: id(2),
  media_type: 'text/{json}; charset=utf-8',
  byte_size: max,
  sent_bytes: '9007199254740993',
}
const artifactBase = {
  artifact_id: id(6),
  object_id: id(7),
  media_type: 'application/json',
  byte_size: max,
  sent_bytes: '0',
}
const projectBase = { project_id: project, initiator_id: id(2), project_version: '2' }
const operation = { ...projectBase, operation_id: id(8), operation_version: '1' }
const creation = { ...projectBase, project_version: '1', creation_id: id(9), creation_version: '1' }
// These inputs are transcribed from the accepted Project HTTP/contract rules:
// internal/central/audit/http/project_wire_test.go projectAuditTestCases and
// projectAuditOptionalCases; contract/{metadata,project,model,knowledge}.go.
// They are controlled frontend vectors, not a newly executed Go oracle.
const vectors: Record<string, Vector> = {
  'secret.create': {
    metadata: { version: '9007199254740993', changed_fields: ['purpose', 'value'] },
    resource: resource('secret'),
  },
  'secret.update': {
    metadata: { version: max, changed_fields: ['value'] },
    resource: resource('secret'),
  },
  'secret.delete': { metadata: { version: '1' }, resource: resource('secret') },
  'secret.resolve': {
    metadata: { lease_id: id(8), consumer: 'model' },
    resource: resource('secret'),
  },
  'outbound.access.deny': {
    metadata: { consumer: 'model', reason: 'address_forbidden', version: '1' },
    resource: { kind: 'outbound_policy' },
    outcome: 'denied',
  },
  'object.upload.complete': {
    metadata: { ...objectBase, phase: 'published' },
    resource: resource('stored_object'),
    actor: service(),
  },
  'object.upload.failed': {
    metadata: { ...objectBase, phase: 'failed', reason: 'integrity_mismatch' },
    resource: resource('stored_object'),
    actor: service(),
    outcome: 'unknown',
  },
  'object.delete': {
    metadata: { ...objectBase, phase: 'deleted' },
    resource: resource('stored_object'),
    actor: service(),
  },
  'object.transfer.issue': {
    metadata: { ...objectBase, transfer_id: id(8), phase: 'issued' },
    resource: resource('object_transfer', 8),
    actor: service(),
  },
  'object.transfer.complete': {
    metadata: { ...objectBase, transfer_id: id(8), sent_bytes: max, phase: 'sent' },
    resource: resource('object_transfer', 8),
    actor: service(),
  },
  'object.transfer.revoke': {
    metadata: { ...objectBase, transfer_id: id(8), phase: 'revoked' },
    resource: resource('object_transfer', 8),
    actor: service(),
  },
  'artifact.create': {
    metadata: { ...artifactBase, source_kind: 'inline', phase: 'published' },
    resource: resource('artifact'),
  },
  'artifact.read': { metadata: { ...artifactBase, phase: 'read' }, resource: resource('artifact') },
  'artifact.download': {
    metadata: { ...artifactBase, phase: 'sent', sent_bytes: '7' },
    resource: resource('artifact'),
    actor: agent,
    associations: { execution_id: id(5) },
  },
  'artifact.list': {
    metadata: { phase: 'listed', count: '0' },
    resource: resource('artifact_collection', 1),
  },
  'outbox.delivery.requeue': {
    metadata: {
      delivery_id: id(6),
      event_id: id(8),
      handler_id: 'handler.alpha-1',
      from_state: 'dead_letter',
      redrive_cycle: max,
      reason_code: 'schema_available',
    },
    resource: resource('outbox_delivery'),
  },
  'project.create.accepted': { metadata: creation, resource: resource('project_creation', 9) },
  'project.create.completed': {
    metadata: creation,
    resource: resource('project_creation', 9),
    actor: service('project-initialization', id(9)),
  },
  'project.update': {
    metadata: { ...projectBase, project_version: max, changed_fields: ['description', 'name'] },
    resource: resource('project', 1),
  },
  'project.archive.accepted': {
    metadata: { ...operation, from: 'active', to: 'archiving', action: 'archive' },
    resource: resource('project_operation', 8),
    associations: { operation_id: id(8) },
  },
  'project.archive.completed': {
    metadata: { ...operation, from: 'archiving', to: 'archived', action: 'archive' },
    resource: resource('project_operation', 8),
    actor: service('project-lifecycle', id(8)),
  },
  'project.restore': {
    metadata: { ...projectBase, from: 'archived', to: 'active', action: 'restore' },
    resource: resource('project', 1),
  },
  'project.delete.accepted': {
    metadata: { ...operation, from: 'active', to: 'deleting', action: 'delete' },
    resource: resource('project_operation', 8),
  },
  'project.lifecycle.retry': {
    metadata: { ...operation, action: 'archive' },
    resource: resource('project_operation', 8),
  },
  'provider.create': {
    metadata: { provider_id: id(6), version: '1', changed_fields: ['created'] },
    resource: resource('model_provider'),
  },
  'provider.update': {
    metadata: {
      provider_id: id(6),
      version: '2',
      changed_fields: ['base_url', 'credential_ref', 'enabled', 'name', 'provider_options'],
    },
    resource: resource('model_provider'),
  },
  'provider.delete': {
    metadata: { provider_id: id(6), version: max, changed_fields: ['deleted'] },
    resource: resource('model_provider'),
  },
  'model.create': {
    metadata: { provider_id: id(7), model_id: id(6), version: '1', changed_fields: ['created'] },
    resource: resource('model_config'),
  },
  'model.update': {
    metadata: {
      provider_id: id(7),
      model_id: id(6),
      version: '2',
      changed_fields: [
        'capabilities',
        'enabled',
        'header_overwrite',
        'model_id',
        'name',
        'parameters',
        'request_overwrite',
      ],
    },
    resource: resource('model_config'),
  },
  'model.delete': {
    metadata: {
      provider_id: id(7),
      model_id: id(6),
      version: max,
      changed_fields: ['deleted'],
      affected_count: '0',
    },
    resource: resource('model_config'),
  },
  'knowledge.delete_subtree': {
    metadata: {
      project_id: project,
      root_id: id(6),
      initiator_id: id(2),
      scope_digest: 'sha256:' + 'a'.repeat(64),
      deleted_count: max,
    },
    resource: resource('knowledge_document'),
  },
}
const summaries: Record<string, string> = {
  'secret.create': 'Secret created',
  'secret.update': 'Secret updated',
  'secret.delete': 'Secret deleted',
  'secret.resolve': 'Secret use recorded',
  'outbound.access.deny': 'Outbound access denied',
}
function row(action = 'secret.create') {
  const v = structuredClone(vectors[action]!)
  return {
    audit_id: id(30),
    created_at: '0000-02-29T12:34:56.000001Z',
    scope: 'project',
    project_id: project,
    actor: v.actor ?? structuredClone(human),
    action,
    outcome: v.outcome ?? 'success',
    resource: v.resource,
    metadata: v.metadata,
    associations: v.associations ?? {},
    summary: summaries[action] ?? 'Audit event',
  }
}
const accept = (value: unknown) => parseProjectAuditRecord(value, project)
const reject = (value: unknown) => expect(() => accept(value)).toThrow('invalid-response')

describe('Project Audit independent closed projection', () => {
  it('covers all 31 formal actions and exactly fourteen resources', () => {
    expect(projectAuditActions.slice().sort()).toEqual(Object.keys(vectors).sort())
    expect(Object.keys(vectors)).toHaveLength(31)
    const schema: {
      components: { schemas: Record<string, { properties?: { action?: { const?: string } } }> }
    } = JSON.parse(
      readFileSync(resolve(process.cwd(), '../api/openapi/project-audit.json'), 'utf8'),
    )
    const formalActions = Object.values(schema.components.schemas).flatMap((item) =>
      item.properties?.action?.const ? [item.properties.action.const] : [],
    )
    expect(Object.keys(vectors).sort()).toEqual(formalActions.sort())
    const covered = new Set(Object.values(vectors).map((v) => v.resource.kind))
    covered.add('agent')
    expect([...covered].sort()).toEqual([...projectAuditResourceKinds].sort())
  })
  for (const action of Object.keys(vectors)) {
    it(`accepts ${action}, publishes immutable data and displays every safe metadata key`, () => {
      const source = row(action),
        parsed = accept(source)
      expect(parsed.metadata).toEqual(source.metadata)
      expect(
        [parsed, parsed.metadata, parsed.resource, parsed.actor, parsed.associations].every(
          Object.isFrozen,
        ),
      ).toBe(true)
      if ('changed_fields' in parsed.metadata)
        expect(Object.isFrozen(parsed.metadata.changed_fields)).toBe(true)
      const display = projectAuditMetadataFields(parsed)
      expect(display.map((field) => field.key).sort()).toEqual(Object.keys(source.metadata).sort())
      expect(Object.isFrozen(display) && display.every(Object.isFrozen)).toBe(true)
      for (const value of [null, { ...source.metadata, private_value: 'must-not-publish' }])
        reject({ ...source, metadata: value })
      for (const key of Object.keys(source.metadata)) {
        reject({ ...source, metadata: { ...source.metadata, [key]: null } })
      }
      reject({ ...source, summary: '<b>untrusted</b>' })
      reject({ ...source, scope: 'system' })
      reject({ ...source, project_id: id(99) })
    })
  }
  it('rejects missing or extra record fields, all System-only actions, and wrong scalar encodings', () => {
    const base = row()
    for (const key of Object.keys(base)) {
      const bad: Record<string, unknown> = { ...base }
      delete bad[key]
      reject(bad)
    }
    reject({ ...base, csrf: 'private' })
    for (const action of [
      'account.login',
      'secret.master.register',
      'model.selection.update',
      '__proto__',
    ])
      reject({ ...base, action })
    for (const audit_id of [
      id(30).toUpperCase(),
      id(30).replace('-7000-', '-4000-'),
      id(30) + '\n',
      null,
      '',
    ])
      reject({ ...base, audit_id })
    for (const created_at of [
      '2025-02-29T12:00:00.000000Z',
      '0000-01-01T00:00:00Z',
      '9999-12-31T23:59:59.1234567Z',
      '2026-01-01T00:00:00.000000+00:00',
    ])
      reject({ ...base, created_at })
    expect(accept({ ...base, created_at: '9999-12-31T23:59:59.999999Z' }).created_at).toBe(
      '9999-12-31T23:59:59.999999Z',
    )
  })
  it('validates Human, AgentRun and all registered project-scoped services without System actor widening', () => {
    const base = row('secret.resolve')
    for (const actor of [
      human,
      agent,
      ...auditServices.map((name) => service(name, 'sha256:' + 'f'.repeat(64))),
    ]) {
      const associations = actor.kind === 'agent_run' ? { execution_id: id(5) } : {}
      expect(accept({ ...base, actor, associations }).actor).toEqual(actor)
    }
    for (const actor of [
      { ...human, project_id: project },
      { ...human, session_id: id(8) },
      { ...agent, project_id: id(99) },
      { ...agent, execution_id: null },
      { kind: 'service', service: 'object', cause_ref: id(3) },
      { ...service(), service: 'unknown' },
      { ...service(), cause_ref: 'sha256:' + 'A'.repeat(64) },
      { ...service(), id: id(2) },
    ])
      reject({ ...base, actor })
    reject({ ...base, actor: agent, associations: {} })
    reject({ ...base, actor: agent, associations: { execution_id: id(99) } })
  })
  it('retains all nine safe associations in a fixed order and rejects null/unknown links', () => {
    const keys = [
      'tool_id',
      'execution_id',
      'tool_call_id',
      'operation_id',
      'request_id',
      'approval_id',
      'runner_id',
      'correlation_id',
      'http_trace_id',
    ]
    const associations = Object.fromEntries(keys.map((key, i) => [key, id(i + 50)]))
    const parsed = accept({ ...row(), associations })
    expect(projectAuditAssociationRows(parsed.associations).map((item) => item.key)).toEqual(keys)
    for (const key of keys) reject({ ...row(), associations: { [key]: null } })
    reject({ ...row(), associations: { agent_id: id(4) } })
    for (const action of ['model.create', 'provider.update']) {
      accept({ ...row(action), associations: { correlation_id: id(8), http_trace_id: id(9) } })
      for (const key of keys.slice(0, 7)) reject({ ...row(action), associations: { [key]: id(8) } })
    }
    for (const key of keys)
      reject({ ...row('knowledge.delete_subtree'), associations: { [key]: id(8) } })
  })
  it('enforces Project creation/operation/transition/initiator and association relations', () => {
    for (const action of Object.keys(vectors).filter((key) => key.startsWith('project.'))) {
      const base = row(action)
      reject({ ...base, metadata: { ...base.metadata, project_id: id(99) } })
      reject({ ...base, resource: resource('project', 99) })
      reject({ ...base, outcome: 'failed' })
      for (const key of ['tool_id', 'execution_id', 'tool_call_id', 'approval_id'])
        reject({ ...base, associations: { [key]: id(8) } })
      reject({ ...base, associations: { operation_id: id(99) } })
      const actor = action.endsWith('.completed')
        ? { ...base.actor, cause_ref: id(99) }
        : { kind: 'human', id: id(99) }
      reject({ ...base, actor })
      reject({ ...base, metadata: { ...base.metadata, from_state: 'active' } })
    }
    for (const action of ['project.create.accepted', 'project.create.completed'])
      reject({ ...row(action), metadata: { ...vectors[action]!.metadata, project_version: '2' } })
    reject({
      ...row('project.update'),
      metadata: { ...vectors['project.update']!.metadata, project_version: '1' },
    })
    const deletion = row('project.delete.accepted')
    accept({ ...deletion, metadata: { ...deletion.metadata, from: 'archived' } })
    reject({ ...deletion, metadata: { ...deletion.metadata, from: 'archiving' } })
    const retry = row('project.lifecycle.retry')
    accept({ ...retry, metadata: { ...retry.metadata, action: 'delete' } })
    reject({ ...retry, metadata: { ...retry.metadata, from: 'active' } })
    const archive = row('project.archive.accepted')
    reject({ ...archive, metadata: { ...archive.metadata, to: 'archived' } })
  })
  it('retains optional secret reasons and all four outcomes, and outbound agent resources', () => {
    for (const outcome of ['success', 'denied', 'failed', 'unknown']) {
      const base = row('secret.resolve')
      accept({ ...base, outcome, metadata: { ...base.metadata, reason: 'timeout' } })
    }
    for (const kind of ['outbound_policy', 'secret', 'agent']) {
      const target = kind === 'outbound_policy' ? { kind } : resource(kind)
      accept({ ...row('outbound.access.deny'), resource: target })
    }
    reject({ ...row('outbound.access.deny'), resource: { kind: 'outbound_policy', id: id(6) } })
    reject({ ...row('secret.resolve'), resource: resource('agent') })
    reject({ ...row('outbound.access.deny'), outcome: 'success' })
  })
  it('validates every object initiator/transfer/reason branch without inventing upload completion equality', () => {
    for (const action of Object.keys(vectors).filter((key) => key.startsWith('object.'))) {
      const base = row(action)
      for (const initiator_kind of ['human', 'agent_run', 'service']) {
        const metadata = {
          ...base.metadata,
          initiator_kind,
          ...(initiator_kind === 'agent_run' ? { initiator_execution_id: id(5) } : {}),
        }
        accept({ ...base, metadata, actor: service('object-maintenance') })
        if (initiator_kind === 'agent_run')
          reject({ ...base, metadata: { ...metadata, initiator_execution_id: null } })
        else reject({ ...base, metadata: { ...metadata, initiator_execution_id: id(5) } })
      }
      reject({ ...base, actor: human })
      reject({ ...base, resource: resource('stored_object', 99) })
      reject({ ...base, metadata: { ...base.metadata, sent_bytes: '9223372036854775808' } })
      reject({ ...base, metadata: { ...base.metadata, byte_size: '0', sent_bytes: '1' } })
    }
    const revoke = row('object.transfer.revoke')
    accept({ ...revoke, metadata: { ...revoke.metadata, reason: 'cancelled' } })
    const complete = row('object.transfer.complete')
    reject({ ...complete, metadata: { ...complete.metadata, sent_bytes: '1' } })
    accept(row('object.upload.complete'))
    const failed = row('object.upload.failed')
    accept({ ...failed, outcome: 'failed' })
    reject({ ...failed, outcome: 'success' })
  })
  it('validates Artifact source/revision and download phases including safe omitted sources', () => {
    for (const action of ['artifact.create', 'artifact.read', 'artifact.download']) {
      const base = row(action)
      for (const source_kind of [
        'inline',
        'uploaded_object',
        'artifact_file',
        'knowledge_file',
        'execution_file',
      ]) {
        const metadata = {
          ...base.metadata,
          source_kind,
          ...(source_kind !== 'inline' ? { source_id: id(8), source_revision: max } : {}),
        }
        accept({ ...base, metadata })
        if (source_kind !== 'inline') {
          const withoutRevision = { ...metadata }
          delete withoutRevision.source_revision
          accept({ ...base, metadata: withoutRevision })
          reject({ ...base, metadata: { ...metadata, source_id: null } })
        } else reject({ ...base, metadata: { ...metadata, source_id: id(8) } })
      }
      reject({ ...base, metadata: { ...base.metadata, source_kind: '' } })
      reject({ ...base, metadata: { ...base.metadata, revision: '1' } })
    }
    const create = row('artifact.create'),
      withoutSource = { ...create.metadata }
    delete withoutSource.source_kind
    reject({ ...create, metadata: withoutSource })
    reject({ ...create, metadata: { ...create.metadata, sent_bytes: '1' } })
    for (const phase of ['issued', 'started', 'sent', 'failed']) {
      const base = row('artifact.download')
      const metadata = {
        ...base.metadata,
        phase,
        ...(phase === 'failed' ? { reason: 'timeout' } : {}),
      }
      accept({ ...base, metadata, outcome: phase === 'failed' ? 'failed' : 'success' })
      reject({ ...base, metadata: { ...metadata, reason: phase === 'failed' ? null : 'timeout' } })
    }
    reject({ ...row('artifact.list'), resource: resource('artifact_collection', 99) })
    reject({ ...row('artifact.read'), actor: service() })
  })
  it('requires exact Model delete replacement changes and Knowledge positive safe count', () => {
    const base = row('model.delete')
    accept({
      ...base,
      metadata: {
        ...base.metadata,
        replacement_id: id(8),
        changed_fields: ['deleted', 'replacement'],
      },
    })
    reject({ ...base, metadata: { ...base.metadata, replacement_id: id(8) } })
    reject({ ...base, metadata: { ...base.metadata, changed_fields: ['deleted', 'replacement'] } })
    reject({ ...base, metadata: { ...base.metadata, selector_kind: 'platform' } })
    const knowledge = row('knowledge.delete_subtree')
    for (const deleted_count of ['0', '01', '9223372036854775808', 1])
      reject({ ...knowledge, metadata: { ...knowledge.metadata, deleted_count } })
    reject({
      ...knowledge,
      metadata: { ...knowledge.metadata, scope_digest: 'sha256:' + 'A'.repeat(64) },
    })
    reject({ ...knowledge, actor: { kind: 'human', id: id(99) } })
  })
  it('keeps canonical Int64 strings, sorted unique changes and the accepted Go MIME grammar', () => {
    for (const version of ['0', '-1', '01', '1.0', '1e3', '+1', '9223372036854775808', 1]) {
      expect(() => parseProjectAuditMetadata('secret.delete', { version })).toThrow(
        'invalid-response',
      )
    }
    for (const changed_fields of [
      [],
      ['value', 'purpose'],
      ['purpose', 'purpose'],
      ['credentials'],
      null,
    ]) {
      expect(() =>
        parseProjectAuditMetadata('secret.create', { version: '1', changed_fields }),
      ).toThrow('invalid-response')
    }
    const base = row('object.delete')
    for (const media_type of [
      'text',
      '{}',
      'text/{json}; charset=utf-8',
      'application/json; charset=us-ascii',
      'x'.repeat(256),
    ])
      accept({ ...base, metadata: { ...base.metadata, media_type } })
    for (const media_type of [
      'Text/Plain',
      'text/plain; charset=UTF-8',
      'text/plain; name=secret',
      'x'.repeat(257),
      'text/plain\n',
    ])
      reject({ ...base, metadata: { ...base.metadata, media_type } })
    expect(auditMetadataJSONBytes({ value: '<>&\u2028\u2029' })).toBe(42)
    expect(() =>
      parseProjectAuditMetadata('secret.delete', { version: '1', private: '<'.repeat(700) }),
    ).toThrow('invalid-response')
  })
})
