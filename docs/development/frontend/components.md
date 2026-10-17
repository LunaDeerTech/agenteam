# 公共组件接口

本页描述新 `web/` 的实际公共合同。基础控件由 [UI 导出](../../../web/src/components/ui/index.ts)统一提供，系统列表组合从下述专属路径导入。旧 `web-crape/` 的组件、Floating UI、业务状态、Debug 示例与历史验收不属于本页接口；历史实现可在归档源码和 Git 历史查询。设计方向见[设计 v2](../../frontend-design/redesign/README.md)，本页接口存在不等于浏览器验收通过，实际检查见[当前状态](../../../.agent-state/current.md)。

## 基础控件

| 组件 | Props / model | 输出与内容 |
| --- | --- | --- |
| `UiButton` | `variant`: `default`（默认）/`primary`/`ghost`/`danger`；`type`: `button`（默认）/`submit`/`reset`；`disabled`、`loading` | 默认插槽提供按钮文字；loading 同时禁用并设置 `aria-busy`，不定义业务成功/失败状态 |
| `UiIcon` | `name: string`、`size?: number` | 装饰性 SVG；需要可访问名称的操作使用 UiIconButton |
| `UiIconButton` | 必填 `label`、`icon`；可选 `disabled`、`active` | 原生 button，label 同时用于可访问名称和 title；点击事件沿 Vue attrs 传递 |
| `UiSwitch` | 必填 `modelValue: boolean`、`label`；可选 `disabled`、`loading` | 受控原生 button，`role="switch"` 与 `aria-checked` 表达已确认值；`update:modelValue` 仅提出新值。loading 保留焦点、标记 busy/aria-disabled 并阻止重复切换；业务 owner 负责写入、错误与最终值，不直连 API。 |
| `UiInput` | 必填 `label`；字符串 `v-model`；可选 `id`、`hint`、`error`、`disabled`、`type`（默认 text）、`secretConfigured`（默认 false） | label 关联 input；attrs 传到 input，hint/error 与外部 `aria-describedby` 合并；error 设置无效状态。`secretConfigured`仅在password类型且草稿为空时显示固定圆点placeholder与已配置的可访问说明，不写入value/model、不触发编辑；仅由已保存配置元数据驱动，不用于登录/账号密码。 |
| `UiSelect` | 必填 `label`、`options: SelectOption[]`；字符串 `v-model`；可选 `id`、`placeholder`（默认“请选择”）、`disabled`、`hint`、`error` | 锚定列表选择，不提供远程查询或搜索服务；选项字段为 `value`、`label`、可选 `disabled` |
| `UiCheckboxGroup` | 必填 `label`、`options: readonly CheckboxOption[]`（`value`、`label`、可选`icon`、单项`disabled`）；字符串数组 `v-model`；可选 `disabled` | 原生fieldset/legend命名的多选组，选项可用Tab/Space和触控切换；两列轻量选项、继承外层fieldset禁用。可选图标复用UiIcon作装饰，保留文字和原可访问名称。仅回传选定值，不猜选、不校验业务最少项、不直连API。 |
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

界面品牌使用 [docs/logos](../../logos/) 已有正式资源，按需复制浅深主题变体到新工程；不以自绘几何图形代替正式 Logo。

[AppShell](../../../web/src/layouts/AppShell.vue) 保留系统顶栏，将剩余空间交给可收缩的主区；项目路由先展示 [ProjectNavigation](../../../web/src/layouts/ProjectNavigation.vue)。普通页面在标准内容区滚动，账号与 OAuth 回调由 [AccountShell](../../../web/src/layouts/AccountShell.vue)承载。

系统、个人和项目设置只使用一份 [SettingsShell](../../../web/src/layouts/SettingsShell.vue)。它从 route metadata 与集中栏目数组获得 scope、标题、分组和导航；设置目录与正文分工，桌面两区各自管理纵向滚动。窄屏隐藏常驻目录，通过同一个 UiDialog 的 left 模式打开，选中路由后关闭目录。

正文默认最大760px；仅system scope的users、invitations、smtp三项启用 `settings-page-body--wide` 并解除该上限，三列表伸展至与页头相同的有效内边距。正文padding、外层1200px设置框和表单自身行宽保持，不把列表宽度推广到其他路由。

[PageHeading](../../../web/src/layouts/PageHeading.vue)提供标题、可选说明与 actions 插槽。当前 [SkeletonPage](../../../web/src/views/SkeletonPage.vue)仅输出空正文；没有旧 `page-*` 布局类合同或已实现的业务表单。新增内容应沿壳的可收缩链分配滚动，不重新铺一层全屏高度或复制第二份设置壳。

## 系统列表与游标分页

[SystemList](../../../web/src/components/system/SystemList.vue)是系统业务域的共享组合，用户、邀请、SMTP投递三页共用状态行、页长选择、读取错误/空态、行容器与分页导航。页面提供字段和行操作，业务详情、命令及Unknown恢复仍归所属页面；不复制第二套列表容器或游标状态。

| 接口 | 合同 |
| --- | --- |
| 必填props | `list: CursorList<T>`、`rowKey(item): string`、`label`、`emptyText`；rowKey使用稳定领域ID。 |
| 可选props | `resultsLabel`、`paginationLabel`覆盖默认可访问名称；`disabled`门禁列表交互，`pageSizeDisabled`单独限制页长选择。普通读取中页长选择仍可用。 |
| 插槽 | `item({ item })`提供行字段，`actions({ item })`提供行操作；`error-extra({ error })`补充该领域的错误恢复入口。 |
| 暴露方法 | `navigate(action: () => Promise<void>)`执行列表操作后聚焦结果或错误；`focusResults({ scroll?: boolean })`显式回焦，`scroll:false`保滚动；`refresh()`回第一页并聚焦。 |

[useCursorList](../../../web/src/system/useCursorList.ts)接受 `read(cursor, current, limit): Promise<CursorPage<T>>`，页结果为 `{ items, next_cursor? }`。领域reader负责端点与严格DTO校验，继续经共享System请求/权限/账号transport，并传递 `current`；hook自身不另建网络owner。

- `options.enabled?: () => boolean`控制当前页面是否允许读取，仍与账号开关/系统权限相交；`immediate:false`由调用者明确启动读取。
- 状态提供 `items/loading/refreshing/pending/loaded/error/index/nextCursor`及只读 `pageSize`。默认每页10，可选5/10/20/50；`setPageSize`清历史/旧项并从首项真实读取，不持久化偏好。
- `first()`读取第一页，`previous()/next()`沿当前游标历史；`refresh()`重读当前已确认页，`reload()`重试最近目标页。`first/refresh/reload({ preserve:true })`仅在已有数据时保留列表DOM与观察，使用refreshing状态；失败仍显示真实错误，不把旧观察当新响应。
- `cancel()`使当前读取失去发布资格，`reset()`另清列表/游标并恢复默认页长；系统权限代际变化执行reset。旧响应不发布，真实请求及body/cancel未结束前仍由底层唯一owner串行协调，页面不能用本地pending解除代替网络完成。
- `updateItem(matches, update)`只更新当前已加载的匹配项，用于已确认的安全投影；不能冒充命令回执、全量刷新或总数。

基础控件、token 和主题独立于页面，不导入业务、归档或测试展示内容。SystemList位于系统域并遵循[系统账号设置合同](../work-items/frontend-system-account-settings.md)的权限与恢复规则。页面级修改遵循文件唯一写者规则；实际尺寸与颜色以新源码和已确认设计为准，不把本页的描述作为视觉接受结论。


## 可搜索的锚定选择

`UiSearchSelect` 是不访问业务 API 的共享控件，使用 `useAnchoredLayer` 锚定触发框，必要时翻到上方并限制在可视 viewport 内。支持浏览器原生 popover 时进入顶层以避免父容器裁切；模态内仍归属原 dialog，不创建第二个居中模态或全页焦点锁。

- Props：必填 `label`、`options: SearchSelectOption[]`；可选 `modelValue`、`selectedLabel`、`query`、`loading`、`error`、`pageIndex`、`hasPrevious`、`hasNext`、`disabled`、`placeholder`、`searchPlaceholder`、`emptyText`。选项为 `{value,label,description?,disabledReason?}`；当前文字由 `selectedLabel` 提供，不要求当前项在已加载页中。空态默认“点击选择模型”。
- Emits：`update:modelValue(value)`、`update:query(value)`、`open-change(open)`、`previous`、`next`、`retry`。外层提供真实分页与搜索；控件不扫描当前页充当全局搜索，不自行保存业务配置。
- Trigger 为具可访问名称的 button，公开 expanded/controls/dialog 关系。浮层包含搜索 combobox 和 listbox；方向键跳过禁用项，Enter 选择，Escape 关闭并回触发框；Tab 可到分页/重试并离开浮层，外部点击关闭而不抢走目标焦点。选项常显禁用原因和当前选择标记；hover 不替换已选文字。
- 浮层宽度最多400px、最高420px并受viewport约束，结果区域独立滚动，搜索/分页保持可达；复用共享overlay动效与 reduced-motion规则。`useAnchoredLayer` 新增的width/maxHeight/topLayer仅为此控件选择启用，原Menu/Select默认合同不变。

系统域 `components/system/ModelSelect.vue` 组合该控件与真实 `/system/models` 查询：无provider过滤的一层列表、250ms搜索、每页10条，读取使用系统权限/身份与游标owner。列表项的provider安全投影供“供应商/模型ID”及资格说明使用；`issue(model)`由调用方提供，最终保存/删除仍由服务端重验。用途页和删除替换入口共用该组件；选择只发出 `select(model)`，取消不改草稿。
