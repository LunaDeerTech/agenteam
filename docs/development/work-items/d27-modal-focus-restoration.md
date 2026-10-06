# D27 前置修复：关闭顶层后把焦点留在剩余模态内

状态：rev1，2026-10-06 已获 `recovery_documentation`（verification_worker）独立静审通过（STATIC PASS），主线程已采纳；被审稿 SHA256 `d1c4d05cebe3cfd823250896150479002a83ed5d1f9fcf3013ecd012deaa1b43`。主线程已确定 `directory_backend`（改任frontend_worker）为§3三条产品路径的唯一作者，并授予独占组件浏览器窗口；实际开工仍等待主线程正式下发，本次页首更新不自行启动产品或资源。邀请 input02 的21源/dist已冻结、作者停止且无所属真实资源；[邀请 UI rev4](d27-system-invitation-ui.md)的22路径与业务真实窗口继续暂停，本卡不消费未验邀请源码作为组件实现/验收依赖。共享修复独立接受后，再由主线程另授邀请页组合返修与验证。本次仅更新页首采纳/调度状态，技术§1–4保持被审原字节；下文设计冻结时的未授叙述以本页首为准，规格采纳及实施安排不代表产品通过。

## 1. 责任边界与固定证据

[浮层样式契约](../../frontend-design/styles/components-and-interactions.md#5-浮层用途层级与焦点)与[公共组件契约](../frontend/components.md#浮层)要求模态限制焦点、关闭恢复触发位置。本结果仅补齐“顶层关闭、下层模态仍存在、原触发节点已不可用或位于模态外”时的恢复规则。邀请页自身的确认层挂载顺序由邀请 rev4 同页宿主修复；本卡不引入稳定层ID、业务层优先级、z-index调节或新的Dialog props。

固定已提交组件输入为 `6be5321c51f24108d886a139c7a31bb0aab92db3`，包含[遮罩 pointer 焦点修复](d27-dialog-outside-focus-repair.md) `b53895f`。`web/src/composables/useLayer.ts` SHA256 `48fb1cd2a063168f7ece5ba7f3df4e16183367b466f906f63a5c8384f23e03a6`；UiDialog为 `65ddffc60af0494f600cc0eae318576585e4e0093085e4b8b2d6a59d071f5065`。现 `remove()` 在移除记录、syncBackground后，仅以 `restore && wasTop && trigger.isConnected` 调用focus；没有检查trigger是否仍位于可交互的剩余模态范围。新挂载的一组模态可共同捕获body、背景按钮或即将卸载的节点，因此连接性不足以保证安全恢复。该缺口由固定源码静态确认；本卡尚无独立组件浏览器复现或候选通过声明。

邀请既有失败只用于说明触发来源，不能混为本卡真实浏览器证据：

- input01 已有浏览器失败是确认逻辑顶层与预挂Teleport锚点的视觉顺序不一致；input02 的App条件挂载处理了首次打开，但未处理确认存活时下层重挂。
- 冻结 `/workspace/scratch/agenteam-d27-invitations-frontend/input02.json` SHA256 `1cc558fc266f3e315f76e73a8a32321e43419410a5c6ddaac057483774173a67`，21源位于同根 `input02-source/`，均已逐SHA核对。独立 `pure06` 挂真实生产App/router/controller及受控HTTP transport，在jsdom中将visibility设为visible并**合成dispatch pageshow**：创建dirty表单→打开放弃确认→Session GET保持checking→同Session返回；恢复后overlay=2、confirmationLast=false、aria-hidden=true、inert=true、newIntentPosts=0。
- 原日志 `/workspace/scratch/agenteam-invitations-ui-verification-pVHEcPxW/runs/pure06/raw.log` SHA256 `fe0ef474f1b9a141a0cca0678c3b95adfb76e29dbb29f99e62f6bb2eef94303b`，对应冻结probe在该run的 `source/probes/app-recovery.spec.ts`。这是纯DOM/层状态失败，未证明真实浏览器pageshow、pointer命中或关闭焦点；原红由原verification负责人保留，不改写成候选PASS。

同页宿主能够保证创建/撤销/重试层先于其确认层注册和呈现，但重挂后原trigger依然可能不属于新模态。两项责任分别验收：共享修复不解决业务层错误注册顺序，邀请页也不能手工focus补偿本通用缺口。没有新的产品待定、后端/迁移或外部服务依赖；Object/tools原停止任务保持停止。

## 2. 最小恢复契约

仍在原 `remove()` 移除记录、syncBackground之后同步处理恢复；严格保留 `restore && wasTop` 门禁。非顶层移除不主动抢焦点，`restore=false`不恢复。层数组顺序、push时机、开启时focus、Tab/Shift+Tab、Escape/outside/action关闭策略、背景inert/滚动锁、过渡与卸载管理均不改变。

| 剩余状态 | 恢复规则 |
| --- | --- |
| 没有剩余modal | 原规则保持：可恢复且原trigger仍连接时按原路径focus，不为无模态页面增加fallback或新焦点策略。 |
| 仍有modal，原trigger当前合法 | 以剩余数组中**最后一个modal及其之后的层**作为允许范围；trigger必须位于该范围某个当前可用panel内，且连接、可聚焦、非disabled、不在inert/hidden/aria-hidden祖先中，并实际可见，才恢复它。较低模态虽仍连接但已inert，不属于合法位置；body或背景节点也不合法。 |
| 仍有modal，原trigger不合法 | 从允许范围的最高层向下找当前连接、可见且非inert/hidden的panel；在该层优先恢复第一个当前合法focusable，否则恢复可程序化聚焦的panel。该层无合法目标才检查范围内下一层，绝不越过最高modal向背景或更低模态回退。 |
| 允许范围暂时没有合法目标 | 不focus body、背景或失效trigger；不伪造tabindex、强开内容或清inert。保持原层自身随后挂载的聚焦流程，不加轮询、延迟focus或重挂循环。 |

合法性针对恢复时点判断：不能只信open、isConnected或旧触发记录。可见性须排除hidden/display:none/visibility:hidden或collapse、无布局区域等情况，disabled包含原生控件当前禁用状态；检查候选本身和祖先的inert/hidden/aria-hidden。panel自身已有tabindex=-1可作为回退目标。通过恢复专用的窄辅助判断复用现有focusable结果并过滤，不能借此重写导出的全局focusable或Tab算法。恢复沿原 `focus({preventScroll:true})` 同步执行，不添加nextTick/setTimeout/requestAnimationFrame、document全局选择器或业务目标回调。

UiDialog/UiDrawer props、CloseReason/emits、Teleport和Transition/CSS保持原字节，已验outside先preventDefault规则保持。UiPopover的正常anchor关闭恢复和无modal行为不变；模态内的Popover关闭须回到仍合法的原anchor。测试必须覆盖被移除的是顶层非模态、但下方仍有modal的情况，不能只按“被关闭的是Dialog”决定门禁。

## 3. 三条候选路径与独立输入

| 路径 | 唯一范围 |
| --- | --- |
| `web/src/composables/useLayer.ts` | 仅remove的模态内恢复选择与必要私有合法性辅助；不新增公共参数、注册身份或改其他层行为。 |
| `web/src/tests/dialog-outside-focus.spec.ts` | 保留原事件/关闭/嵌套断言，追加真实组件生命周期与恢复目标正反例。 |
| `tests/account-captcha-web/e2e/dialog-outside-focus.spec.ts` | 在既有最小组件harness追加初始双层/共同卸载重挂及真实关闭焦点；必要的已提交UiPopover闭包只能在本文件内声明和装配。 |

复用只读 `tests/account-captcha-web/dialog-outside-focus.config.js`，其SHA256 `42516eb6ed5db78278a3169818a67e579e94a8b8671e067ade97830540adafd5`；workers=1、retries=0、每test45秒及原私有目录/nonce/server清理协议保持。UiDialog/UiDrawer/UiPopover等直接依赖、三份样式、package/lock及组件文档只读。新增场景可扩同一harness的受控挂载分支和已有依赖解析，不新增文件、框架或锁漂移；不修改邀请21源、App、router、生产后端、全局样式和归档路径。

实施使用从固定已提交组件/测试及锁构建的最小隔离闭包，只叠本卡三路径；不得因主树含未验邀请改动直接拿整棵活动web判本卡通过。必读[Vue开发技能](../../../.agents/skills/agenteam-vue-development/SKILL.md)、[Vue测试技能](../../../.agents/skills/vue-testing-best-practices/SKILL.md)，独立验收读[验证技能](../../../.agents/skills/agenteam-verification/SKILL.md)并按环境使用Playwright技能。缺资源、需改范围或公共API时交主线程裁定，不用业务补丁替代。

## 4. 验收与移交

纯测试挂真实UiDialog/UiDrawer和适用UiPopover，不stub useLayer。用确定性挂载/卸载重现同时打开的父/确认层捕获body或旧节点，再关闭顶层；验证焦点落在剩余允许层内、背景仍inert/滚动仍锁定，最终全部关闭后恢复原状态。覆盖有效原trigger优先、已断开、背景/body、禁用、隐藏、inert较低模态、最高层无focusable但panel可聚焦、没有合法目标、非顶层卸载及无modal兼容。jsdom可以沿既有方式补可见性测量，但不能把它当真实浏览器默认聚焦；测试走公开props/DOM，不镜像层数组内部实现，每例实际卸载并恢复mock。

真实Chromium先在旧固定useLayer上以同一最小断言复现“关闭确认后焦点未在剩余模态内”的红，再只替换候选useLayer重跑原断言。harness仅保留本地响应式open/挂载状态，按确定性控制使两个Dialog共同卸载、原触发节点断开、然后同时恢复为open；它不导入邀请App/Session、不声称等价于真实会话验收。重挂后确认必须本来就处于正确顶层；不得让错误宿主顺序制造另一失败或以强制focus伪造预期。

以真实Escape、action和可达遮罩pointer/click关闭确认后，验证activeElement确在仍打开的合法下层内，继续Tab/Shift+Tab不逃出，底层背景保持inert；有效原trigger仍精确恢复，无效trigger走fallback。覆盖Dialog/Drawer、390/1440、normal/reduced-motion、后续用户主动聚焦不被迟到回调抢回。保留旧outside/disabled outside/内部点击、正常嵌套和最终关闭恢复场景；Popover在modal内/无modal的原anchor路径补适用真实回归。阴性非顶层事件如使用合成dispatch，单列为门禁证据，不冒充真实命中。不能在断言前手工focus、提高z-index、跳过关闭或延长原预算。

在固定已提交web隔离输入执行适用单测、type-check/build或 `npm run check --prefix web`，记录实际输入/锁SHA和命令；不将编译或纯测冒充浏览器通过。真实执行沿 `tests/account-captcha-web/node_modules/.bin/playwright test --config tests/account-captcha-web/dialog-outside-focus.config.js`，可按新增与既有相关test精确分组；仅任务自有loopback临时Vite和Chromium，阻断非自有origin。无需PG/MinIO/SMTP/Docker、账户API或外部网络。

产品写入与浏览器窗口由主线程在规格独立采纳后另授，当前不启动。作者冻结三路径、闭包/锁/浏览器指纹，保留旧红和所有新失败；未参与实现的verification_worker独立审查门禁及真实重挂/关闭焦点。server/browser/context/所属进程实际join、私有资源清零后交回，再由主线程接受并提交共享结果。之后邀请rev4固定消费该已接受提交，补其App pageshow/检查失败再恢复、导航确认与真实焦点组合；组件PASS不等于邀请UI通过。原证据由既有负责人归档，本卡不另占当前归档路径。
