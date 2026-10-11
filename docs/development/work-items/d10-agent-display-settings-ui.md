# Agent 显示名称与标签颜色编辑

状态：2026-10-11 显示名称/标签颜色编辑与显式清空、真实正常链及原丢回执链已有限独立接受，随本次变更交付。完整 Agent 配置与 D10/F1 未完成。

本片沿 `bd1c8b5f` 的既有 Agent 配置编辑入口，仅增加当前 Human Owner 对 `display_name`、`tag_color` 的编辑及显式清空。契约来自 [Agent 配置卡](d10-agent-configuration.md)、[Agent OpenAPI](../../../api/openapi/agent-configuration.json)与[项目设置布局](../../frontend-design/layouts/project-settings.md#3-agent)。不增加后端能力、公共 Session owner、路由或依赖。

未触达字段省略；显示名称及颜色输入清空后保存传 `null`，有值时保留原文。显示名称为 1–256 Unicode scalar、最多 1024 UTF-8 字节，非全空白且无 Cc；颜色只允许规范小写 `#[0-9a-f]{6}`。保存固定原 Agent、原 `expected_version` 与原 key，Unknown 只查原完整意图；冲突保留草稿与旧版本，须明确取消后重读。现有安全目录摘要、配置回执发布与用户刷新触发 Get/List 保持原逻辑，历史回执不构成当前事实读取证明。

唯一写域为 `web/src/api/agent-configuration.ts`、`web/src/composables/useProjectAgents.ts`、`web/src/components/agents/AgentEditForm.vue`、专属 `web/src/tests/agent-display-settings-*.spec.ts`、本卡和本树 `.agent-state/current.md`。作者 `agent_display_settings`；Git、独立验证和真实浏览器窗口由 root 调度。

验证范围：客户端严格字段闭集、三态/原文/边界及回执匹配；真实 Session 的原 key/version/patch Unknown 恢复；页面共享控件的回填、修改、清空、取消与冲突保留、后续 Get/List 回读。使用锁定依赖离线单 worker，不改锁文件或通用 harness。作者基础检查后冻结，由未参与实现者审查产品、fixture 方法及实际浏览器/资源结果；接受范围见下文。

本片不完成完整二十字段编辑、F1、D10 或 E01。生产 Project initializer 未绑定、`ready=false/503` 与 Object Runtime join、OpenAI tools、Central SPA concurrent publication、Jina/Image STOP 保持。

## 作者基础验证

在本树运行 `./web/node_modules/.bin/vitest run --root web --maxWorkers=1 --no-file-parallelism src/tests/agent-display-settings-page.spec.ts src/tests/agent-display-settings-client.spec.ts src/tests/agent-configuration-client.spec.ts src/tests/agent-configuration-state.spec.ts src/tests/agent-configuration-page.spec.ts src/tests/agent-configuration-session.spec.ts src/tests/agent-directory-client.spec.ts`，`frontend02` 为 7 文件 / 38 测通过（14.51s）。两份专属文件含 9 测：严格 nullable capture/receipt、实际 Session 原意图 Unknown 与共享控件正常/冲突/清空/刷新；页面服务使用受控 typed 结果，不代真实后端或浏览器。

原 `page01` 的 3/5 失败保留：作者初版把两个回填属性误入目录摘要，编辑未回填；已移回 `openEdit` 并经上述复验，目录仍仅九字段。原日志在 `output/ai/agent-display-settings/checks/`，可按命令再生；该目录不入 Git。Node v24.19.0、Vitest 4.1.11，web package/lock 与 Knowledge 树逐字比对后物理复制私有 node_modules，未运行网络或下载。冻结产品 `04705288` 的 `npm run build --prefix web`（含 `vue-tsc --noEmit` 与 Vite）原命令退出 0，`build01.log` 保留；未重跑输入相同的通过前端矩阵。独立验证与真实 native 已按下文限定结果接受。

## 真实浏览器候选

仅扩现有 `tests/projectvariable/agent_edit_web_test.go` 与 `tests/account-captcha-web/e2e/agent-edit.{helpers,spec}.ts`；复用 `^TestAgentEditWeb$`、原 config、同 App/TLS 桥及资源监督链。正常子例仍三次 PATCH：设置含空格/组合字符的显示名与规范颜色、同值 no-op、明确清空两字段；原 name/description/instructions 既有动作保留。每次保存后由用户刷新读取当前 Get/List，分别与对应已验证 receipt 对齐；清空后目录回退标识名称。原丢回执仅 Lookup 子例保持，不将正常链代作 Unknown 证明。

Go 按原 key/version/完整 presence 与持久 command/canonical/Audit/Outbox 核写入；请求观察使用 `map[string]any` 区分 null 与空字符串，目录增加两字段精确比较。页面只增精确 `GET /available-chat-models?limit=50` 分类以消费当前编辑入口的合法模型读取，仍经原请求响应/原应用消费尾证明；无公共 observer 或 driver 修改。浅深、窄屏、减少动效、键盘 Tab/Enter 与取消回焦沿原场景。

已完成 Go 格式、两 TS 格式、config 解析和两个模式分别 `--list`（各恰 1 项）；该离线列举不启动浏览器、后端或网络，不是业务通过。测试工程依赖和锁文件同 Knowledge 树比对相同后物理复制到本树；产品资产为本树 `web/dist` 的 `build01`。Go compile/list 与 native-display01 已完成；资源与缓存由 root/`runtime_recovery` 交接，原窗口全尾后已明确交给 Task 作者。

## 冻结组合与后继真实运行

产品输入 `04705288`；浏览器 fixture 输入 `e3b5877c`，三文件方法经未参与实现者独审接受。最终两个模式的离线列举原退出均 0，`checks/browser-list02/result.json` 绑定三个实际输入前后相同；Playwright 显示的是转译 JS 的 232/235 行，同包 sourcemap 的源码正文与最终 TS 逐字相等。旧 list01 只保原范围，不用行号混作源码身份。

编译 `./tests/projectvariable`：Go 1.27.1、`-c -race -tags=integration`，源为 `e3b5877c`；原 PID 128984 实际退出 0，108.675s，1674 个冻结源码/嵌入输入前后相同。产物 `output/ai/agent-edit-web/compile-display01/projectvariable-race.test` 的原 `-test.list '^TestAgentEditWeb$'` 退出 0且恰一个顶层。`compile-display01/` 留原 argv/env、日志与 terminal；缓存已明确交 Skill 短 helper，再由其交 Task，没有并发双写。

重建使用固定 `/workspace/toolchains/go1.27.1/bin/go`，`GOTOOLCHAIN=local GOENV=off GOWORK=off GOPROXY=off GOSUMDB=off GOTELEMETRY=off CGO_ENABLED=1 GOFLAGS='-mod=readonly -p=2' GOMAXPROCS=2`；当前热 `GOCACHE=/workspace/agenteam-skill-package-http/output/ai/skill-package-http/go-build`，只读模块 `GOMODCACHE=/workspace/agenteam-skills-milestone-resume/output/ai/skills-milestone-resume/go-mod`。新环境先确认可用缓存与唯一 writer；编译和 native 的 GOTMPDIR、XDG_CONFIG_HOME、XDG_CACHE_HOME、DOCKER_CONFIG、TMPDIR、PWTEST_CACHE_DIR 均用任务自有目录。

原 native 入口如下，须在获分配窗口、独立准入以及前一任务的网络/资源/cache 全尾交接后执行，重跑换 fresh namespace。MinIO 使用原 driver 中已锁定的 `agenteam-skills-milestone-resume/output/ai/deps-minio/bin/minio`，不下载或替换。测试源码与原监督链已有可恢复版本，以下输出可按它们再生，不另造框架。

```sh
AGENTEAM_AGENT_EDIT_WEB_DIST="$PWD/output/ai/agent-edit-web/dist-display01" \
AGENTEAM_AGENT_EDIT_WEB_EVIDENCE="$PWD/output/ai/agent-edit-web/native-display01/evidence" \
python3 .agent-state/task-planning-recovery/pg_only_supervisor.py \
  --driver .agent-state/work-owner-http/root_chain_driver.py \
  --binary output/ai/agent-edit-web/compile-display01/projectvariable-race.test \
  --run '^TestAgentEditWeb$' --output /tmp/adw01 --root-chain
```

当前 `native-display01/invocation.json` 固定完整环境/argv、候选摘要与预期两个原子例，`inputs-before.json` 固定原 driver 得出的 2483 个输入；`dist-build01.json` 记录 74 项 fresh dist 与本次 build01 逐项相同。1671 个实际 compile 输入在 runtime closure 中匹配，其余 3 个 cmd 测试仅额外冻结，未编入本测试包。禁止把编译、列举和准入写为浏览器业务通过；normal 新字段与原 lost 旧字段的证明范围分别记录。

## 已接受的真实结果与边界

`native-display01` 原工具会话 3157 实际退出 0，supervisor PID 137213 实际退出 0、147.996s。原 `TestAgentEditWeb` 为 1 top / 2 sub、46.43s：normal `changed-noop-and-clear` 25.94s，lost `lost-update-confirmation-only-lookup` 20.48s。normal 三次 PATCH 精确设置、同值 no-op、null 清空两显示字段，并分别显式刷新当前 Get/List；最终目录回退 Agent 标识名。lost 仍是旧 name/description patch 的一次写后只查询原 Lookup，不冒新显示字段的真实 Unknown 矩阵；新字段原意图恢复另由实际 Session 的受控测试证明。

normal 18 个请求为 **17 finished + 1 PATCH 200 / ERR_ABORTED**，该 703-byte 原请求具完整 EOF/close、公共调用 current/typed digest、原 key/body 及 Go committed 联证；lost 11 个请求为 **10 finished + 1 声明的 PATCH 截断 ERR_CONTENT_LENGTH_MISMATCH**。独立验证核过 normal 15 项 / lost 7 项联合消费证明，不把 response event 或后续读取替代原命令确认，也不写全请求 finished。

两 Node、原 Go、driver/supervisor 与 8 个 adopted Chromium 的原 Wait 均为 0；七个自有资源的 14 次 absence、private/runtime/descendants/HOST_TCP 双尾全部闭合，2483 输入前后完全一致。独立 reviewer `recovery_lead` 未参与实现，已接受产品/fixture 方法、实际业务、资源与六张 PNG 的可见范围；其工具会话退出结论引用作者亲见原终态，不冒 reviewer 亲跑。

六图仅接受实际可见区域：1440 浅色表单包括两个新字段；390 深色清空图只到显示名称，颜色与底部提交未入图；lost uncertain 图为配置中段，Unknown 提示未入图，使用同轮 DOM 断言证明。键盘 Tab/Enter、取消回焦、减少动效和 overflow 有真实操作断言，不扩大为所有二十字段的完整视觉/无障碍审计。

原件在 `output/ai/agent-edit-web/native-display01/`（invocation、terminal、inputs-before/after、evidence）及 `/tmp/adw01/ui-ffa0a2551d5848bf.log`；原 page01 回填失败、旧 list01 范围和仓库既有 Agent 历史 FAIL 保持，不回填。本线已停写产品/fixture，当前无后台进程、cache writer或自有服务，窗口明确交给 Task；Skills 包下载与 Task 历史筛选仍为独立未交付工作，不合并为本结果。
