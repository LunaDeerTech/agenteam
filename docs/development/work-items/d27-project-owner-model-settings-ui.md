# D27 Project Owner 模型设置 UI — rev1＋菜单兼容补充

状态（2026-10-09）：**25 个前端文件已提交，整卡验收未完成。** 当前范围为 31 路径（30 技术＋README），17 HTTP operations、9 IPC、6 个新 top 与 14 个旧回归不变；原 #1–29 编号保留，#30–31 是旧 Audit 单元／真实浏览器菜单期待兼容修正。四个 Go/浏览器 harness 与必要恢复输入已重建；当前恢复验收 recovery 第七轮、read 第二轮、configuration 第三轮与 credential 首轮实际完整通过（4/6 新 top，含完整资源终态）。原 recovery 六次、read 首轮与 configuration 前两轮失败保留。其余两个新 top、旧回归余下 10 项和独立 B 尚未完成；独立 A 第二轮已完整通过；旧 Owner 三项及 Audit 权限／恢复一项已完整通过，已恢复或注册的其他源码不代表真实场景通过。

本卡保存产品规格、验收场景与恢复所需事实；团队调度、稳定输入、证据留存和 Git 交付统一遵循[团队流程](../agent-team/README.md)。旧逐轮 root grant、重复哈希表、README 最后另授和永久归档步骤不再作为日常流程。历史全文可从 `e55ad7d1` 的本卡及当时[任务台账](../agent-team/tasks.md)、[环境交接](../agent-team/recovery-2026-10-08-environment.md)文件历史定位，不改写原失败或未验证范围。

## 0. 固定依据、当前基线与交接状态

当前从 `ai/product-continuation` 接续，已合入保存完整 Model 输入的 `da4953f5`；总体边界见[当前检查点](../../../.agent-state/current.md)。正式规格安装于 `7b8af244`，已接受的 rev1 语义完整保存在 §2–10；唯一精确端点附件为 [d27-project-owner-model-settings-ui-endpoints.json](d27-project-owner-model-settings-ui-endpoints.json)。附件来源状态不表示当前实施状态；本卡消费其 operation/method/path/target/query/effects 闭集，不建立第二套协议。

| 已完成范围 | 可复用事实与限制 |
| --- | --- |
| API / Session 状态 / 页面 | 分别由 `b0ee596d`、`17589540`、`1133152f` 提交；§8 的 25 个 web 路径当前均存在。API 独立 37 场景、state 独立 13 场景及后续限定修复组合已接受，不等于真实浏览器整卡通过。 |
| 前端离线组合 | 历史完整单元 2626 PASS / 2 FAIL 为旧 Audit 菜单期待；#30 一行兼容后该文件 49/49 PASS。格式、类型、私有 build 及独立导航／焦点修复按各自版本组合接受，保留原失败，不宣称当前 HEAD 一次全量重跑。 |
| 真实配置与凭据路径 | `modelsconfig03`、`modelscred01` 历史 actual PASS / fullSTOP；前两次配置 FAIL、Problem.instance 净化路径修复及其有限证据保留。 |
| 恢复路径 | `modelsrecover01`、`modelsrecover02` 均 FAIL；后者缺最终 browser-result 与 durable facts，原 75s host TCP 观察未双清。后续有限窗口释放不能补写原 TCP 通过。 |
| 当前恢复验收 | recovery 第七轮、read 第二轮、configuration 第三轮、credential 首轮完整通过，原各次失败保留。sharedLayer 修复及真实组件已按 §0.2 接受；受影响业务场景仍待补验，不能将历史 PASS 直接写成新资产通过。 |
| 未完成门槛 | authority / navigation 两个新 top、旧 14 回归中的余下 10 项及独立 B 尚未完成；README 尚无本卡整体验收结论。D27、生产 Resolution / Invocation / D24 及 E01 不因前端提交完成。 |

两次恢复失败后的受控结论仅用于后续修复：正式 header/Flush 后零 body 断连已在受控 native fetch 比较中验证；旧分支一次 fetch 可能透明发出两次 POST 并读到完整 EOF。ReadPrivate 的 Lstat→Open→SameFile 与 JS atomic rename 存在源码可确定竞态；候选改为先 NOFOLLOW|NONBLOCK 打开，再以同一 fd 验证 regular/0600/size，保留原有界读取、清理与 Close，pure-file 受控 12 叶／17 RUN/PASS 已接受。原恢复轮未采得具体 error 类别，以上不能回填两次 business FAIL 的确切因果。早期受控 browser launch 失败及 TMP 后续清理同样保留，短根成功不证明原 launch 失败原因。

**实际恢复输入：** 以下未验输入位于活动 `ai/product-continuation` 分支，未随共享组件修复交付 `main`。§8 #24–27 四个 harness 和 `.agent-state/model-ui-recovery/` 的六场景模块、同 body 校验、资源 driver、必要脱敏失败输入，以及 `.agent-state/model-ui-regression/` 两个旧回归 helper 已从 Git 恢复。主线程已重建 `/workspace/agenteam-delivery`，固定正式 `11c16867` 与最后迁移 `00022`，并复制当前两份未完成整卡验收的 Model Go 测试源；两份测试源已在该固定基线上重新 race 编译，不能沿用旧 `dbf` 编译证据。`output/ai/model-ui-recovery/` 私有 binary/helpers、原生 client probe、前端资产及固定 MinIO binary 均已从可恢复源码重建；Go 1.27.1、固定 Chromium 151.0.7922.173 及正式 SHA 已核实，锁定 web／浏览器依赖已恢复。Docker 当前两容器属于既有 `agenteam-dev-infra`，不作为测试资源、不连接或清理；真实测试须另外创建并登记 owned fixture。

下一步由本任务负责人组织：

1. 从已恢复源码重建锁定依赖、隔离 Go binary/helpers、固定 MinIO、原生 client probe 与私有前端资产；核对固定镜像、资源归属与当前后端迁移闭包，不使用历史 scratch 路径代替实际文件。
2. 以当前 Git 基线、限定 diff 和停止写入范围交审，完成受影响的格式、类型、编译、精确 selector discovery 与安全输入检查。原始日志放 `output/ai/<task>/`；涉及 Unix socket 时使用有界短外部目录，恢复前核实际工具、资产和资源所有者。
3. 保持 §9 场景和预算，先验证已有 sharedLayer 修复，再继续 authority / navigation、必要旧回归和独立 A/B。已有结果仅在相关输入／依赖可确认未变时复用；受新共享层行为影响的历史场景须补验。高风险恢复与权限场景须由未参与实现者独立验证。
4. 负责人整合实现、必要测试、README 和简短台账，由主线程一次交付完整结果。不得以编译、受控 helper PASS、静态准备或旧实例 ACK 替代真实整卡验收。

本次恢复后的 authority 第三轮仍 FAIL：已通过原共享遮罩阻挡点及归档后的配置原请求重放，随后在切换到凭据恢复 Project 时等待新的 Provider 列表读取超时；停在 `authority-archived-credential`，没有完成凭据归档场景。闭合脱敏诊断保存在 [authority-credential-navigation-failure.json](../../../.agent-state/model-ui-recovery/authority-credential-navigation-failure.json)，尚不能仅据超时确定产品或 harness 原因。Go 24.57 秒，外层实际 exit=1／110.53 秒；direct child 与四个 adopted child 均实际 wait，watchdog／observer join、七个资源双 absent、子进程空、TCP 双空及输入未变均已核实。本轮失败不回填前两轮因果，也不改变 4/6 边界。

navigation 第三轮同样 FAIL：本轮 Session 已观察到 headers 与 finished、没有 failed event，随后完成 Model 创建与 Models 列表；切到可用模型叶时超时，fixture 另报安全代理终态不完整。闭合脱敏诊断见 [navigation-directory-failure.json](../../../.agent-state/model-ui-recovery/navigation-directory-failure.json)。Go 16.30 秒，外层实际 exit=1／100.69 秒；direct/four adopted 实际 wait、watchdog/observer join、七资源双 absent、进程空、TCP 双空及输入未变全部完成。静态检查发现 navigation 复用的 System 草稿种子名称为 `Owner draft memory*`，与 Model 安全目录准入的 `Models ` 前缀不一致；待纯正反例与受影响真实场景验证，修复限定测试种子，不扩响应白名单。没有生成或验收八张布局图，原两轮 Session 失败原因不回填。

authority／navigation 四路径限定 harness 修复已独立接受：Project 切换等待公开导航发布及新的完整 GET 或真实重读门槛；navigation 改用 Model 专属 System 草稿种子，原安全准入逐字保留。旧种子纯正例实际失败，修后 11 top／33 child、strict TS、integration vet 与 race 编译全部通过；独立 A/B binary 已按新 fixture 重编，仅编译与发现通过，真实场景未执行。

authority 第四轮仍 FAIL：浏览器原 45 秒总预算在 `authority-same-session-checking` 耗尽，仅四个安全响应，尚未到上述 Project 切换修复点；现有观察不能区分响应结束、JSON 读取或控制释放等待。闭合原件见 [authority-session-fourth-failure.json](../../../.agent-state/model-ui-recovery/authority-session-fourth-failure.json)。Go 55.03 秒，外层实际 exit=1／143.10 秒（含原 TCP 尾部观察）；direct 与四个 adopted child 实际 wait、watchdog／observer join、七资源双 absent、子进程空、TCP 双空与输入未变均已完成。保留原 FAIL，下一步仅补有界分段诊断，不加请求或预算；navigation 种子修复可独立复验。

navigation 第四轮已通过新的 Model 专属种子目录读取与 raw-return 检查，随后仍 FAIL：`navigation-draft-and-focus` 中 History 返回后选择“继续编辑”，名称值保留断言通过，但名称输入框恢复焦点的等待超时（模块第 231 行）；本轮未采实际 activeElement，不能先认定焦点去了哪里。闭合原件见 [navigation-focus-fourth-failure.json](../../../.agent-state/model-ui-recovery/navigation-focus-fourth-failure.json)。九个安全响应，尚无八张布局图；Go 19.47 秒、外层实际 exit=1／106.80 秒，direct／四 adopted 实际 wait、watchdog／observer join、七资源双 absent、子进程空、TCP 双空和输入同一全部完成。authority 的局部 5 秒分段诊断另已 strict TS、自查与独立有限审查接受，仅可定位后续失败，不等于原请求或业务通过。

authority 第五轮有界诊断再次 FAIL，明确停在 Session `finished()` 的 5 秒观察：headers 已见、finished event 未见、failed event 已见，受控 release 已返回；尚不能据此区分原生 body 消费结果与 Playwright 请求终态事件。闭合原件见 [authority-session-fifth-failure.json](../../../.agent-state/model-ui-recovery/authority-session-fifth-failure.json)。Go 21.82 秒、外层实际 exit=1／114.68 秒；direct／四 adopted 实际 wait、watchdog／observer join、七资源双 absent、子进程空、TCP 双空与输入同一均完成。原各轮 FAIL 保留，不能通过删掉 EOF 门槛或追加 GET 把观察改成成功。

Session 原生消费的固定八项对照已实际执行：完整 Content-Length／chunked 在原生 reader 与正式 account client 下均读到 EOF、解码及身份相等，Playwright／CDP 均 finished；正式 client 在 EOF 后的 cancel 没有复现假 failed。缺尾 JSON 两控均有 EOF 但解码拒绝；受控断连两控均 read failed、无 EOF，Playwright／CDP failed，`response.finished()` 持续 pending，关闭 context 后观察任务实际 join。该对照不证明 authority 第五轮根因，后续只补同一实际 Session 请求的安全计数诊断，保留原完成门槛。首轮外层仍为 FAIL（实际 exit=1／3.97 秒）：direct／四 adopted 均实际 wait=0、浏览器与两服务关闭、两次进程／监听空、输入同一及八观察 join 全部完成，但 nonce 临时目录遗留一个 Chromium regular 0600 文件，`runtime_empty=false`；root 随后核对精确 nonce／文件身份、已登记进程及监听不存在，清掉该文件、空目录与同 inode marker；这只证明当前资源已清，不改原终态。必要安全事实见 [session-consumption-first-failure.json](../../../.agent-state/model-ui-recovery/session-consumption-first-failure.json)。

navigation 第五轮使用 §0.3 已接受修复的新私有资产，仍在初始 Session 的 `finished()` 5 秒观察处 FAIL：headers／failed 已见、finished 未见；pageshow 前退出登录按钮可用，事件后进入禁用状态。仅一个安全响应、零布局图，尚未到焦点场景，不能据此评判该共享修复的业务效果。闭合安全事实见 [navigation-session-fifth-failure.json](../../../.agent-state/model-ui-recovery/navigation-session-fifth-failure.json)。Go 14.64 秒、外层实际 exit=1／106.91 秒；direct／四 adopted 实际 wait、watchdog／observer join、七资源双 absent、进程空、临时目录移除、TCP 双空及输入同一均完成。下一次运行前统一补 authority／navigation 两个 Session helper 的同响应原生读取、取消与 abort 安全观测；不在观测未变时重复完整场景，不改原 finished／EOF／身份门槛。

authority 第六轮使用已独审的新诊断，本轮 Session 原 finished／JSON／身份门槛通过，但不证明旧间歇失败已修复。已完成归档配置原请求重放，随后切到凭据恢复 Project 时 FAIL：URL 等待已过，公开 Project 设置链接的目标 href 尚未发布（模块第 282 行）；17 个安全响应，尚未开始凭据归档场景，没有 Session 失败诊断产物。必要事实见 [authority-navigation-sixth-failure.json](../../../.agent-state/model-ui-recovery/authority-navigation-sixth-failure.json)。Go 25.15 秒、外层实际 exit=1／114.59 秒；direct／四 adopted 实际 wait、watchdog／observer join、七资源双 absent、临时目录移除、TCP 双空及输入同一均完成。下一步核实真实导航确认或当前 Owner 发布条件，区分产品缺陷与测试前置遗漏，保持原断言与预算。

authority 第七轮整体仍 FAIL：原 45 秒用例预算在 `authority-lifecycle-gates` 耗尽，本轮显式 Session headers／finished 已见且 failed 未见，仅 13 个安全响应，尚未进入归档配置场景；没有新的导航 DOM 或原生 Session 失败产物。当前错误投影只保留 timeout 与通用入口，确切等待子步骤未记录，不能据不同停点猜测根因。必要事实见 [authority-lifecycle-seventh-failure.json](../../../.agent-state/model-ui-recovery/authority-lifecycle-seventh-failure.json)。Go 56.64 秒、外层实际 exit=1／146.76 秒；direct／四 adopted 实际 wait、watchdog／observer join、七资源双 absent、descendant 双空、临时目录实际移除、TCP 双空与输入同一均完成。原 main driver 没有单独记录删除前 runtime-empty 谓词，失败退出不能证明该谓词通过；目录移除后当前资源清空不回填原观察。下一版只补既有 await 的稳定开始／完成步骤及既有清理谓词的安全输出，保留业务门槛、请求与预算，独审后再有意义复验。

独立 A 已由独验者本人首次实际执行，整体 FAIL：停在凭据轮换阶段，仅四个安全响应，最后为 metadata GET 200；原失败投影只有错误数量，没有保留具体断言，不能认定确切根因。必要安全事实见 [independent-a-first-failure.json](../../../.agent-state/model-ui-independent/independent-a-first-failure.json)。Go 13.26 秒、外层实际 exit=1／107.38 秒；direct／四 adopted 实际 wait、Node／proxy／body／service／root join、七资源双 absent、runtime 与临时目录移除、TCP 双空、输入同一及两个 marker 移除全部完成。离线源码确认该构造会同时展示 metadata 与未绑定候选两个 `dl`，而测试使用单元素选择器；只修语义定位并补闭合错误投影，仍需正反控制及独审，不将此候选回填为原 FAIL 的已知原因。

独立 A 第二轮已由独验者本人完整通过：只修“已读版本”的语义定位并补闭合错误投影，作者与独立正反控制、strict TS 均通过；该修复不确定首轮缺失断言的根因。原未知／原请求组合五项检查全部为 true，八份完整响应完成同 body／schema／正式客户端校验，另两次预期断流被正确识别。Go 18.18 秒、外层实际 exit=0／106.16 秒；direct／四 adopted 实际 wait=0、Node／proxy／body／service／root join、七资源双 absent、runtime 与临时目录移除、TCP 双空、输入同一及两个 marker 移除全部完成。消费当轮已冻结的 common Problem.code 既有 Work 错误码补充；B 尚未执行，原 A 首轮 FAIL 保留。

旧 `audit-navigation` 首轮整体 FAIL，原 driver 已保留具体断言：`settingsCurrent` 期待的设置分组仅有“项目资料／安全记录”，实际还包含本卡正式新增的“模型与 Provider”（E2E 第 1542 行，调用第 1715 行）。这是旧浏览器测试期待遗漏；§8 #31 同一数组的兼容修正已通过 strict TS／格式检查及有限独审，所有其他导航断言保留，仍须真实复验。必要安全事实见 [audit-navigation-first-failure.json](../../../.agent-state/model-ui-regression/audit-navigation-first-failure.json)。Go 15.65 秒、外层实际 exit=1／106.58 秒；direct／四 adopted 实际 wait、watchdog／observer join、七资源双 absent、临时目录移除、TCP 双空、输入同一及 marker 移除均完成，零布局图。旧回归仍为 4／14，不把该 FAIL 改写为兼容后通过。

旧回归现为 **4／14 完整通过**。下表四组均用 `run-owned-regression.py --group` 的原精确 selector 与当前私有资产执行，外层实际 exit=0；direct／四 adopted 实际 wait=0、watchdog／observer join、七资源双 absent、临时目录移除、TCP 双空、输入同一及 marker 移除全部确认。四组均使用专属 Owner／Audit 私有资产入口，无全局资产租约，也不消费 Session 诊断源码或 bundle；不据此替代剩余 10 项。

| 已通过旧组 | Go top 秒 | 外层完整秒 |
| --- | ---: | ---: |
| `owner-edit` | 18.64 | 111.29 |
| `owner-recovery` | 14.56 | 100.34 |
| `owner-identity` | 19.58 | 105.55 |
| `audit-authority` | 30.37 | 116.96 |

历史 read 第二轮按未受影响读取路径有限复用，已获独立影响核对接受：正式记录可恢复，读取流程及相关业务代码未变，顺序单 Dialog 不进入已修共享层的叠层或剩余模态解禁回焦分支。该轮原始 run／响应／输入文件当前缺失，因此不声称当前二进制／资产／环境完整闭包重新核验，也不记作新资产实跑通过；此复用不扩展到 authority／navigation 或受影响的 recovery／configuration／credential。

Session 诊断首版的迟到初始化清理缺陷已独立复现；限定返修的类型／私有构建、作者 34 项与独立 20 项离线控制通过，已获有限独审接受。两个 Session helper 共享同响应安全计数，区分 EOF／读拒绝／abort／EOF 前取消与 EOF 后清理；公开 Request ID 仅在浏览器内比较，缺失或迟到不能解释为同响应，原 finished／JSON／身份门槛保留。已用于 authority 第六轮；该轮 Session 未失败，尚无真实失败的原生诊断可用于解释旧因果，不改变各轮原 FAIL。

另一个精确组件正反例已作者与独立者各自实跑：原 trigger 始终可用时通过，确认期间 disabled、同 tick 关闭确认并解除 disabled 时失败；原节点最终已 connected／enabled／非 inert／保值，实际焦点在下层关闭按钮。它证明共享层的局部恢复时序缺口，不回填 navigation 第四轮未采集的 activeElement；后继共享修复及其适用验证仍在进行，§0.2 已接受结论保持绑定原版本。

共享焦点后继修复的 114 项单测、独立 8 项正反例及完整前端 2640 项／类型／build 通过，但首轮真实组件未通过：18 个新用例都在打开确认框后的前置焦点断言失败，trace 证明确认按钮先聚焦、随后收到无指针点击，自动化尚未执行其后显式点击；不能精确指称未采集的键盘阶段。另有外层监督错误：临时附加的 120 秒整轮上限截断第 20 项，并漏收 adopted 实际 wait，末个 Vite close 也未证明。当前已无自有运行进程或监听端口，PID 115611 仅为 PPID 1 的已退出 zombie；不能补写原 Wait／close 或称该轮完整退役通过。必要事实见 [shared-focus-first-failure.json](../../../.agent-state/model-ui-recovery/shared-focus-first-failure.json)。只修该测试专属 Enter handler 的默认行为，保留全部焦点断言；新固定选集监督器按原每项 45 秒预算计总长并实际收取 adopted 终态，先复验单个代表前置场景，尚不正式接受共享修复。

### 0.1 T1 最小共享源码与实际签名

以下为当前已提交接入点的最小核对，不再复制实施前源码哈希表或过期行号：

- `account.ts` 的 `WriteOptions` 保留 `csrfToken`、`key` 与可选 `signal`；本域 API 不持有第二套 Cookie/CSRF owner。
- `createSessionController` 已在原 14 个默认依赖后追加第 15 项 `projectModelSettingsAPI`，公开本域封闭 facade；旧依赖次序保持，详见 §6.1。
- `createProjectWorkspace` 仍公开只读 `currentReadContext`，含完整 identity、ProjectID、generation、readGeneration；`confirmLeave(target?: string)` 返回 `Promise<boolean>`。此读归属不能作为 Mutate 授权。
- `App.vue` 已创建并 provide 唯一本域 controller，Logout 聚合确认已追加本域；路由、设置菜单和 ProjectNav 已包含两个合法后缀。保持既有 Owner/System/Selection/Summary 的独立 intent 与实际 owner 尾部约束。
- 审查按上述入口及实际改动补读必要依赖；上述静态签名核对不替代 §0 的实际恢复状态或真实验收。

### 0.2 共享浮层遮挡修复

authority 第二轮的真实 hit-test 证明：后激活的确认框虽然获得焦点与模态所有权，其遮罩仍可能被 DOM 顺序较后的旧 Provider 遮罩覆盖。共享 `useLayer` 现按同一激活栈同步绘制顺序，`UiDialog` 整体遮罩与 `UiPopover` 绑定该层级，Drawer 继承 Dialog 行为；焦点、inert 与关闭策略保持原契约，设计规范同步说明。这是单独可交付的通用组件修复，不表示 D27 剩余场景已通过。

当前 `npm run check --prefix web` 完整通过：格式、60 个文件共 2632 个单元测试、类型及生产构建；首轮因现存依赖目录缺少锁定 `go-captcha-vue` 导致的失败保留，经 `npm ci --prefix web` 恢复后通过。真实组件浏览器浅色 8 项与深色补集 8 项分别通过，覆盖 Dialog/Drawer、390/1440 宽度、正常/减少动效、逆 DOM 激活、实际指针命中、Popover/模态叠加、移除重开、Tab/Escape 与焦点恢复；两轮 child 实际 wait=0，Vite close 已返回，worker 已退出。深色第一次选择器未命中产生的 0 tests setup FAIL 保留。六路径限定实现、完整工程检查与真实结果已获独立接受，不复用为 Project authority/navigation 的业务通过。

### 0.3 确认关闭后的原焦点恢复

共享层新增一次受保护的 DOM 更新后重试：正常关闭确认框时，若剩余模态内的原触发控件仍暂时 disabled，先保留既有合法兜底焦点，更新后再检查原节点与控件可用性。组件卸载／重开、层栈变化、页面或 panel 替换、用户自主移焦均使旧重试失效，不改变 Model 禁用策略或原焦点契约。技术范围仅 `useLayer.ts`、对应单元测试及真实组件 E2E；设计契约未变。

最终版本完整 `npm run check --prefix web` 通过：格式、60 文件／2640 单测、类型及生产 build；其中共享组件单测 114／114，独立正反例 8 项通过。纠正测试专属 Enter 默认动作后，单代表场景与最终 21 项真实组件分别完整通过：Dialog／Drawer 的浅深色×390／1440×正常／减少动效共 16 项、2 个稳定启用对照，以及永久禁用 anchor、直接销毁、剩余模态／非 top 移除三项旧边界。最终 21 项无 skip／flaky／unexpected，direct 与四个 adopted child 实际 wait=0、Vite close、输入同一及两次子进程／监听空均已确认。该限定实现与最终结果已获独立接受；不以此替代 Project authority／navigation 或其余 D27 验收。首轮 18 个前置失败、外层缺失 Wait／close 及后续单例单复数解析 setup FAIL 保留，不回填为成功。

## 1. 完整结果与开工门槛

本卡交付一个完整结果：当前 Human Owner 在 Project 设置中管理本 Project chat Providers/Models、Model-purpose Credential，并浏览安全的 System/Project 可用 chat 目录；所有写入具有明确结果、版本冲突和原请求恢复。两个菜单叶子遵守正式 project-settings §1/4：Providers 与可用模型。本 Project Models 的管理放在 Providers 叶子的内部“项目 Models”面板，不新增第三个设置叶子。

| 直接依赖 | 正式接受与本卡消费 | 仍不据此完成 |
| --- | --- | --- |
| Project Model 五读 HTTP | a0b012ce，[project-model-owner-read-http-verification.md](../agent-team/project-model-owner-read-http-verification.md)；Provider/Model list/get、七字段 available、当前逐页 Owner/Session、8 MiB完整表示 | 配置可调用、外部Provider网络或生产Resolver |
| Project Model Credential HTTP | e4b1b891，[project-model-credentials-http-verification.md](../agent-team/project-model-credentials-http-verification.md)；Create/Update/Delete、metadata、被动lookup、默认根 | 材料GET、Credential列表、跨命令原子绑定 |
| Project Model 配置写 HTTP | cc850b22，[project-model-configuration-write-http-verification.md](../agent-team/project-model-configuration-write-http-verification.md)；六写、found/receipt lookup、同默认根读写 | Agent rewrite、删除preview、Project selector |
| Owner UI 最终24 | [project-owner-workspace-ui-final24-verification.md](../agent-team/project-owner-workspace-ui-final24-verification.md)；稳定ID、Resolve→Get、Session、导航确认、当前Owner与只读生命周期 | 创建/归档/恢复/删除UI或生命周期链完整绑定 |
| 既有 System/账号前端基础 | frontend README 对应完整验收；复用唯一Cookie owner、Ui/Dialog/SettingsShell、严格标量/Instant等纯工具 | 不复用System端点、admin谓词、按Provider分页或Impact状态机 |
| Audit UI 完整22接受 | [完整验收](../agent-team/project-owner-audit-ui-verification.md)已接受；共享接入点按§0.1与当前限定 diff 核对 | 有限版本组合，不声称所有producer或当前HEAD一次全新全测 |

后端三卡已提供此UI所需全部公开协议，无新增迁移、公共contract、SQL或生产root修改理由。31个产品路径（30技术＋README）是同一设置结果的有限范围；不先做只有列表的界面卡。Provider与Credential是显式独立命令，Model共享这些稳定引用和当前Owner，不引入自动创建/补偿工作流。若实现确需额外共享路径，向直接负责人说明理由与影响；涉及跨任务所有权或公共契约时由主线程协调并修订范围。

正式 SPEC、Audit 整卡和端点协议已接受；前端当前交付及恢复输入见 §0。负责人按依赖组织剩余实现和验证，指定文件、Go/cache、资产及测试资源的唯一所有者；跨任务窗口由主线程协调，不能占用其他任务资源。

## 2. 路由、布局与用户行为

只追加以下两个合法Project设置后缀，其他Owner/Audit路由与默认基本信息保持：

- `/:username/:project_name/settings/model-providers`：Providers列表/详情/表单，以及内部“项目 Models”面板/表单。
- `/:username/:project_name/settings/available-models`：只读安全可用chat目录。

安全return仍按既有raw path闭集验证；query/hash、编码绕过、未知尾部不能成为返回目标。ProjectNav仅将这两个同Project合法后缀认作“设置”当前项，默认href仍general。只在当前Owner Get完成后显示页面与入口，不从路由username、admin角色、Resolve候选或旧缓存推导权限。三个列表均独立页态，默认25，可选50/100；不是HTTP缺省50的变更。

Providers面板显示name/protocol/enabled/稳定ID，正式详情显示本Project配置、version、时间与credential_ref；协议创建后只读。Models面板标题明确“项目 Models（全部 Providers）”，不要求先选一个Provider才能列表，不在请求附provider_id、不静默筛掉本页其他Provider的行。模型归属用稳定ProviderID；已加载同Project Provider名称可作附加显示，未知时显示ID，不自动扫全库或按行发N+1查询。创建Model从一个正式读取的本Project Provider选择（独立分页）或其详情进入；编辑Model的Provider/type只读，协议依据同Project Provider正式Get，不从名字猜测。

可用模型仅显示七字段与capabilities的安全子信息，标明System/Project来源。System行没有编辑按钮、System管理链接或补读完整System配置；本Project行也不能把目录七字段当作编辑详情，明确编辑时另走Project Get。Provider/Model配置enabled与可用目录是不同观察，禁用Provider仍可管理合法配置；保存不声称已连通、能调用或已用于Agent。

所有列表/详情/表单独立loading/empty/error；empty只来自完整合法200空页。已读页可保留为明确旧观察，不冒当前状态；cursor只在内存，不进URL/storage，翻前页重新读原cursor、刷新回首页、limit变更回首页。CURSOR_INVALID保条件与错误，用户明确从第一页重读，无自动扫页/重试。三列表cursor互不共用，首水位不解释为配置快照；无total或本地计算的全库总数。

复用双导航、SettingsShell和Ui组件。桌面表单正常换行，390窄屏单列/行堆叠，菜单Drawer与对话框键盘/Tab/Escape/焦点遵原框架。长ID/URL文本不成为自动可点击外链，不使用v-html。页面标题、字段错误与反馈可访问；动作禁用说明当前原因，不能只靠颜色。

## 3. 闭合数据、API与容量

新增Project专用read/write类型，不能把System Provider/Model强转为Project DTO。合法读取与当前写policy分开：HTTP可读取的非空配置对象或合法非空reasoning_efforts不能被客户端静默删掉/压缩。当前编辑器只支持§4的写集合，遇合法但超出该集合的既有值可完整安全只读并说明编辑限制，不能清空后提交。

| DTO | 必有闭集 |
| --- | --- |
| ProjectProvider | id,scope,input,version,created_at,updated_at；scope恰kind=project/project_id=本请求ID；input恰name/protocol/base_url/enabled/credential_ref/options |
| ProjectModel | id,provider_id,scope,input,version,created_at,updated_at；scope同上；input恰name/provider_model_id/type/enabled/parameters/request_overwrite/header_overwrite/capabilities，type=chat |
| AvailableChatModel | 恰id/provider_id/scope/name/provider_name/version/capabilities；scope为system或本Project，无第八字段，无endpoint/protocol/credential/原生型号/参数/时间 |
| 三种Page | 恰items非null数组、next_cursor显式null或合法token；≤请求limit≤100，ID不重复；空页cursor=null，非null cursor要求满页；Provider/Model按公开created_at/id严格倒序，目录不伪验不存在的created_at |
| ConfigurationReceipt | kind/resource_id/version/affected_references四字段；六kind；本已绑Project能力的成功affected_references必须"0" |
| ConfigurationLookup | found=false/receipt=null，或found=true/合法receipt；不是Secret observation |
| CredentialMetadata | credential_id/purpose=model/version三字段 |
| CredentialMutation | Metadata＋deleted；create版本1且false，update=expected+1且false，delete=expected+1且true |
| CredentialLookup | observed=false/result=null，或observed=true/合法Mutation；无in_progress，不是Owner Update三态 |

UUID用canonical v7；version/计数/容量用精确十进制字符串，不经JS Number。配置expected_version接受1..MaxInt64；首次递增溢出交原InvalidState，不能把配置界改成凭据的Max−1。Credential update/delete/lookup expected_version为1..9223372036854775806，metadata/result仍完整正int64。Instant沿既有UTC微秒及日历/created≤updated校验。严格Unicode/字节/rune规则，拒lone surrogate，不trim名称、base_url、型号或材料。

Capabilities十字段沿正式schema：四bool、四非null数组、context_length/max_output必有null或正int64字符串；parallel→tools，非reasoning时efforts空，modalities四值与structured两值闭集去重，effort safeToken≤32B、**无新业务数组总数上限**，两容量有值时max_output≤context_length。读投影保原顺序，不把写policy当读取损坏判据。

两新API模块共17个封闭方法，参数只收ProjectID、typed ID/input/query、AbortSignal或既有私有WriteOptions；不收任意URL、scope/actor/user/header/cap。所有输入在await前同步捕获/验证/冻结，调用者后改数组/对象不改变请求：

| 方法组 | 精确资源与请求 |
| --- | --- |
| 5读 | GET `/projects/{P}/model-providers`、其`/{provider}`、`/models`、其`/{model}`、`/available-chat-models`；均位于`/api/v1`。列表只cursor/limit，详情无query/body；不发key/CSRF |
| Provider三写 | POST collection `{input}`；PUT detail `{expected_version,input}`；DELETE detail `{expected_version}` |
| Model三写 | POST collection `{provider_id,input}`；PUT detail `{expected_version,input}`；DELETE detail `{expected_version,replacement}`，replacement必有null或非自身ModelID |
| 配置lookup | POST `/projects/{P}/model-commands/lookup`，body恰`{command:<六kind>}`；原key，不能发target/input/expected或selector kind |
| Credential metadata | GET `/projects/{P}/model-credentials/{C}`，无query/body/key/CSRF |
| Credential三写 | POST collection `{value}`；PUT detail `{expected_version,value}`；DELETE detail `{expected_version}` |
| Credential lookup | POST `/projects/{P}/model-credential-commands/lookup`；create恰`{kind:"create"}`，update/delete恰kind/credential_id/expected_version；不含value |

所有写及两个POST lookup均同源Cookie、CSRF、恰一原Idempotency-Key；无PATCH、无collection Credential GET、无HEAD客户端功能扩张。ID逐段校验后编码，ProjectID始终来自稳定当前绑定；query一次编码、无裸?、原32KiB/8192B cursor界。新动作须独立Project分类，不能进入System/admin或现Owner64KiB分支。

五读成功cap统一8,388,608B；配置receipt/lookup、Credential metadata/mutation/lookup成功cap1,024B；Problem保原600000B。配置七POST/PUT/DELETE请求cap1,048,576B；Credential create/update 409600B、delete/lookup 1024B，decoded value1..65536B，不能复用System 512KiB材料请求界。原其他端点cap不变，不给caller可调cap。

transport必须实际chunk累计/fatal UTF8/EOF/完整JSON/全对象refinement后一次发布不可变结果；不信Content-Length，不截断、丢坏末行、减limit或额外查详情救坏页。完整200 application/json/no-store及既有RequestID/Problem校验保持。body/reader.cancel、releaseLock与真实transport finally结束前不释放Cookie owner。cap是响应准入界，不宣称Go/浏览器全RSS或后端事务硬期限。

### 3.1 精确 typed 方法、Session action 与 endpoint

两新API文件保持原写域#2/#3。下列签名为本卡沿已接受候选采用的精确接口建议；ID/版本仍string以兼容既有风格，但捕获时严格UUIDv7/精确十进制校验，不代表任意string合法。`WriteOptions`原样取`api/account.ts:81`，仅Session私有owner注入CSRF/key/signal。读取query恰cursor?/limit?，limit验证1..100；UI可选25/50/100不缩窄HTTP typed API。无provider_id、任意path/header/scope/actor或caller cap。

```ts
type ProjectChatProtocol = 'openai-chat-completions' | 'anthropic-messages';
type ProjectModelPageQuery = Readonly<{ cursor?: string; limit?: number }>;
type ProjectModelPage<T> = Readonly<{ items: readonly T[]; next_cursor: string | null }>;
type EmptyObject = Readonly<Record<string, never>>;
type ProjectProviderWriteInput = Readonly<{
  name: string; protocol: ProjectChatProtocol; base_url: string;
  enabled: boolean; credential_ref: string | null; options: EmptyObject;
}>;
type ProjectModelWriteInput = Readonly<{
  name: string; provider_model_id: string; type: 'chat'; enabled: boolean;
  parameters: EmptyObject; request_overwrite: EmptyObject; header_overwrite: EmptyObject;
  capabilities: ProjectChatWriteCapabilities;
}>;
type ProjectModelWriteContext = Readonly<{ provider_id: string; protocol: ProjectChatProtocol }>;
type ProjectModelWriteTarget = ProjectModelWriteContext & Readonly<{ id: string }>;
type ProjectConfigurationKind = 'provider.create' | 'provider.update' | 'provider.delete'
  | 'model.create' | 'model.update' | 'model.delete';
type ProjectConfigurationReceipt<K extends ProjectConfigurationKind = ProjectConfigurationKind> =
  Readonly<{ kind: K; resource_id: string; version: string; affected_references: '0' }>;
type ProjectConfigurationCommand =
  | Readonly<{ kind: 'provider.create'; input: ProjectProviderWriteInput }>
  | Readonly<{ kind: 'provider.update'; id: string; expected_version: string; input: ProjectProviderWriteInput }>
  | Readonly<{ kind: 'provider.delete'; id: string; expected_version: string }>
  | (ProjectModelWriteContext & Readonly<{ kind: 'model.create'; input: ProjectModelWriteInput }>)
  | (ProjectModelWriteTarget & Readonly<{ kind: 'model.update'; expected_version: string; input: ProjectModelWriteInput }>)
  | Readonly<{ kind: 'model.delete'; id: string; expected_version: string; replacement: string | null }>;
type ProjectConfigurationObservation = Readonly<{ found: false; receipt: null }>
  | Readonly<{ found: true; receipt: ProjectConfigurationReceipt }>;
type ProjectCredentialLookupTarget = Readonly<{ kind: 'create' }>
  | Readonly<{ kind: 'update' | 'delete'; credential_id: string; expected_version: string }>;
type ProjectCredentialMetadata = Readonly<{ credential_id: string; purpose: 'model'; version: string }>;
type ProjectCredentialCreated = ProjectCredentialMetadata & Readonly<{ version: '1'; deleted: false }>;
type ProjectCredentialUpdated = ProjectCredentialMetadata & Readonly<{ deleted: false }>;
type ProjectCredentialDeleted = ProjectCredentialMetadata & Readonly<{ deleted: true }>;
type ProjectCredentialObservation = Readonly<{ observed: false; result: null }>
  | Readonly<{ observed: true; result: ProjectCredentialCreated | ProjectCredentialUpdated | ProjectCredentialDeleted }>;
interface ProjectModelsAPI {
  listProviders(projectID: string, query: ProjectModelPageQuery, signal: AbortSignal): Promise<ProjectModelPage<ProjectProvider>>;
  getProvider(projectID: string, providerID: string, signal: AbortSignal): Promise<ProjectProvider>;
  listModels(projectID: string, query: ProjectModelPageQuery, signal: AbortSignal): Promise<ProjectModelPage<ProjectModel>>;
  getModel(projectID: string, modelID: string, signal: AbortSignal): Promise<ProjectModel>;
  listAvailableChatModels(projectID: string, query: ProjectModelPageQuery, signal: AbortSignal): Promise<ProjectModelPage<ProjectAvailableChatModel>>;
  createProvider(projectID: string, input: ProjectProviderWriteInput, options: WriteOptions): Promise<ProjectConfigurationReceipt<'provider.create'>>;
  updateProvider(projectID: string, providerID: string, expectedVersion: string, input: ProjectProviderWriteInput, options: WriteOptions): Promise<ProjectConfigurationReceipt<'provider.update'>>;
  deleteProvider(projectID: string, providerID: string, expectedVersion: string, options: WriteOptions): Promise<ProjectConfigurationReceipt<'provider.delete'>>;
  createModel(projectID: string, context: ProjectModelWriteContext, input: ProjectModelWriteInput, options: WriteOptions): Promise<ProjectConfigurationReceipt<'model.create'>>;
  updateModel(projectID: string, target: ProjectModelWriteTarget, expectedVersion: string, input: ProjectModelWriteInput, options: WriteOptions): Promise<ProjectConfigurationReceipt<'model.update'>>;
  deleteModel(projectID: string, modelID: string, expectedVersion: string, replacement: string | null, options: WriteOptions): Promise<ProjectConfigurationReceipt<'model.delete'>>;
  lookupConfiguration(projectID: string, command: ProjectConfigurationCommand, options: WriteOptions): Promise<ProjectConfigurationObservation>;
}
interface ProjectModelCredentialsAPI {
  getCredentialMetadata(projectID: string, credentialID: string, signal: AbortSignal): Promise<ProjectCredentialMetadata>;
  createCredential(projectID: string, value: string, options: WriteOptions): Promise<ProjectCredentialCreated>;
  updateCredential(projectID: string, credentialID: string, expectedVersion: string, value: string, options: WriteOptions): Promise<ProjectCredentialUpdated>;
  deleteCredential(projectID: string, credentialID: string, expectedVersion: string, options: WriteOptions): Promise<ProjectCredentialDeleted>;
  lookupCredential(projectID: string, command: ProjectCredentialLookupTarget, options: WriteOptions): Promise<ProjectCredentialObservation>;
}
type ProjectModelSettingsAPI = ProjectModelsAPI & ProjectModelCredentialsAPI;
```

`ProjectProvider`、`ProjectModel`、七字段`ProjectAvailableChatModel`和只读Capabilities使用本卡§3全DTO；不得alias成System DTO或以上较窄WriteInput。`ProjectChatWriteCapabilities`为原十字段、四bool、modalities/structured闭集、reasoning_efforts恰readonly []、两容量原nullable string；协议相关policy在捕获的`context.protocol`检查。读取的合法非空options/overwrite/efforts继续完整接受并只读。此补充不改变本卡§3/4任何字节/policy边界。

Context/Target中的protocol/provider_id是从原正式Provider/Model观察捕获的本地校验背景；create HTTP仅发provider_id+input，update HTTP仅发expected_version+input。protocol从不额外上网，update不改变provider_id/type；原重放不用新Get查现Provider。Provider编辑保原protocol，正式后端仍判不可变；无新增Provider Get前置。delete不用Context强迫无关Provider读取，replacement必有null或明确候选；有reference仍DependencyUnbound。

lookupConfiguration接收已捕获六kind完整Command以核receipt目标/版本，但HTTP体只发`{command: command.kind}`；Credential lookup只发上述闭集target且绝不发value。found/observed结果都是历史观察，不能严格确认输入或材料。API不会借lookup做额外Get、自动Execute或新key。

所有路径以下列相对`/api/v1/projects/{project_id}`定义，均预期200 application/json；每行唯一transport分类和Session Action，不折叠进System或Owner64KiB分支。

| # | typed方法 | Session action | client endpoint key | HTTP与相对path |
| --- | --- | --- | --- | --- |
| 1 | `listProviders` | `project-model-provider-list` | `listProjectModelProviders` | GET `/model-providers` |
| 2 | `getProvider` | `project-model-provider-get` | `getProjectModelProvider` | GET `/model-providers/{provider_id}` |
| 3 | `listModels` | `project-model-list` | `listProjectModels` | GET `/models` |
| 4 | `getModel` | `project-model-get` | `getProjectModel` | GET `/models/{model_id}` |
| 5 | `listAvailableChatModels` | `project-model-available-list` | `listProjectAvailableChatModels` | GET `/available-chat-models` |
| 6 | `createProvider` | `project-model-provider-create` | `createProjectModelProvider` | POST `/model-providers` |
| 7 | `updateProvider` | `project-model-provider-update` | `updateProjectModelProvider` | PUT `/model-providers/{provider_id}` |
| 8 | `deleteProvider` | `project-model-provider-delete` | `deleteProjectModelProvider` | DELETE `/model-providers/{provider_id}` |
| 9 | `createModel` | `project-model-create` | `createProjectModel` | POST `/models` |
| 10 | `updateModel` | `project-model-update` | `updateProjectModel` | PUT `/models/{model_id}` |
| 11 | `deleteModel` | `project-model-delete` | `deleteProjectModel` | DELETE `/models/{model_id}` |
| 12 | `lookupConfiguration` | `project-model-configuration-lookup` | `lookupProjectModelConfiguration` | POST `/model-commands/lookup` |
| 13 | `getCredentialMetadata` | `project-model-credential-get` | `getProjectModelCredentialMetadata` | GET `/model-credentials/{credential_id}` |
| 14 | `createCredential` | `project-model-credential-create` | `createProjectModelCredential` | POST `/model-credentials` |
| 15 | `updateCredential` | `project-model-credential-update` | `updateProjectModelCredential` | PUT `/model-credentials/{credential_id}` |
| 16 | `deleteCredential` | `project-model-credential-delete` | `deleteProjectModelCredential` | DELETE `/model-credentials/{credential_id}` |
| 17 | `lookupCredential` | `project-model-credential-lookup` | `lookupProjectModelCredential` | POST `/model-credential-commands/lookup` |

前5成功cap8MiB，余12成功cap1KiB；Problem统一原600000B。6配置写+配置lookup请求1MiB；Credential create/update400KiB，delete/lookup1KiB，decoded65536B；六GET不发key/CSRF。9mutation+2POST lookup共11调用使用原WriteOptions。DTO、capture与body一次冻结，所有HTTP前无await；发布要真实EOF、严格UTF8/JSON/全对象验证。cap只管准入，不当RSS保证。

## 4. 表单与独立凭据命令

Provider支持name、两chat协议openai-chat-completions/anthropic-messages、base_url、enabled、nullable credential_ref，options固定{}。协议创建后只读；scope不由用户编辑。名称1..128 rune且≤512B，URL沿原http/https、有host、无userinfo/query/fragment/空port等endpoint规则，≤8192B且不规范化成另一串。

Model支持name、provider_model_id、enabled及capabilities；Provider创建时选择、之后只读；type始终chat。parameters/request_overwrite/header_overwrite固定{}，reasoning_efforts固定[]，无任意JSON/header/effort编辑器。chat输入允许text/image/file/vector子集，输出仅text子集；四bool沿正式约束；OpenAI structured可text/json_schema，Anthropic仅text。context_length/max_output可空且精确字符串。保原CAPABILITY_UNSUPPORTED和typed InvalidArgument边界，不修改后台policy或暗示这些声明经过实际模型调用验证。

Credential没有目录。入口仅为：Provider已绑定ref的安全metadata；用户明确输入同Project CredentialID并读取metadata；在本页创建新Credential后保留其安全ID。不得列出所有Credential、Get材料、用System credential或Project Variable/Secret替代。metadata与材料分区：已有材料不回显；新值以password输入，空输入表示不旋转，删除为独立确认动作。

**显式分步，禁止自动链式写：** 用户单独确认“创建凭据”后生成Credential intent并执行。只有严格Execute Mutation确认才清材料并把安全ref作为“已创建、尚未绑定”的本地候选；用户再明确选择/使用该ref到Provider草稿，再明确保存Provider。不会在Credential callback自动发Provider写，不自动创建默认Model，不做失败补偿删除。Provider失败保已确认Credential事实与ref，重读冲突后新Provider保存不能再次创建Credential。显示可复制的安全CredentialID以便随后手动metadata管理；key/材料/digest不可复制或展示。

rotate/delete仅针对当前明确读取的metadata ID/version，分别冻结新value或expected_version。Secret删除有引用或lease时保RESOURCE_BUSY；用户可另行编辑Provider解绑并确认保存后，再明确重读metadata、重新发新删除，不自动解绑/删Provider/释放lease。删除Provider只释放其引用，不删除Credential；历史ref已删除不应阻断已捕获原写重放。

Provider/Model配置与已读baseline无变化时禁保存；Credential无法从metadata判断新旧材料是否相同，仅空输入不旋转，不能把相同长度或旧version当作材料相同证明。原字段值保留且验证错误零网络。409版本/当前状态冲突保输入，由用户显式重读、核对、选择采用当前值后形成新意图；不换版本自动提交，不自动合并。已确认写后GET失败保写确认，只提供读重试，不重复写或以当前对象缺失取消历史确认。

## 5. 删除与引用边界

Provider删除前显示对象及影响说明并捕获当前version；**全Project Models某页为空、首项不属于该Provider或本地未见其Model都不能证明该Provider无Model**。没有按Provider查询或删除preview，因此不虚构影响数、不自动扫全部页；最终DeleteProvider由正式库判断，有任何Model按原INVALID_STATE拒绝。

Model删除确认捕获正式Get的目标ID/version。replacement必由用户明确选择“无替代”(null)或从独立分页的安全available目录选非自身System/本Project chat；不从目录补System Get，不用当前Models页证明目录资格。候选启用和引用竞态最终由DELETE重验；UI预选不冻结它们。

当前已接受Project能力：有任何Model reference返回DEPENDENCY_UNBOUND，无论候选是否提供；不能把没有preview、索引没有公开计数或目录可见说成“无引用”。用户说明为“仍被使用的模型暂不能在此删除”，保原记录、选择、输入和错误。无引用的合法删除实际affected_references=0；成功也不得宣称更新Agent/approval_model/Project Summary。不增加引用迁移、Agent编辑、selector/Summary override或后端替代adapter。

## 6. 唯一Session owner与App期状态

createSessionController按§0.1在原参数末尾保留已追加的带默认值ProjectModelSettings API依赖（内部可组合两新API），旧参数位置/省略调用不变。新增17个明确action及本域revision/封闭facade，使用原runAuthorized与私有Cookie/CSRF持有者；不导出任意callback，不重建fetch队列，不伪装personal/System/旧project-read。

当前predicate要求authenticated Human、完整user/session/identity epoch、operation generation与本域revision；普通Owner和admin Owner同样处理，每次正式API仍由后端当前Owner授权。System denied/admin变化不能授予或撤销本Project；局部403/404不能污染System。当前合法401会话错误、当前写CSRF失败沿既有全局失效链；旧identity/generation的晚401/403/finally不得清新Session。

App创建/provide唯一`useProjectModelSettings(auth,workspace)`，生命周期跨Session checking临时卸载。只读消费workspace已公开currentReadContext来绑定稳定ProjectID/完整identity/读代次，**它仅是UI读归属，不是Mutate grant**；当前detail同ID/phase=current时的lifecycle用于新编辑提示，后端重验Mutate。不要拿workspace.canSave/readOnly中的Owner基本资料冲突或旧草稿状态代替Model权限，也不改workspace自己的draft/intent。

状态区分读页/详情观察、用户draft、已确认write receipt、历史lookup observation、pending原intent。Provider配置、Model配置、Credential分别持有准确kind/目标；共同遵唯一实际owner。同一确认流内只允许一个待决提交，若另一类待决需先明确保留/查证/放弃，不能覆盖其原材料。已确认但未绑定的Credential事实独立于Provider draft，回退不撤销它。

同完整identity checking或Owner当前读取暂不可用：隐藏受保护数据和模态、停止新请求；保内存dirty/未决/已准备ref以便同identity恢复，旧只读页失效，重新获得同stable Project当前Get后才可显示编辑和发请求。真正user/session/CSRF epoch变化、Logout、失去本Project当前Owner归属、切换稳定Project或确认离开，清本域旧材料/观察/代次；无localStorage/history/URL恢复旧key。名字只用于路由定位，旧名复用到新Project不可带入任何草稿。不能以read generation刷新自动覆盖dirty草稿或原expected。

导航到另一个本组叶子、其他Project、System、history返回及Logout接入App既有聚合确认；取消保持URL/输入/原焦点，确认才放弃相应本地追踪。分别保System四用途和Summary的现有独立草稿，不默认同意其确认。不确定/dirty注册既有beforeunload约束；关闭或放弃不撤销服务端命令。焦点/await/nextTick回调须再核identity/Project/页面实例，不能抢新层焦点。

30秒可见截止与实际owner分开。已发请求即使取消/超时/卸载仍持owner直到fetch/body/cancel/finally真实结束，Logout/Session恢复/旧域操作均不能越过。只读取消提供显式重读，尾部结束只解锁；不自动发写或查证。后端五读/lookup2s、写30s保持，Model原ctx私有Unknown确认与Secret不自动确认的差异不能在UI抹平。

### 6.1 既有默认依赖位置与封闭分派

实施前 `createSessionController` 的14个默认依赖按次序为：1 api、2 systemAPI、3 invitationAPI、4 providerAPI、5 modelAPI、6 selectionAPI、7 accountSecurityAPI、8 smtpAPI、9 smtpDeliveryAPI、10 outboundAPI、11 auditAPI、12 runtimeInformationAPI、13 projectAPI、14 projectAuditAPI。当前已追加且须保持的第15项为：

```ts
projectModelSettingsAPI: ProjectModelSettingsAPI = createProjectModelSettingsAPI(), // 15
```

组合factory放#2 `project-models.ts`，仅组合自己的12方法factory与#3凭据5方法factory；默认同一个可选Fetch注入用于受控测试，不增加第三API路径，不导出System callback。旧14个参数的位置、省略调用、System Selection第6参数内的Summary组合保持。Session facade建议`auth.projectModelSettings`：相同17方法名，但读方法去除signal，9写去除WriteOptions，两个lookup改无参、只消费本域原intent；显式retryOriginal复用原9写之一的action，不新增第18HTTP。`abandonReads`/`abandonPending`为本域封闭控制，不接受callback/URL/key。

Action union为表内17个literal。建议按action持17个revision计数，以6读/6配置写+lookup/3凭据写+lookup分组；普通读取消只退休六读，不清三类写intent或旧域。runAuthorized的revision/current分派及失败分派均显式接本17种，当前authenticated Human+完整user/session/epoch/op/revision，绝不落旧admin默认。6GET的CSRF_FAILED不触发身份门禁；9写和2POST lookup的有效current CSRF拒绝按私有CSRF失效语义；其余局部权限错误不设System denied。same-identity checking只隐藏观察和暂停新提交，保原写材料；真正identity/CSRF变化清本域原材料。实际finally才释放owner。

App唯一创建/provide/dispose新controller；原9个confirmLeave及selection所持双intent不替换。新confirm接同一聚合流程，逐域保留明确选择，不能顺带丢Owner/System/Summary。router/auth只在其现有私有导航注册域添加新controller的闭合confirm；workspace只读currentReadContext/detail，绝不新增workspace写方法。

两个后缀是`/settings/model-providers`、`/settings/available-models`。`projectRoute`返回suffix union及同一个raw regexp同时加两literal；原`[%\\?#]`拒绝、用户名/保留根/大小写规范不变，不开任意settings子树。router/index现Project Settings children仅新增`model-providers`（name project-model-providers）和`available-models`（name project-available-models），settings默认redirect仍general。ProjectNav仅把同username/同project两suffix加入设置current条件，href仍general；ProjectSettings新增“模型与Provider”组两个leaf，general与Audit原组不删。authentication.spec增加两合法后缀及`/`、child、query、hash、percent、backslash反例；project-workspace菜单期待最小兼容，原Owner导航/恢复不改。

## 7. 写结果、Unknown与原请求恢复

明确保存才生成一次随机合法key，私有冻结完整identity/原CSRF/ProjectID/kind/target/provider/replacement/expected_version/完整typed输入及最终body bytes。表单后改、双击、query变化不能改变它；输入验证未派发为零网络。Credential材料只在password输入/必要私有内存中持有，发出后不在反馈/历史详情重新显示；严格确认、身份清理或明确放弃时清输入和引用，不承诺JS字符串物理擦除。

首次明确拒绝必须是完整合法Problem、此前无不确定，code/status及not_started/not_committed同时匹配下表；unknown无条件优先。这里的闭集是保守UI分类，未列情况仍不确定，不修改服务端错误语义。

| 首次响应来源 | 可解释为明确拒绝的原配对与UI行为 |
| --- | --- |
| 任一新配置/Credential写的严格输入边界 | 400 INVALID_ARGUMENT、413 PAYLOAD_TOO_LARGE、415 UNSUPPORTED_MEDIA_TYPE；保草稿，用户明确修正后再保存，不自动换key |
| 六配置写业务拒绝 | 409 VERSION_CONFLICT/INVALID_STATE/RESOURCE_BUSY/PROJECT_NOT_ACTIVE，422 CAPABILITY_UNSUPPORTED，503 DEPENDENCY_UNBOUND；保原错误及输入；版本/状态冲突须明确重读，引用未绑不能解释成已替换 |
| Credential三写业务拒绝 | 409 VERSION_CONFLICT/INVALID_STATE/RESOURCE_BUSY/PROJECT_NOT_ACTIVE；用途或目标404按下一行，不能用metadata证明材料同义 |
| 当前权限/目标拒绝 | 401 UNAUTHENTICATED/SESSION_REVOKED；403 FORBIDDEN/CSRF_FAILED/ORIGIN_DENIED；404 NOT_FOUND。仅合法完整Problem与上述明确commit态才说明本次被拒；显示/材料清理按§6与真实identity/current-Owner变化，404不可武断区分“目标删除”与“Project失权”，隐藏不可确认内容并经Owner显式重读再决定 |
| 原key发生异义 | 409 IDEMPOTENCY_KEY_REUSED，单独冲突态：禁原重放/自动新key/把别的receipt当本意图确认；既往未知不得降为未执行 |

503 DEPENDENCY_UNAVAILABLE、500 INTERNAL_ERROR、未知code/status或其它未列配对即便携not_started，也不在本稿首次拒绝闭集；保守待决并提供原恢复。只读和lookup的失败不产生新mutation intent；对既有intent只更新观察错误。任何一次之后的拒绝都不能抹掉此前不确定。

Unknown、超时、断线、body截断、错200/DTO/媒体、写后响应不能完整验证均保原intent不确定。此前不确定有粘性：后一次拒绝/未观察/新GET相等/资源404都不能改成“从未执行”。IDEMPOTENCY_KEY_REUSED禁止自动换key与原重放，保说明并只许明确放弃追踪；不拿另一body历史当成功。普通读Unknown只属读取失败，无写intent或自动lookup。

| 恢复请求 | 页面精确解释 |
| --- | --- |
| 配置lookup found=false | 本次未观察到，非回滚证明；保原intent |
| 配置lookup found=true | 六kind、receipt闭合并与已知原目标/版本可比字段校验；是该命令identity的历史观察，不验证原input semantic；**不作为严格Execute确认，不自动推进后续操作** |
| Credential lookup observed=false | 同样非回滚证明，不代表可安全换key |
| Credential lookup observed=true | 历史Mutation按kind/ref/version/deleted校验；不验证完整value，不清材料、不自动发Provider或宣布材料同义确认 |
| 用户显式原重放 | 同完整当前identity/原CSRF、原P/path/kind/key/body/版本/候选/材料完全一致，走原Execute；不先用当前GET/metadata/候选存在性阻断历史，仍由服务器授权/semantic判断 |

没有任意key查证控制台、自动轮询、自动retry、新key修复、查证后自动重放。只有原Execute的严格成功DTO才能确认对应写；create版本1，update/delete正确target及expected+1、kind/deleted/affected严格。损坏lookup只是本次观察失败，原写仍待决；不能造in_progress或借Model锁等待超时输出not found。

| 当前Project事实 | 配置六写 | Credential三写 | 两类lookup/metadata/配置读 |
| --- | --- | --- | --- |
| active且当前Owner/Session有效 | 允许新写/合法原重放，库终局判定 | 允许新写/合法原重放，Purpose/材料由库检查 | 正常只读当前授权 |
| archiving/archived且当前Owner有效 | 禁新草稿提交；**保已捕获原Execute重放**，库Read→历史可返回旧receipt | 禁新写且**禁Execute重放，连已有receipt也先Mutate拒绝**；保待决与显式lookup，不伪称已撤销 | Read允许；Credential observation仍不是材料同义确认 |
| deleting/未初始化/不存在/失Owner/会话撤销 | 不越权恢复 | 不越权恢复 | 保原gate错误，不从历史绕过；清/隐藏按§6 |

UI内存跨checking恢复不扩大为跨新Session恢复；真正新Session/CSRF后旧材料清除。后端同User新Session可能接受历史语义是后端能力，本卡不因此持久保存原请求。

## 8. 唯一产品写域与作者

下表保留31个产品路径及原编号。25个web文件已提交，#24–27四个harness已恢复，README #29需随后续实际验收结果同步。负责人按前端、Go harness、浏览器验证等完整子目标分派唯一写入者并整合；文档与相关实现同次交付，不再设置README末件许可。Git由主线程负责，共享资产／资源指定唯一所有者。除 §0.2 已单独授权的共享浮层修复外，不扩展到公共 Ui／样式、HTTP/schema或生产后端。

| # | 路径 | 最小作用 |
| --- | --- | --- |
| 1 | web/src/api/client.ts | 17封闭Project model操作、路径/请求/成功cap；旧分类不变 |
| 2 | 新web/src/api/project-models.ts | 五读＋六写＋lookup typed输入/完整Project投影 |
| 3 | 新web/src/api/project-model-credentials.ts | metadata/三写/lookup及Project材料界 |
| 4 | web/src/composables/useSession.ts | 追加默认依赖/17 action/本域revision/私有原intent/单owner |
| 5 | 新web/src/composables/useProjectModelSettings.ts | App期Coordinator、三类草稿/恢复、独立页态与确认 |
| 6 | web/src/App.vue | 唯一controller provide/dispose及既有聚合离开确认 |
| 7 | web/src/router/auth.ts | 仅两新Project合法return后缀 |
| 8 | web/src/router/index.ts | 两受保护Project child，无新全局navigation |
| 9 | web/src/views/projects/ProjectSettingsView.vue | 模型与Provider组/两叶子，general默认与audit位置保持 |
| 10 | web/src/components/layout/ProjectNav.vue | 同Project两后缀设置选中；原href/品牌保持 |
| 11 | 新web/src/views/projects/ProjectModelProvidersView.vue | Providers主面板、详情、内部Models面板入口与Provider删除 |
| 12 | 新web/src/views/projects/ProjectModelsPanel.vue | 全Project Model列表、Provider归属、创建/编辑/删除入口 |
| 13 | 新web/src/views/projects/ProjectAvailableModelsView.vue | 七字段安全只读目录 |
| 14 | 新web/src/views/projects/ProjectProviderEditor.vue | 受控Provider表单/凭据ref选择，无自动跨命令写 |
| 15 | 新web/src/views/projects/ProjectModelEditor.vue | chat及原policy支持的能力表单 |
| 16 | 新web/src/views/projects/ProjectModelCredentialEditor.vue | metadata、独立create/rotate/delete、write-only输入 |
| 17 | 新web/src/views/projects/ProjectModelDeleteDialog.vue | 捕获目标/版本、nullable候选、安全删除说明 |
| 18 | 新web/src/tests/project-models-client.spec.ts | 五读/配置请求、Project scope/目录/8MiB/lookup |
| 19 | 新web/src/tests/project-model-credentials-client.spec.ts | 400KiB/65536B/Max−1/Mutation/Observation |
| 20 | 新web/src/tests/project-model-settings-state.spec.ts | 真实Session/workspace factory、原请求/身份/尾部/归档差异 |
| 21 | 新web/src/tests/project-model-settings.spec.ts | App/router/组件与原聚合确认/焦点/分页 |
| 22 | web/src/tests/authentication.spec.ts | 两后缀合法及绕过拒绝代表 |
| 23 | web/src/tests/project-workspace.spec.ts | 原设置菜单期待的精确兼容，不改Owner写契约 |
| 24 | 新tests/account/project_owner_models_web_fixture_test.go | 真实root/私有dist/正式事实、安全body/有界私有控制与actualjoin |
| 25 | 新tests/account/project_owner_models_web_test.go | 六完整新top及证据键，旧top原样 |
| 26 | 新tests/account-captcha-web/project-owner-models.config.js | 六case/单worker/零retry/45s/固定工具与私有输出 |
| 27 | 新tests/account-captcha-web/e2e/project-owner-models.spec.ts | 六真实UI场景、同body/schema/client及8图 |
| 28 | web/src/tests/session.spec.ts | 本域与旧mutator单owner/安全错误隔离兼容 |
| 29 | docs/development/frontend/README.md | 随实现及验收同步能力、边界和真实命令 |
| 30 | web/src/tests/project-audit.spec.ts | 旧 Audit 设置菜单期待仅追加模型与 Provider，保留其余断言 |
| 31 | tests/account-captcha-web/e2e/project-owner-audit.spec.ts | 同一旧 Audit 菜单期待的真实浏览器兼容修正，保留全部导航断言 |

workspace仅消费公开currentReadContext/detail，不新增workspace写域。System API/composables只读，纯类型/标量工具能原样复用才引用，不能导出System callback或调用其HTTP。默认不改ProjectGeneral/ProjectWorkspaceView、原account TestMain、test-objects/security/postgres脚本、除#31外的旧browser spec、公共fixture、schema/contract/store/root/迁移/锁文件。旧测试菜单期待兼容仅限#23、#30与#31；其它必要修改先交直接负责人评估，跨范围变化由主线程协调。

菜单兼容补充已交付：#30仅在原设置菜单组的期待数组追加“模型与 Provider”，保留其余断言及安全／权限边界。旧完整单元2626 PASS／2 FAIL与修后该文件49/49 PASS按版本组合保留；此结果只支持菜单兼容，不代替剩余真实验收。

## 9. 必要验收与执行前提

前端执行者按任务读取design/vue-development/vue-testing-best-practices；Go harness读取Go技能，独立验证者读取verification，真实browser使用仓库测试工程技能。先核实 §0 的恢复输入，以当前Git基线、限定diff和停止写入状态交审，记录实际工具、锁文件及必要依赖；不要沿用已缺失的scratch或未经核实的执行状态。

离线：新增两个API、state、App/router组合和#22/#23/#28；完整`npm run check --prefix web`及私有outDir生产build。Go两新源须完成精确integration/race compile/vet与six-top discovery，再在负责人安排的隔离资源中实际验证；编译/list不是业务通过。只核必要依赖与真正新增的运行时imports，不复制System/Audit依赖图或另生成全闭包manifest；已有检查在相关输入未变时复用。

pure/controlled必须覆盖：17个request分类；全Project models query拒provider_id；三个Page和详情正确scope/ID/最后项/完整EOF；七字段目录敏感字段和值canary拒绝且无System请求；五读合法>600000B、8MiB边界/cap+1/实际reader取消join，合法大reasoning数组与非法最后项；1KiB写/lookup cap；Credential最大decoded65536B含末字节/合法最坏转义/raw cap、不得泄漏；MaxInt64配置与CredentialMax−1不同界；typed输入已冻结后caller改对象无效；两协议及所有当前写policy反例；两个lookup union不可互用；Model/Secret/Owner三种恢复差别；首次拒绝与未知粘性、key-reused、坏写receipt、lookup异常、已确认后GET失败；same-identity checking、变Session/CSRF、换Project/旧名复用、晚401/403/ finally、真实transport/body/cancel hold使所有旧操作不能抢owner；App旧Selection/Summary两个未决域与新域聚合确认互不默认清除。

拟六个新top，每轮一个精确锚定名称：

| top | 真实代表与最小证据 |
| --- | --- |
| TestAccountProjectOwnerModelsWebConfigurationLifecycle | 正常Owner经默认root创建/改Provider与chat Model、两协议/type/provider不可变、enabled与目录重读、空Provider删除及有Model拒绝；配置事实/receipt/引用与Audit按正式生产者 |
| TestAccountProjectOwnerModelsWebCredentialLifecycle | create→仅ref准备→显式Provider绑定、metadata/rotate、被引用delete拒绝、显式解绑后delete、Credential已确认但Provider失败的保留与后续Provider单步重试 |
| TestAccountProjectOwnerModelsWebOriginalRecovery | 严格目标私有proxy丢/截已完成安全响应，配置与Credential各至少一个真实原请求恢复；lookup仅观察且零隐式write；至少一次资源已删后的原DELETE重放，不以新GET修复；原请求bytes等值只报布尔/长度，不公开body/key/material或其digest |
| TestAccountProjectOwnerModelsWebReadAndPagination | ≥26Providers、跨≥2Providers的≥26Project Models、混System/Project的≥26available；逐页当前权限、无provider_id、无System补读、详情、disabled配置与目录区别；坏页/大页的受控证据不冒真实PG大页 |
| TestAccountProjectOwnerModelsWebAuthorityAndIdentity | 非admin Owner成功、另一admin/另一Owner拒绝；会话checking/恢复、当前revocation与项目切换、辅助archived读/新写拒绝；配置原重放与Credential仅lookup差别；带reference原DEPENDENCY_UNBOUND无Agent rewrite |
| TestAccountProjectOwnerModelsWebNavigationAndLayouts | Providers/Models面板/available实际导航、dirty/待决/部分成功离开确认、键盘/焦点；浅深×1440x900/390x844×正常/减少动效共8图（凭据输入已清空且模态安全退出后） |

root/fixtures须使用正式Account/Project/Secret/Model API和当前同一Authority。Project持久创建可沿已接受私有Skills fixture；辅助归档状态与私有隔离reference准备均准确标明，不证明生产Skills、完整归档执行链或Agent adapter。Model引用负例按原完整scope/target谓词设置及恢复，不能改生产consumer或让假adapter成功。控制代理限本轮独立Project、精确method/path/请求token和已注册请求；outerhandler实际终局后才记join，body读完、浏览器EOF、pending请求与事务提交分别观察。所有server/handler/child/控制goroutine/finally必须注册并actual join；无宽泛截获、网络I/O绕过或重复强制终局。

真实schema对安全原response bytes及实际method/path/status/Content-Type/Content-Length/X-Request-ID/目标/run/input绑定，以已固定标准Python/jsonschema本地refs核；同原body还经原生browser内的新client解析，不用手造重编码样本。日志/截图/trace/video/证据不得采集Credential材料、原key/body、Cookie/CSRF、登录材料、DB协议帧；合成配置/capability仅在可公开fixture安全集合内。禁止在凭据输入/原材料未清时截图。

### 9.1 精确14旧回归及资产 reader

14个受影响旧top的覆盖要求保持；相关输入和依赖可确认未变时复用既有结果，缺证或受影响项各按精确`^名称$`执行，不合并大regex。下表为static selector，不代表当前binary discovery已通过。命令模板为`sh scripts/test-objects.sh -run <one exact anchored selector>`，包`./tests/account`；执行前核实工具、ENV、必要输入和资源所有权。

| 组 | 精确selector | tests/account来源 | 资产 | 必要理由 |
| --- | --- | --- | --- | --- |
| auth-lifecycle | `^TestAccountAuthenticationWebSessionLifecycle$` | `authentication_web_test.go:11` | global | 原bootstrap/Login/Session/Logout及两条正式审计事实；第15依赖不能改变省略调用。 |
| auth-revocation | `^TestAccountAuthenticationWebRevocationAndExpiry$` | `authentication_web_test.go:51` | global | 原revocation与expiry两subcase；失效后隐藏受保护内容，两个browser各45s仍共用原120s top。 |
| owner-edit | `^TestAccountProjectOwnerWebEditAndRename$` | `project_owner_web_test.go:46` | owner-private | 原更新、版本冲突、rename与dirty-navigation；两后缀和App新确认不能损坏基本资料。 |
| owner-recovery | `^TestAccountProjectOwnerWebOriginalRecovery$` | `project_owner_web_test.go:50` | owner-private | Owner三态lookup/原PATCH与新Model两类历史观察区分，保原key/body与local-abandon。 |
| owner-identity | `^TestAccountProjectOwnerWebIdentityAndOwnership$` | `project_owner_web_test.go:54` | owner-private | 同Session checking、换Session/Owner、late-read、aggregate与正式Logout。 |
| audit-authority | `^TestAccountProjectOwnerAuditWebAuthorityAndRecovery$` | `project_owner_audit_web_test.go:151` | audit-private | 旧独立Audit action/revision及actual body/cancel尾部与新域单owner互不混淆；依Audit最终accepted版本重绑。 |
| audit-navigation | `^TestAccountProjectOwnerAuditWebNavigationAndLayouts$` | `project_owner_audit_web_test.go:154` | audit-private | 原settings-current/default-general/raw-return/focus与既有写草稿guard；两新leaf不能改Audit与ProjectNav。 |
| provider-recovery | `^TestAccountSystemProvidersWebOutcomeRecovery$` | `system_providers_web_test.go:37` | global | 现top同时有credential_replayed/provider_replayed/historical_only/confirmed_read_failure，足够作旧System凭据＋Provider恢复代表。 |
| model-recovery | `^TestAccountSystemModelsWebOutcomeRecovery$` | `system_models_web_test.go:41` | global | 原create/update/delete重放、历史lookup与删后原请求，保System DTO及容量分类。 |
| selection-recovery | `^TestAccountSystemModelSelectionWebOutcomeRecovery$` | `system_model_selection_web_test.go:40` | global | accepted-cut、lookup-only、old404、same-original；新的Project配置不得误用selector command。 |
| summary-recovery | `^TestAccountSystemMeetingSummaryWebRecovery$` | `system_meeting_summary_web_test.go:30` | summary-private | 真实双intent/cross-key/local-abandon保持，防新域清Summary或旧Selection未决材料。 |
| summary-authority-navigation | `^TestAccountSystemMeetingSummaryWebAuthorityNavigation$` | `system_meeting_summary_web_test.go:33` | summary-private | 现有aggregate/checking/new-session/current403/logout/owner/focus，覆盖App和旧两个intent的聚合确认。 |
| personal-theme | `^TestAccountPersonalSettingsWebThemeAndNavigation$` | `personal_settings_web_test.go:43` | global-via-auth | App theme preview/cancel、dirty-menu/back/logout与同Session checking的最小旧personal代表。 |
| system-audit-authority | `^TestAccountSystemAuditWebAuthorityAndOwnership$` | `system_audit_web_test.go:41` | global | 旧admin只读分支代表：ordinary-forbidden、两GET、cancel-join/cross-domain/logout/late-isolated；防新Project分支吞旧System默认分类。 |

旧System Provider Outcome已含credential与Provider原请求恢复，不默认再跑其整6组；Model/Selection同取恢复代表。Summary Recovery保双intent、AuthorityNavigation保aggregate，不能只留一个。Owner新增OriginalRecovery是为新恢复分类不能吞旧committed三态。SystemAudit取一个只读admin/actual-tail代表；Runtime专用分支、leaf和导航若最终未改，不另加其两top；仅当最终diff修改runtime-information专有operation/cap/failure分支，才回报并重绑`^TestAccountSystemRuntimeInformationWebReadAndAuthority$`；仅当修改该leaf、其owner导航或busy-pageshow语义，才回报并重绑`^TestAccountSystemRuntimeInformationWebNavigationAndLifecycle$`。这两项沿原selectors c36bed42的conditional_not_scheduled，仍未排入14轮，不由本卡自动增加执行。全旧pure check仍按本节执行。

Owner三轮用`AGENTEAM_PROJECT_OWNER_WEB_DIST`；Audit两轮用`AGENTEAM_PROJECT_AUDIT_WEB_DIST`；Summary两轮用`AGENTEAM_MEETING_SUMMARY_WEB_DIST`。其余7轮读旧global dist（personal经authentication fixture）。新6轮只私有dist。负责人须让这些asset reader使用同一个已审新build；global资产指定唯一负责人执行备份／交换，并在最后reader实际退休后恢复，不能从ENV存在推断私有支持。独立driver若仍逐组校验global/private相等，也算global reader。

新case45s、workers1/retries0；每Go top120s含cleanup、包6m、TCP尾75s、fresh≥5GiB。每轮实际4容器＋3网络7个ID，direct/adopted actualwait、watchdogjoin、两次精确资源不存在/owned runtime/进程与TCP观察按旧正式driver验；现有root/innerjoin限制不声称修复。共享资源串行，失败保留并实际退休后再由负责人安排受影响修复／复验，不自动续跑。新Go私有dist；旧组若读global dist，由资产唯一负责人交换/备份/最后reader退休后恢复，禁止并行改资产。单独独验至少配置/凭据恢复一个组合与当前权限/归档一个不同构造；审查者未参与实现，限定复用已验HTTP语义，不为UI重跑全后端套件。

### 9.2 每轮预算、独立验证与执行件

六新top每轮1case、workers1/retries0、browser45s含同body检查；Go top120s含cleanup，建议准备35+browser45+终局事实10+实际cleanup30秒。总包原6m，TCP尾75s，fresh5GiB；每轮4容器/3网络共7ID，direct/adopted实际wait、watchdog实际join，两次exact absent/owned与runtime/browser-runtime空。14旧轮原budget/内部subcase保持；AuthRevocation两case共用同120s，不能倍增。IPC单ack8s计入45s，不加额外case时间。预算耗尽只能FAIL/完整退休后STOP，不能先pass后清理或自动续轮。

负责人统一安排资源／Go-cache／asset窗口，共享部分串行；按6新top→14必要旧top核对剩余覆盖，每轮实际退休后再继续，已通过且输入未变者不机械重跑。独立至少再2轮：不同构造的配置＋Credential Unknown/原Execute组合，以及current权限＋归档两恢复路径组合；其selector/source须由独立作者恢复并核对，不借实现者结果预填。外层driver/watchdog、必要输入与隔离资源检查须在运行前可用；当前缺失输入不满足执行前提。

## 10. 私有 harness 协议与精确 DTO

协议仍为 `project-owner-models.v1`。本节完整组合已审 T3 rev2 的精确字段，补足原 rev1 抽象描述；没有第18业务操作或第10IPC；产品范围以§8的31路径为准。下列 typed 代码是规格，不是 TypeScript/Go 实现或已编译 schema。

所有下述对象均恰含所列字段，必需 nullable 用显式 null，不用缺字段；重复/未知键、第二个JSON值、坏UTF8及非法 union 拒绝。`ID`=正式小写 UUIDv7；`Version`=1..MaxInt64 的 canonical decimal string；`DBCount`=0..MaxInt64 decimal string；`Count`=非负安全整数。配置 expected 可到 MaxInt64，凭据 expected 只到 MaxInt64−1，沿正式写 DTO。Token 是本 case Go 登记器按序产生的 `r000001`/`a0001`；与 input_hash 一起解释，重启/另一case无效，不取自 key、body 或任何digest。

### 10.1. material：安全 ID 期望与原登录 bootstrap

material 顶层仍恰 `protocol,input_hash,mode,actors,projects,expected,system`。actors 固定 `owner,other_owner,other_admin`，每个仍恰 `email,password,user_id,username`。这只保留原私有0600登录bootstrap文件及原退休删除规则，不能复制进证据；**Credential value、原写body/key及其digest从不进入material或任何其它文件**。原 fixture 登录密码和产品待写 Credential 是两种不同材料，不能用“私有文件”给后者新增落盘许可。

Node 每 case 在内存生成 Credential value→实际受保护输入→由产品正式HTTP提交；代理只在受控内存捕获/比较原字节。不得通过新IPC送值、绕过UI填充、存入trace/video/截图/console/error、`expect(value)`差异输出或digest。填充失败也必须把可能含 fill 参数的 Playwright 错误净化为固定安全消息；只在受保护值已清空、材料对话框关闭后拍图。不承诺JS string/Go decoder所有内部副本可可靠擦除。

```ts
type Mode = 'configuration'|'credential'|'recovery'|'read'|'authority'|'navigation';
type ProjectKey = 'main'|'second'|'other'|'admin_owned'|'archiving'|'archived'
 |'deleting'|'pending'|'config_recovery'|'credential_recovery'|'referenced';
type ProjectLocator = { id:ID; username:string; name:string; normalized_name:string;
 owner_user_id:ID; initialized:boolean; lifecycle:'active'|'archiving'|'archived'|'deleting' };
type ProviderRef = { id:ID; project_id:ID; name:string;
 protocol:'openai-chat-completions'|'anthropic-messages'; version:Version; credential_ref:ID|null };
type ModelRef = { id:ID; project_id:ID; provider_id:ID; name:string; version:Version };
type CredentialRef = { project_id:ID; credential_id:ID; purpose:'model'; version:Version };
type SeedRefs = { providers:ProviderRef[]; models:ModelRef[]; credentials:CredentialRef[] };
type DirectoryRef = { id:ID; provider_id:ID; scope:{kind:'system'}|{kind:'project';project_id:ID};
 name:string; provider_name:string; version:Version };
```

`ProjectLocator` 保原7字段，不添猜测版本；archive 控制的 expected_version 唯一取自下一节 `snapshot.project.version`。Provider/Model Ref 只作 fixture 期望，不取代原HTTP完整安全DTO；DirectoryRef 是期望用安全子集，不是七字段目录响应的新版本，正式 `capabilities` 仍由实际HTTP＋公开client/schema验证。禁止从System Get补目录字段。

projects 与 expected.projects 的键集按mode精确相同：configuration/credential只有main；recovery为main/config_recovery/credential_recovery；read/navigation为main/second；authority为全部11个ProjectKey。值分别为ProjectLocator/SeedRefs。所有Ref所属Project、Model.provider_id与相应Provider、Provider.credential_ref与相应Credential须一致；ID不得重复，数组按ID排序。空数组为[]，不是省略。登记集只包含该case正式准备得到的safe ID/版本及后述正式create结果；不允许任意新ID/SQL/URL。

expected 是以下闭合联合：除read外恰 `{projects:<上述精确键表>}`；read恰 `{projects,pagination}`，pagination恰 `{provider_ids,model_ids,available}`，前两数组分别恰26个不同本main ID，available恰26个DirectoryRef且含本main和system两scope。Models的26条来自两Provider；完整顺序/分页取实际GET结果核验，ID数组只作覆盖集合，不能编造排序列。所有正式DTO、版本及名字仍由同root准备实值填入，不复制大配置/options/overwrite/efforts/材料。

system 在非navigation模式必须null；navigation恰：

```ts
type SystemDraftFacts = {
 selection:{ initial:{id:ID;version:Version;configured:
   {embedding:ID;memory:ID;reranker:ID|null;image:ID|null}|null};
   draft:{purpose:'memory';model:{id:ID;name:string}} };
 summary:{ initial:{id:ID;version:Version;model:ID|null}; draft:{model:{id:ID;name:string}} };
};
```

只给现有System Selection与统一Meeting Summary两表单的安全初值和一个不同的可选项，测试草稿/确认隔离，不新增Project selector、独立initial/update字段或Summary override。navigation的owner登录身份必须经正式账户准备就是admin且确为main/second Owner；实际Session响应再核role/user，禁止改role SQL或在组件里豁免权限。非navigation原ordinary Owner代表保持。

### 10.2. snapshot：当前资源、历史提交、原请求比较分开

`snapshot` args仍恰 `{project:ProjectKey}`，必须是本mode已登记key。只读同root Store、同一有界read Tx内的本Project安全列；结果只能在该Tx成功提交且ctx有效后发布。无额外HTTP、自动lookup/Execute、授权替代、后台读取或SQL输入。私有fixture事实不作为产品授权。不得复制旧fixture的 `to_jsonb(row)` 全行快照到ack。

```ts
type ProviderFact = {id:ID;present:true;version:Version;credential_ref:ID|null}
 |{id:ID;present:false;version:null;credential_ref:null};
type ModelFact = {id:ID;present:true;version:Version;provider_id:ID}
 |{id:ID;present:false;version:null;provider_id:null};
type CredentialFact = {credential_id:ID;metadata:{credential_id:ID;purpose:'model';version:Version}|null};
type ConfigReceipt = {kind:'provider.create'|'provider.update'|'provider.delete'|
 'model.create'|'model.update'|'model.delete';resource_id:ID;version:Version;affected_references:'0'};
type CredentialResult = {credential_id:ID;purpose:'model';version:Version;deleted:boolean};
type ReplayComparison = {request_token:RequestToken;
 body_equal:boolean;key_equal:boolean;target_equal:boolean;identity_equal:boolean;method_equal:boolean;
 original_body_bytes:Count;replay_body_bytes:Count};
type OriginFact = {origin_token:RequestToken;original_request_token:RequestToken;
 operation:MutationOperation;project_id:ID;target_id:ID|null;original_body_bytes:Count;
 history:{family:'configuration';committed_rows:'0';receipt:null}
   |{family:'configuration';committed_rows:'1';receipt:ConfigReceipt}
   |{family:'credential';committed_rows:'0';result:null}
   |{family:'credential';committed_rows:'1';result:CredentialResult};
 comparison_count:Count;comparison:ReplayComparison|null};
type SnapshotResult = {
 project:{project_id:ID;version:Version;initialized:boolean;lifecycle:ProjectLocator['lifecycle']};
 current:{providers:ProviderFact[];models:ModelFact[];credentials:CredentialFact[]};
 history:{configuration:{committed_commands:DBCount;audit_records:DBCount;events:DBCount};
   credential:{committed_commands:DBCount;audit_records:DBCount}};
 reference_presence:{models:{id:ID;present:boolean}[];credentials:{credential_id:ID;present:boolean}[]};
 origins:OriginFact[];
 fixture_only:{archive_recovery_applied:boolean;reference_fact_state:'present'|'absent'|null;
   rename_reuse_applied:boolean};
};
```

current只返回登记ID；不存在时null/false不能推成历史删除成功。Credential没有canonical行就metadata=null，不能补造deleted Mutation；deleted只来自正式安全历史result。history中的配置计数严格本Project `agenteam_model.commands.phase='committed'`、Audit/Outbox `producer='model'`；Credential计数严格本Project Secret receipt及 `producer='secret'` Audit。Nonce/payload/prepared计划不属于这些提交计数，拒绝时不能要求整个DB零变化；也不把Credential说成产生Model/Project事件。两族当前/历史不可混淆，HTTP尝试和POST lookup次数不进这些计数。

每origin历史仅查其私有内存内正式identity：配置namespace model.project/owners=[Project,Human]，Credential secret/相同owners及正式digest计算；只取安全receipt列。0行不证明原Unknown回滚，committed_rows=1须有同族完整receipt/result，否则fixture_failed/无候选。各Project的Audit/Event总数是明确的同scope总数，不能冒充逐origin Audit关联；测试比较串行步骤前后差量，并另核该origin唯一receipt。引用查询只本Project/登记Model或Credential，不能读全库ref；fixture_only三项单列，不宣称真实归档或引用owner adapter绑定。

origin_token **等于首个由arm选中的mutation真实original_request_token**，最多4个。每个 `(ProjectKey, operation, target_id)` 只允许一个selected origin；collection create的target_id=null仍只一项，避免根据key“匹配”而漏掉换key。后续同精确端点请求只成为比较候选，不依赖key相等来决定配对；这不是同command identity的证明，更不能把正常新意图写冒称原重放。受控case须用既有不同recovery Project/operation隔离同端点其它合法新意图，并结合key/body布尔及明确UI原Execute动作判断。只有两份完整有界body、header key、登记身份均实际捕获后才产生comparison，尚未比较为null。latest comparison＋comparison_count足够逐步骤检查；不存重放body历史。若同tuple需要第二个独立origin则本卡该case不能猜关联/覆盖，须由直接负责人核对既有case安排；涉及协议／预算变化再交主线程协调，不增加IPC动作或产品能力。

原body通过正式请求被实际消费时的有界tap收集，不预读后重构请求；原/重放UTF8 bytes与header key原样比较，不trim/排序/remarshal。identity_equal指原命令的稳定Human+Project+namespace+kind，Session不是命令identity组成部分；仅来自已登记正式Login/Session的私有关联，不接受caller声明。未知身份/不完整捕获的comparison必须null并让要求该证据的case失败，不能false冒已比较。该布尔不是当前授权判定；同User新Session可原同义重放，当前Session有效性仍由真实请求验证。Secret材料和request/key/digest仅内存，不进snapshot/日志/sidecar。

历史found/observed、current GET相等/404、snapshot已提交都不能令UI确认/清材料；仅显式原Execute严格成功才可。archiving/archived上配置可原同义Execute，Credential被动lookup可见但原Execute仍Mutate拒绝。失败/Unknown不自动重key、补GET或后台重放。

### 10.3. 精确IPC与请求token、终局

request仍恰 `{protocol,input_hash,sequence,action,args}`，严格next sequence1..128；ack仍恰 `{protocol,input_hash,sequence,action,ok,result,error}`。ok=true时error=null、result是该action精确DTO；ok=false时result=null，error仅固定 `invalid_envelope|invalid_sequence|invalid_action|invalid_arguments|unknown_target|arm_busy|token_mismatch|not_ready|budget_exhausted|fixture_failed`，无message/stack/SQL。未知/错误控制置安全失败标记并触发原已注册退休，不能继续装作PASS。8KiB request/64KiB ack/8s ack界原样保留。

17 operation literal及方法/路径/target/query条件见[唯一精确端点附件](d27-project-owner-model-settings-ui-endpoints.json)（原字节6c85ae88）；只再允许既定私有 `getCurrentSession` GET。Project target只能material已登记ID或**完整正式create安全结果中产生的新ID**；后者在施加cut/disconnect之前原子登记，即使browser没收到也能供后续原请求/metadata精确控制。必须同时核scope/operation/receipt类型和resource ID，不能据任意JSON `id`登记。分页cursor同样只取本case完整正式safe页，仍通过原cursor校验。

arm args精确 `{operation,project,target_id,query,effect}`，所有字段必有；Session为project/target_id/query=null；17端点中的collection无target，detail必登记target；非list query=null。list query是`CanonicalPageQuery`字符串：只含0/1个cursor、0/1个limit，limit1..100，cursor取上述登记值，按正式编码、无重复/额外key/ForceQuery；记住此原字符串并与下一真实请求RawQuery逐字比较，不能放宽为任意query字典。所有canonical path由操作表＋登记ID生成，无URL/method/header参数。

```ts
type Effect = 'before_dispatch_hold'|'after_complete_hold'|
 'after_complete_cut'|'after_complete_disconnect';
type ArmResult = {arm_id:ArmToken;state:'armed'};
type ControlState = {arm_id:ArmToken;request_token:RequestToken|null;
 origin_token:RequestToken|null;
 state:'armed'|'claimed'|'upstream_complete'|'released'|'joined';
 held:boolean;release_requested:boolean;upstream_complete:boolean;safe_admitted:boolean;
 effect_applied:boolean;joined:boolean};
type ReleaseResult = {arm_id:ArmToken;request_token:RequestToken;release_requested:true};
```

一次只有一个未实际终局的arm，且只claim下一匹配browser请求一次。before_dispatch_hold仅六read/Session；after_complete_hold仅六read/九mutation；cut/disconnect仅九mutation且必须先收到200完整、严格绑定的safe final receipt/result，不控制lookup或伪造COMMIT Unknown。此处read精确指六GET；两个POST lookup不在故障effect域，仍正常正式执行/计数/安全取证。Session只before_dispatch_hold、不留其body。arm不命中须仍有原界内取消/退休；不能给handler/body另加预算。

control-state args恰 `{arm_id}`；token由该真实outer handler入场时登记，arm无请求时null。state是派生快照，不能假设统一顺序：joined优先，其次release_requested，其次upstream_complete，再claimed/armed；例如取消可令joined=true而upstream_complete=false。held表示该token实际进入hold，safe_admitted只表示完整上游通过安全证据准入（Session固定false），effect_applied单列。任何字段都不声称浏览器收到、Tx提交或Cookie owner已释放。

release args恰 `{arm_id,request_token}`，两者必须匹配且已进入hold；重复相同release可返回同一收取事实，但不能释放下一arm、null或wildcard。ack只表示释放许可；随后必须从同一control-state观察joined=true，并对实际hold核held恰增1、held_joined==held，最后server_finished==server_started。只有D1已接受的outer handler返回/unwind（ReverseProxy body/write/ErrorHandler实际退出后）可以记joined；失败/Fatal之前注册所有release+join，不在ctx.Done/Close/release处提前加数。

`counts` args={}，result精确 `{operations,session,server,controls,browser_eof,schema_bodies,client_bodies}`：operations是按endpoints表顺序恰17行 `{operation,setup,browser,control,upstream_complete,handler_joined}`（Count）；session是 `{setup,browser,control}`；server是 `{started,finished}`；controls是 `{armed,claimed,held,held_joined,cut,disconnected}`。后三个browser_eof/schema_bodies/client_bodies **固定null**，Go控制器没有其观察权。总数分source、mutation和lookup由精确operation表归类，不与提交计数混用。

Node独立保持native EOF/typed client/schema验证记录，final_result的counts精确 `{server:<最后counts结果>,browser:{attempts,complete_eof,typed_client_ok,schema_ok,incomplete}}`（均Count）；旧顶层schema_bodies/client_bodies须与此一致。无第10个“browser上报”IPC。Go最终只验证Node原子result文件与已取证字节/实际child终局，不能在counts IPC伪造这些数。server.joined、native body/cancel/finally的Cookie owner、snapshot读Tx提交是三个独立事实，禁止建立“owner必持至server.joined”的假因果。

其余五action保持原精确形状：

| action | args | result及界 |
| --- | --- | --- |
| snapshot | `{project}` | 上述SnapshotResult；同Tx、完整输出后发布 |
| logout | `{session_id}` | `{session_id,revoked:true}`；只能已登记正式browser Session，调用正式Logout，不写revoked_at |
| archive-recovery-project | `{project:'config_recovery'\|'credential_recovery',expected_version}` | `{project_id,initialized:true,lifecycle:'archived',fixture_only:true}`；snapshot版本作CAS、预登记active→archived辅助事实；不承诺参与者运行或推断version增量 |
| reference-fact | `{project:'referenced',state:'present'\|'absent'}` | `{project_id,model_id,reference_present,fixture_only:true}`；唯一预分配slot，真实delete仍DEPENDENCY_UNBOUND，不改Agent |
| rename-reuse | `{project:'main'}` | `{renamed:ProjectLocator,replacement:ProjectLocator}`；正式Update＋既有私有创建fixture，旧稳定ID不变，新ID与旧名注册给本case，不冒完整创建/生命周期绑定 |

所有snapshot/ack先完整有界编码，超过64KiB失败不截断；ID数组只本case登记集，未知形状或额外材料不先落盘。原sidecar仅已闭集合格的安全response；Login/Session、原body/key及其digest、Credential材料不留证。原45s/120s含cleanup/6m/75s/5GiB/7ID预算保持，共享资源由指定负责人串行管理。

### 10.4 文件、取证、成功键与原预算的闭集

下列对象保留rev1中未被精确DTO补充替换的运行契约；是静态规格数据，不证明driver已恢复。私有路径是现已缺失的历史候选，恢复时使用`output/ai/<task>/`或有界短外部目录并记录实际路径；产品路径以§8的31项为准。`final_result.counts` 使用本节10.3的明确形状，不能再采用旧 counts 描述冒充浏览器观察。原 material/expected/system、各action粗略result描述与旧pending状态不在此重复，已由10.1–10.3和§0明确替换。

```json
{
  "files": {
    "material": "project-models-material.json",
    "request": "project-models-ipc.json",
    "ack": "project-models-ack-{sequence}.json",
    "result": "project-models-result.json"
  },
  "environment": {
    "go_required": [
      "AGENTEAM_AUTH_WEB_RUNTIME",
      "AGENTEAM_PROJECT_MODELS_WEB_DIST",
      "AGENTEAM_PROJECT_MODELS_WEB_EVIDENCE",
      "AGENTEAM_PROJECT_MODELS_WEB_INPUT_HASH"
    ],
    "node_explicit": [
      "AGENTEAM_AUTH_WEB_ORIGIN",
      "AGENTEAM_AUTH_WEB_PRIVATE",
      "AGENTEAM_AUTH_WEB_CHROMIUM",
      "AGENTEAM_PROJECT_MODELS_WEB_CASE",
      "AGENTEAM_PROJECT_MODELS_WEB_DIST",
      "AGENTEAM_PROJECT_MODELS_WEB_EVIDENCE",
      "AGENTEAM_PROJECT_MODELS_WEB_INPUT_HASH"
    ],
    "navigation_additional": "AGENTEAM_AUTH_WEB_IMAGES absolute path; trace/video off, no automatic failure screenshot with private input"
  },
  "private_paths_proposed_only": {
    "fixture": "tests/account/project_owner_models_web_fixture_test.go",
    "tops": "tests/account/project_owner_models_web_test.go",
    "config": "tests/account-captcha-web/project-owner-models.config.js",
    "spec": "tests/account-captcha-web/e2e/project-owner-models.spec.ts",
    "private_dist_proposed": "/workspace/scratch/project-model-settings-ui/dist-ui01",
    "browser_runtime_parent_proposed": "/workspace/scratch/pmui"
  },
  "ipc_publication": "0600 temp+atomic rename under exact task directory; no symlink/traversal; full JSON+EOF, exact members; fixed error enum only and result/error exclusive",
  "ipc_unknown_control": "never dispatch SQL/HTTP or fake an ok; store safe failure flag, cancel owned work and fail after real retirement",
  "proxy_registration_and_join": "register servers, each handler, observer, control loop, Node/schema children, prep Store/root and cleanup before start; held joined only at actual outer-handler return/unwind after ReverseProxy body/write/ErrorHandler. Release/body-close/EOF alone cannot join. Apply the accepted D1 boundary; do not reopen stopped join repairs.",
  "evidence": {
    "source": [
      "setup",
      "browser",
      "control"
    ],
    "filenames": "response-{sequence:03}.json and body-{sha256}.json0600 only after safe response admission",
    "sidecar_keys": [
      "protocol",
      "sequence",
      "source_run",
      "input_hash",
      "source",
      "operation",
      "method",
      "endpoint",
      "query",
      "status",
      "content_type",
      "content_length",
      "request_id",
      "project_id",
      "resource_id",
      "request_token",
      "body_file",
      "body_sha256",
      "body_bytes",
      "transfer_kind",
      "body_stage"
    ],
    "transfer_kind": [
      "forwarded",
      "hold",
      "cut",
      "failure"
    ],
    "body_stage": "complete_formal_upstream; not browser EOF or commit proof",
    "admission": "Only own fixed Project17 endpoints safe typed responses/Problems; synthetic configuration values allowlisted. Unknown shape/echo/private material is failed in memory, never written. Auth/Session/material request bodies excluded. Response metadata/Mutation/receipt safe body is allowed, raw Credential value is not.",
    "browser_correlation": "exact request-token correlation observed privately plus method/path/query/status/content-type/length/request-id and full browser bytes/EOF; upstream-full, browser-complete, controlled-tail and durable Tx are separate sets",
    "schema": "same original safe admitted response bytes, actual operation/status and two formal Project models/credentials schemas with exact local common/security references; fixed Python/jsonschema import bundle; no network resolver",
    "client": "same retained original response bytes passed through actual browser-native new public17 APIs with exact request/response adapter; production request still actual root; replay parser request side effects disabled; no remarshal sample or fabricated DTO",
    "screenshots": "navigation exactly8: light/dark ×1440x900/390x844 ×normal/reduced; capture only after material input cleared and safe modal closed; actual image review needed"
  },
  "checks_by_mode": {
    "configuration": [
      "owner_formal_commands",
      "two_chat_protocols",
      "immutable_provider_protocol_and_model_provider_type",
      "enabled_and_catalog_separate",
      "empty_provider_delete",
      "provider_with_models_rejected",
      "unique_durable_facts",
      "safe_schema_client"
    ],
    "credential": [
      "create_safe_ref_only",
      "explicit_provider_bind",
      "metadata",
      "rotation",
      "referenced_delete_rejected",
      "explicit_unbind_delete",
      "partial_success_retained",
      "original_provider_only_recovery",
      "safe_schema_client"
    ],
    "recovery": [
      "configuration_response_loss",
      "credential_response_loss",
      "lookup_observation_only",
      "same_original_bytes_and_key",
      "unique_committed_facts",
      "deleted_target_original_delete",
      "no_implicit_writes_or_rekey",
      "actual_owner_tail",
      "safe_schema_client"
    ],
    "read": [
      "providers_26",
      "models_26_cross_two_providers",
      "available_26_mixed_scopes",
      "cursor_limit_only",
      "no_provider_id",
      "no_system_detail_bypass",
      "safe_seven_fields",
      "metadata_no_material",
      "explicit_page_recovery",
      "safe_schema_client"
    ],
    "authority": [
      "ordinary_owner",
      "admin_owner_only",
      "other_owner_and_admin_rejected",
      "current_revocation",
      "same_session_checking",
      "true_identity_change",
      "cross_project_and_name_reuse",
      "aux_lifecycle_gates",
      "archived_config_original_replay",
      "archived_credential_lookup_only",
      "reference_unbound",
      "late_tail_isolation",
      "safe_schema_client"
    ],
    "navigation": [
      "two_suffixes",
      "default_general_and_audit",
      "raw_return_rejection",
      "draft_pending_partial_guard",
      "selection_summary_separate_intents",
      "keyboard_focus_drawer",
      "private_input_cleared",
      "eight_layouts",
      "reduced_motion",
      "no_overflow",
      "no_debug",
      "safe_schema_client"
    ]
  },
  "final_result_keys": [
    "protocol",
    "input_hash",
    "completed",
    "mode",
    "checks",
    "counts",
    "schema_bodies",
    "client_bodies",
    "layouts"
  ],
  "final_result_completed": true,
  "final_result_checks": "exact true keys from checks_by_mode; never publish completed before every required check + body validator succeeds",
  "final_result_failure_boundary": "on failure retain evidence already produced; missing finish/schema/client/DOM remain unobserved, not reconstructed as pass",
  "budgets": {
    "workers": 1,
    "retries": 0,
    "browser_case_seconds": 45,
    "top_including_cleanup_seconds": 120,
    "new_top_allocation_seconds": {
      "preparation": 35,
      "browser_including_schema_and_client": 45,
      "final_facts": 10,
      "actual_cleanup": 30
    },
    "package_minutes": 6,
    "tcp_tail_seconds": 75,
    "fresh_disk_GiB": 5,
    "resources_each": {
      "containers": 4,
      "networks": 3,
      "exact_ids": 7
    },
    "ipc_ack_seconds_within_browser": 8,
    "old_groups": "original45s case/120s top retained; auth revocation two browser subcases share one120s top, not two budgets",
    "timeout": "fail closed; registered owned actual waits/joins and both retirement observations still required; exceeding budget cannot pass or authorize successor; the assigned owner serializes shared resources/cache/assets. The outer driver watchdog and required inputs must be ready and reviewed before execution."
  }
}
```

### 10.5 双作者确认与完整恢复矩阵

既有前后端静态确认见[规格验收档](../agent-team/project-model-settings-spec-verification.md)。这些确认补足协议可消费性，不替代 §7 全部错误/Unknown/历史观察/原Execute/归档差异矩阵、§9 pure/controlled与六top、10.4的逐mode检查。

后端三tuple仅说明原 Recovery top 的最低代表可在现有限额中排布；不得用三行替代完整矩阵，也不得认定每个未知/坏receipt/拒绝/身份/放弃场景都已实际覆盖。false lookup、坏receipt、未知粘性、明确拒绝、KeyReused、局部放弃、配置与Credential两类归档差异、Selection/Summary独立intent以及每个原要求全部保留。最多4个selected origin是每case限制，六新top不得合成一个case。未来详细步骤若出现同tuple第二origin或超过4个独立origin，必须向直接负责人报告具体冲突，涉及契约或预算变更再由主线程协调；不得覆盖/重置origin、删场景、扩IPC、扩预算或自动拆轮。tuple仅选择候选，最终还须真实完整key/body/target/identity/method比较与明确UI原Execute动作，不能把正常新意图冒作重放。

静态agreement本身不证明安全准入、材料输入失败净化、原请求比较、原子result与实际join已运行；已获得的有限历史结果与当前未完成范围按§0记录。

## 11. 正式化、唯一写权与交付

无新增用户产品待决。本卡保留一个完整结果、31产品路径（30技术＋README）、17 HTTP operations＝6 GET＋9 mutation＋2 POST lookup、9 IPC、六新top＋14旧selector。UI不发布Audit/Event；配置与Secret事务事实仍由既有正式后端拥有，不加生产后端/SQL/迁移或Invocation适配。

当前接续以§0为准，从已恢复harness与必要构建产物按§9–10补齐验收。主线程负责全局约束、跨任务资源与Git交付；一级负责人自主拆分、整合和组织独立验证，下级可按收益继续委派，模型与运行时约束统一见[团队流程](../agent-team/README.md)。同一文件和共享资源只有一个写入者／所有者；范围内工程问题由直接负责人解决，跨任务契约、预算或产品含义变化才升级。

前端遵[现有开发基础](../frontend/README.md)及[Project设置设计](../../frontend-design/layouts/project-settings.md)。稳定输入使用Git基线、限定diff及停止写入范围；原始日志与截图放`output/ai/<task>/`或必要短外部目录。相关检查输入未变时复用，修复后只重跑受影响检查，权限／恢复等高风险场景由未参与实现者独立验证。失败原件和未验证边界保留，命令与自有资源的结束状态必须实际确认。

必要README、规格和简短台账与实现／测试作为同一完整结果交付，由主线程执行Git；不再另开补哈希、README末件或永久归档任务。既有证据文件原位保留，可用Git文件历史恢复旧协作记录。

本卡不包括Project创建/生命周期/Owner转移、Agent配置或引用替代、非chat Project模型、生产调用/Resolver root/Invocations、平台selector、Project Summary override/默认复制、凭据目录或明文读取、连接测试、Provider discovery、生产SPA发布与E01。Object runtime join、OpenAI tools独立验收、Central SPA concurrent-publication三停止及Jina/Image来源阻塞保持；完整D09/D26/D27/D28与E01不因该卡完成。
