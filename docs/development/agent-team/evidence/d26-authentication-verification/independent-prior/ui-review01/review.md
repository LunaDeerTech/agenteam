# D26 UI-review01 独立静审

**BLOCKED：U1–U4 需要窄修。** 本轮仅静态审查，没有执行 npm、产品 JavaScript、浏览器、Go、Docker 或网络，未读活动 fixture4/修复源。core02 已审结论及两个独立 pure 通过继续有效；它们不能代替下面的完整页面/路由接缝。

固定基线 `457b1979c9d6563740543b2011eedc06cce34c71`，UI manifest `/workspace/agenteam-d26-auth-author-jrfhr6h1/evidence/ui-review-01/manifest.json` SHA `6e08def62ab40b7d4f6916467eeab1e3c425c07c05ebc30c7fe519555d7c669b`，8 production + 2 tests；依赖 core02 manifest `0bd7611e421549ef4d13a148b50e04359b3b53845fabfb6a6b597fdddf74eece`。本文所有行号都指上述固定副本。作者收到早报后正在修复，不影响原副本结论。

## 必要修复与最小反例

| 项 | 确定路径 | 验收建议 |
| --- | --- | --- |
| U1：明确放弃/上下文失效后仍保留旧表单秘密 | `LoginView.vue:36,58` 只在已认证/卸载清 password；`:145` 直接调用 auth.restart，CSRF_FAILED 导致 unavailable 也只是通过 showForm 隐藏 DOM，email/password refs 不清。Unknown 后明确放弃再 GET401/bootstrap，旧密码可再次显示并进入新意图。 | 卡 §4 要求明确放弃/失效清表单及不再需要引用；普通明确凭据错误仍应保留。现 authentication.spec 增加 Unknown→放弃→新上下文及无 pending 的 CSRF_FAILED 两例，验证旧密码不会重新出现/被新请求偷用。不要让程序性清表单意外触发 inputChanged、破坏尚需保留的 Unknown/错误原意图；按实际已明确放弃/失效边界处理。 |
| U2：挑战失效后焦点没有返回创建动作 | `LoginView.vue:22–29` 仅 passReady 成功聚焦登录，`:51–54` 只覆盖手工关闭。core02 verify 拒绝/到期/transport 失败或30s未确认会将 challenge 置 null，`:106–113` 卸载原聚焦 range/验证按钮，但没有失效焦点接续。 | 卡 §5 明确“关闭/失效回创建挑战按钮”。用真实 controller + mounted page，分别拒绝 verify 和超时，要求创建按钮实际得到焦点；保留成功→登录、手动关闭→创建，同时不能在用户编辑输入时无故抢焦点。可以手动聚焦一个已 enabled 的控件来隔离本反例，但那不能替代 U4 的展开焦点验证。 |
| U3：离开后在旧 Cookie 尾部结束前返回，可永久 checking | pending login 时离开认证路由，`router/auth.ts:21`/LoginView 卸载调用 leave，core 递增 generation、phase=checking、保留 actual owner。旧尾部未结束前返回 `/login`，guard `:12` 调 restore；core run 对不同 kind 的现存 owner 立即返回 resolved，guard放行。旧 login 实返只清 busy，不发布身份；LoginView 的 checking 只有 loading，恢复按钮限 uncertain/unavailable，App watcher也不处理 checking。 | 使用真实 router/controller 的 deferred login，经 History/已注册非认证或 NotFound 路由离开并在 held 时返回。释放旧尾部后应有明确可用的一次 Session 检查入口/接续，不能永久 loading；点击检查才做一次 GET 同样可接受，不要求自动重试或轮询。我的独立 pure 在旧尾部结束后显式调用 restore 才恢复，不能把该 pure 当本 UI 缺口已闭合。保持四 Cookie 串行与旧代不发布，不为修 UI 提前清 owner。 |
| U4：挑战首次挂载 busy/disabled 时焦点丢失 | `RotateChallenge.vue:57–68` 在 mounted 无条件 range.focus，`:100` busy 时原生 range disabled；`:70–78` 只观察 challenge.id。core02 runChallenge 先 apply(result) 发布题，再在 Promise finally 清 challengeBusy；Vue 可先挂载 disabled range。disabled input 的 focus 不生效，随后启用不会自动补焦点。 | 卡 §5 展开必须聚焦说明/键盘控件。需在当前题目标实际 enabled 后进行受控焦点交接，旧题/卸载的异步回调不得聚焦新题，也不任意抢用户焦点。应保留不手动 focus 的真实 controller/page 展开反例。主线程转述作者已在 U2 首轮实际遇到此“前置”失败；本轮未读活动原日志，不据转述冒称独立动态重现。它是独立产品缺口，不能只在测试里加 focus 后忽略。 |

以上仅要求已授权 UI/现测试内的必要修复；若执行者发现必须改 core 接缝，应先交主线程确认，验证者不改业务。

## 其余已核与原失败链

- 独立 Login 页面使用既有 UiField/Input/Button，无已登录 AppShell；密码保持原字符串、可粘贴、无 maxlength，Unknown readonly/提交防重入，错误是文本和 alert。正式目标只有 `/`，return 无法导出任意 URL。首页只有标题、Dashboard 空容器和初始密码建议，无未来设置/项目链接。
- Home/login guard 实际调用 Session restore；受保护 RouterView 仅 authenticated 渲染，重查时清旧正文。AppShell 只透传原 SystemNav slot，当前路由没有虚构导航项。Debug 路由/动态 import 仍受 import.meta.env.DEV 条件控制，非认证路由不请求认证。生产 dist 是否确实无 Debug 仍需冻结产物/浏览器验收。
- 正式 GoCaptcha 2.0.7 的 Rotate data/events/config 与 wrapper 匹配：真实题图来自 typed DTO，官方 confirm 与原生 range 共用有界整数 verify，keyboardOrigin 不随拖动 rotate callback 回写而改变拖动起点；busy 阻止动作，题 ID 重置角度。range 支持原生方向键/Home/End、Enter；存在视觉判断限制的可见说明，不伪称非视觉替代。当前 unit 直接调用官方 callback，实际拖动几何与真实 pass 消费仍待浏览器。
- 页/组件使用现 tokens、最小宽度与折行、range宽度随容器调整、服务端 theme 由已验核心发布。两新页 mounted 聚焦标题，挑战成功/手工关闭有现行焦点路径，但 U2/U4 明确保留。390px、200%缩放后的导航及按钮可达、浅深主题、ResizeObserver/官方 CSS 和 reduced motion 仍只能由真实浏览器证明，不能从 CSS 或 jsdom宣布通过。
- Vite 开发代理只由显式 AGENTEAM_DEV_API_TARGET 启用，限制 loopback HTTP/端口/无用户信息/根路径，不向构建注入目标；保留原127.0.0.1:5173/strictPort，changeOrigin:false，无额外 CORS、Cookie rewrite 或身份头。代码未发现 Host/Origin 改写；真实转发与PUBLIC_ORIGIN匹配仍待正式同源fixture，未读取活动fixture实现。

原 `web-check-01/manifest.json` SHA `19906aa324b24d5b2a49f84ec0902a33f73d0320c94d5cff00e40bf9d26edf31` 的17项全部匹配；本轮 UI 前后差量在 `loop-to-ui01.patch`。App/Login 新增 navigating try/finally，覆盖 replace过程中 guard再次restore引起phase变化的重入；authentication 回归新增 GET 精确3次（首次/登录确认/保护导航）和注销后4次，未仅以“不再挂起”作为证据。另 bootstrap >1 是现有断言，不能改写成该项精确计数。

失败历史保留：web-check-01 实际 exit143，原17 manifest记有界人工 SIGTERM；原raw在终止前已报告 styles 1 FAIL 和 rotate 1 FAIL，没有完整失败栈/总终态。styles.spec 的既有文档依赖确是 `docs/frontend-design/styles/colors-and-themes.md`，作者后补固定基线文档，不能把该部分归为新产品通过。之后 ui-unit-02 仍 exit1（1 FAIL/5 PASS）：关闭按钮的全文等值选择器没有考虑 UiButton 固有的隐藏 loading/success 文案；改 includes 后保持关闭 emit/无 verify 断言。以上均不删除或伪称原首轮全绿。

有效作者通过证据：web-check-02 实际完整 format check、6文件51 tests、vue-tsc、Vite production build exit0；另 ui-navigation-03 实际4 tests PASS，覆盖上述最终精确请求计数。两者作为 manifest 关联的作者证据复用，不是我的执行，也不覆盖 U1–U4。npm 的升级 notice 不是依赖升级，本候选锁仍同 core02。

## 实际静态检查与限制

可复跑命令为 `python3 /tmp/agenteam-d26-ui-review01-v-vgj2lp5h/verify_inputs.py`。检查 35 个 UI/原17/作者raw payload、7个core02条目、10个固定 Git 选读物件、5个已锁 GoCaptcha 文件，并记录 SHA；无产品运行。脚本/实际结果见 checks.json、check-command.json，定位过程只用只读文本/固定Git。几次候选 SDK 路径未命中后按 package exports 定位 `.es.js`，不是测试失败。

本报告只冻结私有产物，原件、业务、仓库、既有归档均未改，没有 Git 写操作。修复按新冻结 delta 窄复审，随后独立正式浏览器窗口另授；完整 D26/D28、Provider、上游 Object/Artifact 阻断均不在本轮通过范围。报告冻结后 all-stop。
