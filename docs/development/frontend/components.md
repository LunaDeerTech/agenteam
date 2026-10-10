# 公共组件接口

组件统一从 `web/src/components/ui/index.ts` 导入，样式由应用入口加载。普通组件使用原生语义，内容通过插槽传入；不要复制 Debug 模板实现业务控件。

## 控件与状态

| 组件 | 主要接口 |
| --- | --- |
| UiButton | `variant: default / primary / ghost / danger`；`state: idle / loading / success`；`loadingLabel`、`successLabel`、`disabled`、`type`、`icon`；图标按钮传入 `aria-label` |
| UiField | `label`、`hint`、`error`、`required`、可选 `id`；默认插槽提供 `id / invalid / describedby`，调用方传给实际输入 |
| UiInput / UiTextarea | 字符串 `v-model`，`invalid`；原生 id、name、readonly、disabled、aria 属性及事件透传到输入元素 |
| UiCheckbox / UiSwitch | 布尔 `v-model`；disabled、name；原生属性透传到 input；默认插槽提供标签 |
| UiRadioGroup | 字符串 `v-model`；`options: ChoiceOption[]`、legend、name、disabled；未传 name 时自动生成，保留原生组内键盘行为 |
| UiSelect | 字符串 `v-model`；options、label、id、disabled、invalid；锚定列表、选中标记、方向键、Home / End、文字首字符查找 |
| UiMenu | label、`actions: MenuAction[]`；`select(id)`；disabled / danger 动作各自标记；关闭恢复触发焦点 |
| UiProgress | value、max（默认 100）、label；有可访问名称与百分比文字 |
| UiBadge / UiIdentity | badge 使用语义 tone；identity 使用 1 / 2 / 3 示例身份色或调用方传入的 CSS color；正式身份映射由调用方决定 |

`ChoiceOption` 使用 value、label、可选 disabled；`MenuAction` 使用 id、label、可选 disabled / danger。业务 ID 和状态映射由页面负责，不把领域枚举写入基础组件。

`useActionFeedback().run(action)` 接受真实操作函数。操作期间 loading，完成后 success 保留 2400ms；异常保留 error 并恢复 idle。组件卸载后不继续更新反馈。默认按钮为 `type="button"`，表单提交显式指定 submit。

## 内容、消息与导航

| 组件 | 主要接口 |
| --- | --- |
| UiTree | nodes、label、`v-model:selected`、`v-model:expanded`；TreeNode 使用 id、label、children、disabled 和可选 expandable；suffix 插槽接收 node；展开与选择独立 |
| UiCollapse | label、`v-model:open`；正文插槽；默认折叠、无边框，动态高度可中断过渡；maxHeight 默认 min(320px, 40dvh)，超出内部滚动 |
| UiCard | 默认内容插槽 |
| UiList | items（id、label、disabled）、label；select(id)；suffix 插槽 |
| UiTable | columns（key、label）、rows（稳定 id）、caption；`cell-字段名` 插槽接收 row / value；容器局部横向滚动，不提供业务排序和筛选 |
| UiMessage | role（user / agent）、name、time；正文插槽；无头像，用户右侧、Agent 左侧 |
| UiReference / UiCode | reference 的 select 事件由页面处理；code 属性按纯文本输出，不解释 HTML / Markdown，plain 去掉代码表面和边框 |
| UiComposer | 字符串 v-model、state、disabled、error、label；send(text) 发出非空且去除首尾空格的文本，父级负责存储与清空；actions 插槽；label 仅作可访问名称，不显示标题，内部输入无边框或聚焦高亮 |
| UiState / UiSkeleton / UiSpinner | state 使用 kind（loading / empty / error）、title、description 与恢复操作插槽；skeleton 可指定 lines；独立 spinner 需搭配等待文字 |
| UiBreadcrumb | items（label、可选 to）；最后一项为当前位置 |
| UiTabs | 字符串 v-model、items（ChoiceOption[]）、label；默认插槽接收 value；方向键、Home / End 自动激活，跳过禁用项；指示线和内容滑动，退出面板 inert，支持快速反向和减少动效 |
| UiPagination | 数字 v-model、total（页数）、label；上一页、下一页与当前页附近页码，超出范围会修正 |

`TreeNode.expandable` 未传时保留按 `children?.length` 判断的原行为；`true` 支持子级尚未加载的节点，`false` 按叶子处理。展开按钮、键盘、子级可见性和 `aria-expanded` 使用同一判断；业务组件负责按展开动作读取子级。

Tree 使用可见节点的方向键、Home / End、Enter / Space；右键展开或进入子节点，左键折叠或回到父节点。标签页需要传入有效初始 value，列表和树节点 ID 必须唯一且稳定。

## 浮层

UiDialog / UiDrawer 使用 `v-model:open`、title、closeOnOutside、closeOnEscape。两种关闭策略默认 true；危险或未保存表单由业务调用方明确设置，不以原型默认值推导产品规则。`close(reason)` 区分 escape / outside / action；footer 插槽提供 close 函数。

UiDialog 另支持可选 `fallbackFocus: HTMLElement | null`，由调用方提供当前本地 DOM 引用，例如已有 `tabindex="-1"` 的页面标题。正常 `open` 从 true 变 false 且没有剩余模态时，先恢复合法且实际可聚焦的原触发位置；失败后才尝试关闭时读取的最新后备目标。目标须属于同一 document、仍连接且可见可聚焦，不得为 BODY/HTML、禁用或隐藏/inert 范围内节点；组件不补 tabindex、不搜索其它页面目标或延迟抢焦点。直接卸载组件不尝试该后备，原触发位置恢复规则保持；仍有模态时完全沿其合法范围恢复，不使用页面后备。未传或当前 null/undefined 时保留原行为。调用方正常关闭须保留 Dialog 实例并设置 open=false；checking、离页或身份失效导致的整体卸载不触发此能力。UiDrawer、UiPopover 等封装器未新增此 typed API。

UiPopover 提供锚定说明容器，使用 `v-model:open`、label、disabled，trigger / 默认插槽；默认插槽提供 close 函数。UiMenu 和 UiSelect 在同一定位基础上保留独立动作和选择语义，不使用搜索对话框替代。

UiSearchDialog 使用 title、items（id、label）和 `v-model:open`，发出 select(id)。这里只负责本地列表过滤，不定义正式搜索权限、范围或 API。UiTooltip 只接受 text 和触发标签，说明不可含交互控件。

浮层通过共享层管理器处理顶层 Escape、焦点恢复和模态焦点限制。模态背景 inert，退出内容立即 inert 并从可访问树移除。锚定定位使用 Floating UI 自动翻转、视口边缘避让和尺寸限制。Tab 从下拉返回正常页面顺序；tooltip 在原位置所属层内定位，不覆盖更高模态。

所有组件使用共享主题和 `prefers-reduced-motion`。参数修改同步[样式文档](../../frontend-design/styles/README.md)，不在正式页面另设一套控件颜色或字号。
