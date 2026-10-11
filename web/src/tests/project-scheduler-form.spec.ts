import { mount, type VueWrapper } from '@vue/test-utils'
import { reactive } from 'vue'
import { afterEach, describe, expect, it } from 'vitest'
import ProjectSchedulerForm from '../components/projects/ProjectSchedulerForm.vue'
import type { ProjectSchedulerDraft } from '../api/project-scheduler'

const wrappers: VueWrapper[] = []
afterEach(() => wrappers.splice(0).forEach((wrapper) => wrapper.unmount()))
function fixture(extra: Partial<{ busy: boolean; locked: boolean; readOnly: boolean }> = {}) {
  const draft = reactive<ProjectSchedulerDraft>({
    expected_version: '9007199254740993',
    enabled: true,
    unlimited: false,
    limit: '3',
  })
  const wrapper = mount(ProjectSchedulerForm, { props: { draft, ...extra } })
  wrappers.push(wrapper)
  return { draft, wrapper }
}

describe('Scheduler configuration form', () => {
  it('submits a paused full replacement while preserving finite concurrency and its draft version', async () => {
    const { wrapper, draft } = fixture()
    await wrapper.get('input[role="switch"]').setValue(false)
    await wrapper.get('input[name="scheduler-limit"]').setValue('9223372036854775807')
    await wrapper.get('form').trigger('submit')
    expect(wrapper.emitted('submit')).toEqual([
      [
        {
          expected_version: '9007199254740993',
          scheduler_enabled: false,
          scheduler_max_concurrency: '9223372036854775807',
        },
      ],
    ])
    expect(draft.expected_version).toBe('9007199254740993')
    expect(wrapper.text()).toContain('不会终止已有执行')
  })
  it('uses explicit unlimited without erasing the retained finite input', async () => {
    const { wrapper } = fixture()
    await wrapper.get('input[name="scheduler-unlimited"]').setValue(true)
    expect(wrapper.find('input[name="scheduler-limit"]').exists()).toBe(false)
    await wrapper.get('form').trigger('submit')
    expect(wrapper.emitted('submit')?.[0]).toEqual([
      {
        expected_version: '9007199254740993',
        scheduler_enabled: true,
        scheduler_max_concurrency: null,
      },
    ])
    await wrapper.get('input[name="scheduler-unlimited"]').setValue(false)
    expect((wrapper.get('input[name="scheduler-limit"]').element as HTMLInputElement).value).toBe(
      '3',
    )
  })
  it('retains invalid raw input and presents the validation message without emitting a write', async () => {
    const { wrapper } = fixture()
    await wrapper.get('input[name="scheduler-limit"]').setValue('01')
    await wrapper.get('form').trigger('submit')
    expect(wrapper.emitted('submit')).toBeUndefined()
    expect(wrapper.get('[role="alert"]').text()).toContain('前导零')
    expect(wrapper.get('input[name="scheduler-limit"]').attributes('aria-invalid')).toBe('true')
    await wrapper.get('input[name="scheduler-limit"]').setValue('2')
    await wrapper.get('form').trigger('submit')
    expect(wrapper.emitted('submit')).toHaveLength(1)
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  })
  it.each(['busy', 'locked', 'readOnly'] as const)(
    'rejects a programmatic submit while %s and preserves the draft',
    async (state) => {
      const { wrapper, draft } = fixture({ [state]: true })
      await wrapper.get('form').trigger('submit')
      expect(wrapper.emitted('submit')).toBeUndefined()
      expect(wrapper.get('input[role="switch"]').attributes('disabled')).toBeDefined()
      expect(draft).toEqual({
        expected_version: '9007199254740993',
        enabled: true,
        unlimited: false,
        limit: '3',
      })
    },
  )
  it('exposes cancellation as an explicit action without modifying the captured version', async () => {
    const { wrapper, draft } = fixture()
    await wrapper
      .findAll('button')
      .find((button) => button.find('.button-label[aria-hidden="false"]').text() === '取消修改')!
      .trigger('click')
    expect(wrapper.emitted('cancel')).toEqual([[]])
    expect(wrapper.emitted('submit')).toBeUndefined()
    expect(draft.expected_version).toBe('9007199254740993')
  })
})
