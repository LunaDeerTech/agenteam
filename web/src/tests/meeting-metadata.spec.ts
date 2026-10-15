import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { defineComponent, h, nextTick, ref } from 'vue'
import type { DirectoryAgent } from '../api/agent-directory'
import type { MeetingDetail } from '../api/meetings'
import MeetingMetadataEditor from '../components/meetings/MeetingMetadataEditor.vue'
import MeetingResourcePicker from '../components/meetings/MeetingResourcePicker.vue'

const id = (n: number) => `01990000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const at = '2026-10-14T00:00:00Z'
const directory: DirectoryAgent[] = [5, 6].map((n) => ({
  id: id(n),
  project_id: id(1),
  name: `agent-${n}`,
  display_name: `Agent ${n}`,
  tag_color: null,
  description: '',
  version: '1',
  created_at: at,
  updated_at: at,
}))
const detail: MeetingDetail = {
  session: {
    id: id(2),
    project_id: id(1),
    version: '9007199254740993',
    status: 'active',
    title: 'Discussion',
    summary: null,
    deleting: false,
    created_at: at,
    updated_at: at,
  },
  participants: directory.map((agent, order) => ({
    id: id(order + 10),
    meeting_id: id(2),
    kind: 'agent',
    source_id: agent.id,
    name: agent.display_name!,
    order,
    active: true,
  })),
  references: [{ kind: 'task', id: id(30), label: 'Original task' }],
  turns: [],
  decisions: [],
}
const wrappers: VueWrapper[] = []
afterEach(async () => {
  wrappers.splice(0).forEach((w) => w.unmount())
  await flushPromises()
})
function fixture() {
  const submit = vi.fn(async () => true),
    lookup = vi.fn(async (): Promise<string | null> => id(2))
  const w = mount(MeetingMetadataEditor, {
    props: {
      detail,
      agents: directory,
      moreAgents: false,
      agentError: '',
      mutable: true,
      busy: false,
      unsettled: false,
      canLookup: false,
      feedback: '',
      submit,
      lookup,
    },
    global: { stubs: { Teleport: true, MeetingResourcePicker: true } },
  })
  wrappers.push(w)
  const button = (name: string) =>
    w.findAll('button').find((button) => {
      const label = button.element.cloneNode(true) as Element
      label.querySelectorAll('[aria-hidden="true"]').forEach((node) => node.remove())
      return (label.getAttribute('aria-label') ?? label.textContent?.trim()) === name
    })!
  return { w, button, submit, lookup }
}

async function closeBeforeReadFixture() {
  const host = document.createElement('div')
  host.id = 'app'
  document.body.append(host)
  const busy = ref(false),
    currentDetail = ref(detail)
  const rects = vi
    .spyOn(HTMLElement.prototype, 'getClientRects')
    .mockImplementation(() => [{ width: 10, height: 10 }] as unknown as DOMRectList)
  const wrapper = mount(
    defineComponent({
      setup: () => () =>
        h(MeetingMetadataEditor, {
          detail: currentDetail.value,
          agents: directory,
          moreAgents: false,
          agentError: '',
          mutable: !busy.value,
          busy: busy.value,
          unsettled: false,
          canLookup: false,
          feedback: '',
          submit: async () => true,
          lookup: async () => null,
          onAgents: () => {
            busy.value = true
            // The real Chromium probe covers disabling the focused trigger;
            // jsdom needs the corresponding native blur represented explicitly.
            ;(document.activeElement as HTMLElement).blur()
          },
        }),
    }),
    { attachTo: host, global: { stubs: { MeetingResourcePicker: true } } },
  )
  let mounted = true
  const dispose = () => {
    if (mounted) wrapper.unmount()
    mounted = false
    host.remove()
    rects.mockRestore()
  }
  const trigger = wrapper.get('.metadata-actions > button').element as HTMLButtonElement
  try {
    trigger.focus()
    trigger.click()
    await flushPromises()
    expect(trigger.disabled).toBe(true)
    expect(document.querySelector('[role="dialog"]')?.contains(document.activeElement)).toBe(true)
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await flushPromises()
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(host.inert).toBe(false)
    expect(trigger.disabled).toBe(true)
    expect(document.activeElement).toBe(document.body)
  } catch (error) {
    dispose()
    throw error
  }
  const focus = vi.spyOn(trigger, 'focus')
  return {
    host,
    trigger,
    focus,
    busy,
    currentDetail,
    release: async () => {
      busy.value = false
      await flushPromises()
    },
    unmount: () => {
      if (mounted) wrapper.unmount()
      mounted = false
    },
    dispose,
  }
}

describe('Meeting membership and pinned references', () => {
  it('returns focus once when the original read finishes after the member dialog was closed', async () => {
    const f = await closeBeforeReadFixture()
    try {
      await f.release()
      expect(f.trigger.disabled).toBe(false)
      expect(document.activeElement).toBe(f.trigger)
      expect(f.focus).toHaveBeenCalledTimes(1)
      f.trigger.blur()
      f.busy.value = true
      await flushPromises()
      await f.release()
      expect(f.focus).toHaveBeenCalledTimes(1)
    } finally {
      f.dispose()
    }
  })
  it.each(['pointerdown', 'keydown', 'focus'] as const)(
    'cancels the pending return after a new %s instead of taking focus back',
    async (action) => {
      const f = await closeBeforeReadFixture()
      const other = document.createElement('button')
      f.host.append(other)
      try {
        if (action === 'focus') {
          other.focus()
          other.blur()
        } else if (action === 'keydown')
          document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', bubbles: true }))
        else other.dispatchEvent(new Event('pointerdown', { bubbles: true }))
        await f.release()
        expect(f.focus).not.toHaveBeenCalled()
      } finally {
        f.dispose()
      }
    },
  )
  it('does not cross a different modal even if that layer has not focused a control yet', async () => {
    const f = await closeBeforeReadFixture()
    const other = document.createElement('section')
    other.setAttribute('role', 'dialog')
    document.body.append(other)
    try {
      await f.release()
      expect(f.focus).not.toHaveBeenCalled()
      other.remove()
      f.busy.value = true
      await flushPromises()
      await f.release()
      expect(f.focus).not.toHaveBeenCalled()
    } finally {
      other.remove()
      f.dispose()
    }
  })
  it.each(['project', 'meeting', 'version', 'unmount'] as const)(
    'retires the pending target when its original %s changes',
    async (change) => {
      const f = await closeBeforeReadFixture()
      try {
        if (change === 'unmount') f.unmount()
        else
          f.currentDetail.value = {
            ...detail,
            session: {
              ...detail.session,
              ...(change === 'project' ? { project_id: id(90) } : {}),
              ...(change === 'meeting' ? { id: id(91) } : {}),
              ...(change === 'version' ? { version: '9007199254740994' } : {}),
            },
          }
        await flushPromises()
        await f.release()
        expect(f.focus).not.toHaveBeenCalled()
      } finally {
        f.dispose()
      }
    },
  )
  it('restores the original member trigger after the directory read takes focus before the dialog opens', async () => {
    const host = document.createElement('div')
    host.id = 'app'
    document.body.append(host)
    const busy = ref(false)
    let completeRead!: () => void
    const read = new Promise<void>((resolve) => {
      completeRead = resolve
    })
    // jsdom has neither layout nor Chromium's blur on disabling the active
    // button. Model that focus loss at the public directory-read boundary.
    const rects = vi
      .spyOn(HTMLElement.prototype, 'getClientRects')
      .mockImplementation(() => [{ width: 10, height: 10 }] as unknown as DOMRectList)
    const wrapper = mount(
      defineComponent({
        setup: () => () =>
          h(MeetingMetadataEditor, {
            detail,
            agents: directory,
            moreAgents: false,
            agentError: '',
            mutable: true,
            busy: busy.value,
            unsettled: false,
            canLookup: false,
            feedback: '',
            submit: async () => true,
            lookup: async () => null,
            onAgents: () => {
              busy.value = true
              ;(document.activeElement as HTMLElement).blur()
              void read.then(() => {
                busy.value = false
              })
            },
          }),
      }),
      { attachTo: host, global: { stubs: { MeetingResourcePicker: true } } },
    )
    try {
      const trigger = wrapper.get('.metadata-actions > button').element as HTMLButtonElement
      trigger.focus()
      trigger.click()
      await flushPromises()
      expect(trigger.disabled).toBe(true)
      expect(host.inert).toBe(true)
      expect(document.querySelector('[role="dialog"]')?.contains(document.activeElement)).toBe(true)
      completeRead()
      await flushPromises()
      expect(trigger.disabled).toBe(false)
      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
      await nextTick()
      expect(document.querySelector('[role="dialog"]')).toBeNull()
      expect(host.inert).toBe(false)
      expect(document.activeElement).toBe(trigger)
    } finally {
      completeRead()
      wrapper.unmount()
      host.remove()
      rects.mockRestore()
    }
  })
  it('saves the visible member order and original version, including an explicitly empty roster', async () => {
    const { w, button, submit } = fixture()
    await button('编辑成员').trigger('click')
    expect(w.emitted('agents')).toEqual([[false]])
    expect(w.get('[role="dialog"]').text()).toContain('保留其历史发言')
    await button('上移 Agent 6').trigger('click')
    expect(w.emitted('dirty')?.at(-1)).toEqual([true])
    await button('保存修改').trigger('click')
    await flushPromises()
    expect(submit).toHaveBeenLastCalledWith({
      kind: 'participants',
      meeting: id(2),
      input: { expected_version: detail.session.version, agents: [id(6), id(5)] },
    })
    expect(w.find('[role="dialog"]').exists()).toBe(false)
    await button('编辑成员').trigger('click')
    await button('移除 Agent 5').trigger('click')
    await button('移除 Agent 6').trigger('click')
    expect(w.text()).toContain('添加后才能发送新一轮消息')
    await button('保存修改').trigger('click')
    await flushPromises()
    expect(submit).toHaveBeenLastCalledWith({
      kind: 'participants',
      meeting: id(2),
      input: { expected_version: detail.session.version, agents: [] },
    })
  })
  it('rejects an edit after the session changes and requires an explicit reload before another edit', async () => {
    const { w, button, submit } = fixture()
    await button('编辑成员').trigger('click')
    await button('移除 Agent 5').trigger('click')
    await w.setProps({
      detail: { ...detail, session: { ...detail.session, version: '9007199254740994' } },
    })
    expect(w.text()).toContain('会议已有新变化')
    expect(button('保存修改').attributes('disabled')).toBeDefined()
    await button('保存修改').trigger('click')
    expect(submit).not.toHaveBeenCalled()
    await button('重新载入当前内容').trigger('click')
    expect(w.emitted('dirty')?.at(-1)).toEqual([false])
    await button('移除 Agent 6').trigger('click')
    await button('保存修改').trigger('click')
    expect(submit).toHaveBeenCalledWith({
      kind: 'participants',
      meeting: id(2),
      input: { expected_version: '9007199254740994', agents: [id(5)] },
    })
  })
  it('allows removing an unavailable pinned identity and validates new links without following them', async () => {
    const { w, button, submit } = fixture()
    await button('管理引用').trigger('click')
    expect(w.findAll('a')).toHaveLength(0)
    await button('取消固定 Original task').trigger('click')
    await w.get('input[type="url"]').setValue('https://user:password@example.test/')
    await button('加入引用列表').trigger('click')
    expect(w.text()).toContain('不含用户名或密码')
    expect(button('保存修改').attributes('disabled')).toBeDefined()
    await w.get('input[type="url"]').setValue('https://example.test/plan?q=1#part')
    await button('加入引用列表').trigger('click')
    expect(w.findAll('a')).toHaveLength(0)
    await button('保存修改').trigger('click')
    expect(submit).toHaveBeenCalledWith({
      kind: 'references',
      meeting: id(2),
      input: {
        expected_version: detail.session.version,
        references: [{ kind: 'link', id: 'https://example.test/plan?q=1#part' }],
      },
    })
  })
  it('keeps an uncertain update for original lookup and never submits a replacement command', async () => {
    const { w, button, submit, lookup } = fixture()
    submit.mockResolvedValue(false)
    lookup.mockResolvedValueOnce(null)
    await button('编辑成员').trigger('click')
    await button('移除 Agent 6').trigger('click')
    await button('保存修改').trigger('click')
    await flushPromises()
    await w.setProps({
      mutable: false,
      unsettled: true,
      canLookup: true,
      feedback: '操作结果待确认',
    })
    expect(w.text()).toContain('操作结果待确认')
    expect(button('移除 Agent 5').attributes('disabled')).toBeDefined()
    await button('核对原操作').trigger('click')
    await flushPromises()
    expect(w.find('[role="dialog"]').exists()).toBe(true)
    await button('核对原操作').trigger('click')
    await flushPromises()
    expect(w.find('[role="dialog"]').exists()).toBe(false)
    expect(submit).toHaveBeenCalledTimes(1)
    expect(lookup).toHaveBeenCalledTimes(2)
  })
  it('captures selected resource identities only while the original Meeting version remains editable', async () => {
    const { w, button, submit } = fixture()
    await button('管理引用').trigger('click')
    const picker = w.getComponent(MeetingResourcePicker)
    picker.vm.$emit('choose', { kind: 'file', id: id(41), label: 'Published file' })
    await flushPromises()
    expect(w.text()).toContain('Published file')
    await w.setProps({ mutable: false })
    expect(picker.props('disabled')).toBe(true)
    picker.vm.$emit('choose', { kind: 'task', id: id(42), label: 'Blocked task' })
    await flushPromises()
    expect(w.text()).not.toContain('Blocked task')
    await w.setProps({
      mutable: true,
      detail: { ...detail, session: { ...detail.session, version: '9007199254740994' } },
    })
    picker.vm.$emit('choose', { kind: 'knowledge', id: id(43), label: 'Stale document' })
    await flushPromises()
    expect(picker.props('disabled')).toBe(true)
    expect(w.text()).not.toContain('Stale document')
    await button('重新载入当前内容').trigger('click')
    picker.vm.$emit('choose', { kind: 'file', id: id(41), label: 'Published file' })
    await flushPromises()
    await button('保存修改').trigger('click')
    expect(submit).toHaveBeenCalledWith({
      kind: 'references',
      meeting: id(2),
      input: {
        expected_version: '9007199254740994',
        references: [...detail.references, { kind: 'file', id: id(41), label: 'Published file' }],
      },
    })
  })
  it('asks before discarding a local edit when the dialog is closed', async () => {
    const { w, button, submit } = fixture()
    await button('编辑成员').trigger('click')
    await button('移除 Agent 6').trigger('click')
    await button('返回会议').trigger('click')
    expect(w.get('[role="alert"]').text()).toContain('修改尚未保存')
    await button('继续编辑').trigger('click')
    expect(w.find('[role="dialog"]').exists()).toBe(true)
    await button('返回会议').trigger('click')
    await button('放弃修改').trigger('click')
    expect(w.find('[role="dialog"]').exists()).toBe(false)
    expect(submit).not.toHaveBeenCalled()
    expect(w.emitted('dirty')?.at(-1)).toEqual([false])
  })
})
