# D27 前置修复：Dialog 关闭后的显式页面焦点后备

状态：rev1，2026-10-06 五条共享产品路径已通过作者与独立验收、主线程采纳，提交推送 `fd32120eba4c76f67b649248377f4825a78d5d79`，主线程核远端一致；见[正式报告与最小证据](../agent-team/dialog-fallback-focus-verification.md)。作者114纯测／45真实组件例通过，独立8纯测按两轮组合／3真实例通过，原红及清理边界保留。原静审全文 SHA256 `c4ad9be1faaaddfe1abcdad92324e0822117cfa7cc5712eac3d219c72f34dfb8`，技术§1–4 SHA256 `e1e5606cbb493f5079aa51c72eaa46462ae80d6794059ca4db9904903e4cef75`保持原字节；页首接受状态覆盖下文历史派工时态，不扩大技术范围。

固定组件输入为已接受 `870ebbb986f56bb62be34ccdfa2c77819ce995a6`，包含共享模态恢复 `79f922e`；作者input01 manifest为 `604a4738761e428b0bff3f8ea64319ae07f6e8e072b97dead925c309c09f6860`。本卡独立结果没有消费未验 Account 候选；账号安全按rev2继续实际Session／Navigation组合验收，尚未产品接受。SMTP及其它业务结果不在本次接受范围。

## 1. 责任边界与固定证据

[共享模态恢复 §2](d27-modal-focus-restoration.md#2-最小恢复契约)明确限定“仍有modal”的合法恢复，**没有剩余modal时保持原规则、不增加fallback**。本卡新增可选显式目标，单独接受该扩展；不把原通过结论改写为已经覆盖单层确认重挂。[公共浮层契约](../frontend/components.md#浮层)及[焦点样式规则](../../frontend-design/styles/components-and-interactions.md#5-浮层用途层级与焦点)的正常触发位置恢复保持，无全局任意目标搜索。

固定 `useLayer.ts` SHA256 `92de6b58cd53e813cd8c90bc23478ce935eca36ddc88ce96fdd2caf7a3f3227f`，UiDialog为 `65ddffc60af0494f600cc0eae318576585e4e0093085e4b8b2d6a59d071f5065`。现remove在无剩余modal时仅向仍connected的开层trigger调用focus；没有最新页面目标。开层时open=true的重挂组件可能早于页面ref就绪，仅在open时捕获后备会丢失后续ref。以上是静态源码事实，不说明某次运行究竟捕获哪个trigger。

业务原红及诊断证据分列：

- 冻结根 `/workspace/scratch/agenteam-account-security-frontend-b0df6sdm/input01` 的manifest SHA256 `a748554b1502fe5878ee55751d54c95f203fa547467e3725af217d17c01cd437`，26源；已核View `eff4bdf03cd2dda77ee384cea83d5923e40ee57b127b420777a7a57f690565a6`、controller `97261a121b31d9dcb5cb8e10a869cbe40d0cc48611a82aa3fa2d97db49a8ad82`及其所用共享组件均匹配manifest/固定Git。
- new01的Navigation原断言要求：dirty离页确认→合成pageshow触发真实Session GET→一次受控503→同Session恢复→继续编辑后overlay=0、零新PUT，activeElement仍应connected、非BODY、非inert/aria-hidden。主线程和独立负责人确认这是真实行为红，另外四组通过不替代本组。
- 仅测试安全诊断的input03 SHA256 `ca7cbe322cc50e875e5f30d788ee7950b27888cd90cd607ad87e65ee2b2f52f4`保持input01产品不变；new02仍actual exit1/55.758秒、Navigation6.87秒。已只读核原 `runs/new02/raw.log` SHA256 `8b7a2acebda38ddd2a9752066a455e44ba63e737cd217e0839dbc64a7b1f9a83`：断言点activeElement=BODY、connected=true、inert/ariaHidden/disabled=false、overlays=0、checking=false，采样旧BUTTON已断开。**未直接采样开层所捕获的trigger，不能推断它就是该BUTTON或BODY。** result SHA256 `2e938d1c5b306eb0638ab51f5ec8fd245ad2c4e76e7f5098f2337b1e199b5506`与cleanup `be6dcb2510ea9dec747e1354e37e4bacaf661674847d872b716049642b6b8be2`记录7资源/82所属进程/4实际adopted wait双清、输入不变。

上述浏览器执行由原作者/验收负责人组织，本角色仅核固定证据；合成pageshow不证明BFCache，业务红不冒充本卡独立最小组件红绿。问题是单层确认正常关闭时没有可表达“当前页面逻辑位置”的后备目标；仍保留App期Promise、View同宿主和checking隐藏纪律，不靠业务手工focus或更改检查流程修复。

## 2. 最小 API 与恢复生命周期

仅给 `UiDialog`新增可选 `fallbackFocus?: HTMLElement | null`，无默认目标；prop是调用方本地DOM引用，不是selector、元素ID、函数/任意回调或组件实例。UiDialog以响应式ref将该prop接入 `useLayer`新增末尾可选参数 `fallbackFocus?: Readonly<Ref<HTMLElement | null | undefined>>`，原五参数/trigger语义保持。**在关闭恢复时读取最新ref**，不能在setup/open/层record创建时复制其value；新open=true挂载时最初为null、挂载后成为标题的情况必须可用。

仍在remove移除自身记录、syncBackground后同步恢复，保留 `restore && wasTop`。不改变层数组顺序、注册/开启聚焦、背景inert/滚动锁、Tab/Escape/outside/action策略、过渡、事件或Popover的原trigger捕获。恢复只按下表执行：

| 关闭状态 | 规则 |
| --- | --- |
| 未提供fallbackFocus，或当前undefined/null | 全部既有行为保持；没有目标不是授权搜索document或安排稍后聚焦。 |
| 仍有modal | 完全沿已接受restoreWithinModal，最高剩余modal及其后层仍是唯一允许范围；不读取/使用页面fallback越界，也不改变原合法trigger与模态内fallback次序。 |
| 正常open从true变false，且无剩余modal、提供非空目标 | 先尝试当前合法且实际能接受焦点的原trigger；成功即结束，不能用fallback覆盖它。原trigger不可用或focus未实际成功时，才尝试关闭时读取的显式fallback。两者均不可用则不另选目标。 |
| onBeforeUnmount移除记录 | **不尝试新增fallback**；原trigger及有剩余modal的既有卸载恢复规则保持。不能在checking/真正离页/身份失效导致组件卸载时聚焦即将撤离的页面；正常关闭必须由open=false表达，不能依赖销毁组件来触发新能力。 |
| 非顶层移除或restore=false | 不主动恢复或尝试fallback，保持原门禁。 |

新分支复用已接受恢复专用合法性判断：同document、仍连接、具非零实际布局、可程序化聚焦，非disabled（含fieldset禁用）、不在inert/hidden/aria-hidden/display:none/visibility:hidden或collapse中；检查节点和祖先。BODY/HTML及无tabindex且不能聚焦的普通容器不是新fallback。当前页面标题已有tabindex=-1可合法返回，不动态补tabindex、清hidden/inert或强开内容。原trigger与fallback都调用 `focus({preventScroll:true})` 并核actual activeElement；只凭isConnected或focus无异常不算成功。新分支不改导出的focusable/Tab算法。

没有异步focus、nextTick/setTimeout/requestAnimationFrame、轮询或全局querySelector；调用方在所属View仍有效、当前完整身份/admin确认时提供本地ref，否则null，组件不持久保存/跨实例重建目标。UiDrawer、UiPopover、UiSearchDialog等不新增typed API或改源码，本卡不声称已给所有封装器交付fallback接口。原无参数Dialog/Drawer行为与正常关闭精确trigger优先仍须回归。

## 3. 五条候选路径与独立输入

| 路径 | 唯一范围 |
| --- | --- |
| `web/src/composables/useLayer.ts` | 新末尾可选ref、正常关闭与卸载fallback门禁、无剩余modal时的显式目标恢复；保留原其它分支。 |
| `web/src/components/ui/UiDialog.vue` | 可选fallbackFocus prop及最新ref传递；不改Teleport、Transition/CSS、close策略或事件。 |
| `web/src/tests/dialog-outside-focus.spec.ts` | 真实UiDialog新API/最新ref/正常关闭与卸载正反例，原Dialog/Drawer/Popover断言保持。 |
| `tests/account-captcha-web/e2e/dialog-outside-focus.spec.ts` | 既有最小组件harness添加单层确认与页面一起卸载/恢复、关闭回当前本地目标的真实红绿；原检查保持。 |
| `docs/development/frontend/components.md` | 仅浮层段记录UiDialog可选prop、正常关闭/最新ref、活trigger优先、卸载不fallback及模态边界。 |

复用只读 `tests/account-captcha-web/dialog-outside-focus.config.js`与870ebbb组件/锁/样式闭包，不新增fixture/config/依赖。账号安全App/controller/View/26源与生产dist不作本卡输入；除上述UiDialog/useLayer外的共享组件（含UiDrawer）、全局CSS、Vitest配置、旧组件setup与全部后端/迁移/台账/归档只读。实现者必读[Vue技能](../../../.agents/skills/agenteam-vue-development/SKILL.md)、[Vue测试技能](../../../.agents/skills/vue-testing-best-practices/SKILL.md)，独立验收读[验证技能](../../../.agents/skills/agenteam-verification/SKILL.md)及可用Playwright技能；超出五路径或改变旧默认语义先报主线程修卡。

## 4. 验收、业务接缝与移交

纯测挂真实组件、不stub useLayer：初始open=true且fallback最初null、挂载后ref就绪；open期间将旧后备替换为新本地节点，关闭只认最新ref；合法活trigger优先；trigger断开/禁用/隐藏/inert/body/拒绝focus时尝试合法后备；后备同样各类非法/跨document/无布局/实际focus拒绝时不全局兜底。非顶层移除、正常open=false与直接unmount严格区分；卸载即使给有效页面目标也不得调用新fallback。原restore=false门禁静态保持，不为测试暴露remove私有面。剩余modal存在时显式背景目标绝不能胜过已接受模态范围，原Popover/Drawer及无prop恢复不变。保留既有beforeEach布局spy，仅用于jsdom前提，不视为真实焦点证据。

真实Chromium使用同一最小组件harness在旧固定组件先取得目标明确的行为红，再叠候选五路径复跑原断言。只用本地open/页面挂载状态：页面触发单层确认→页面与Dialog共同卸载、旧触发节点断开→新页面恢复且Dialog初始open=true→关闭确认。新页面提供自己的已有tabindex=-1标题，正常close经open=false且Dialog实例保留；关闭后精确focus当前标题、旧节点仍断开，再Tab到当前可操作控件、无overlay/inert/滚动锁。harness不导入账号安全/Session，不声称已覆盖真实会话。不能靠断言前手动focus、任意document首控件、更改z-index或跳过恢复制造PASS。

覆盖Escape、action及允许的遮罩pointer；390/1440与normal/reduced-motion，合法原trigger精确优先、最新ref替换、禁用/隐藏/无目标不聚焦、直接销毁不fallback、剩余modal不逃逸与非顶层不抢焦点。旧outside先preventDefault、disabled outside/内部点击、Dialog/Drawer/Popover与正常Tab困陷保留；后续用户聚焦不得被迟到回调抢回。新API的基线行为红须来自真实关闭焦点断言，未知prop编译失败/装配失败不算复现。

在固定已提交闭包执行适用纯测、旧 `components.spec.ts`、type/build或 `npm run check --prefix web`；独立原输入固定后才组织浏览器。仍沿 `tests/account-captcha-web/node_modules/.bin/playwright test --config tests/account-captcha-web/dialog-outside-focus.config.js`，workers=1/retries=0、每test45秒，仅任务自有loopback Vite/Chromium并阻断其它origin，不需要PG/MinIO/SMTP/Docker。作者冻结源/闭包/锁/浏览器指纹、实际命令/原log/退出及首红，交未参与实现的verification_worker；server/browser/所属进程实际join、私有资源清零后交回。

账号安全rev2维持原27路径，仅在共享结果独立接受后，按其第9/12/22路径接入和复验：确认UiDialog驻**本View末尾**，移除外层 `v-if=confirmation.open`、内部overlay仍由open控制；关闭读当前View合法标题ref，App期Promise/草稿不迁移。checking卸载整个View禁止新fallback，恢复同身份重建新ref；新身份/失权不给旧目标。不得手工focus弥补共享层或放宽原activeElement/Tab断言。实际App/pageshow/失败恢复及待决导航/退出仍需该业务卡真实验收；独立组件PASS不能直接使账号安全通过。两卡产品与各自资源窗口均待主线程另授，SMTP规格不在本次同步范围。
