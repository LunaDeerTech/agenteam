import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { h } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import SettingsShell from '../components/layout/SettingsShell.vue'
const system = () => [
  {
    key: 'users-invitations',
    label: '用户与邀请',
    children: [
      { label: '用户', path: '/system/users' },
      { label: '待注册邀请', path: '/system/invitations' },
    ],
  },
]
let wrapper: VueWrapper | undefined,
  narrow = false
beforeEach(() =>
  vi.stubGlobal(
    'matchMedia',
    vi.fn((media: string) => ({
      media,
      matches: narrow,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })),
  ),
)
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  document.body.innerHTML = ''
  narrow = false
  vi.unstubAllGlobals()
})
async function router() {
  const r = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/:pathMatch(.*)*', component: { render: () => null } }],
  })
  await r.push('/system/users')
  await r.isReady()
  return r
}
describe('SettingsShell compatible one-group multiple leaves', () => {
  it('shows one heading/toggle, folds both leaves without navigation, and exactly selects either leaf', async () => {
    const r = await router()
    wrapper = mount(SettingsShell, {
      props: { title: '系统设置', groups: system(), showLogout: false },
      global: { plugins: [r] },
    })
    const toggle = wrapper.get('.settings-group-toggle'),
      menu = wrapper.get('nav')
    expect(wrapper.findAll('.settings-group')).toHaveLength(1)
    expect(menu.findAll('a').map((a) => a.text())).toEqual(['用户', '待注册邀请'])
    expect(menu.findAll('[aria-current="page"]').map((a) => a.text())).toEqual(['用户'])
    expect(wrapper.get('.settings-group').classes()).toContain('selected')
    const controls = toggle.attributes('aria-controls')
    expect(wrapper.get('ul').attributes('id')).toBe(controls)
    await toggle.trigger('click')
    expect(wrapper.findAll('a')).toHaveLength(0)
    expect(r.currentRoute.value.path).toBe('/system/users')
    expect(toggle.attributes('aria-expanded')).toBe('false')
    await toggle.trigger('click')
    await wrapper.get('a[href="/system/invitations"]').trigger('click')
    await flushPromises()
    expect(menu.findAll('[aria-current="page"]').map((a) => a.text())).toEqual(['待注册邀请'])
    expect(wrapper.get('.settings-group').classes()).toContain('selected')
    expect(wrapper.get('ul').attributes('id')).toBe(controls)
    await r.push('/system/invitations/more')
    await flushPromises()
    expect(wrapper.find('[aria-current="page"]').exists()).toBe(false)
    expect(wrapper.get('.settings-group').classes()).not.toContain('selected')
  })
  it('retains IDs and folded state across label, order and children changes; adds expanded keys and removes old groups', async () => {
    const r = await router()
    wrapper = mount(SettingsShell, {
      props: {
        groups: [
          ...system(),
          { key: 'other', label: 'Other', children: [{ label: 'Other', path: '/other' }] },
        ],
      },
      global: { plugins: [r] },
    })
    const originalID = wrapper.findAll('.settings-group-toggle')[0]!.attributes('aria-controls')
    await wrapper.findAll('.settings-group-toggle')[0]!.trigger('click')
    await wrapper.setProps({
      groups: [
        { key: 'added', label: 'Added', children: [{ label: 'Added', path: '/added' }] },
        {
          key: 'users-invitations',
          label: 'Renamed',
          children: [
            { label: 'Invites', path: '/system/invitations' },
            { label: 'Users changed', path: '/system/users' },
          ],
        },
      ],
    })
    expect(wrapper.findAll('a').map((a) => a.text())).toEqual(['Added'])
    expect(wrapper.text()).not.toContain('Other')
    const retained = wrapper.findAll('.settings-group-toggle')[1]!
    expect(retained.attributes('aria-expanded')).toBe('false')
    expect(retained.attributes('aria-controls')).toBe(originalID)
    await retained.trigger('click')
    expect(wrapper.findAll('a').map((a) => a.text())).toEqual(['Added', 'Invites', 'Users changed'])
    expect(wrapper.get('[aria-current="page"]').text()).toBe('Users changed')
    await wrapper.setProps({
      groups: [
        { key: 'other', label: 'Restored', children: [{ label: 'Restored', path: '/other' }] },
      ],
    })
    expect(wrapper.findAll('a').map((a) => a.text())).toEqual(['Restored'])
  })
  it('separates legacy paths from explicit group keys and namespaces repeated labels across sibling instances', async () => {
    const r = await router()
    const groups = [
      { label: 'Repeated', path: '/same', leaf: 'Repeated' },
      { key: '/same', label: 'Repeated', children: [{ label: 'Repeated', path: '/second' }] },
    ]
    wrapper = mount(
      { render: () => h('div', [h(SettingsShell, { groups }), h(SettingsShell, { groups })]) },
      { global: { plugins: [r] } },
    )
    const ids = wrapper.findAll('.settings-group-toggle').map((b) => b.attributes('aria-controls'))
    expect(new Set(ids).size).toBe(4)
    for (const value of ids)
      expect(wrapper.findAll('ul').filter((ul) => ul.attributes('id') === value)).toHaveLength(1)
    await wrapper.findAll('.settings-group-toggle')[0]!.trigger('click')
    expect(wrapper.findAll('a')).toHaveLength(3)
  })
  it('retains default personal labels, routes, title and logout event', async () => {
    const r = await router()
    wrapper = mount(SettingsShell, { global: { plugins: [r] } })
    expect(wrapper.get('nav').attributes('aria-label')).toBe('个人设置')
    expect(wrapper.get('h2').text()).toBe('个人设置')
    expect(wrapper.findAll('a').map((a) => [a.text(), a.attributes('href')])).toEqual([
      ['基本资料', '/settings/profile'],
      ['主题', '/settings/appearance'],
      ['修改密码', '/settings/password'],
    ])
    await wrapper
      .findAll('button')
      .find(
        (b) =>
          b.element.querySelector('.button-label[aria-hidden="false"]')?.textContent?.trim() ===
          '退出登录',
      )!
      .trigger('click')
    expect(wrapper.emitted('logout')).toHaveLength(1)
  })
  it('retains single-leaf custom calls and new legacy paths expand without resetting retained state', async () => {
    const r = await router()
    wrapper = mount(SettingsShell, {
      props: {
        title: 'Custom',
        showLogout: false,
        groups: [{ label: 'Group', path: '/system/users', leaf: 'Single' }],
      },
      global: { plugins: [r] },
    })
    const toggle = wrapper.get('button'),
      control = toggle.attributes('aria-controls')
    await toggle.trigger('click')
    await wrapper.setProps({
      groups: [
        { label: 'Updated', path: '/system/users', leaf: 'Changed' },
        { label: 'Second', path: '/second', leaf: 'New' },
      ],
    })
    expect(wrapper.findAll('a').map((a) => a.text())).toEqual(['New'])
    expect(wrapper.findAll('button')[0]!.attributes('aria-controls')).toBe(control)
  })
  it('keeps both narrow Drawer links and closes it on leaf selection or Escape', async () => {
    narrow = true
    const r = await router()
    wrapper = mount(SettingsShell, {
      props: { title: '系统设置', groups: system(), showLogout: false },
      attachTo: document.body,
      global: { plugins: [r] },
    })
    await flushPromises()
    const trigger = wrapper.get('.settings-menu-button')
    await trigger.trigger('click')
    await flushPromises()
    expect(document.querySelectorAll('[role="dialog"] a')).toHaveLength(2)
    ;(
      document.querySelector('[role="dialog"] a[href="/system/invitations"]') as HTMLAnchorElement
    ).click()
    await flushPromises()
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(r.currentRoute.value.path).toBe('/system/invitations')
    await trigger.trigger('click')
    await flushPromises()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await flushPromises()
    expect(document.querySelector('[role="dialog"]')).toBeNull()
  })
})
