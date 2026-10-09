import { describe, expect, it } from 'vitest'
import { captureAuditQuery, parseProjectAuditRecord } from '../api/project-audit'
import { parseSystemAuditRecord } from '../api/system-audit'

const id = (n: number) => `01970000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const project = id(1)
function record(change: 'create' | 'update' | 'delete') {
  return {
    audit_id: id(10),
    created_at: '2026-10-09T12:00:00.123456Z',
    scope: 'project',
    project_id: project,
    actor: { kind: 'human', id: id(2) },
    action: `project.secret_variable.${change}`,
    outcome: 'success',
    resource: { kind: 'project_variable', id: id(3) },
    metadata: {
      variable_id: id(3),
      version: change === 'create' ? '1' : '2',
      changed_fields: [change === 'create' ? 'created' : change === 'delete' ? 'deleted' : 'value'],
    },
    associations: {},
    summary: 'Audit event',
  }
}

describe('Secret Variable Audit closed read compatibility', () => {
  it.each(['create', 'update', 'delete'] as const)(
    'parses %s without granting write authority',
    (change) => {
      const row = record(change)
      expect(parseProjectAuditRecord(row, project).metadata).toEqual(row.metadata)
      expect(() =>
        captureAuditQuery({ action: row.action, resource_kind: 'project_variable' }),
      ).not.toThrow()
      expect(() => parseSystemAuditRecord({ ...row, scope: 'system' })).toThrow()
      for (const patch of [
        { action: 'project.secret_variable.unknown' },
        { project_id: id(88) },
        { resource: { kind: 'secret', id: id(3) } },
        { resource: { kind: 'project_variable', id: id(88) } },
        { outcome: 'denied' },
        { associations: { request_id: id(9) } },
        {
          actor: {
            kind: 'service',
            service: 'project-lifecycle',
            cause_ref: id(8),
            project_id: project,
          },
        },
        { metadata: { ...row.metadata, version: '0' } },
        { metadata: { ...row.metadata, changed_fields: [] } },
        { metadata: { ...row.metadata, value: 'private-canary' } },
        { metadata: { ...row.metadata, credential_id: id(5) } },
        { metadata: { ...row.metadata, 'private-canary/key': null } },
      ])
        expect(() => parseProjectAuditRecord({ ...row, ...patch }, project)).toThrow(
          'invalid-response',
        )
    },
  )

  it('rejects unordered/duplicate fields and incorrect version transitions', () => {
    const row = record('update')
    for (const changed_fields of [
      ['value', 'name'],
      ['value', 'value'],
      ['created'],
      ['credential_id'],
    ]) {
      expect(() =>
        parseProjectAuditRecord({ ...row, metadata: { ...row.metadata, changed_fields } }, project),
      ).toThrow()
    }
    expect(() =>
      parseProjectAuditRecord({ ...row, metadata: { ...row.metadata, version: '1' } }, project),
    ).toThrow()
    const create = record('create')
    expect(() =>
      parseProjectAuditRecord(
        { ...create, metadata: { ...create.metadata, version: '2' } },
        project,
      ),
    ).toThrow()
    expect(() => captureAuditQuery({ action: 'project.secret_variable.unknown' })).toThrow()
  })
})
