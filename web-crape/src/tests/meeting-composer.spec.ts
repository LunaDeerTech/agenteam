import { afterEach, describe, expect, it } from 'vitest'
import { defineComponent, h, nextTick, ref } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import type { MeetingNode } from '../api/meetings'
import MeetingComposer from '../components/meetings/MeetingComposer.vue'
import {
  focusMeetingEditable,
  meetingLink,
  meetingReferenceKey,
  meetingSelection,
  readMeetingEditable,
  replaceMeetingRange,
  type MeetingMentionChoice,
} from '../composables/meeting-composer'
const id = (n: number) => `01990000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const mention: MeetingMentionChoice = {
  node: { type: 'mention', participant_id: id(1) },
  label: 'Agent',
  kind: 'agent',
}
const wrappers: VueWrapper[] = []
afterEach(() => {
  wrappers.splice(0).forEach((wrapper) => wrapper.unmount())
  document.body.replaceChildren()
})
function fixture(initial: MeetingNode[] = [{ type: 'text', text: '' }]) {
  const nodes = ref(initial),
    mentions = ref<readonly MeetingMentionChoice[]>([mention]),
    disabled = ref(false)
  const w = mount(
    defineComponent({
      setup: () => () =>
        h(MeetingComposer, {
          modelValue: nodes.value,
          'onUpdate:modelValue': (value: MeetingNode[]) => {
            nodes.value = value
          },
          meetingId: id(2),
          mentions: mentions.value,
          disabled: disabled.value,
          label: '发送到会议',
          placeholder: '继续讨论…',
        }),
    }),
    {
      attachTo: document.body,
      global: { stubs: { Teleport: true, MeetingResourcePicker: true, MeetingFileUpload: true } },
    },
  )
  wrappers.push(w)
  const editor = () => w.get<HTMLElement>('[role="textbox"]').element
  const caret = (offset: number) => focusMeetingEditable(editor(), new WeakMap(), offset)
  return { w, nodes, mentions, disabled, editor, caret }
}
describe('Meeting ordered inline composer', () => {
  it('keeps a dropped File out of canonical nodes until publication supplies its stable identity', async () => {
    const f = fixture([{ type: 'text', text: 'Before after' }]),
      file = new File(['hello'], 'report.txt', { type: 'text/plain' })
    expect(f.w.get('button[aria-label="添加到消息"]').attributes('aria-haspopup')).toBe('menu')
    f.caret(7)
    const drop = new Event('drop', { bubbles: true, cancelable: true })
    Object.defineProperty(drop, 'dataTransfer', { value: { files: [file] } })
    f.editor().dispatchEvent(drop)
    await nextTick()
    const upload = f.w.findComponent({ name: 'MeetingFileUpload' })
    expect(upload.props('file')).toBe(file)
    expect(f.nodes.value).toEqual([{ type: 'text', text: 'Before after' }])
    upload.vm.$emit('choose', { kind: 'file', id: id(40), label: 'report.txt' })
    await flushPromises()
    expect(f.nodes.value).toEqual([
      { type: 'text', text: 'Before ' },
      { type: 'file', reference: { kind: 'file', id: id(40), label: 'report.txt' } },
      { type: 'text', text: 'after' },
    ])
    expect(document.activeElement).toBe(f.editor())
  })
  it('inserts and removes atomic identities at the caret without parsing their labels', () => {
    const original: MeetingNode[] = [{ type: 'text', text: 'Before after' }]
    const nodes = replaceMeetingRange(original, 7, 7, [mention.node])
    expect(nodes).toEqual([
      { type: 'text', text: 'Before ' },
      mention.node,
      { type: 'text', text: 'after' },
    ])
    expect(replaceMeetingRange(nodes, 7, 8, [])).toEqual(original)
    expect(replaceMeetingRange(nodes, 3, 10, [{ type: 'text', text: 'new' }])).toEqual([
      { type: 'text', text: 'Befnewter' },
    ])
    expect(() => replaceMeetingRange(nodes, -1, 0, [])).toThrow()
  })
  it('keeps path query and fragment during minimal link normalization and rejects unsafe identities', () => {
    const link = meetingLink('HTTPS://Example.test:443/a/../b?q=1#part', 'Design')!
    expect(link).toEqual({
      kind: 'link',
      id: 'https://example.test/a/../b?q=1#part',
      label: 'Design',
    })
    expect(meetingReferenceKey({ kind: 'link', id: 'https://example.test:443' })).toBe(
      'link:https://example.test/',
    )
    for (const value of [
      'javascript:alert(1)',
      'data:text/plain,a',
      'https://u:p@example.test/',
      'https://example.test:0/',
      'https://example.test/%xx',
      'https://example.test/ a',
      'https://example.test\\@other.test/',
    ])
      expect(meetingLink(value)).toBeNull()
  })
  it('keeps only private captured chips typed when the browser DOM changes', () => {
    const root = document.createElement('div'),
      chip = document.createElement('span'),
      forged = document.createElement('span')
    const chips = new WeakMap<Node, MeetingNode>()
    chip.textContent = '@Agent'
    chips.set(chip, mention.node)
    forged.dataset.participantId = id(30)
    forged.textContent = '@Other'
    root.append(document.createTextNode('Text '), chip, forged)
    expect(readMeetingEditable(root, chips)).toEqual([
      { type: 'text', text: 'Text ' },
      mention.node,
      { type: 'text', text: '@Other' },
    ])
    document.body.append(root)
    root.contentEditable = 'true'
    focusMeetingEditable(root, chips, 6)
    expect(meetingSelection(root, chips)).toEqual({ start: 6, end: 6 })
  })
  it('uses @ to replace only the trigger, restores focus, and retains an invalidated roster identity', async () => {
    const f = fixture()
    f.editor().textContent = 'Start @ end'
    f.caret(7)
    f.editor().dispatchEvent(
      new InputEvent('input', { bubbles: true, data: '@', inputType: 'insertText' }),
    )
    await nextTick()
    await f.w.get('[aria-label="提及 Agent（Agent）"]').trigger('click')
    await flushPromises()
    expect(f.nodes.value).toEqual([
      { type: 'text', text: 'Start ' },
      mention.node,
      { type: 'text', text: ' end' },
    ])
    expect(document.activeElement).toBe(f.editor())
    f.mentions.value = []
    await nextTick()
    expect(f.nodes.value[1]).toEqual(mention.node)
    expect(f.w.text()).toContain('不在参会名单')
    expect(f.editor().getAttribute('aria-invalid')).toBe('true')
    await f.w.get('[aria-label="移除 @不可用成员"]').trigger('click')
    expect(f.nodes.value).toEqual([{ type: 'text', text: 'Start  end' }])
  })
  it('retains the trigger range through keyup and blur before choosing and re-adding a mention', async () => {
    const f = fixture([{ type: 'text', text: 'Review ' }])
    const typeTrigger = async () => {
      f.editor().textContent = 'Review @'
      f.caret(8)
      f.editor().dispatchEvent(
        new InputEvent('input', { bubbles: true, data: '@', inputType: 'insertText' }),
      )
      // Typing ends with the browser caret after @, then the dialog takes focus.
      f.editor().dispatchEvent(new KeyboardEvent('keyup', { key: '@', bubbles: true }))
      f.editor().blur()
      await nextTick()
      await f.w.get('[aria-label="提及 Agent（Agent）"]').trigger('click')
      await flushPromises()
    }
    await typeTrigger()
    expect(f.nodes.value).toEqual([{ type: 'text', text: 'Review ' }, mention.node])
    await f.w.get('[aria-label="移除 @Agent"]').trigger('click')
    expect(f.nodes.value).toEqual([{ type: 'text', text: 'Review ' }])
    expect(document.activeElement).toBe(f.editor())
    await typeTrigger()
    expect(f.nodes.value).toEqual([{ type: 'text', text: 'Review ' }, mention.node])
  })
  it('pastes a URL as an inert link at the selection and keeps other clipboard content plain', async () => {
    const f = fixture([{ type: 'text', text: 'AB' }])
    f.caret(1)
    const paste = (text: string) => {
      const event = new Event('paste', { bubbles: true, cancelable: true })
      Object.defineProperty(event, 'clipboardData', { value: { getData: () => text } })
      f.editor().dispatchEvent(event)
    }
    paste('https://example.test/doc')
    await nextTick()
    expect(f.nodes.value).toEqual([
      { type: 'text', text: 'A' },
      { type: 'link', reference: { kind: 'link', id: 'https://example.test/doc' } },
      { type: 'text', text: 'B' },
    ])
    expect(f.w.find('a').exists()).toBe(false)
    paste('<script>bad()</script>')
    await nextTick()
    expect(f.nodes.value.at(-1)).toEqual({ type: 'text', text: '<script>bad()</script>B' })
    expect(f.w.find('script').exists()).toBe(false)
  })
  it('leaves Enter on a focused chip button available for native activation', async () => {
    const original: MeetingNode[] = [
      { type: 'text', text: 'Before ' },
      mention.node,
      { type: 'text', text: ' after' },
    ]
    const f = fixture(original)
    const remove = f.w.get<HTMLButtonElement>('[aria-label="移除 @Agent"]').element
    remove.focus()
    const enter = new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true })
    remove.dispatchEvent(enter)
    expect(enter.defaultPrevented).toBe(false)
    expect(document.activeElement).toBe(remove)
    expect(f.nodes.value).toEqual(original)
    // The DOM harness does not synthesize keyboard default actions. Dispatch the
    // native button activation separately after verifying Enter was not consumed.
    remove.click()
    await nextTick()
    expect(f.nodes.value).toEqual([{ type: 'text', text: 'Before  after' }])
    expect(document.activeElement).toBe(f.editor())
    const newline = new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true })
    f.editor().dispatchEvent(newline)
    expect(newline.defaultPrevented).toBe(true)
    expect(f.nodes.value).toEqual([{ type: 'text', text: 'Before \n after' }])
  })
  it('replaces # with the selected project identity without pinning it', async () => {
    const f = fixture()
    f.editor().textContent = 'Read # next'
    f.caret(6)
    f.editor().dispatchEvent(
      new InputEvent('input', { bubbles: true, data: '#', inputType: 'insertText' }),
    )
    f.editor().dispatchEvent(new KeyboardEvent('keyup', { key: '#', bubbles: true }))
    f.editor().blur()
    await nextTick()
    f.w
      .findComponent({ name: 'MeetingResourcePicker' })
      .vm.$emit('choose', { kind: 'task', id: id(20), label: 'Task' })
    await flushPromises()
    expect(f.nodes.value).toEqual([
      { type: 'text', text: 'Read ' },
      { type: 'task', reference: { kind: 'task', id: id(20), label: 'Task' } },
      { type: 'text', text: ' next' },
    ])
    expect(f.w.find('[role="dialog"]').exists()).toBe(false)
    expect(document.activeElement).toBe(f.editor())
  })
  it('does not open a picker or normalize the DOM during an IME composition', async () => {
    const f = fixture()
    f.editor().dispatchEvent(new CompositionEvent('compositionstart', { bubbles: true }))
    f.editor().textContent = '@'
    f.caret(1)
    f.editor().dispatchEvent(
      new InputEvent('input', { bubbles: true, data: '@', isComposing: true }),
    )
    await nextTick()
    expect(f.w.find('[role="dialog"]').exists()).toBe(false)
    f.editor().dispatchEvent(new CompositionEvent('compositionend', { bubbles: true, data: '@' }))
    await nextTick()
    expect(f.nodes.value).toEqual([{ type: 'text', text: '@' }])
    expect(f.w.find('[role="dialog"]').exists()).toBe(false)
  })
})
