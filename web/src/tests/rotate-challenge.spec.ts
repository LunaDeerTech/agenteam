import { afterEach, describe, expect, it } from 'vitest'
import { mount, type VueWrapper } from '@vue/test-utils'
import { Rotate } from 'go-captcha-vue'
import RotateChallenge from '../components/account/RotateChallenge.vue'
const question = {
  id: '01900000-0000-7000-8000-000000000001',
  mode: 'rotate' as const,
  master: 'data:image/png;base64,AA==',
  thumb: 'data:image/png;base64,AQ==',
  expires_at: '2026-10-05T12:34:56.123456Z',
}
let wrapper: VueWrapper | undefined
afterEach(() => {
  wrapper?.unmount()
  document.body.innerHTML = ''
})
describe('the official rotate wrapper', () => {
  it('uses real public pictures and submits the same bounded angle from keyboard and official callbacks', async () => {
    wrapper = mount(RotateChallenge, {
      props: { challenge: question, busy: false },
      attachTo: document.body,
    })
    expect(document.activeElement?.id).toBe('rotate-angle')
    const official = wrapper.getComponent(Rotate)
    expect(official.props('data')).toMatchObject({
      image: question.master,
      thumb: question.thumb,
      angle: 0,
    })
    const range = wrapper.get('input[type="range"]')
    await range.setValue('137')
    await range.trigger('keydown', { key: 'Enter' })
    expect(wrapper.emitted('verify')).toEqual([[137]])
    expect(official.props('data')).toMatchObject({ angle: 137 })
    const events = official.props('events') as {
      rotate: (v: number) => void
      confirm: (v: number) => void
    }
    events.rotate(222.8)
    events.confirm(222.8)
    expect(wrapper.emitted('verify')).toEqual([[137], [223]])
    expect(official.props('data').angle).toBe(137)
    expect(wrapper.text()).toContain('仍需要视觉')
  })
  it('keeps actual verification pending and invalidates the old angle on a new question', async () => {
    wrapper = mount(RotateChallenge, {
      props: { challenge: question, busy: false },
      attachTo: document.body,
    })
    await wrapper.get('input').setValue('87')
    await wrapper.setProps({ busy: true })
    expect(wrapper.get('input').attributes()).toHaveProperty('disabled')
    const events = wrapper.getComponent(Rotate).props('events') as { confirm: (v: number) => void }
    events.confirm(87)
    expect(wrapper.emitted('verify')).toBeUndefined()
    await wrapper.setProps({
      busy: false,
      challenge: { ...question, id: '01900000-0000-7000-8000-000000000002' },
    })
    expect(wrapper.get('input').element).toHaveProperty('value', '0')
    expect(document.activeElement?.id).toBe('rotate-angle')
    const close = wrapper.findAll('button').find((b) => b.text().includes('关闭验证'))!
    await close.trigger('click')
    expect(wrapper.emitted('close')).toEqual([[]])
    expect(wrapper.emitted('verify')).toBeUndefined()
  })
  it('hands focus to a newly enabled question once and preserves later external input focus', async () => {
    wrapper = mount(RotateChallenge, {
      props: { challenge: question, busy: true },
      attachTo: document.body,
    })
    expect(document.activeElement?.id === 'rotate-angle').toBe(false)
    await wrapper.setProps({ busy: false })
    expect(document.activeElement?.id).toBe('rotate-angle')
    const input = document.createElement('input')
    document.body.append(input)
    input.focus()
    await wrapper.setProps({ busy: true })
    await wrapper.setProps({ busy: false })
    expect(document.activeElement === input).toBe(true)
    await wrapper.setProps({
      busy: true,
      challenge: { ...question, id: '01900000-0000-7000-8000-000000000002' },
    })
    await wrapper.setProps({ busy: false })
    expect(document.activeElement === input).toBe(true)
  })
})
