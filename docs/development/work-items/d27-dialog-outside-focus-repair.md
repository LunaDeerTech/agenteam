# D27 前置修复：遮罩关闭后保持触发按钮焦点

状态：rev1，2026-10-06 四路径已由 `directory_backend` 完成、`recovery_verification` 独立 PASS，主线程采纳提交推送 `b53895f7eb1d020276e8f54a99a7c0821b286481`，远端一致。[正式报告与最小持久证据](../agent-team/dialog-outside-focus-repair-verification.md)保留旧组件真实红、新组件11例通过、独立一组两轮通过，以及作者旧轮两个 PPID1 Z 和独立首轮原 clean=false 的限制。本次仅更新页首，技术 §1–4 保持被审稿 SHA256 `787a893476b2f925ea1d855a9ee5616517252f3a0a8f1393ba8baf9d055fdb5e` 的原字节。接受范围仅为共享组件修复；系统用户目录 UI 须消费已接受版本并另行组合验收，当前未接受。

## 1. 已证问题与输入

[浮层规范](../../frontend-design/styles/components-and-interactions.md#5-浮层用途层级与焦点)及[公共组件接口](../frontend/components.md#浮层)已经要求模态关闭后恢复原触发位置。真实 UI `new02` 在390px宽度完成 Escape 关闭与恢复焦点后，遮罩 pointer 关闭虽移除了对话框，却未保持触发按钮焦点；原断言位于 `tests/account-captcha-web/e2e/system-user-directory.spec.ts:544`，必须保留，不能先手工 focus 再断言。

固定主树基线 `0f68d445df66bb05228fca3604618d4f2a7e6ffa`；相关已接受组件与 `3affc01` 相同：UiDialog SHA256 `95baa82c19853ac299f314b64cc12957f0f007265b2a08a872f840df24c221d0`，useLayer `48fb1cd2a063168f7ece5ba7f3df4e16183367b466f906f63a5c8384f23e03a6`，UiDrawer `e62cc686f6fe8c3981680c3c88ec4880effbb9e84aedf80a5aff9978939a2def`。`useLayer` 对 open 使用 `flush:'sync'`：关闭处理期间立即移除层、恢复背景和 trigger.focus；UiDialog 遮罩的 pointerdown 没有取消默认行为。Escape 已执行 preventDefault，关闭按钮在 click 阶段关闭，二者顺序不同。

独立 verification 的真实 Chromium 原生 DOM 对照已观察：未取消 pointerdown→同步恢复 trigger→微任务移除遮罩→兼容 mousedown 目标为 body→trigger失焦→最终body；先 preventDefault 后最终保持 trigger。该对照只证明事件机制，尚不是真实 Vue 组件修复验收。

原证据位于 `/workspace/scratch/agenteam-d27-directory-ui-verification-nrfghn71/`：`runs/new02/raw.log` SHA256 `ea421557434f5d918b9849297d6cbd208e4df76bfd3fd64a7dfb9918db255409`，原退出1/72.622s；其中 Read通过，Authority和Navigation各有失败，本卡只处理Navigation焦点。原候选input02为 `0a2a8e7628a4afade78700ee31c14aa84469d36fc3979bf8ecbecef6794d340b`。`runs/focus-mechanism02/events.json` SHA256 `d4e3bfa501955bfe0815f682a763db9ba1791f2f8f476c07a7f8ab05852ca989`，对照退出0/1.189s；原首次长TMPDIR启动失败亦保留。交付时由验证负责人归档这些原件，不将原生DOM对照写成组件或完整UI PASS。

## 2. 最小修复契约

UiDialog 为遮罩增加一个 PointerEvent handler，继续由模板 `.self` 限定 event.target 为 overlay 本身。handler 在 `layer.isTop()` 且 `closeOnOutside=true` 时，**先同步 `event.preventDefault()`，再沿既有 `close('outside')`** 更新 open 和发出 close；不增加 stopPropagation。不要把 `.prevent` 无条件放在所有 overlay 事件上，或放在 `.self` 之前而取消内部输入默认行为。

| 触发 | 保持或修复的行为 |
| --- | --- |
| 顶层、允许 outside、真正遮罩 pointerdown | 恰好一次 update:open=false 与 close(outside)，取消该事件默认焦点动作；完成整个真实 pointer/click 序列后，仍连接且可聚焦的原触发节点获得焦点。 |
| closeOnOutside=false | 保持打开，不发关闭事件；新增handler不取消该事件默认行为。此参数不改变既有Escape/action策略。 |
| 当前非顶层 | 不关闭该层、不发其关闭事件、不由该handler取消默认行为；isTop仍在原overlay处理时点判断，不改变文档capture处理其他层的既有顺序。 |
| panel内部按钮、输入、链接、文本 | `.self` 不命中；保留默认聚焦、编辑、选择和点击，不能被当作遮罩关闭。 |
| Escape、关闭按钮、footer action | 沿原路径、CloseReason、props门禁及同步焦点恢复；closeOnEscape=false仍拒绝Escape，action仍可关闭。 |

UiDrawer 通过既有 UiDialog 继承修复。保持共享 useLayer 的层栈、Tab/Shift+Tab、背景inert、滚动锁、卸载和过渡清理；不增加 setTimeout、延迟focus、after-leave补焦点或业务壳专用focus逻辑。当前触发节点不可用时仍沿既有行为，不新增焦点fallback策略。没有产品待定决定、API、权限、持久化或依赖升级。

## 3. 独立验证与 UI 组合

单元测试直接挂载真实 UiDialog/UiDrawer，走 DOM 事件和公开 props/emits；覆盖表内允许/禁用/非顶层/内部事件、一次关闭原因、Escape/action与嵌套层恢复。仅可沿既有测试方式补 jsdom 的可见性测量；每例真实卸载wrapper、还原全局mock，不让层栈或监听器泄漏。可取消 PointerEvent 的 defaultPrevented 是门禁证据；jsdom不能证明浏览器默认焦点修复，禁止把手工 blur/focus 模拟说成真实复现。

新增最小真实组件 harness，仅引入固定输入的 UiDialog/UiDrawer 及其直接组件、useLayer、types与三份已接受样式；不导入 App、router、SettingsShell、认证或活动系统页面。复用已有锁定 Vue/Vite、web的Vue插件和 `tests/account-captcha-web` 的 Playwright1.56.1，不改package/lock，不新增框架。浏览器spec可在任务私有临时目录生成挂载壳并程序化启动Vite；使用configFile=false、独立#app、明确源码根/单一Vue解析、loopback临时端口和nonce，阻断非自有origin请求。harness不调用账户API，不需要PG、MinIO、SMTP或Docker。

真实Chromium先在旧组件上用同一最小遮罩焦点断言复现一次红，再对仅替换UiDialog的候选执行原字节断言。覆盖 UiDialog/UiDrawer、390/1440宽度、normal/reduced-motion：打开后焦点进入层，真实可达遮罩坐标的完整pointer/click序列结束后层关闭、触发按钮focused、背景inert与滚动锁恢复；随后用户主动点另一输入应保持新焦点，不能被迟到补焦点抢回。补内部输入可点击编辑、disabled outside不关闭、Escape及action恢复、两层模态只关闭当前顶层并回到下层触发节点。非顶层overlay阴性如需合成事件，明确只是门禁覆盖，不冒充被上层遮挡节点的真实命中。

候选只读固定输入必须排除活动UI二十路径；针对已接受web输入叠加本修复运行组件相关单测、type-check和build，记录输入与锁摘要。可在隔离的有界web源码副本上执行 `npm run check --prefix web`，不得用活动UI造成的通过/失败判断本基础修复；不机械复制后端或全仓库。实际组件harness运行 `tests/account-captcha-web/node_modules/.bin/playwright test --config tests/account-captcha-web/dialog-outside-focus.config.js`，配置限定该新spec，worker=1、retries=0、每test45s；显式任务私有目录/Chromium路径，记录真实版本及SHA、精确命令/exit/事件与必要截图。结束await server关闭、browser/context和所属进程实际退出，清理只处理自有路径与端口。

未参与实现的验证者对固定四路径与必要组件依赖独立核关键真实pointer焦点和门禁。基础结果接受后，主线程将该已接受组件加入当前UI冻结输入，再沿原 `TestAccountSystemUserDirectoryWebNavigationAndLayouts` / 原navigation浏览器用例复验390px遮罩、Escape、内部导航及布局；不改第544行期望，不将独立harness通过直接算作UI组合通过。UI其他失败和表格换行仍由原卡负责人处理，00019邀请索引不进入这次组合。

## 4. 精确候选路径与交付

| 路径 | 唯一范围 |
| --- | --- |
| `web/src/components/ui/UiDialog.vue` | §2专用遮罩handler及模板接线。 |
| `web/src/tests/dialog-outside-focus.spec.ts`（新） | 真实组件的事件门禁/公开行为与兼容单测。 |
| `tests/account-captcha-web/dialog-outside-focus.config.js`（新） | 独立、短预算、单worker、零重试的组件浏览器配置。 |
| `tests/account-captcha-web/e2e/dialog-outside-focus.spec.ts`（新） | 最小Vue挂载壳/自有server生命周期与真实pointer回归；临时壳仅生成于任务私有目录。 |

useLayer、UiDrawer、SettingsShell、样式、旧UI测试/fixture及所有锁文件仅作固定依赖，不授写权。需要新增其他路径或改变层栈/关闭策略时先报告主线程；不能因本卡把共享浮层全面重构。实施者读取[Vue开发技能](../../../.agents/skills/agenteam-vue-development/SKILL.md)、[Vue测试技能](../../../.agents/skills/vue-testing-best-practices/SKILL.md)及可用Playwright技能；独立验证读取[验证技能](../../../.agents/skills/agenteam-verification/SKILL.md)。

作者冻结四路径及闭包/锁指纹、保留旧组件真实红和全部原始失败，交独立核验；原new02证据由其原负责人保管并归档。Git由主线程执行：先交付共享组件完整小结果，再明确UI消费的已接受版本和组合结论。设计阶段只检查规格来源、链接、格式与路径，不启动浏览器或占用仍在清理的new02资源。
