import { describe, expect, it } from 'vitest'
import {
  captureProjectSchedulerUpdate,
  parseProjectSchedulerConfiguration,
  parseProjectSchedulerLookup,
  parseProjectSchedulerReceipt,
} from '../api/project-scheduler'

const project = '01900000-0000-7000-8000-000000000001'
const owner = '01900000-0000-7000-8000-000000000002'
const other = '01900000-0000-7000-8000-000000000003'
const max = '9223372036854775807'
const update = {
  expected_version: '9007199254740993',
  scheduler_enabled: false,
  scheduler_max_concurrency: max,
}
const metadata = {
  id: project,
  owner_user_id: owner,
  name: 'demo',
  normalized_name: 'demo',
  description: '',
  lifecycle: 'active',
  version: '99',
  current_sprint_id: null,
  created_at: '2026-10-11T00:00:00.000000Z',
  updated_at: '2026-10-11T00:00:00.000000Z',
  archived_at: null,
}

describe('Project Scheduler wire codecs', () => {
  it('captures all fields including false, null and integers beyond Number precision', () => {
    const captured = captureProjectSchedulerUpdate(update)
    expect(captured).toEqual(update)
    expect(Object.isFrozen(captured)).toBe(true)
    expect(captureProjectSchedulerUpdate({ ...update, scheduler_max_concurrency: null })).toEqual({
      ...update,
      scheduler_max_concurrency: null,
    })
    expect(
      parseProjectSchedulerConfiguration(
        {
          project_id: project,
          version: max,
          scheduler_enabled: false,
          scheduler_max_concurrency: max,
        },
        project,
      ),
    ).toEqual({
      project_id: project,
      version: max,
      scheduler_enabled: false,
      scheduler_max_concurrency: max,
    })
  })
  it.each(['0', '-1', '+1', '01', '1.0', '1e2', ' 1', '1\n', '9223372036854775808', 1])(
    'rejects noncanonical/overflow/number version and finite limit %s',
    (value) => {
      expect(() =>
        captureProjectSchedulerUpdate({ ...update, expected_version: value }),
      ).toThrowError(expect.objectContaining({ kind: 'invalid-input' }))
      expect(() =>
        captureProjectSchedulerUpdate({ ...update, scheduler_max_concurrency: value }),
      ).toThrowError(expect.objectContaining({ kind: 'invalid-input' }))
    },
  )
  it('requires the whole replacement and rejects metadata fields or nonboolean enabled', () => {
    for (const input of [
      { expected_version: '1', scheduler_enabled: false },
      { ...update, scheduler_enabled: null },
      { ...update, scheduler_max_concurrency: undefined },
      { ...update, name: 'injected' },
    ])
      expect(() => captureProjectSchedulerUpdate(input)).toThrowError(
        expect.objectContaining({ kind: 'invalid-input' }),
      )
  })
  it('binds a current configuration to its Project and refuses extra runtime facts', () => {
    const current = {
      project_id: project,
      version: '1',
      scheduler_enabled: true,
      scheduler_max_concurrency: null,
    }
    expect(parseProjectSchedulerConfiguration(current, project)).toEqual(current)
    for (const value of [
      { ...current, project_id: other },
      { ...current, scheduler_enabled: 1 },
      { ...current, running: true },
      { project_id: project, version: '1' },
    ])
      expect(() => parseProjectSchedulerConfiguration(value, project)).toThrowError(
        expect.objectContaining({ kind: 'invalid-response' }),
      )
  })
  it('accepts only no-op/next-version PATCH acknowledgements without configuration', () => {
    for (const version of [update.expected_version, '9007199254740994'])
      expect(
        parseProjectSchedulerReceipt({ project_id: project, version }, project, update),
      ).toEqual({ project_id: project, version })
    for (const value of [
      { project_id: other, version: update.expected_version },
      { project_id: project, version: '9007199254740995' },
      { project_id: project, version: update.expected_version, scheduler_enabled: false },
    ])
      expect(() => parseProjectSchedulerReceipt(value, project, update)).toThrowError(
        expect.objectContaining({ kind: 'invalid-response' }),
      )
  })
  it('keeps a key-only Lookup historical without validating Scheduler intent/current state', () => {
    const result = parseProjectSchedulerLookup(
      { state: 'committed', result: { command: 'update', project: metadata } },
      project,
      owner,
    )
    expect(result).toEqual({ state: 'committed', receipt: { project_id: project, version: '99' } })
    for (const state of ['in_progress', 'not_observed'] as const)
      expect(parseProjectSchedulerLookup({ state }, project, owner)).toEqual({ state })
  })
  it('refuses wrong Lookup identity, command, shape and invented configuration', () => {
    for (const value of [
      { state: 'committed', result: { command: 'scheduler', project: metadata } },
      {
        state: 'committed',
        result: { command: 'update', project: { project_id: project, version: '99' } },
      },
      {
        state: 'committed',
        result: { command: 'update', project: { ...metadata, owner_user_id: other } },
      },
      { state: 'committed', result: { command: 'update', project: { ...metadata, id: other } } },
      {
        state: 'committed',
        result: { command: 'update', project: { ...metadata, scheduler_enabled: true } },
      },
      { state: 'not_observed', result: null },
    ])
      expect(() => parseProjectSchedulerLookup(value, project, owner)).toThrowError(
        expect.objectContaining({ kind: 'invalid-response' }),
      )
  })
})
