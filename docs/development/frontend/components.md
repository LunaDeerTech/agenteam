# 公共组件接口

本页描述新 `web/` 的实际公共合同，由 [UI 导出](../../../web/src/components/ui/index.ts)统一提供。旧 `web-crape/` 的组件、Floating UI、业务状态、Debug 示例与历史验收不属于本页接口；历史实现可在归档源码和 Git 历史查询。设计方向见[设计 v2](../../frontend-design/redesign/README.md)，本页接口存在不等于浏览器验收通过，实际检查见[当前状态](../../../.agent-state/current.md)。

## 基础控件

| 组件 | Props / model | 输出与内容 |
| --- | --- | --- |
| `UiButton` | `variant`: `default`（默认）/`primary`/`ghost`/`danger`；`type`: `button`（默认）/`submit`/`reset`；`disabled`、`loading` | 默认插槽提供按钮文字；loading 同时禁用并设置 `aria-busy`，不定义业务成功/失败状态 |
| `UiIcon` | `name: string`、`size?: number` | 装饰性 SVG；需要可访问名称的操作使用 UiIconButton |
| `UiIconButton` | 必填 `label`、`icon`；可选 `disabled`、`active` | 原生 button，label 同时用于可访问名称和 title；点击事件沿 Vue attrs 传递 |
| `UiInput` | 必填 `label`；字符串 `v-model`；可选 `id`、`hint`、`error`、`disabled`、`type`（默认 text） | label 关联 input；attrs 传到 input，hint/error 与外部 `aria-describedby` 合并；error 设置无效状态 |
| `UiSelect` | 必填 `label`、`options: SelectOption[]`；字符串 `v-model`；可选 `id`、`placeholder`（默认“请选择”）、`disabled`、`hint`、`error` | 锚定列表选择，不提供远程查询或搜索服务；选项字段为 `value`、`label`、可选 `disabled` |
| `UiTabs` | 必填 `label`、`items: TabItem[]`、字符串 `v-model` | 默认插槽接收 `{ value }`；项字段为 `value`、`label`、可选 `disabled`，显示一个当前面板 |
| `UiMenu` | 必填 `label`、`items: MenuItem[]`；可选 `disabled`、`align: start/end`（默认 end） | `select(id)`；trigger 插槽可定制触发内容；项字段为 `id`、`label`、可选 `checked`、`disabled`、`icon` |
| `UiDialog` | 必填 `open`、`title`；可选 `side: center/left`（默认 center）、`closeOnOutside`、`closeOnEscape`（默认 true） | `update:open(false)`、`close(reason)`；默认/footer 插槽均接收 `{ close }` |

`MenuItem`、`SelectOption`、`TabItem` 从同一 `index.ts` 导出。选择器与标签页的 item value 应唯一；菜单 id 应唯一。选择/切换只输出本地交互值，业务存取与确认由页面后续合同定义。

Select 和 Menu 使用方向键、Home/End、Enter/Space、Escape 及字符定位，并跳过禁用项；Tabs 使用左右方向键与 Home/End 切换可用项。具体浏览器覆盖由 harness 与独审报告确认，不能从键盘处理代码推定所有设备均已验收。

## 浮层

UiDialog 使用原生 `<dialog>.showModal()`，可通过 `v-model:open` 控制。`side="left"` 提供左侧目录载体；本工程没有独立 UiDrawer。关闭原因是 `escape`、`outside` 或 `action`，由 `close` 事件发出；close 插槽函数默认发出 action。关闭按钮也发出关闭请求，页面有未保存或提交门禁时，应在受控 setter 中处理关闭意图。

模态负责自身滚动锁、Tab 焦点循环和关闭时尝试恢复仍可用的原触发位置。新组件没有 `fallbackFocus`、`beforeClose`、通用 `size` 或 `data-autofocus` 扩展合同；如后续业务需要，须通过实际实现与对应检查增加。原生 `autofocus` 属性可按浏览器语义使用，不等于旧组件的焦点管理器。

Menu 与 Select 使用内部 `useAnchoredLayer`，依据触发位置和 viewport 处理翻转、边缘约束、尺寸与滚动/resize 更新；模态内的列表留在所属 dialog。未引入 Floating UI，内部定位 helper 不是对页面开放的公共 API。样式和动效使用共享 token，并处理 `prefers-reduced-motion`。

## 页面组合与滚动

[AppShell](../../../web/src/layouts/AppShell.vue) 保留系统顶栏，将剩余空间交给可收缩的主区；项目路由先展示 [ProjectNavigation](../../../web/src/layouts/ProjectNavigation.vue)。普通页面在标准内容区滚动，账号与 OAuth 回调由 [AccountShell](../../../web/src/layouts/AccountShell.vue)承载。

系统、个人和项目设置只使用一份 [SettingsShell](../../../web/src/layouts/SettingsShell.vue)。它从 route metadata 与集中栏目数组获得 scope、标题、分组和导航；设置目录与正文分工，桌面两区各自管理纵向滚动。窄屏隐藏常驻目录，通过同一个 UiDialog 的 left 模式打开，选中路由后关闭目录。

[PageHeading](../../../web/src/layouts/PageHeading.vue)提供标题、可选说明与 actions 插槽。当前 [SkeletonPage](../../../web/src/views/SkeletonPage.vue)仅输出空正文；没有旧 `page-*` 布局类合同或已实现的业务表单。新增内容应沿壳的可收缩链分配滚动，不重新铺一层全屏高度或复制第二份设置壳。

共享控件、token 和主题独立于页面，不导入业务、归档或测试展示内容。页面级修改遵循文件唯一写者规则；实际尺寸与颜色以新源码和已确认设计为准，不把本页的描述作为视觉接受结论。
