import type { ChoiceOption, TreeNode } from '../../components/ui'
export const sections = [
  { id: 'buttons', label: '按钮与反馈' },
  { id: 'forms', label: '表单控件' },
  { id: 'selectors', label: '选择器与菜单' },
  { id: 'tree', label: '树与折叠' },
  { id: 'status', label: '状态与身份' },
  { id: 'cards', label: '卡片与列表' },
  { id: 'messages', label: '消息与输入' },
  { id: 'layers', label: '浮层与提示' },
  { id: 'states', label: '加载与异常' },
  { id: 'navigation', label: '导航与分页' },
  { id: 'tokens', label: '样式参数' },
]
export const choices: ChoiceOption[] = [
  { value: 'document', label: '文档' },
  { value: 'code', label: '代码' },
  { value: 'long', label: '具有很长名称的组件选项，用于检查文字换行和截断表现' },
  { value: 'disabled', label: '暂不可选', disabled: true },
]
export const treeNodes: TreeNode[] = [
  {
    id: 'design',
    label: '设计规范',
    children: [
      { id: 'colors', label: '颜色与主题' },
      {
        id: 'interactions',
        label: '控件与交互',
        children: [{ id: 'keyboard', label: '键盘操作规范与完整的长名称示例' }],
      },
      { id: 'pending', label: '尚未开放的文档', disabled: true },
    ],
  },
  { id: 'development', label: '开发说明' },
]
export const tokenNames = [
  '--bg',
  '--canvas',
  '--surface',
  '--surface-alt',
  '--card',
  '--text',
  '--muted',
  '--border',
  '--control-border',
  '--accent',
  '--accent-soft',
  '--action-fill',
  '--action-text',
  '--success',
  '--warning',
  '--danger',
  '--agent-1',
  '--agent-2',
  '--agent-3',
  '--font-sans',
  '--font-mono',
  '--font-size',
  '--line-height',
  '--space',
  '--section-gap',
  '--content-gap',
  '--radius',
  '--radius-lg',
  '--nav-height',
  '--control-height',
  '--motion',
  '--motion-panel',
  '--motion-exit',
  '--ease',
]

export const executionLog = Array.from(
  { length: 40 },
  (_, index) =>
    `[10:38:${String(index).padStart(2, '0')}] DEMO ${String(index + 1).padStart(2, '0')} · 本地执行日志示例，仅用于验证滚动容器。`,
).join('\n')
