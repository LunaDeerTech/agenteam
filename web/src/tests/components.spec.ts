import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent, nextTick, ref } from 'vue'
import {
  UiCheckbox,
  UiSwitch,
  UiRadioGroup,
  UiDialog,
  UiSelect,
  UiMenu,
  UiTree,
  UiTabs,
  UiComposer,
} from '../components/ui'
import { useActionFeedback } from '../composables/useActionFeedback'
import { initializeTheme, useTheme } from '../composables/useTheme'

beforeAll(() => {
  // jsdom has no layout; expose focusable elements to the layer's visibility check.
  vi.spyOn(HTMLElement.prototype, 'getClientRects').mockImplementation(
    () => [{ width: 10, height: 10 }] as unknown as DOMRectList,
  )
  vi.stubGlobal('CSS', { escape: (value: string) => value })
  window.matchMedia = vi
    .fn()
    .mockReturnValue({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() })
})
afterEach(() => {
  document.body.innerHTML = ''
  vi.useRealTimers()
})

describe('controlled form components', () => {
  it('emits actual boolean values and prevents disabled changes', async () => {
    const checkbox = mount(UiCheckbox, { props: { modelValue: false }, slots: { default: '提醒' } })
    await checkbox.get('input').setValue(true)
    expect(checkbox.emitted('update:modelValue')?.[0]).toEqual([true])
    const toggle = mount(UiSwitch, { props: { modelValue: true, disabled: true } })
    expect(toggle.get('input').attributes('role')).toBe('switch')
    expect(toggle.get('input').attributes()).toHaveProperty('disabled')
    checkbox.unmount()
    toggle.unmount()
  })
  it('keeps native radio names shared without mixing other groups', async () => {
    const options = [
      { value: 'a', label: '甲' },
      { value: 'b', label: '乙' },
    ]
    const host = mount(
      defineComponent({
        components: { UiRadioGroup },
        setup() {
          return { options }
        },
        template:
          '<UiRadioGroup model-value="a" legend="类型" :options="options" /><UiRadioGroup legend="另一组" :options="options" />',
      }),
    )
    const [first, second] = host.findAllComponents(UiRadioGroup)
    const inputs = first!.findAll('input')
    expect(inputs[0]!.attributes('name')).toBe(inputs[1]!.attributes('name'))
    expect(inputs[0]!.attributes('name')).not.toBe(second!.get('input').attributes('name'))
    await inputs[1]!.setValue(true)
    expect(first!.emitted('update:modelValue')?.[0]).toEqual(['b'])
    host.unmount()
  })
})

it('select skips disabled values, emits selection, closes and restores its trigger', async () => {
  const wrapper = mount(UiSelect, {
    attachTo: document.body,
    props: {
      label: '选择',
      modelValue: 'a',
      options: [
        { value: 'a', label: '甲' },
        { value: 'b', label: '乙', disabled: true },
        { value: 'c', label: '丙' },
      ],
    },
  })
  const trigger = wrapper.get('button')
  ;(trigger.element as HTMLElement).focus()
  await trigger.trigger('click')
  await flushPromises()
  expect(document.activeElement?.textContent).toContain('甲')
  document.activeElement?.dispatchEvent(
    new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }),
  )
  expect(document.activeElement?.textContent).toContain('丙')
  ;(document.activeElement as HTMLElement).click()
  await nextTick()
  expect(wrapper.emitted('update:modelValue')?.[0]).toEqual(['c'])
  expect(trigger.attributes('aria-expanded')).toBe('false')
  expect(document.activeElement).toBe(trigger.element)
  wrapper.unmount()
})

it('modal handles nested menu Escape before closing itself and restores background/focus', async () => {
  const host = document.createElement('div')
  host.id = 'app'
  document.body.append(host)
  const Demo = defineComponent({
    components: { UiDialog, UiMenu },
    setup() {
      return { open: ref(false) }
    },
    template:
      '<button id="launch" @click="open=true">打开</button><UiDialog v-model:open="open" title="确认"><UiMenu label="菜单" :actions="[{id:\'a\',label:\'操作\'}]" /><template #footer><button>确认</button></template></UiDialog>',
  })
  const wrapper = mount(Demo, { attachTo: host })
  const launch = wrapper.get('#launch')
  ;(launch.element as HTMLElement).focus()
  await launch.trigger('click')
  await flushPromises()
  expect(host.inert).toBe(true)
  expect(document.querySelector('[role=dialog]')).not.toBeNull()
  const menu = document.querySelector<HTMLButtonElement>('.popover-trigger')!
  menu.click()
  await flushPromises()
  document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
  await nextTick()
  expect(document.querySelector('[role=menu]')).toBeNull()
  expect(document.querySelector('[role=dialog]')).not.toBeNull()
  expect(document.activeElement).toBe(menu)
  document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
  await nextTick()
  expect(host.inert).toBe(false)
  expect(document.activeElement).toBe(launch.element)
  wrapper.unmount()
})

it('keeps tree disclosure separate from selection and uses arrows to reach children', async () => {
  const wrapper = mount(UiTree, {
    attachTo: document.body,
    props: {
      label: '树',
      nodes: [{ id: 'p', label: '父', children: [{ id: 'c', label: '子' }] }],
      selected: 'p',
      expanded: [],
    },
  })
  await wrapper.get('.tree-disclosure').trigger('click')
  expect(wrapper.emitted('update:expanded')?.[0]).toEqual([['p']])
  expect(wrapper.emitted('update:selected')).toBeUndefined()
  await wrapper.setProps({ expanded: ['p'] })
  await wrapper.get('[data-tree-id=p]').trigger('keydown', { key: 'ArrowRight' })
  expect(document.activeElement?.getAttribute('data-tree-id')).toBe('c')
  await wrapper.get('[data-tree-id=c]').trigger('keydown', { key: 'Enter' })
  expect(wrapper.emitted('update:selected')?.[0]).toEqual(['c'])
  wrapper.unmount()
})

it('tabs skip disabled items using keyboard navigation', async () => {
  const wrapper = mount(UiTabs, {
    props: {
      label: '标签',
      modelValue: 'a',
      items: [
        { value: 'a', label: '甲' },
        { value: 'b', label: '乙', disabled: true },
        { value: 'c', label: '丙' },
      ],
    },
  })
  await wrapper.get('[role=tablist]').trigger('keydown', { key: 'ArrowRight' })
  expect(wrapper.emitted('update:modelValue')?.[0]).toEqual(['c'])
  wrapper.unmount()
})

it('composer rejects whitespace and emits plain text without rendering it as HTML', async () => {
  const wrapper = mount(UiComposer, { props: { modelValue: '  ' } })
  await wrapper.get('form').trigger('submit')
  expect(wrapper.emitted('send')).toBeUndefined()
  expect(wrapper.text()).toContain('请输入内容')
  await wrapper.setProps({ modelValue: ' <script>alert(1)</script> ' })
  await wrapper.get('form').trigger('submit')
  expect(wrapper.emitted('send')?.[0]).toEqual(['<script>alert(1)</script>'])
  expect(wrapper.find('script').exists()).toBe(false)
  wrapper.unmount()
})

it('only reports success after completion, recovers on error, and cancels success timer on disposal', async () => {
  vi.useFakeTimers()
  let feedback!: ReturnType<typeof useActionFeedback>
  const wrapper = mount(
    defineComponent({
      setup() {
        feedback = useActionFeedback()
        return () => null
      },
    }),
  )
  let resolve!: () => void
  const task = feedback.run(
    () =>
      new Promise<void>((done) => {
        resolve = done
      }),
  )
  expect(feedback.state.value).toBe('loading')
  resolve()
  await task
  expect(feedback.state.value).toBe('success')
  await vi.advanceTimersByTimeAsync(2400)
  expect(feedback.state.value).toBe('idle')
  await feedback.run(() => {
    throw new Error('保留失败原因')
  })
  expect(feedback.error.value).toBe('保留失败原因')
  expect(feedback.state.value).toBe('idle')
  await feedback.run(() => {})
  wrapper.unmount()
  expect(vi.getTimerCount()).toBe(0)
})

it('system theme is default and manual theme overrides media changes', () => {
  initializeTheme()
  const theme = useTheme()
  expect(theme.mode.value).toBe('system')
  expect(theme.resolved.value).toBe('light')
  theme.setTheme('dark')
  expect(document.documentElement.dataset.theme).toBe('dark')
  const listener = vi.mocked(window.matchMedia).mock.results[0]!.value.addEventListener.mock
    .calls[0][1]
  listener()
  expect(theme.resolved.value).toBe('dark')
  theme.setTheme('system')
})

it('drawer inherits Escape closing by default and keeps explicit close policy', async () => {
  const { UiDrawer } = await import('../components/ui')
  const wrapper = mount(UiDrawer, { attachTo: document.body, props: { title: '详情', open: true } })
  await flushPromises()
  document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
  await nextTick()
  expect(wrapper.emitted('update:open')?.[0]).toEqual([false])
  wrapper.unmount()
  const fixed = mount(UiDrawer, {
    attachTo: document.body,
    props: { title: '需确认', open: true, closeOnEscape: false },
  })
  await flushPromises()
  document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
  expect(fixed.emitted('update:open')).toBeUndefined()
  fixed.unmount()
})

it('buttons retain accessible action labels during loading and success feedback', async () => {
  const { UiButton } = await import('../components/ui')
  const wrapper = mount(UiButton, {
    props: { state: 'loading', loadingLabel: '保存中', successLabel: '已保存' },
    slots: { default: '保存修改' },
  })
  expect(wrapper.get('button').attributes('aria-label')).toBe('保存中')
  expect(wrapper.get('button').attributes()).toHaveProperty('disabled')
  await wrapper.setProps({ state: 'success' })
  expect(wrapper.get('button').attributes('aria-label')).toBe('已保存')
  expect(wrapper.get('button').attributes()).not.toHaveProperty('disabled')
  wrapper.unmount()
})

it('composer keeps an accessible name without a visible title and blocks disabled submission', async () => {
  const wrapper = mount(UiComposer, {
    props: { modelValue: '内容', disabled: true, label: '输入消息' },
  })
  expect(wrapper.find('label').exists()).toBe(false)
  expect(wrapper.get('textarea').attributes('aria-label')).toBe('输入消息')
  await wrapper.get('form').trigger('submit')
  expect(wrapper.emitted('send')).toBeUndefined()
  await wrapper.setProps({ disabled: false, modelValue: '' })
  await wrapper.get('form').trigger('submit')
  const error = wrapper.get('[role=alert]')
  expect(wrapper.get('textarea').attributes('aria-describedby')).toBe(error.attributes('id'))
  expect(wrapper.get('textarea').attributes('aria-invalid')).toBe('true')
  wrapper.unmount()
})
