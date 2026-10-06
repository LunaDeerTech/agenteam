# D27：平台模型用途取消读取后的状态恢复

状态：rev1.1，2026-10-06 **已独立静审通过（STATIC PASS），主线程已采纳规格**；被审全文 SHA256 `4297222baa684534545e6d7ef906f12e3dc3ef84c7f750196877def2290f2edc`，技术 §1–4 SHA256 `f1ee727039ff8724ed83764e6149e6df052950e1a26500a36762c4817376fbe6` 保持。相对 rev1 仅澄清 §3 两个测试文件的共享所有权与后继菜单适配，其他技术保持。当前仅规格接受；§3 四路径产品实施、独立固定副本及动态资源窗口均待主线程另授。Account 安全候选及其固定 A/B 输入保持冻结，不因本卡取得其共享文件写权。实施者、私有输入和资源窗口由主线程另行下发。

## 1. 结果、固定输入与证据限制

完整结果：用户在平台模型用途复查恢复后、当前配置或保存引用仍在读取时明确放弃编辑，取消的读取应呈现可显式恢复的错误态，不能在请求实际结束后继续显示正在加载。保留完整已完成观察和保存的 ID；原 `canSave` 严格性正确且不改，不自动补读、重试或产生新写。此为 Selection controller 的局部取消状态缺口，不是 Account 配置或共享焦点修复。

实现与独立验收以已接受产品 `fd32120eba4c76f67b649248377f4825a78d5d79` 为固定基线。Selection 已接受 `870ebbb986f56bb62be34ccdfa2c77819ce995a6`；从该提交至 fd32120，Selection controller、View、Editor 及本卡三测试文件原字节相同。消费[原 Selection 卡 §2–5](d27-system-model-selection-ui.md#5-分域owner分页与确认生命周期)、已验原 API/Session 同 owner、cohost 与共享焦点能力；不消费未验 Account 26 源作为本修复的已验依赖。§2 不改变 HTTP、DTO、命令、权限、用途资格或后端事务契约。

独立冻结诊断根为 `/workspace/scratch/agenteam-selection-discard-diagnostic-0ikwbn_6`，`diagnosis.md` SHA256 `c83432dbf6190c1f4a79938c3bdc777a5c047ce3d6a63fcd2642041459f4d9df`。其输入使用固定 Account input04 App/Session 组合，但 Selection 三产品文件分别保持870字节：controller `10f37e124c7852a895272ac65b86dd919b359aba997e99e72f5804efec8feb67`、View `3276ec40d8468ee3cac1785c90f47d906b0b38a7a8848aeb451558a0f0e94683`、Editor `b27430f537ab2e59946cc7f9f843a1c2f5ae4afdcf19455becebb230c39113cd`。诊断中 Account 请求为0；这份混合输入只作已观察缺口证据，后续必须在上述已接受基线重新固定最小用例，不能把 Account 候选改称已接受。

| 证据 | 实际发现与不能外推的结论 |
| --- | --- |
| `logs/diagnostic02`，actual exit0 / 2.432s，两例 | 真实 App 公开操作触发 pageshow、同 Session 重挂，Memory native body pending 时“放弃修改”可操作；取消的实际尾部完成、reader 解锁、owner busy=false 后，memory/reranker/image 仍 loading，零自动续读/PUT。重开并清 image 时 changed=true、blocked/locked=false，保存因缺完整证据正确禁用；显式重读 Memory/Reranker 可恢复。等四引用全读完再放弃的对照可保存。raw SHA256 `cf2f16bf9d2b34cbd63928b19932c4d092fc34b7736e4c8a70ab02631b3b4488`。 |
| `logs/diagnostic04`，actual exit0 / 2.176s，一例 | 恢复的 selection-state native body pending 时公开放弃；实际尾部后 selection.phase=loading/value=null，配置按钮禁用，但刷新按钮可用；无续读/PUT，显式刷新后配置及四引用 ready。raw SHA256 `e5428eb86524ca6aedf7072da99109e6fa60e3e8146c1da7acfddc66c3736a0b`。 |
| 原真实 `oldselection01` 与后续 `oldselection02` | 主线程已核前者为 Save disabled timeout，但现场没有 refs/busy/请求序号分项，不能由受控复现倒填唯一归因。后者实际 Navigation PASS、保存前四 refs ready，说明该轮读取完成，不否定另一合法时序的缺口；两轮原件独立保留。 |

diagnostic02/04 是 jsdom、生产 App/controller/解析器和受控原生 ReadableStream 的离线诊断，非 Chromium/真实网络或旧失败现场；用例通过表示复现了缺口，不是产品 PASS。diagnostic01 的最小闭包导入前提失败、diagnostic03 的“编辑用途/配置用途”按钮名前提失败均保留，不改写为产品红。问题不是完全无法操作或 Cookie 死锁；显式重读/刷新原本可恢复，修复的是错误的持续 loading 表达及相应取消收尾。

## 2. 最小取消恢复契约

固定 `useSystemModelSelection.ts` 的 `discard` 同步推进 revision 并调用 `api.abandon()`；`readReferences` / `readCurrent` 捕获旧 revision 后正确拒绝晚发布，但前者在旧 catch 直接返回、末尾收尾也被跳过，后者同样不再把先前 loading 转错误。实际 owner join 后 `resume` 没有初始或候选待办，不会再读取。责任在当前 controller 的主动取消边界，不能放宽旧 continuation 的身份/代次校验。

1. 合法放弃导致本批读取退役时，在同一同步状态转换中结算当前观察中**没有有效读取代次可继续发布**的 loading。正在读取的 saved reference 和该批尚未派发的其余 saved references 都要收尾，不能只处理被中止的那个 Model。仅 `loading→error`，保留当前观察的 saved ID，value 为 null，使用明确“读取已取消/尚未完成，请在请求结束后明确重读”的局部信息。ready 的完整 Model+Provider、既有 error 和可选 null/empty 保持，不能回填更旧的详情来使草稿看似已核实。
2. 若退役的是 selection-state GET，当前 selection 仍 loading/value=null，则转 error、value 仍 null，提供已有“重新读取配置/刷新配置”入口；不把未知配置称为未配置或制造旧版本成功。未读到主观察时不伪造四项引用。若主观察已 ready，仅结算其未完成引用，保留正式 singleton id/version/configured。
3. 该转换只处理本次实际取消的当前批次，在同步边界捕获/退役其范围；不在旧 catch/finally 中无条件改全局 references，也不能把新读取、同 ID 的新观察或新身份的 loading 改错。真实离页、身份失效、失权、注销和 dispose 仍按原 invalidate 清理并结束待决确认；checking 卸载/同身份重挂仍沿原 attach/detach 复查行为，不能抢发额外恢复读。
4. 可见状态转 error 不代表底层 I/O 已 join。原 api.abandon、各 scope 的取消与 Cookie owner 分域不变；native fetch/read/cancel 和 finally 未实际结束时仍 busy/blocked，重读/刷新不可穿越旧 owner。旧结果无论随后成功、失败或迟到都不能恢复已退役候选、改新身份、触发剩余 Model/Provider GET 或产生 PUT。
5. 用户通过现有 `retryReference(purpose)` 或 `refresh()` 明确恢复。重读完成的严格 Model+Provider pair 才恢复对应 ready/draftDetails；共享同一 saved ModelID 的用途沿原合并观察，最大4+4与协议完整解析保持。不新增后台队列、定时重试、自动续发，也不通过把 initial 重置为 true 来绕过“取消后不续发”。普通继续编辑而未放弃的有效读继续按原流程完成；已经全部 ready 的放弃对照不被改成错误。
6. `canSave`、mandatory Embedding/Memory、可选 null、enabled/type/json_schema、完整四项 PUT、原 expected_version/key/body、lookup 仅观察和原 Execute 恢复规则保持。没有完整证据时保持禁用保存；明确重读或合法重新选择后才可新保存。此次收尾不抹去已确认 receipt、不把放弃解释为撤销服务器配置；`requiresReload` 与不确定写追踪的既有含义不变。

View 已有每项“重读详情”，Editor 已有同 saved ID 的“重读”按钮，主观察 error 已有重新读取入口，故不需要新 UI 控件、文案宿主或手动焦点。只修改 controller 的局部取消收尾；Session、API、App、View/Editor、UiDialog/useLayer 和路由均不是产品改动候选。

## 3. 四路径、独立输入与所有权

| # | 唯一候选路径 | 限定用途 |
| --- | --- | --- |
| 1 | `web/src/composables/useSystemModelSelection.ts` | 上节主动取消对 current/reference 的同步状态收尾，保持旧代次、owner、读取/写入准入。 |
| 2 | `web/src/tests/system-model-selection-state.spec.ts` | 实际 Session/API/controller 与 native stream 屏障，覆盖两类取消、实际 tail、显式恢复及旧晚结果隔离。 |
| 3 | `web/src/tests/system-model-selection.spec.ts` | 生产 App/router/controller 的公开 dirty确认→checking→同身份恢复→放弃、局部错误与显式重读/刷新组合。这里是 App 测试文件，不授权改 `App.vue`。 |
| 4 | `tests/account-captcha-web/e2e/system-model-selection.spec.ts` | 基于固定 fd32120 的旧五叶菜单 browser，用已有正式 fixture 控制稳定进入取消时序，保留最后保存、焦点和布局强断言。 |

四路径合成一个可独立接受的结果；前三路径的修复/纯验证及第四路径的真实验证均只以 fd32120 已提交依赖构建，第三、第四测试文件均保持原五叶菜单、两组与第九 return。旧 Go fixture/test、Playwright config/driver、package locks、后端/迁移、README/归档不在候选内；不为本次设计新增公共接口或 fixture 控制。

第三路径也是 Account 候选 #17，已有菜单5→6叶、分组2→3适配；第四路径另有两处菜单数量5→6适配，不能只把 browser 视为重叠。当前 Account input04/A-B 源与 dist 不改；本卡尚未授实施。主线程后续须先冻结该候选并明确这两个测试文件的唯一作者与私有输入，再移交 Selection 修复范围。两测试文件均从 fd32120 原五叶、两组版本取得，在任务自有私有目录形成精确四路径补丁，不覆盖工作区的 Account 六叶、三组候选，不执行分支/worktree/Git mutation。提交前由主线程从已验固定版本整合四路径，不能用未验 Account App/Session/菜单来替代本修复依赖。

Selection 修复独立接受后，Account 作者只重放原已授权增量：第三文件的菜单5→6叶与分组2→3、第四文件的两处菜单数量5→6。以新的已接受两测试文件为基底，保留新增取消恢复的全部强断言及原断言，重新固定 Account 组合输入并按主线程授权复验。发生同文件冲突交主线程处理，禁止用旧六叶、三组文件整体覆盖新修复；不因冻结副本分开便声称共享工作区可同时写同路径。真实资源窗口另行移交，不能与 Account 正在进行的 A/B 共用 fixture/端口/浏览器窗口。

必读[Vue 开发技能](../../../.agents/skills/agenteam-vue-development/SKILL.md)、[Vue 测试技能](../../../.agents/skills/vue-testing-best-practices/SKILL.md)、[验证技能](../../../.agents/skills/agenteam-verification/SKILL.md)及原 Selection 卡相关段落；浏览器用可用 Playwright 技能。不改变旧退出、共享焦点、未验 Account、SMTP 卡或停止的 Object/tools 原任务，Summary 待决保持。若确实需要第五路径、额外 fixture 能力或改变读取/写入语义，先报告主线程修卡。

## 4. 验收与冻结交付

| 验收层 | 必需结果 |
| --- | --- |
| 已接受输入首红 | 以 fd32120 正式工厂/六依赖 Session、固定 controller/View/Editor 复建最小纯用例，在未修 controller 上以正确终态断言取得 reference/current 两红，保留输入和原件；不使用 Account API 或立即 resolve 的替身绕过公开流程。既有 diagnostic02/04 可复用思路与事实，但不直接把混合候选结果算作已接受基线红绿。 |
| 状态/native tail | reference 的 Model GET 与后续 Provider GET 两阶段各设原生 read/cancel 屏障：放弃后未完成项转 error、ready/ID保持、零后续 GET/PUT；可见取消后实际 tail 未放行仍busy，放行后解除。selection-state pending 同样终结为可刷新 error/value=null。明确重读/刷新恢复完整观察；完成后放弃对照正常。旧迟到成功/错误、同 ID 新代次、身份改变、checking卸载与真实离页不污染新状态。 |
| 公开 App 行为 | 通过按钮打开 dirty确认，pageshow触发实际 Session controller checking；受控503后恢复相同完整身份，在两类读取 pending 时合法点击放弃。确认只结算一次、overlay退出/焦点沿旧契约，显示可恢复错误，保存缺证据仍禁用。使用现有按钮显式恢复后才可保存；无新 key/PUT/自动读，Account 请求0。另核继续编辑保留正常读取、当前失权/新 Session/待决导航清理。 |
| 真实浏览器/后端 | 生产构建与任务自有正式 Account/System Model 根，原五叶导航。复用现有 `hold-get` 对精确 Memory Provider GET 的至多2s受控响应及 `hold-facts` / `release-get`，在确认恢复后断言该读取确实尚未结束再放弃；提前结束是前提不成立，不能计为取消成功。前后正式保存引用/版本不因放弃改变，已完成项仍显示、未完成项显示重读错误。明确重读后 reopen/清可选 image，原严格保存及正式 receipt/数据库旁证成立；配置读取取消另由上述实际 App/native 屏障证明。 |

真实控制只延迟正式 GET 的响应，不替造 DTO、Session 成功或写 receipt。Go fixture 的2s到期/释放证明该受控服务器响应结束，不能代替 native reader/cancel 的实际 owner 尾部证据。浏览器既有 native 观察函数继续按真实请求/结束序号核零续发、零意外写；不记录 response body、Cookie/CSRF/key 或私有材料。常规 Navigation 中若合法取消了恢复读，按本契约验证可恢复错误并显式重读后继续；不能仅增加等待、跳过 pageshow/确认、批量重试或删除最终保存断言使其变绿。原五叶计数、return、cohost层序、Escape/遮罩/焦点、八布局与正常完整读对照保持。

作者先运行有意义的 targeted Vitest 首红/修后测试与相关旧测试，再按原锁执行前端 `npm run check --prefix web`。正式检查基于任务自有固定接受组合；不读正在运行的 Account 工作树构建。只导出需要的已提交闭包/配置，保留输入 SHA，避免全库/依赖重复复制。Go fixture 复用原 bytes；真实检查继续显式 Go1.27.1、readonly/off 依赖纪律。

资源获主线程独占移交后，使用原 `scripts/test-security.sh -run` 精确选择 `TestAccountSystemModelSelectionWebNavigationAndLayouts`，另以 `TestAccountSystemModelSelectionWebReadAndPagination` 和 `TestAccountSystemModelSelectionWebOutcomeRecovery` 回归显式恢复、取消晚读与放弃原写追踪。保留45s browser、2m顶层、6m包、worker1/retries0及既有内部预算；不足时按实际顶层分组报告，不延长或减少断言。其他领域未变证据复用，不机械重跑全部 D27。

作者冻结四路径，交已接受基线/候选 SHA、实际命令/退出/原日志、两类首红与修后结果、未验证限制；未参与实现的验证者独立核相同语义和公开流程。原 oldselection01、diagnostic01/03 前提红及 oldselection02 PASS 均分列保留，不回写历史归因。所有命令、原生尾部、owned进程和 exact-ID fixture 实际 join/双次清零后交回窗口，由主线程采纳提交，再进行 Account 六叶后继组合。本次仅规格，尚未运行修复或产品验收。
