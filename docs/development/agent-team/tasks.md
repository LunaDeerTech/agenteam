# 团队任务台账

每次仅有一个活动工作项；当前协作规则见团队流程与活动任务卡。新会话先核对实际仓库状态与文件历史，再恢复未完成项。

## AT-0001：建立串行开发团队与技能

- 用户目标：主线程负责讨论、规划、下发与汇报；三个低思考强度专业角色执行明确小任务，在 main 串行开发。
- 方案修订：2（首批加入三个项目技能、Vue 测试技能及已有 Playwright）。
- 基线：`446f4c8`，`main`，开始时工作区干净。
- 范围：团队规范、角色配置、项目技能、技能来源和开发文档入口。
- 既定配置：`gpt-6.1-sol / low`，最多一个子 agent；自动本地提交，推送需明确指令。
- 状态：已验收，随本工作项创建本地提交。

| 子任务 | 执行者 | 状态 |
| --- | --- | --- |
| 01：团队规则与三个角色配置 | `team_setup`，普通 worker 显式指定模型与强度 | 已完成 |
| 02：三个简短项目技能 | 同一执行者，顺序下发 | 已完成 |
| 03：固定版本技能安装、团队文档与来源 | 主线程，必要配置与文档集成 | 已完成 |
| 04：配置加载、技能发现、逐角色只读演练和链接检查 | 主线程与顺序验收执行者 | 已完成，运行证据见初始化验证记录 |

验收：[初始化验证记录](verification.md)。实际角色运行记录确认三个角色均使用 `gpt-6.1-sol / low`；配置加载、技能发现、项目技能结构、外部技能完整性、文档链接和 whitespace 检查通过。本次未运行产品测试、浏览器或数据库验收。

提交结果：完成后由主线程创建本地提交；通过本文件的 Git 历史定位该提交。没有推送指令。

## AT-0002：制定正式开发计划

- 用户目标：先确定基础和模块契约，按依赖逐个完整实现、调试、验收模块，供后续 AI 持续执行。
- 基线：`fe5b833`；本次环境检出 `work`，初始工作区干净，没有切换分支。正式产品开发前由主线程核对团队的 `main` 要求。
- 范围：[开发计划](../development-plan.md)、根 README、团队文档入口和本台账。
- 状态：计划文档已验收，随本工作项创建本地提交；D00–D28 尚未执行。

| 子任务 | 执行者 | 状态 |
| --- | --- | --- |
| 01：读取架构与实现基线，编排模块和验收门槛 | 主线程，规划职责 | 已完成 |
| 02：只读检查覆盖范围、依赖、完成门槛和链接 | `verification_worker`，`gpt-6.1-sol / low` | 已完成，发现 Event Replay 范围歧义 |
| 03：按当前架构修正为失败重投及 canonical rebuild，最终检查 | 主线程，文档集成与审查 | 已完成 |

验收：D00–D28 章节与依赖表一致，29 个工作项均有实现范围和验收要求，依赖无前向引用；相对链接目标检查通过；新文件及 tracked diff 的 whitespace 检查通过。验收发现的通用历史事件回放歧义已依据 `internal-domain-events.md` 第 24 节修正，未新增首版架构范围。

验证边界：仅验收计划文档与链接，没有运行 Go、前端、浏览器、数据库或产品端到端测试，也未实施 D00 的完整契约审计。下一正式开发项是 D00，然后 D01、D02。

提交结果：由主线程创建本地 Conventional Commit，通过本台账的 Git 历史定位；没有推送、发布或部署指令。

## AT-0003：将开发模式与交接规则加入 AGENTS

- 用户目标：在 AI 的仓库入口规范中明确基础先行的开发模式与跨对话交接机制。
- 范围：根 `AGENTS.md` 和本台账；不改变产品代码、架构范围或模块开发进度。
- 交付：模块设计与验收门槛、正式端口与文档归属、新会话必读入口、交接记录字段及中途停止写入要求。
- 状态：已通过 `verification_worker` 独立文档验收，随本工作项创建本地提交。
- 下一步：正式产品开发仍从 D00 开始，D00–D28 尚未执行。

验收：新增规则与开发计划一致，交接字段完整，相对链接与 `git diff --check` 通过。验证边界仅为文档，没有运行产品测试。本地提交通过本台账的 Git 历史定位；未推送。

## AT-0004：D00 设计与实现基线核对

- 用户目标：按规范从首个未完成项推进，完成第一项相关工作；本轮范围为 D00。
- 计划编号：D00；状态：已完成；规格/任务卡：[D00 规格](../work-items/d00-baseline-audit.md)，修订 2；[审计报告](../work-items/d00-baseline-audit-report.md)。
- 基线：`80a9d31`，初始 `work`、工作区干净；主线程创建同提交的本地 `main`，`origin/main` 仍为 `fe5b833`，无用户改动。
- 前置依赖：无。已完成开工核对、规格、D00-01 审计报告、D00-02 只读验收、主线程独立审查及台账同步；随本项创建本地 Conventional Commit。

| 子任务 | 执行者 | 状态 |
| --- | --- | --- |
| D00-01：四类审计清单、源码和环境证据 | `verification_worker`，`gpt-6.1-sol / low` | 已完成，修订 2 |
| D00-02：完整交付的只读验收 | 同一执行者，前卡结束后顺序下发 | 已完成 |
| 最终审查、交接同步及本地提交 | 主线程 | 已完成，随本项提交 |

交付：覆盖 77 个架构文件、12 个一级专题；实现清单、设计待定项、文档冲突、依赖能力/工具可用性四类齐全。P01–P25、C01–C08 均关联 D01 或具体后续工作项门槛；没有在 D00 擅改业务规则。保留现有前端、Debug 生产隔离、组件与锁文件，未实现业务模块。

实际验证：`git status --short --branch`、`git log -5 --oneline`、`git branch -avv` 核对基线；`git ls-files`、`rg --files`、逐文件小段阅读核对输入；Python 验证 77/77 覆盖、全部行数、package/lock 版本、33 个 UI 组件导出；80 文件相对链接目标检查、三个交付文件 trailing whitespace 和 `git diff --check` 通过。D00-02 重新执行检查，主线程独立审查完整报告与重要冲突原文。最终同步开发计划启动点后，主线程复查 81 文件相对链接及四个交付文件 whitespace 通过。可重跑命令及结果见审计报告 §6。

环境：Node 24.19.0、npm 11.9.0、显式 Go 1.27.1；PATH 的 `go version` 失败。PG binary 17.8、MinIO RELEASE.2025-09-07T16-13-09Z、pgvector image tag 0.8.1 仅为环境证据，不是项目版本决定或集成通过。未验证已加载扩展、SQL/MinIO endpoint、产品构建/测试、浏览器、并发/恢复及 Provider/MCP/Runner 集成；D00 为纯文档审计，相应能力由后续模块真实验收。

过程问题：聚合阅读两次输出截断，执行者停止返回；主线程以修订 2 改小段读取并补齐覆盖后恢复。已结束全部子任务及命令写入，无 D00 未完成项或阻塞。D00 没有生产端口；正式端口归 D01，后续真实绑定责任见 P 清单与开发计划。

提交/交接：本地提交通过本台账及 D00 文件 Git 历史定位，不另补自身哈希。本项文件为台账、规格、报告及开发计划当前启动点；提交后工作区状态由主线程核对，未推送、发布或部署。

明确下一步：D01，先建立跨模块契约规格，逐项关闭本报告标记的 D01 冲突/未决门槛。本轮目标 D00 已完成，D01–D28 待开始。

## AT-0005：审计事项逐项决策与文档归位

- 用户目标：依次讨论 P01–P25、C01–C08，由用户决定；复杂项拆小问题。用户因选项不可见改用文字 A/B，没有等待超时自动采纳建议。
- 状态：已验收，S01–S05、V01 和主线程终审完成，随本项创建本地提交；本项不是 D01 契约验收，也未启动产品实现。
- 基线：开始为 `main / 71657b9`、工作区干净；期间 AT-0006 独立提交为 `2f9ba63`，本项讨论改动保留。D00 前置证据见 AT-0004。
- 规格/当前任务卡：[审计决定归位](../work-items/d00-decision-sync.md)修订 2；全部决定与历史候选见[决策记录](../work-items/d00-audit-decisions.md)，逐项正式归属见[审计处置映射](../work-items/d00-baseline-audit-report.md#7-at-0005-决策处置映射)。
- 补充输入：[Skills 设计提案](../work-items/d00-skills-design-proposal.md)、[Harness 一手来源调研](../work-items/d00-tool-call-harness-research.md)。前者作为架构归位输入，后者仅为静态调研，不是第三方集成验收。

已完成：P01–P25 的方案决定/既有规则核对；C01–C08 按序核对，C02/C03/C04 的用户选择已记录，其余按已有明确规则消除旧文歧义，无新增待问事项。用户指定的开发范围删除已落实；正式架构、前端设计与 D01–D28 责任/门槛均已归位并通过独立文档验收。

| 卡 | 执行与当前证据 |
| --- | --- |
| S01 基础、认证、项目与 Audit | verification_worker（gpt-6-astra / max）完成 10 文件；主线程逐份 diff 审查并补清理边界；100 个本地链接/fragment、UTF-8/LF/末尾换行/尾空格及 git diff --check 通过 |
| S02 模型、Agent/Skills、Knowledge/Memory | 15 文件/125 链接及格式、git diff --check 通过；主线程审查并完成小修 |
| S03 工具/Runner/治理 | 20 文件/158 链接及格式、git diff --check 通过；主线程审查通过，基础连接与业务出站边界明确 |
| S04 执行/调度/会议/实时 | 24 文件/229 链接、743 个 fence 配对及格式、git diff --check 通过；主线程审查通过 |
| S05 前端设计/索引与计划核对 | 13 文件/144 链接、14 个 fence 配对及格式通过；计划只读复核 29 项/50 依赖、76 链接，无必须返修项 |
| V01 全量独立验收 | 新 verification_worker（gpt-6-astra / max）独立审查全部 87 份改动，未发现需修复项；117 文件/1208 本地链接及 fragment、87 文档格式、1789 对 fence、29 项/50 依赖与 33 项映射通过 |

- 已确认重点：契约按职责组织且无循环；环境主密钥与管理员运行配置分开；统一账号恢复；用户名项目路径、归档与永久删除；项目技能按 Agent 分配、新增下轮生效；Linux/macOS；Owner Session Tunnel；工具无平台统一总期限、人工审批不自动过期；未知 Launch 沿现有流程继续核对；单一 Model System 请求重试；Runtime 更新进度独立于 item seq。
- 本项无未完成文档或阻塞。D01 类型/签名/矩阵、后续模块全部真实实现与验收仍未开始；全局搜索依 P25 另立产品设计工作项。
- 阻塞/待问：本轮无新增产品选择待回答。工程参数、依赖版本和真实绑定按责任模块规格落实，不能视为已关闭实现门槛。
- 未绑定端口：仍为文档阶段；技能初始化/安装/分配/运行绑定、生命周期停止清理、按 Launch key 查询、Runtime 快照更新衔接等由 D01 固定并按计划绑定。
- 验证边界：仅文档/静态来源审查；未运行 Go、npm、浏览器、数据库、Provider/MCP/Runner 或故障恢复测试，无产品验收结论。
- 提交范围：本台账、决定/提案/调研/同步卡、审计报告、开发计划、仓库结构及授权架构/前端设计，共 87 份 Markdown；无产品代码、样式或配置改动。主线程收尾复查同一文件集合与链接/格式/依赖/映射，全部通过。
- 提交/交接：执行及验证子 agent 均已停止，无后台命令或剩余写入；主线程仅暂存本项文件并创建本地 Conventional Commit，通过本台账 Git 历史定位，不推送。下一正式开发项为 D01：将已确认方向落实为具体跨模块契约，不重新讨论已定产品选择。

## AT-0006：统一子 Agent 模型与思考强度

- 用户要求：所有子 agent 使用 GPT-6 astra、Max；正式标识为 `gpt-6-astra / max`。
- 状态：已完成；规格/任务卡：[模型调整任务卡](model-update.md)，修订 2。
- 基线：`main / 71657b9`；存在 AT-0005 的台账与两个讨论文档改动，完整保留，不纳入本项提交。
- 交付：项目默认配置、三个角色 TOML、当前团队规范与模板/计划均同步；初始化验证记录仅补历史说明，旧模型运行事实不改写。
- 执行：新建 verification_worker 角色实例 `/root/verification_model_update`，显式指定 `gpt-6-astra / max`、fork_turns=none；后续新子任务继续使用该组合，不复用旧模型实例。
- 验收：主线程独立 diff/TOML/name 角色集合/并发上限/现行规范核对通过；11 文件 UTF-8/LF、106 个相对链接及 `git diff --check` 通过。仅 docs/config，不运行产品测试。过程中的输出截断经修订 2 小段读取解决。
- 限制：CLI 0.159.0-alpha.3 的 strict app-server config/read 能解析正确项目 agents 层，但因 trust 被禁用，effective agents 为空；未改变全局配置/信任，不声称 CLI 自动启用。当前会话以显式模型参数成功执行，无需依赖默认配置加载；具体证据及过滤脚本修正见任务卡。
- 提交/下一步：仅暂存本项 11 文件范围，台账只纳入本节；本地提交通过文件历史定位，不推送。完成后恢复 AT-0005 的 P09 讨论，具体业务契约仍未验收。

## AT-0007：优化开发协作工作流

- 用户目标：只与主线程讨论产品，由主线程完成工程转译、拆解、委派和验收，改进角色与并行效率。
- 基线：`main / d8320fc`，初始工作区干净；规格/任务卡：[工作流优化](workflow-upgrade.md)修订 2。
- 状态：已完成；R01、I01/I02、V01 均已停止执行，保留所有子 agent 的 `gpt-6-astra / max`。
- 范围：团队规范、角色配置、项目技能、任务模板与开发计划执行规则；不修改产品定义，不启动 D01。
- 已完成：独立诊断及十条协作契约；六角色/五技能、团队规则、共享规格与精简卡片模板、开发入口同步。常态最多两个/硬上限三个子任务，保留单模块完整验收。
- 验证：作者限定检查与 V01 独立一致性验收通过，主线程审查完成；TOML/五技能、14 份 Markdown/171 本地链接及 3 个 fragment、历史保护与 whitespace 通过。开发计划 397 行模块/依赖/门槛区与原提交完全相同。命令和稳定输入摘要见任务卡；收尾只补验完成记录及暂存内容，不重复未变输入的检查。
- 运行边界：一次 CLI config/read、skills/list 确认项目配置正确、五技能可发现且启用、项目错误 0；agents 层仍因 trust 禁用，effective 为空，不声明自动加载生效。实际子任务均显式指定 astra/max；I01/I02 实际并行，未测量提速比例。无产品测试、逐角色演练、依赖安装或全局配置修改。
- 交接：精确 21 文件授权范围，所有执行者和 app-server 已停止；主线程仅暂存本项并创建本地 Conventional Commit，通过台账/任务卡 Git 历史定位，不推送。无未完成文件、阻塞或待定产品问题，生产端口不适用。
- 下一步：正式开发仍从 D01 跨模块契约恢复，沿新团队流程执行，当前不启动产品实现。

## AT-0008：持续开发与 D01 跨模块契约

- 用户目标：按依赖完成全部既定产品能力，小块验证提交并推送 main；最终用平台实际组织开发，完成 Minecraft 或 Terraria 至少 50% 内容复刻，保留可复现覆盖率与真实试玩、协作、执行、审核证据。
- 当前进度：D01 已完成静态契约验收；规格：[D01 跨模块契约](../work-items/d01-cross-module-contracts.md)修订 1。
- 基线：`19d300f`；环境初始分支 `work` 且干净，主线程创建同提交 main；`git ls-remote origin refs/heads/main` 确认远端同基线（旧本地 remote tracking ref 落后）。不覆盖他人改动。
- 前置：D00 / AT-0004、AT-0005 已验收；D01–D28 尚无实现验收，不将前端 Debug 当产品能力。
- C01：architecture_worker / gpt-6-astra / max 正在独占新增 `work-items/d01-contracts/` 文档；V01 待独立接口审查；主线程维护规格/台账。
- 本次授权：用户已明确要求验证后及时同步 main 并推送；不需逐卡重新询问。
- 未绑定端口：D01 设计端口均待后续责任模块实现；本阶段无产品测试结论。
- 下一步：完成 C01 契约、冻结写入并独立走查，处理缺口后提交推送；D01 完整通过才启动 D02。最终游戏验收为 D28 后的硬门槛，不以文档完成或主观估计替代。

### G01 已定最终验收要求归位

- documentation_worker / gpt-6-astra / max 完成并停止写入；主线程审查开发计划与 E01 规格，D00–D28 原依赖顺序保持不变，追加 E01→D28。游戏/版本/清单仍待 E01 前冻结，无产品通过声明。
- 作者检查 81 个本地链接/2 个 fragment、Markdown 格式和依赖表；主线程 `python3 /tmp/agenteam-check-markdown.py` 对本次四份文档检查 97 个链接/fragment、UTF-8/LF/末尾换行/尾空格，通过；`git diff --check` 通过。环境 Python 3，基线 `19d300f`，稳定输入以本次提交文件为准。
- 交付范围：开发计划、E01 验收规格、D01 开工规格和本台账；C01 契约设计仍独立进行，不纳入此提交。仅文档，不运行产品测试。主线程创建小块提交并按已有授权推送，提交通过本节 Git 历史定位，实际推送结果在下一条执行记录同步。

- G01 实际交付：`b369ad1` 已成功 `git push -u origin main`，远端从 19d300f 快进到 b369ad1。
- D01 V01a 独立只读审查前三份稳定契约，发现 4 项待返修，详见 D01 规格记录；作者已恢复写入进行修复，全量 V01b 待冻结后执行。D01 仍设计中，无产品实现验收。

- D01 V01b：8 份契约全量静态审查完成，前轮 4 项已修；另 5 项跨域接口缺口返修中（Skill 校验/唯一绑定、删除返回类型、preparing InTx 捕获、归档内部收敛），具体记录见规格。C02 正将已采纳边界归位基础契约/Project/Skills 三份架构文档，禁止把文档审查当产品验收。

### D01 完成与交接

- C01 契约 8 份/1334 行，含职责/依赖/绑定目录、基础类型/访问/事务、领域生命周期、资源/Skills、Model/Tool/Runner、执行/编排、事实/Realtime 和 W01–W43 走查。C02 将必要边界归位 3 份架构。
- V01a、V01b 发现已集中修复，V01c 独立验收通过，主线程审查通过；记录及稳定指纹见 D01 规格。C01 manifest `93b2ce8732e965aefa7b8f87e60a22e0477d537be88b53c3a11bb2ef07ea584b`；93 链接/11 fragment/39 代码块/18 表/43 场景检查通过，C02 34 链接/6 fragment 通过；主线程复核 11 份/127 链接和 UTF-8/LF/末尾换行/尾空格通过，Git whitespace 检查通过。收尾只更新状态与入口，复查最终链接/格式。
- 所有作者/验证者已停止、无后台写命令；无 D01 未完成项/产品待定。当前生产端口全部未实现，提供/消费与真实绑定责任已列入契约索引；没有运行 Go/数据库/Provider/Runner/MCP/浏览器或产品恢复测试，没有平台能力或游戏覆盖率验收结论。
- 本项提交只含契约、三份架构、规格、台账与开发计划；通过本节 Git 历史定位。主线程按用户授权提交并推送 main，推送结果由下一执行记录补充；提交后不遗留本项未提交文件。
- 明确下一步：D02 开工，先确定 Go 工程/配置/程序生命周期的实现规格，建立真实 test/vet/build 和进程停止检查。D03–D28 及 E01 尚未执行，E01 版本/清单/权重仍待其开工前冻结。

## AT-0009：D02 工程与程序生命周期

- 状态：D02 已完成；下一模块 D03；规格：[D02](../work-items/d02-engineering-foundation.md)修订 1。
- 前置：D01 完整静态验收，提交 `bb89da2` 已成功推送，远端由 b369ad1 快进；工作区清洁后开工。D01 文档最终 14 份/223 链接格式检查通过，产品行为仍未实现。
- 环境：显式 Go 1.27.1 linux/amd64 可执行，GOTOOLCHAIN=auto；Node 24.19.0/npm 11.9.0。尚未运行新后端编译/测试。
- S01 先固定可执行规格；B01/B02 按基础与生命周期两个完整结果顺序实现和独立验收，验证后小块提交并推送。无端口/测试库占用，无用户待定产品问题。
- 未绑定：D03/04/05 数据库/Secret/对象存储与 D15 Runner 身份协议仍待后续，禁止基础程序伪报 ready/online。下一步完成 S01 后实施 B01。

- S01 已冻结并由主线程确认，实施规格修订 1；当前转 B01，工具链临时编译运行成功，仓库实现测试仍待执行。B01 所有权与 F01–F04 验收见规格；无新增产品问题。

- B01 完成：foundation/HTTP/OpenAPI 公共 schema 共 19 新文件、删除 central 占位。默认 Go1.27.1/local test/vet/race/build/modtidy 检查通过；V01a 两项 Fault 发现已修复，V01b 验证真实 HTTP/HTTP2、JSON安全边界及整数 schema，V01c 修正并通过字段路径 schema 边界；完整命令/范围见 D02 规格。最终 manifest `350f43cc6faf7f6bed8fe9fbec049ad3822b3068fd24c773f09f82ae579c5fd2`。作者/验证者均停止，主线程审查通过，将本小块提交推送；不将 B01 当 D02 或产品完成。下一步 B02，仍无产品待定或环境阻塞。

- B01 实际提交推送：`896fa2e`，origin/main 从 bb89da2 快进。B02 已开工，所有权与 P01–P05 验收见规格；不更改已验收库，主线程维护规格与台账。

### D02 完成与交接

- B02已由V02b独立验收，无剩余阻塞；IPv6修复与Node URL 6,005例差分通过，两真实cmd信号/安全CLI/诊断/冲突和Runner无socket通过，app正常/强停/启动/异常Serve及两种新增取消/失败交错race通过。证据及限制见D02规格；最终27文件manifest `91ebac84fef6c9411d314d9b53bf78699a0eb4db4b90453411a29f31cf611442`，另6占位删除。
- B01与B02均验收，主线程已核对代码/文档/验证证据，D02标为完成。环境Linux/amd64+Go1.27.1/local，其他OS未运行进程验证；web/db/runnerprotocol未改。数据库、Secret、对象、账号、协议与业务仍未绑定，不宣称产品ready或游戏验收。
- 作者/独立验证者均停止，无后台命令；主线程精确提交本项并按授权推送main，提交通过本节历史定位，实际推送在下一记录补充。下一步D03开工，先设计PG/pgvector/迁移/事务与隔离真实数据库测试。无用户待定或现有环境阻塞。

## AT-0010：D03 PostgreSQL 与全局迁移

- 状态：D03已完成；下一模块D04；[规格](../work-items/d03-postgresql-foundation.md)修订1。前置D02全部验收，`c4e0320`已成功推送origin/main（896fa2e→c4e0320）；开工前工作区干净。
- S01固定真实PG/pgvector/pgx/Goose、事务锁和migration/恢复验证规格，再分B01/B02实施和独立验收，小块提交推送。Docker 28.4.0 client/server linux/amd64可用；未创建/访问任何数据库或容器，版本镜像待核验。Go继续1.27.1/local。
- 主线程独占本台账/主规格/开发计划，架构者独占新增实施规格。仅允许任务专属隔离数据库与必要依赖；D04+未绑定，无用户待定产品问题。下一步完成S01并核验镜像，不能将D02非ready诊断当完整产品。

- D03环境探针：固定pgvector镜像amd64 digest `sha256:99a149d3c84cfb0f32d8da7d72737e4643468787220af2223418730f8e9e9cdc` 实际PG17.8/vector0.8.1，隔离无网络临时容器内SQL读写/向量运算通过；已删除owned容器，无现有服务访问。这不是迁移/事务产品验收，S01仍进行中。

- D03 S01已完成并获主线程确认，B01开始。固定pgx5.11.0/Goose3.28.0及已实测PG17.8/vector0.8.1 digest；具体所有权/Tx/迁移/恢复/TLS/提交未知测试见实施规格。原规格SHA `ffefabd7efeca79d05b3cdd5ddd2b7c458e721aa682bd87cb70d569257a80cc0`，实际库编译与D03行为验收仍待B01，不以设计或环境探针代替。

- D03 B01已独立V01b验收：31文件manifest `ac1aa25a95b405e76cc65fa080ec5d788f46907a0e865ff01e48e76b570972a0`，普通test/vet/race/build及真实隔离PG17.8/vector0.8.1和PG16.12拒绝通过；事务/锁/unknown/迁移并发与non_tx恢复/TLS/force关键场景独立验证通过。最初公共类型两项缺陷已修复，COMMIT驱动误提示已据真实代理修正保守分类。详细命令/nonce/指纹/限制见D03规格。
- 独立nonce2b890221af2952af7d2b07356a9911ba资源全清理，label容器网络计数0；所有作者/验证者已停止。主线程审查通过并将本小块提交推送。B02入口尚未实施，D03未整体完成，下一步必需数据库启动/健康/CLI/关闭整合，无用户待定或环境阻塞。

- B01实际交付：`cd31f34`已推送origin/main（c4e0320→cd31f34）。B02已开工，范围包含Central必需DB装配/健康/repair CLI/同预算关闭与真实进程测试；详见规格增补所有权。B01库未解冻，root维护台账/规格/计划。

- B02真实进程取消探针发现迁移guard等待backend在Central exit0后仍存活；已登记D03规格，授权最小返修migrate.go及回归测试，其他B01实现继续冻结。局部race通过不替代整批验收；当前仍B02，无用户待定，后续需实际确认owned backend退出及独立验证。

- D03完成：B02经独立V02验收，30输入manifest `043a4e90eea13ff522173d24658b9facdad94997197676ebbce7e4f747c01023`。普通test/vet/race/build与完整真实integration通过；独立新增4组app边界及迁移取消/HTTP+DB/真实process选定集通过。启动取消后服务端迁移guard残留缺陷已最小修复，并以owned PID退出和repairing恢复证明；详细日志/时长/nonce/限制见D03规格。
- 所有作者与验证者均停止；最终独立nonce资源为0，冻结指纹/格式通过，主线程代码/脚本/文档审查通过。数据库真实绑定而整体ready=false，其余业务仍未实现。不将D03完成当产品或E01完成。主线程按授权提交推送，提交通过本节历史定位；下一步D04 Secret/出站/Audit规格，无产品待定或环境阻塞。

- D03实际交付：`9beaa7f`已成功推送origin/main（cd31f34→9beaa7f），工作区清洁。

## AT-0011：D04 Secret、出站访问与 Audit

- 状态：D04已完成；下一模块D05；[规格](../work-items/d04-security-foundation.md)修订1，基线main@9beaa7f。D03数据库基础与入口全验收，未绑定Secret/对象/身份/协议及业务。
- architecture_worker独占新增实施规格，root维护主规格/台账/计划；先固定密钥环/恢复、动态出站与Audit数据接口和真实验收，再按完整结果实施、独立验证、提交推送。无用户待定或环境阻塞；当前不创建测试资源、不提前实现D05+。

- S01实施规格修订2已获主线程确认。独立静态审查发现的Audit重放/HTTP隐式重试/故障状态及旧策略receipt问题已集中明确化；按Audit/cursor→Secret/轮换→动态出站顺序实施，尚无D04产品行为验收。设计基线先按授权提交推送，再开工B01，范围见主规格/实施规格。

- S01实际交付：`e34dfaf`已推送origin/main，修订2 SHA `869f1fd0179dea0ecf2fe2099a47088af17e3fc90cd6514de121e171d1d55b98`；4文档117链接/格式通过。B01已开工，作者独占Audit/cursor/最小identity contract、00002与必要配置诊断测试，范围增补见主卡；root维护规格/台账/计划，D03生产库保持冻结。

- B01a identity/cursor子范围通过独立V01a。6文件最终manifest `18234b86451a5f843643cc25cbf0486bbe21232b4e40e26021bf6778e4d77bdc`，generation字符串契约修正已以原失败probe/独立MAC/race闭环；其余权限类型/篡改/敏感输出检查通过。root精确提交此小块及规格记录，Audit活动范围不纳入；B01/D04仍实施中，D07/D08授权仍未绑定。

- B01a实际交付：`d623004`已推送origin/main（e34dfaf→d623004）。公共identity/cursor保持已验收冻结，作者继续B01 Audit/配置诊断/真实PG验收；活动Audit未纳入该提交。

- B01完整验收通过：Audit核心、00002迁移、授权窄端口/分页/清理、必需cursor配置与入口真实验证完成；52最终manifest `3ec6ac44591b98ffb3e703727bcc82d6ccad7ede5f50f5fff306c84b95b978f4`。独立发现的cleanup unknown错误状态已以原真实探针闭环（security1.904s），其余完整test/vet/race/build/PG/进程证据复用，详见D04规格。最后nonce725c5bcf2ac1e88959ba89a0330f4607资源清理，作者/验证者全部停止。root审查通过并按授权提交推送。D04未整体完成，下一步B02 Secret，无产品待定；D07/D08授权仍未绑定。

- B01实际交付：`088185e`已推送origin/main（d623004→088185e），工作区清洁后B02开工。Secret/contract/00003、真实轮换恢复与入口整合由backend_worker独占，补充文件范围见主卡；root仍维护规格/台账/计划，已验收Audit/cursor/identity及D03库冻结，B03未开工。

- B02a纯加密6文件独立V02a通过，manifest `06bec83b821afd4b8c7301335fd741d60d9fa42af9d3f3e36a3cbeff9e94caa4`。独立Python向量/逐字节篡改/keyring材料隔离/销毁并发与安全输出、race通过；root已读实现并精确提交此小块。作者继续SQL registry/nonce、业务lease与轮换；B02/D04未整体完成，真实持久化验证未由此替代。

- B02a实际交付：`33a61db`已推送origin/main（088185e→33a61db）。纯加密6文件保持验收冻结，作者继续B02持久化/lease/轮换与入口，尚未整体验收。

- B02持久化25核心文件已冻结并交V02b独立验证，manifest `e7c145a9f26217ba8f47fb6fb80e141a0fc060898c5d70e61b41fe3a83bd6ea7`；作者真实PG/race全TestSecret13.402s及SIGKILL/unknown证据见主卡。验证在33a61db稳定副本进行，核心禁写；作者仅继续入口及说明，Docker资源顺序交接。主线程核心生产/SQL初审尚无确定阻塞；B02仍未整体验收。

- B02b持久化核心独立V02b通过：security真实race18.846s、contract race1.020s，新增并发重放/pending canary/cleanup unknown/双进程nonce探针通过，25指纹未变；主线程审查通过，按授权精确提交推送。nonce04a7bba599c7b9bf84ee35da74b04e6f资源清理，验证者停止；作者继续入口真实验证及说明，B02/D04未整体完成，授权依赖仍未绑定，详见主卡。

- B02b实际交付：`6feda6b`已成功推送origin/main（33a61db→6feda6b）。持久化25核心文件继续冻结，作者正在验证Central入口/启动/信号与说明；本提交不包含活动入口，B02尚未整体验收。

- B02完整验收通过：V02c定向race及真实app18.124s/process29.097s通过，新增真实worker损坏/不可用/恢复/移除旧key探针通过；52最终manifest `f7a8c2dbe80dd50c1fb3ec58a7b42e03736ca6e33d35ca7137878c72a64c8415`。作者完整Go/PG、V02a/V02b稳定证据复用，主线程审查通过；nonce12ebe47977ec1b6f6dd1668ff88e7865资源清理，全部作者/验证者停止。root按授权提交推送入口，B03待开工；D04未整体完成，未来授权仍未绑定。

- B02入口实际交付：`fffd0dc`已推送origin/main（6feda6b→fffd0dc），工作区干净后B03开工。backend_worker接管动态出站/00004/真实网络fixture与必要入口，详细所有权见主卡；root维护规格/台账/计划，B01/B02和D03库冻结。先完成D04，不提前D05或E01。

- B03a纯边界5文件独立通过：origin mapped身份/hex数字host两缺陷以原失败probe闭环，race1.075s/vet/格式通过，manifest `4993cd4047121664e58ce04e116a096d9619efb8ae79afa73876e3b811cfc166`。root精确提交推送；活动policy/gate/DNS/HTTP不纳入，B03/D04未整体完成。DNS严格partial-error与专用H1的工程选择、IANA在线403资料限制记录于主卡。

- B03a实际交付：`1898748`已成功推送origin/main（fffd0dc→1898748）。纯出站边界5文件保持验收冻结，作者继续活动policy/gate/DNS/H1与隔离网络fixture；B03尚未整体验收。

- B03b策略存储/门禁7文件独立通过：Reload未等待旧unknown writer的竞态以真实原probe闭环；原probe仅2处代理arming适配，断言未改，security race5.591s、vet/格式通过。manifest `39e6b55cba77b2d82d1e2e8087f5e9974c487b5c9f4682457478552306be4322`；nonce928c77669ccdad099df921c4f9c127f8资源清理，root按授权精确提交推送。DNS原label/referral/NS-SOA及跨zoneCNAME修复另行复验，HTTP实际网络/入口尚未通过，详见主卡。


- B03b本地提交`ee8ddb7`；push因GitHub认证不可用失败，当前GH_TOKEN被报告无效。远端仍1898748，等待安全凭据配置恢复后补推，继续本地实施。
- B03c DNS2文件完成独立定向race1.075s/vet/格式复验，三份原probe不变，跨zone CNAME终局NODATA及既有拒绝回归全部通过；manifest `238222cc10eed8d3e9d5c9c0349c4e058abca7ba5b05ef34a7a9d5e8f2537bef`。仅自建loopback UDP/TCP，无残留资源，root按授权精确本地提交；HTTP实际网络/入口继续，B03/D04未完成。完整证据及工具中断后安全恢复记录见主卡。

- B03c实际本地提交：`d6e92c8`；与ee8ddb7待认证恢复后补推。当前HTTP/入口仍由作者实施，独立验证者已停止。

- V03 HTTP/SMTP独立真实race16.984s通过其余网络/SDK/协议与关闭场景，但3项明确阻塞待修：unsupported响应慢排空、SMTP并发deadline放宽2s首写、自动请求头漏计上限。原probe SHA7418af334367928c97bca5b84f2b1c6544678de39df51bc8b86b9ead2949c557；独立全部停读/资源清理后root精确解冻返修，Docker交还作者。入口普通/race通过但真实待验收；详见主卡。


- B03d HTTP/SMTP核心21输入独立复验通过：原前两probe race5.046s、新实际头边界1.244s、真实受影响network/SMTP/Audit13.616s，无剩余阻塞。manifest0321829f3205d8fec989b3e6991ae7ca092e288434107108ef9e750cfa59caae；原helper测试替换原因/可逆diff/资源清理详见主卡。root精确本地提交，认证未恢复暂待推；入口23独立及无过滤test-security最终整组继续，D04仍未完成。

- B03d实际本地提交：`79af090`。GitHub认证仍不可用，与ee8ddb7/d6e92c8/6c2aea2共4提交待补推；入口23独立及完整无过滤suite继续。


- D04完整验收通过：V03入口独立及无过滤test-security最终exit0，postgres1.042/database36.063/app58.639/process51.828/security57.810s；完整日志SHA25607dd87e8c2bcad49f4baa1ea0483e54dc4646e8d3c75f699ac0e0986a5a95。入口24 manifest dcd8d93edcdc9415c4c3b7a7295b8b39ad54495799b7b14c54657fe5a42896f2、全59 manifest9de98ae356466157aa30d18f0e8bdeb61e5ddeb17811812480f5164100779bd0，源/独立副本一致；两轮资源exact确认全0，作者/验证者全部停止。root完成生产/说明/证据审查并精确提交入口，GitHub认证阻塞暂待推。未绑定真实授权/业务适配与ready=false限制不变；D04完成不代表平台/E01完成。下一步D05规格，无产品待定。


- D04入口实际本地交付：`abf5c37`，提交后工作区干净。GitHub认证仍不可用，ee8ddb7/d6e92c8/6c2aea2/79af090/abf5c37共5提交待补推。

## AT-0012：D05 对象存储与 Artifact

- 状态：已完成；B01/B02/B03独立验收及root审查通过，[主卡](../work-items/d05-object-storage-artifact.md)修订3/设计修订6，开工基线main@abf5c37。最终证据及后续未绑定端口见主卡末尾，下一模块D06。
- architecture_worker独占新增实施规格，root独占主卡/计划/台账；R01仅有界MinIO SDK/镜像/环境探针，不写仓库实现。按D01正式对象/引用/transfer边界落实表/状态/授权/stream/一致性/恢复/Artifact服务，S01确认后实施。未来身份/Project/Runner/Tool适配仍未绑定，禁止生产stub或匿名业务API。
- Go1.27.1/local与owned PG/固定源码MinIO/TLS真实验证已完成；所有执行者停写、资源清零。无产品待定，推送认证阻塞不冒称已远端同步。

- R01/R02真实MinIO单PUT/签名/完整性及零marker补证完成，研究修订2冻结；S01修订1停写并交独立静态审查。报告/规格指纹、资源清理和实际限制见主卡。root尚未批准实施，生产代码未改；推送认证仍待恢复。

- S01修订2通过独立复验、root采纳，四项规格问题全部闭环；设计SHA4b3241bb37246386ad56bd5f613b2c63aaae673e7d2fea2afd7f806793e65a30，输入manifest3a1ffc916c1a81cbfd5f4c9e36fd029e52b552e710daa426049d43bb49bc3f65。B01对象可靠存储开工，独占文件/依赖/Docker与冻结边界见主卡；B02/B03未开始，产品未验收。

- B01a公共13文件独立通过：审计phase/outcome、MIME名称参数和schema状态3缺陷用不变原probe闭环；manifest2b152004f06fc9d59248dbe40564835141f28d36d6b9920ae1295888f144f56a。相关5包race/vet/schema通过，root审查后精确本地提交；object/SDK活动范围与00005真实兼容不在此次验收。详见主卡。

- B01b object正式契约4文件及设计rev3增量独立通过，Reader原两缺陷probe不变闭环，exactRead object/稳定lease/深拷贝/race通过；manifest399edce187a0a310de23acf10d1602350dbcb7422034429eeb72bfe5772df181。root精确本地提交，活动持久化/SQL/SDK与真实存储恢复仍未验收，详见主卡。

- B01核心35文件已停写，manifest19eb670b311524bd8a2c4b1837700b34b74e5ec7958992bbab032577faa7a3bd。独立首轮无过滤test-objects.sh全部exit0，含旧数据库/进程/安全兼容与真实MinIO对象场景，日志SHA570228f8d870e7d29f4382cdcd798cb2223e3c3bc1c1d7ca832d56d1f95a5d48；三nonce资源独立核零。独立审查仍在复现unknown语义竞争及spool崩溃候选；ExecutionPayload真实锁映射的正式端口缺口已交架构只读分析，未解冻实现。B01尚未验收，B02/B03不提前开工；main@23d1cfa，共10本地提交因既有GitHub认证失效待推。

- V收束暂不通过：unknown异义误成功、spool删除崩溃窗口、Project受保护前100阻塞后续批次、外活实例阻断本地Recover，4项实际复现；真实owner/Actor锁预收集正式端口缺口静态确认。证据索引SHA82a05e660df66e51383c27358a102049d990edf612b3845c037e5e5213303ff2，原探针保留，9nonce资源全0且V全停。root解冻core/README给原backend修4项，Docker归backend；architecture仅设计rev4补AccessLockPlan，冻结并确认后再改contract/锁实现；公共13保持已验冻结。详细所有权、证据和复验要求见主卡。

- 前4缺陷作者原probe逐字复验exit0，真实objects21.060s/spool1.042s，新增异义语义与跨service公平进展通过，资源全清；root核稳定副本，最终独立复验待锁修订后完成。设计rev4 SHA d79d7718c6816555cc813103867a98bac8ef2dd540dbb67b09733610707be39d独立静态无阻塞（输入9e2c9c69773b3b51e08f47739231055dc833262198c2c0f5175b2d8037e3d411）；root采纳并解冻object契约/正式锁计划及调用链给backend修第5项。D03/D04公共13不改，设计冻结；B01仍未验收，当前仅本地提交，认证阻塞未解除。

- rev4锁规划契约6文件独立通过，manifest7f44286d3a5b84b06bb0dc55add6291ca74f286abd481a9d30da708c8224c191，18依赖72e590045e6a7469fb1eaec2d84d76a000668d4cbe5c36e8b27707370ce3d4f9；普通test/race1.047s/vet及12请求替换、issuer/Tx/plan、最强union、深复制/安全投影probe通过，证据4ddd8e4a7c688b5d20385ebd28404b43c1572e98082b3937b8759eeb26c9dce0。V全停后root精确提交契约；核心作者真实gate组通过，整TestObject仅旧fixture锁等待位置观测失败待定向修正，check-go/最终core冻结与独立完整复验仍待完成。spool首次写入截断窗口已并入同类恢复修复，详见主卡。

- B01最终修复41核心文件已冻结，manifest b947acd610a8452c2f41c427325e1bb54570baa11b786a6c218ead867d73db80；61全输入与160依赖均由独立副本逐项核验。作者check-go通过、Consume/Cancel精确锁观测修正定向通过，15nonce资源全清；V独占Docker运行无过滤test-objects与原探针/独立边界。root冻结审查未新增确定缺陷；Recover单项失败提前返回候选待真实行为验证。B01尚未验收，B02/B03未开始；main@cb531bc，13本地提交仍待既有认证恢复。详见主卡。

- 最终独立全量兼容及原缺陷回归通过；新probe确认Recover单项RESOURCE_BUSY仍阻塞后续独立对象，唯一剩余阻塞。Session/真实User gate及整Tx回滚补证通过；证据索引b54ca84b5800de9ff87fe6f63751c6cf3d0a0289c44d027703b0196b9551619f。V12nonce资源全0并全停，root仅解冻recovery.go/对应测试/README最小说明给backend，Docker顺序移交；其它冻结，不提前B02。详见主卡。

- B01最终独立通过，无剩余阻塞；core42 manifest0cc639c48822ccece700e7cd0268382d64a602fd036de0d00de8c303c5503566/all62 cc3e894c582070b43585a34b6d7330bbb7fc6675b65eea074f59c974b6ed8296。恢复窄修独立race9.715s及原不变probe/新增三对象首错与checkpoint验证过，证据3793e5474aad3b1e4e482f8e95a8168e2017db25aaf3ef93bd164c4dba43be6e；结合先前无过滤完整兼容和授权/幂等/spool证据验收。所有执行者全停、资源全0，root精确提交B01核心后推进B02；D05整体未完成。

- B01核心提交d31aecd，开工工作区干净；主卡修订2授权完整B02 Artifact/浏览器下载，按设计rev4，backend独占新领域/download/00006/真实测试和Docker；B01冻结接口所需增量先报root，公共/依赖/旧迁移冻结。下一步完成B02作者实现与真实自测后独立验收，B03未开始；15本地提交待认证恢复。

- B02设计rev5 SourceReads原子组合与typed Download provider必要补口已独立静态通过，设计SHA4251c2ee79a04d34627f755c894d2e6990babbeb11e3a8d00b8212b9fb5b9f86/证据23147513c4a5dc8326d2e5e8552b267da40cfe3424810a3c95b011b0c7df9b8a；root正式解冻最小source/access补口给backend。独立下载keyring+Secret只读材料检查4文件先冻结交V纯验证；其余B02活动，不读活动源码，Docker仅backend。详见主卡修订2新增授权。

- B02纯keyring4文件独立normal/race/vet与全部历史用途隔离/安全投影边界通过，证据617aa4b68667dc1053687fc4f49c44b14c90d9fd5d5004efee9b9b37d7e7283c。只绑定初始92稳定副本；活动SourceReads access依赖漂移未读且不纳入该结论。root确认88依赖与提交HEAD全同后精确提交4文件，整B02仍活动，后续冻结验最新组合；详见主卡。

- SourceReads 8文件作者真实组通过但独立新增停止probe确认唯一阻塞：未开流Cancel在真实Object EX等待时Drain提前nil。原日志SHA1cccf62c824b6020bb7dd4e4dfdc6232e7cba780dfcc7d66790f3768819f7393；其余SourceLease/rollback-unknown/当前receipt与Actor回归通过。V全停资源0后root交backend窄修，并仅额外解冻service.go清理计数/通知/drained fence、read.go同ctx释放抽取；原probe不变复验，B02仍未验收，详见主卡。

- SourceReads10文件补口独立通过，source manifest f35b630c653ee018d60a37472a7185b4c68cfe6f4eec522f6b7f28391d53e990/219依赖2cad6c7ef3a214f05dbe800f0f5fde3246d015d3e31e275279693725de177b7d；关闭原probe逐字闭环及真实生命周期17.393s、normal/race/vet通过，证据50bbb6cd854aee2c18f45b801510a6947e655b1cb8ae281e30a2acdf21664311。V全停/资源0；root精确提交补口后Docker交backend继续B02 Artifact/download集成，00006等活动范围尚未验收。

- B02主实现作者真实三路径、下载HTTP/审计/损坏/关闭、来源/发布/下载COMMIT unknown、Project批次/外实例reader保护及来源失权原事实恢复已分组通过；期间修复忽略JSON错误导致source/upload语义摘要退化的产品缺陷，保留原异义反例与失败日志。最新projection/recovery日志6f9c7b2e81116841241915f806b595f7354b626bcef9c3c6e5328990998a511e（objects7.196s）；详细各组指纹/fixture修正见主卡。正在最后check-go、全部TestArtifact/Download和文档冻结，主实现尚未独立验收；Docker仍backend，V待最终稳定输入，无过滤兼容仅由V运行。

- B02主块39文件正式冻结，manifest f7b2e64f8ffecb718665d7e3c8a97254a26de1902d2f21e19edc2dad21594338；237依赖逐项等HEAD08e64e5，已验SourceReads/keyring未改。作者check-go与全部Artifact/Download真实组44.832s通过，证据索引e33a615286f3e1f9ab15054923eafc6e10c55a20031e045b3ea8edbe62ae6df2。作者全停/资源清理后Docker交V；独立副本/tmp/agenteam-d05-b02-verify-8yj7bvf1开始无过滤完整兼容及风险审查，root只读核冻结实现。B02仍待独立结论，B03未开工；详见主卡。

- B02独立无过滤组exit1：唯一batch/foreign-reader用例31.25s返回INTERNAL_ERROR，旧数据库/应用/进程/安全组全过；日志2e171b6ab19286b9118ada74b856496a115b6b6187067a3972507fa983233596，资源核零。V定向区分30s测试/代理预算与产品问题，并实测永久清理后业务metadata残留候选；原预算/断言先保持，源码继续冻结，尚未验收或授权返修，详见主卡。

- B02独立probe确认唯一新增生产阻塞：Project cleanup completed仍残留command/intent名称描述、source JSON和download filename；payload/business行已0，pending foreign source lease保护与他Project隔离正确。原probe88fc221cf7814767e3e6686ec64977412b4a05bd459367ab6e0b9dbbd63a9ee9固定，真实日志59c85d097ad45e0761976ab6a243b7f2af412a8e457e01135e57d7972354c403。下载rollback-unknown/流中撤销独立通过；batch定向15.39s过但原全组超时原因未确证。待V全停后窄修最终metadata清理，再闭环原probe及全量门槛，详见主卡。

- B02最终metadata清理窄修冻结，完整42源40b41ccd27c8db1e8c0298421d874772801c22efd06f39b441bbf607ce8ea1ac/窄10 eab8b5673c6e2f09a3010e0e8c383d2a85c1233070fd9c159a9f1bd1cc22721f，237依赖不变且等HEAD7281a6f。原probe逐字通过、finalTx真实rollback/unknown/迟到终局/新旧key防复活及batch两组27.753s/24.631s、完整check-go通过；作者全停6nonce资源0后Docker交V，最终无过滤与原probe独立复验中。root核冻结diff无新增确认问题，尚未验收，详见主卡。

- B02最终独立验收通过：完整无过滤含原88fc probe exit0，objects171.296s，旧数据库/应用/进程/安全全过，日志336973288159d6a1c991c26e47353fb2b2668b262529c9e9f83a9028d2c5374d；metadata残留已闭环，101batch34.29s通过，finalTx真实rollback/unknown与迟到防复活通过。42源/237依赖三方匹配，V全停、3nonce资源0。root精确提交B02后继续B03，D05整体未完成；旧失败历史/未来未绑定边界保留，详见主卡。

- B02实现提交2c1dca3，工作区干净；主卡修订3开始B03工程接口细化，architecture_worker仅设计rev6明确transfer端口/00007/锁及最小已验补口，源码暂冻结，规格采纳后再由backend实现。B03目标为Runner单对象直传与Central MinIO入口/生命周期，未绑定D17拒绝生产grant；当前24本地提交待既有认证恢复。

- B02最终证据索引ede336f558c6a571aad275b9c239d4f348a824a41377b3ee99e791b8b99f619d已归档，源码提交2c1dca3；B03设计rev6首轮冻结d952a3de675ad44dd6e5a699d9ae07a0383083512dd64d671c099ae865364814经独立静态审查需三处澄清：同受信host reboot恢复、Runtime/Service唯一worker真实join、业务completed与外部lease退休授权分离。V全停无资源后root仅授权设计窄修，其他规则/39repo及3SDK依赖匹配、格式链接过；B03代码仍冻结，详见主卡修订3。

- B03设计rev6窄修独立通过，最终SHA4ed8a62d70e71ecfde5bd2aafbcd5ad012ba1e2d6c30a38e55447268cad6abd6，独立索引0d19dd7d8d728ffc6a440501a02e5ebb743aa5e53f76403481484867c4f7c4ed；39repo/3SDK未变，三项规格问题闭环。root采纳§1最小兼容范围并授权backend完整B03及独占Docker，主卡修订3新增精确移交；源码实现/产品验证尚未通过。设计提交前main@e96f93f，26本地提交待既有认证恢复，继续实现不重问。

- B03设计提交3e15bc3；纯contract4首块独立通过，源manifest362614c0976803232dbf3367fe65c8b685ff609481b687a87140df2c8dab0b58，31依赖稳定；独立29字段/plan-token、证据分离、并发复制/安全投影探针及race/vet通过，索引647aea6adfa72961fd931bbfe631ab15c1bb39736bb382343b80b0024fb9755d。V全停无资源，root精确提交冻结契约；backend继续活动core/Runtime/入口与真实测试，不读取或暂存未冻结实现。B03及D05仍未完成。

- B03纯contract提交a6c5329；完整64源已停写，manifest743417712e70b300c187fb3d4b99839bf416c4aa68fccebe2937ce8015bfe1a0，251依赖b611aaeb7b37b3b8f14ddb5660cbb30a906e4fc432d8383c66af54913bab0e42。最终check-go/相关对象102.594s/全部Central83.270s/反向worker2.689s作者通过，69nonce资源0。V独立315输入副本开始无过滤完整兼容与风险探针，root只读审查；主卡记录全部实际修复及边界，B03/D05仍未验收，28本地提交待原认证恢复。

- B03完整无过滤独立suite exit0（objects265.343s/旧兼容全过），但额外真实probe确认合法1s在原期限仍剩908ms时提前INVALID_STATE，原probe ae52f8d6dda0aefee4575d6af598ac5023aab9cf8165a15315e07365b8385482保留。V已停读停测/资源清零，root仅授权签名整秒计算及对应测试/单处文档名窄修；315输入原指纹保持，B03/D05未验收。详见主卡，HEAD fd83764，29本地提交待既有认证恢复。

- D05最终验收通过：B03期限窄修原ae52探针逐字通过、真实objects18.333s/race1.853s/vet通过，65源329c2c54f9adde6daef8fa2e05658dfa1163105f9eebf695515d1bb2c1c7f140及251依赖匹配，独立索引fe1a3a9af210968952d9fc7b3dce8f8c661bf66354bc2bb6feb505fbec6a08e3。复用未变全量兼容exit0，V全停/资源0；root采纳并精确提交。B01/B02/B03全部完成，无剩余确认阻塞，后续正式绑定/未执行真实host reboot等限制见主卡；下一D06，D06–D28/E01仍待完成。

## AT-0013：D06 Transactional Outbox 与事件投递

- 状态：已完成；D06 S01/B01/B02独立验收与root采纳通过，[主卡](../work-items/d06-transactional-outbox.md)修订2/设计修订5；原基线main@6b2ca24，下一模块D07。
- architecture_worker仅新增完整工程设计，明确注册屏障/typed事件/Tx组合/独立投递与重投/顺序/Project清理/Central生命周期，旧源码和契约只读；root拥有本卡/计划/台账，设计冻结后独立审查再授权实现。
- 00008为候选新全局迁移，旧00001–00007和Go依赖冻结；真实环境沿D05固定owned PG/MinIO，当前不使用Docker。D07–D28/E01不提前实施，当前无新产品待定。
- D05最终实现提交6b2ca24；31本地提交待既有GitHub认证恢复，未声称推送。

- S01 rev1静态审查需5项窄修：契约分层、Sequence编码、cursor可变limit、永久删除gate/幂等顺序、Restore后新准入。原设计626cd2c95cf1b0cac42b5855a5c58056766ce584bf41052c1cf12f1f21b7c0aa；独立证据620dff76132d9374d1de569925c5ddb730b9496d937af5f87aca4b3d882477d7。V全停无资源，root仅解冻设计rev2和D01 README最小定位给architecture，源码未授权；主卡记录完整输入与边界。HEAD671d95c，32本地提交待原认证恢复。

- S01 rev2独立静态通过，5项闭环，设计4703840c9216a7e9949549478bebfdcc266eab343302aec36e98069401e42f28，证据98172d8bdfa3cbc54004bee2fc305406c70ef258e3dee8aec6f2b1ccc733c58e；56输入匹配、52稳定依赖未变，所有执行者停写/资源0。root采纳§1最小增量，授权backend完整B01及owned Docker；app/D05核心/B02未解冻。详见主卡实施授权，D06产品未验收。

- B01中立event/contract四源独立通过：先确认并修复原error泄露、Header/Scope大小写别名覆盖、required时间戳缺失3项；原a7d7探针逐字闭环，12项race1.119s/vet/格式通过。source f358d3ae72639bd65be1e7ee3332afbf31dea0b635bb417e7041b06fd1c595f7、18依赖不变，证据5905f7806b1f2a405d03a98410bd86892e4aecb698a34b7be1b90c32df5dface。V全停无资源，root采纳并精确提交四源；backend继续非重叠持久化/迁移，B01/D06未验收。详见主卡。

- B01完整32源码已冻结交V，独立无过滤exit1：新Outbox/对象组通过，旧app退出后backend观察超时与SMTP取消误InternalError；原两用例定向过，app根因未明保留。独立确认Audit跨域FK阻塞正式Project清理（23503）与SMTP异步取消映射竞态；完整证据1d1adbdf751fd53731ef168cca2cb3194277a608b62270c55ec80f46914e2508。V346输入三份匹配、9nonce全清/全部停后，root仅解冻00008/smtp.go及相应回归给backend，Docker顺序移交；原probe逐字闭环后最终无过滤门槛仍必需，B01/D06未验收。详见主卡。

- 两窄修独立闭环，最终完整总组仅旧app blocked_io仍失败；清理前确证强停后PG长语句仍active/PgSleep20.249s。完整证据9a1ed8b79652ef95e8d07cbd13f8406b0516f479024aff25ec99dbdfd57af219，348输入匹配/3nonce全清/V全停。backend只读指出borrower移除早于异步cancel完成、Force仅快照operations可漏target；不把CleanupDone当server证明，不推定该PID单支原因。root仅授权architecture新增最小取消协调修复规格，源码继续冻结、预算/原断言不变；独立规格采纳后再修上游，B01整体仍阻塞。详见D06主卡。

- 取消修复规格rev2独立静态通过，设计ae1761dd12b5bca976dd1128351cb917be21875528349eeab9f639ff8e47a31a，索引884958a4feb7f442586faa384eebb072ee0cfe0b84c4ac36cfc4fb7b0929722d；唯一必修已区分caller/force/work与Rows内部cleanup，27依赖未变。作者/V全停后root采纳并授权backend最小store/新协调器及测试、独占Docker，先真实红probe再修复；sql/Tx/app/原预算及其他源冻结。最终独立无过滤门槛未通过，B01仍阻塞、B02未开始，详见主卡授权。

- 取消修复6增量已冻结，完整41源69736e0107f572255be2ed4ed6ac254f42a4b77b196f66438e7d8c132630da89与313依赖逐项匹配；旧实现确定性红、原probe修后green、本地barrier/真实取消复用/共享预算/迟到连接及check-go通过。作者末组原app启动migrating等待超时未明，一次有界复核过仍保留失败；27nonce全清后Docker交V。稳定354输入7a88d267f4c36211652e23e13a0d0dc8473d5f978a56699577802766bda4e152开始独立完整验收，无过滤门槛尚未完成，B02未开始；详细报告和限制见主卡。

- B01最终独立通过：单次无过滤exit0/379.319s，全部database/app/process/security/outbox/objects通过，原Audit/SMTP/取消probe和原app四模式通过；354稳定输入匹配、3nonce清零/V全停，索引90171448469612618f750d0035ebd937f3a12cc5421c76a2ce3767329c0b42da。root采纳并精确提交（上游取消8a2a4a4、SMTP8b881a1），保留一次历史启动未明失败。按设计rev2授权backend完整B02运行时/恢复/重投/诊断/Project清理/app生命周期，精确范围及共享ProcessGuard/预算见主卡；D06仍实现中，D07–D28/E01待完成，既有推送认证阻塞不变。

- B01提交f8dd8a9后B02实施中；两个公共载体缺口经设计rev3与独立窄审闭环，设计3c81bf8054c06f18eb0cc3dcd02bdf59324c3c11cd2eee9dc64b2988a339e704、索引c37f2a522670843f9128f90c6c78fb5eb60ff1513519fa46f43673133cbda61b。root追加authority.go/diagnostics.go及专用新测试给backend，LifecycleStep与typedSummary不加迁移、不变产品范围；其余契约冻结。内部调度恢复并行继续，B02尚未验收，详见主卡。

- B02公共四源首轮独立确认诊断解码2缺陷：合法67463字节误受事件64KiB限制、required截断bool接受null丢语义。原probe bdf946a5791173eb39f5df904f1a0ad295438df7a4b928172bf504403d09dbc5，索引d438193dfb79915043d82ef6e30f553eb6b5b8a5285c3ffb99894c199ce2a9d1；其他绑定/安全/JSON边界过且35输入不变。V全停后仅解冻diagnostics.go/新测试集中修，内部B02继续，公共补口未验收。详见主卡。

- 公共四源返修独立通过，source8a77532a002ea075db5a13d3ca58b92f70d754c0426b7d093b3c2c8d759f6a55、31deps不变，原bdf946探针逐字闭环，normal0.026/race1.370/vet过且35输入匹配；索引ecd7a1cd42cfd32ede2a936c36c0cc79d0f7aa5514d3dcfa6744cce024e4c4d1。V全停无资源后root采纳精确提交；内部B02仍实施，未纳入契约验收。

- 公共rev3实现提交d866d5c。零尝试停止约束缺口经设计rev4独立静态通过（a01ec4ca6cf992b481eca2e4a3e173294036667bf966308f266fc82f5f23ce94，索引72cbc8f9a777d29be6fd94ebc0a9792468410a49b3350215d71562ff11af7d1d）；root授权新00009/迁移测试及authority RequeueTarget/专用测试，旧00008和真实callback身份不改，不造attempt/latency。同步最小旧process capability断言，完整授权及未完验收见主卡。B02仍实施中。

- RequeueTarget两源独立通过（source50c07e5d80078884d9f2ced2816f374432a8d6d75167af2db7feba1711e5f099、34deps不变），normal0.031/race1.366/vet过，36输入匹配，索引e6334594e01f025ddc58f1e31baef74f48b5068caf33f7ce2edd94bc823b6332；V全停后root采纳精确提交。真实迁移/两阶段锁union/人工重投和B02整体仍待完成，不以纯契约通过代替。

- 设计rev5可信生命周期恢复/精确预取消独立静态通过，设计f295dd0dd98408efa0681016258ef8b0785f87e8492323e6637d1e3cd15e8865、索引6a50926acbffcba3eab079e62c9c53ac4d9288cedc24be00cf4440368fea995f。root采纳并授权新可选LifecycleActorResolver及测试，不改既有ProjectAuthority/schema；真实T15纳入完整B02。作者诊断/迁移/停止清理定向已通过，尚未独立验收；授权和证据见主卡。

- B02完整45源/345依赖已冻结，390 union7c2a7dfb7f7c34d535fd4c320f743771ac9de288c346ea28ccc7401ee19199dd逐项匹配；最终check-go/60真实顶层作者通过，51nonce清零且全停。V独占Docker在稳定副本独立审查与无过滤验收中，terminalProof错误分类疑点待验证；业务源码尚未采纳，详见主卡。

- B02首轮V确认V-B02-01生命周期吞终止证明硬错误；原probe085815e19ec04320b79876980b9d1fea9b7d190412a95f10752d3f0f9c551563，索引b0eafff3e74f83d54984f27b0a99f16c0cfc97ab538a3dfe231ad2a139888669。9项独立真实风险通过但整体未通过；V全停/9nonce归零。root仅解冻cleanup.go及新lifecycle_errors_test.go，保留独立推进并返回硬错，修后原probe和无过滤最终验收，详主卡。

- V-B02-01原probe修后逐字关闭，但无过滤exit1/440.813s唯一失败为旧database显式Terminate清理，具体底层分支未确证；其他app/process/security/outbox/objects过。索引41234373f5c6771a99ecf8fb8d61c99e58e2a59ede6885e79a5912b9dd10c997，391输入匹配/3nonce清零/V全停。root最小授权fixture.Terminate与新真实回归，先确定性旧红后修exact PID已消失幂等且foreign/真实错误拒绝，原产品行为/测试预算不改，完整门槛仍待。

- D06最终完成：48源/344依赖稳定，单次无过滤exit0/422.711s全部包通过，原78449b/085815逐字闭环及新增真实42501探针过。最终索引b876565946459a7d24f3f53c8fbe209eb26579c96879559e274c6ad3b88f83cd，392冻结/395执行输入一致，3nonce清零/V全停，root审查采纳。helper提交1ac84e8，其余B02随最终提交；未绑定责任及历史失败根因限定见主卡。下一D07；D07–D28/E01待完成，既有推送认证阻塞不变。


## AT-0014：D07账号、Session、SMTP与个人资料

- 状态：实现中；唯一活动模块D07，[主卡](../work-items/d07-account-session-smtp.md)修订2，基线main@57bfadb，D06已完整验收提交且工作区干净。
- S01 architecture独占新工程规格；R01 backend仓库只读与/tmp有界技术证据，二者不使用Docker或改源码/依赖；root拥有主卡/计划/台账。采用既定GoCaptcha和账号/SMTP产品规则，不重复产品确认。
- D07后端/HTTP/正式绑定先完成，D26统一客户端/账号个人页面、D27系统设置按计划后续接入；不提前业务UI或D08。00010候选、旧迁移与依赖冻结，新增口/依赖须先规格采纳。GitHub认证既有阻塞未解除，仅本地提交。

- D07 R01只读研究完成，报告65f8ec602bb4a67f10fcfba10838a53a51c94ecb6e98eecc9cef06ed8c03ae01，具体版本/短基准/素材与未验边界见主卡。S01 rev1独立确认唯一response lease组合缺口，索引d06e47a1fa63745251dba5e31f7c465f1c01611c65997b7bcf0ede575413abe4；87输入一致/V全停。root仅解冻设计补真实独立读取attempt及有界file sink澄清，代码未授权、D07仍设计中。

- S01设计rev2独立静态通过，源4e968882be968f79b00556032571645860ef9656816dc0494e68cb0f09b50321，索引29ac3d4cdd0000638281d4bbd1979f35db2687b3132948b47150ee1657085c4a；88输入稳定/作者与V全停。root采纳正式补口及固定依赖，主卡修订2授权backend完整B01身份/安全基础及独占owned Docker；B02–B04与app/HTTP/产品UI仍未解冻，D07产品尚未验收。

- B01组合核对补充：logout须同Tx sessions-revoked，root将设计已定account/events及contract/events的该事件codec/producer子集提前B01，验证真实撤销/Audit/event/receipt原子性；delivery-requested/handler仍B02，旧Outbox不改。详主卡追加授权。

- B01发现旧Secret InTx口无法显式传UsageDependencies；root暂停仅该新增公开接口，由architecture在稳定Git基线只读下修设计rev3，拟Tx外Discover→完整union→ApplyUsageInTx。独立窄审后再实施，backend其余基础继续，活动源不被审查。

- usage补口设计rev3独立通过，源df7338362a074b535710918863cf97b0c23425ec5497a15ab1c78c3c6a7f5312、索引9fffad6747c241df37591200da839d3a08bc8e44076ef41e550e197a5ab63ad8；11稳定语义输入匹配、活动代码零读取、全停。root采纳并解冻最小公开口及实现给backend，B01继续，主卡记录旧5测试/fixture包列表最小适配。

- B01 recoverylog独立两源通过，source c2fd8ad79232307d9cf2dc7c9df4313a40cd216e71f026e76a7bd81b55730688，索引c8f2d6caf63687c04c6b46b2782c1043caf8db75e5c70b1f9ec3a4e111324b3b；普通/race/vet与5风险probe过、全停。root另验旧提交基线+两源独立可编译测试过后精确提交，B01其余源码仍活动。Unknown非join及B03首写门禁组合限制见主卡，不算账号/投递完整验收。

- B01作者第五轮真实账号现有组race通过18.322s，bootstrap/login/两独立回应lease/logout真实Outbox等局部过，前轮迁移夹具/新Audit约束/摘要/回应ctx错误及修复保留主卡。源码仍活动未独立验收；恢复、unknown与授权竞争继续，不能推进B02。

- B01完整77源/364依赖已冻结，441union b4a1c9cf8606bf5f096e899b7d0a9b98e02e20937eda0fd7567f81bfc49d67c0匹配；最终check-go/Account真实race140.120s/integration vet作者过，42nonce归零全停。V独占Docker开始稳定副本完整独立验收及无过滤兼容，root同输入审查；源码尚未采纳，B02未开始，详主卡。

- B01独立确认D07-B01-V01：response-plan原unknown被确认55P03掩盖，删除local join证据，原晚commit后计划不收敛；原probe a43110d41d3447e46fd3efe47215fb0b38916f00e60419ce8b3a60be85661bc2，索引9e23ed1521e986c63f2a9e96eb76a19c7e18cce4cc80b9e81226b2ee2c64f25a。441输入一致/6nonce0/V全停，root仅解冻response.go与新回归给backend，原probe/预算不变，修后独立完整门槛尚待。

- V01窄修两增量已冻结，78源/364依赖442union 73644f824ff09ea9aff0106668d35684d9b3fdd3655c993d362120c8b4fb45df匹配，原a43110逐字作者green/邻组race/check-go过，索引2329a1145e871e17924d86b5e28721edb432df70871cb4347250f2384f11633a。6nonce0/全停，V独占Docker继续最终独立风险+无过滤全组，B01仍未验收。

- V01原probe独立闭环，populated旧数据升级未失败；新增V02六phase真实55P03导致Unknown误标not_committed，分类probe29448f3d07bed7aed76855cb1eb059ead2c82d5afc97063d67226ce5012cdb88，索引5d63aafc2c9f454c728f1b318c75a24c6a03fe6200353efe94b17bad2d700cc0。442一致/3nonce0/中断后全停，root授权account五文件最小统一状态/cause修正及新回归，原预算不动。Logout并发仍未验候选，无过滤仍待，详主卡。

- V02统一7增量作者原两probe/新分类与failed-login/邻组race162.684s/check-go过，80源444union8a6155cdf0d7a9e96e2e683ebe8e833dc5911357d7263366ef9034423bab8082匹配。另真实Logout内部序列竞争已确认，674caee24adc7f742723d147bb92415346351bdf4ee86d1b70a7ab562791d0a0同key持续Busy；9nonce0全停后root仅授权logout.go+新回归重规划尚未提交内部seq，已提交事实不改。V02/此修复待最终独立验收，无过滤未跑。

- D07 B01 Logout两文件窄修冻结并交最终独立复验：81源b4b792b7/445union74182c10，root全匹配，原probe逐字作者green8.486s、新旧Logout真实race42.817s/check-go全过；6nonce资源0/所有命令停止。V独占Docker复验V01/V02/Logout原probe并单次无过滤test-objects；B01未验收、B02未授权。详见主卡末尾。

最终V首个无过滤实际exit1/519.722s，日志SHA `22c4b153e3771795327ad0ed87f302171bf7b9e211a8b232760a96651af5355a`，目录`/tmp/agenteam-d07-b01-complete-verify-xn6_txkh`。Account真实race303.590s，三原缺陷probe及populated升级/全部正式回归通过；其它已过组含PG1.073s、database117.015s、app72.961s、process152.918s、security190.654s。未通过：Outbox TestOutboxRuntimeProtectedSixtyFourDoNotStarveTail 在startRuntime返回INTERNAL_ERROR（4.29s，包199.177s）；objects包原6m整体超时360.146s，栈在Transfer新fixture的Descriptor.Verify/owned docker inspect，不能据此断定产品或负载原因。完整原日志/栈保留，不以Account green标B01验收。root授权V先精确清理、在/tmp稳定副本仅增加失败cause日志作一次Outbox定向诊断，原源码/断言/ItemTimeout20ms/预算不变；对象尚不重跑，B02未授权。

Outbox单次诊断在保持原20ms/断言下再次失败（用例8.55s、包8.610s/exit1）；日志`/tmp/agenteam-d07-b01-outbox-diagnostic-mf9008qi/evidence/outbox-diagnostic.log` SHA `6c7de1b5347a320bc4eacf3f968ea9475defdecec5ee69128dcb79fe1f3eddc3`。phase=initialize，cause为Fault(INTERNAL_ERROR/not_committed)→postgres.Error→pgconn.connLockError→errorString，无SQLSTATE且Is(Canceled/DeadlineExceeded)false。类型本身不能区分conn closed/busy或EOF包装，尚不认定根因；V停止新增测试，先清理并只读定位最小上游范围。首轮完整资源清理SHA `51792928fa60a0bb3ce48efbdaada8c2838fc63bebb9beffb8bed272c806645b`。objects总包6m报警时该传输子用例只运行1s，不能误称单项卡6m。

### Outbox上游连接关闭诊断授权

V核pinned pgx5.11.0的connLockError.Unwrap，cause尾部errorString只对应ErrConnClosed，不能称conn busy；本次未采postgres.Error.Code，尚不能区分Begin/set_config或Tx SQL。静态已排除批次Rows未关/QueryRow未关的相邻路径。pool watcher取消后关闭data、driver仅返ErrConnClosed可能丢取消归因，为待验解释而非确认根因；不得无条件吞connclosed、放宽20ms或把hard错误都改Busy。第二批3nonce/进程0，V不再测试、仅整理/tmp报告。

root授权backend下一步仅只读冻结代码和自有/tmp诊断副本，不修改仓库/依赖/旧预算/断言。Docker独占移交backend；先对原Outbox公平性用例增加安全的postgres.Code及准确阶段/实际ctx与本地取消来源记录，有界复现以区分真实自有取消关闭、独立连接断开和提交后Unknown。必要时在/tmp增加受控真实PG/TCP探针取得确定性正反证，最多两轮诊断，仍无新证据则停止报告。原用例/日志/probe完整保留。当前没有授权生产修复；报告确认机制、最小文件范围、旧红与对照后由root下达窄修。对象整体超时仍未归因，暂不另跑对象/无过滤全组。B01与D07保持未验收，B02不解冻。

最终独立阶段报告已全部停写：`/tmp/agenteam-d07-b01-complete-verify-xn6_txkh/review-report.md` SHA `a22943edada819f582ee19e37190e972321ddb9c203b7dfbd72bcdab5f355f6f`，36项证据索引 `bab07a08e9a491d947ca1f1c98bcc2d6aa475cbb874ce5de2b388cbef39557d9`。root全文核读，445根/副本、449完整执行、450诊断执行均末次无漂移，6nonce及测试进程0。Account三修独立闭环但完整兼容失败，B01不验收；backend仅/tmp诊断继续。

诊断第一轮原公平性单项exit0/6.588s，64条正常保护记录，无watcher/connclosed，不能覆盖原失败。第二轮受控探针在Open前因临时MAX_CONNS=1低于冻结Config最小2而失败，未进入目标协议场景，属准备错误而非产品证据。root追加仅将/tmp探针MAX_CONNS改为2的机械修正并补跑原第二轮一次；不改生产/断言/时序/预算，不新增第三种场景，保留原失败日志。若修正后仍未达目标，停止汇报，不继续扩大。

### D03取消归因确认缺陷与窄修授权

纠正后第二轮真实旧红仅目标断言失败（database0.935s）：ownedPID129的20ms期限→实际watcher→真实CancelRequest ACK→Prepare阻在Unwatch→work关闭data→SQL owner discard→Exec返回ErrConnClosed/DATABASE_SQL_FAILED，却Is(DeadlineExceeded)=false、父ctx仍live，Outbox分类仍InternalError/not_committed。独立TCP先断开再到期对照保hard错误通过（该对照是EOF类，不冒称同ErrConnClosed）。日志SHA `ffebddafc0100878174dca4207cab3a3d09d6a9a9a40576fba3d2d99e9d2aeb1`，`/tmp/agenteam-d07-outbox-cancel-diagnostic-2l3l_tq3/evidence/round2-observations.json`保存真实顺序；原probe `round2-probe-frozen.go.txt` SHA `2aceacc33082fdddad7fe3785229782db71210a36539ae1c93243be3fb170320`。root核读原probe与完整观察，采纳为确认的上游取消原因丢失缺陷；并未证明它是历史公平性失败唯一原因。作者九nonce资源/进程0、445根输入不变、执行命令全停。

root仅解冻backend的`internal/central/postgres/{pool_cancellation,sql,transaction}.go`，新增同包取消归因测试与`tests/database/`实际回归。目标：在精确physical target、实际watched context/取消原因、非Force/非cleanup且SQL owner确认由该次本地discard关闭driver的证据下，仅对精确ErrConnClosed保留原cause并补取消原因；不能依据ctx后来到期、连接closed或存在work泛化归因。SQL/Rows/BEGIN/set_config/AcquireAll等适用pre-COMMIT边界使用一致私有规则；COMMIT已经调用后原Unknown分支不改，不把取消当远端已停止。原Outbox recoveryBudgetError、所有旧断言/20ms/20s/6m、D03实际join/Force共享预算与nonce安全保持不变。store.go或额外旧文件如必要先报告。

必须保留旧红探针逐字修后闭环（临时诊断helper仅作同语义观察），新增精确非watcher ErrConnClosed、无关watcher/后到期、Force与正常Rows清理不误归因等负例；不以原EOF对照冒充。运行适用普通/race/真实PG取消、unknown/原公平性与相邻恢复检查、check-go，冻结精确delta及扩大依赖manifest、命令全停/资源0后再独立验收。原公平性即使后续green也保留历史未采具体phase限制。objects累计超时另待组合验收处置，当前不修改其预算或断言。D07仍唯一活动模块，必要上游返修不代表D03整体重做；B01未验收、B02未授权。

诊断最终报告`/tmp/agenteam-d07-outbox-cancel-diagnostic-2l3l_tq3/diagnostic-report.md` SHA `3a35aa1b9514a18478c10b9b2f477ba46e34597b123e583d82f6ec69c08583ae`，索引 `9d92f90ca3676c194b9551c9e0791f29c696f4e8c0c2ccfd5b757900ce4c99cb`；root全文核读。原观察不变副本`round2-observations-frozen.json` SHA `10e820d9c4ecf436ab3c39177fa8b4e6b3e6ec26c6b0200e5b6f58ee63c498d4`，逐字probe复跑可覆盖其固定raw输出路径，不能覆盖-frozen证据。真实独立断开时driver也可能自行发CancelRequest，故单凭取消包不构成平台watcher归因。cleanup/root-inputs SHA `63d45f1746d3883a677f07b8bf8e6404b05812d7af75d72aa21c259770586d04`，9nonce0/445不变。诊断全停后进入另行已授权三文件窄修。

D03窄修首轮普通postgres0.065s通过；新增真实回归首轮begin探针只匹配begin而真实SQL为`begin isolation level read committed`，属新测试前置未命中，原real-first日志保留；其余6正例和2真实同ErrConnClosed独立关闭负例通过。机械匹配修正后真正命中BEGIN→ReadyForQuery→Unwatch本地关闭→BeginTx成功→set_config新scope丢原因，real-begin-second.log保留。root采纳该连续初始化阶段证据，允许授权transaction.go内BEGIN+set_config同一私有初始化scope，进业务callback前结束，不能跨独立业务SQL或COMMIT传播；补旧初始化scope不能复用负例，不扩源/预算。作者证据目录`/tmp/agenteam-d07-cancel-attribution-repair-vdga3kll`，仍活动未冻结。

作者原2aceacc探针逐字修后green（overlay database9.982s，含原COMMIT unknown/相邻事务），但同组原公平性仍在startRuntime失败为DEPENDENCY_UNAVAILABLE（5.69s），未采cause不得推同根因或已修。日志`/tmp/agenteam-d07-cancel-attribution-repair-vdga3kll/evidence/repair-regression-first.log`保留、3nonce已清。该轮filter尾锚未包含新增七路径前缀，不计其通过。root授权先正确filter复核新增实际组，再最多一次/tmp原公平性失败安全cause/Code/SQLSTATE/phase诊断，原Outbox/预算/断言仍不改；超出三源的必要修复先报告。

作者正确prefix真实取消组通过：postgres2.170s/database10.145s，覆盖七路径、真实同ErrConnClosed独立关闭先/后取消及原PoolCancellation的Force/join/Rows/失败边界，real-all-cancellation.log保留，3nonce0。获准一次公平性诊断随后exit0/outbox11.735s，fairness-state.json记录64项，其中SQL/AcquireAll/Rows实际三次ErrConnClosed经精确归因保留Deadline，parent live/初始化成功；3nonce0。先前DEPENDENCY_UNAVAILABLE未复现、具体分支仍未知，不能被本次green覆盖。停止新增诊断，作者进入最终check-go/冻结；B01完整门槛待独立检查。

- D07必要D03取消归因窄修冻结450输入23f17dc7，root匹配/三源diff核读；最终check-go通过、15nonce0/命令全停。Docker交V独立原probe/归因正反及最终无过滤兼容，B01仍未验收。详见主卡最新记录。

独立V定向真实组exit0（postgres4.223s/database16.239s），原2aceacc逐字通过；重建原453观察输入manifest `9988068c8a30f116a83ab08fea43ef376055e00d32999c4b694c9f74d3c28fa6`完全匹配。日志`/tmp/agenteam-d07-cancel-verify-6b_flvvy/evidence/targeted-real.log` SHA `c9d473901087af7ba9118a834ede7b6fdec0d63b98b269b2c23ff12b80a03c19`：实际watcher ErrConnClosed+Deadline→ResourceBusy/parent live，独立断开仍hard，原frozen观察不变。3nonce0，450根/副本、454纯源执行/453观察执行末次一致；V开始默认调度、原6m预算、纯源加四Account原probe的单次无过滤，完整门槛仍待。

上游修后独立纯源无过滤再次出现旧公平性startRuntime DEPENDENCY_UNAVAILABLE（outbox包208.171s），其它包尚运行，不能提前判整组成功。root已授权V收齐全组/精确清理后最多一次/tmp安全Code/cause增强定向诊断，不改生产/预算/断言；只读候选是store.borrow在pool.Acquire成功但late ctx取消时AdmissionStopped(nil)丢原因，尚未证实该次分支，不作为根因结论。当前三源保持冻结，暂无额外生产授权。

修后第二次独立无过滤完整exit1/525.343s，日志SHA `30889aa7e7aad43d18cecaf2c1d3f775554998feefabb09fcfbf6fcb25354c1d`。Outbox208.171s在启动DependencyUnavailable，objects360.077s原6m累计超时；其余10包通过，Account319.585s含四原probe。objects报警时TransferIssueWaitsForRealAuthorityAndProjectGates仅2s、operation子例1s，栈仍fixture→Descriptor.Client/Verify→owned docker inspect，不能推该子例卡6m或根因负载。3nonce全0/原命令及fixture/test PID消失，V继续既有一次安全cause诊断，不扩大预算或重跑full。

修后V唯一诊断再次真实失败（test9.40s/package9.502s，exit1/84.128s），`/tmp/agenteam-d07-late-checkout-diagnostic-msusku69/evidence/diagnostic.log` SHA `9cdb786796e3c269493313fe054627a3a72d66f4d5df26bbad02a03c7a7762bc`。phase initialize，Fault InternalError/not_committed→DATABASE_SQL_FAILED(hasCause,无SQLSTATE)→ErrConnClosed，IsCanceled/Deadline均false；此轮不是borrow AdmissionStopped(nil)候选，亦不能解释前次full的DependencyUnavailable。归因机制原probe通过但实际公平性仍未闭环，V停止追加测试/清理/报告，root等待完整交接再下有界精确顺序诊断。不得因此放宽一般closed规则或预算。

### 修后剩余ErrConnClosed有界诊断

V本轮9nonce资源0/执行命令全停，final-cleanup SHA `26bef2ee7cc21b1d97705fdd97a41d26fc210d0433bbcc0c7183366578001a23`；仅/tmp阶段报告整理。root将Docker独占交backend，所有450仓库技术输入继续冻结。授权最多两轮仅/tmp观察诊断：对原公平性同一代码/20ms/20s/6m/断言记录每次SQL私有上下文、actualwatcher选择/拒绝原因、exacttarget/work/Force/internal标志、SQL-owner Unwatch前后driver状态、首次错误阶段和原Code/ErrConnClosed/ctx状态；不记录SQL值或凭据、不从异步watcher读driver可变状态。每轮保完整原probe/diff/trace，出现明确失败再据实选择第二轮受控复现或正反；无新证据则收束，不反复跑绿。此次可同时记录late checkout拒绝分类，但不把它预设为根因。

不得在仓库修改/放宽归因规则、Outbox分类或预算；实际driver已先关闭、ctx不匹配、work已selected等只作候选，需真实时序。已通过原2aceacc及正反证据保留，不能用新机制否认旧修复有效，也不能用旧probe替代现实失败。取得确定机制与最小必要修复范围后再由root授权。objects累计超时不另跑，B01/D07未通过、B02不解冻。

修后独立阶段报告全部停止，`/tmp/agenteam-d07-cancel-verify-6b_flvvy/review-report.md` SHA `1f79ef656ea450a4aff5e4d5bf01c608cc7ab8b81e3c8f5efd57f9265aa5c660`，45项索引 `03dce372da0dc219f17ab96cc69fe84494381e7553683d44e82bea05b610283c`；root全文核读，450/454/453/451各输入末次匹配。静态五类遗漏候选另存late-checkout诊断目录static-candidates.md，未裁定，已转作者有界观察；全部历史失败和独立通过范围保留。

剩余路径首轮/tmp观察实际exit0/outbox7.554s，目录`/tmp/agenteam-d07-cancel-remaining-cf460kh_`，2099记录/65次BEGIN-set_config/339scope，但watcher/Unwatch/ErrConnClosed均0，因此无新失败顺序，不消除历史失败。作者遵守无进展收束停止随机原用例重跑，保持450输入冻结。root仅请求只读评估业务相邻SQL受控候选（SQL真实完成ReadyForQuery→取消Unwatch→第一SQL成功→同caller下一SQL ErrConnClosed），未经新授权不执行。

### 业务相邻SQL确定时序补证

作者只读pinned pgconn ResultReader.Close：真实ReadyForQuery分支先Unwatch再返回既有tag/err，可在本地取消关闭后仍返回nil；当前Exec仅err非nil提归因，defer结束scope后同caller下一Exec新scope可能失去原因。root采纳为值得一次确定时序检验的候选，追加仅/tmp一次受控探针授权，不重跑随机公平性：原代理放行Prepare第一个Z，在真实Execute第二个Z设置barrier，观察实际ResultReader.Close/Unwatch及CancelRequest ACK；同item/caller连续两Exec分别核第一nil/第二exactClosed及相同target。独立真实断开+当前ctx未取消/后来到期对照保持hard。不得伪造PG结果/扩大20ms等预算/修改生产，准备机械错误保留后可在同一目标范围修正；取得结果后冻结完整probe/trace/输入和资源0，报告机制与最小方案，不能冒称历史公平性唯一根因。

业务相邻SQL受控补证真实命中旧红（database1.121s，仅预期归因断言失败）：`/tmp/agenteam-d07-adjacent-sql-829y6xy0`，原probe SHA `476d960eeea5cdbb1988c32a7720defdc22bbd40a6853c6c8406573abfb10af1`。same target1/operation3/caller：scope4实际watcher→ACK→Unwatch open→closed/discardedtrue→第一Exec nil；scope6下一Exec raw exactClosed但无reason/discard证明，IsDeadlinefalse、InternalError/not_committed。独立真实断开先于item到期对照保hard（该对照非exactClosed）。root采纳此新确定机制，不称历史唯一根因；等待完整清理冻结报告与最小方案，当前未授权源码。建议若保留物理连接终局证明，必须sameoperation/same原caller/同取消原因，不跨新caller/Force/internal/独立close；任意context接口身份比较不得panic，未知形状安全拒绝。

### 业务相邻SQL终局关闭证明窄修

补证执行已结束，仅预期正例归因断言红，三nonce/进程0，450仓库与453执行末次相同。root采纳最小方案并授权backend：仅解冻`postgres/pool_cancellation.go`、`postgres/transaction.go`；在新`pool_attribution_test.go`增量归因测试、新`tests/database/`相邻SQL真实回归，允许本块新`cancellation_attribution_proxy_test.go`仅添加Execute第二Z阶段，已有阶段/旧断言原样。其它源特别sql.go/store/Outbox/依赖/迁移/预算冻结，额外必要文件先报告。

已证owner Unwatch open→closed时产生不可变本地终局证明，绑定exact physical target/operation/业务域原caller及原取消原因；后续业务scope只能在相同operation/caller与原取消仍匹配时对精确ErrConnClosed附原原因。它不是复活已结束scope权限，不跨初始化→业务、COMMIT、不同caller/operation、Force/internal清理或独立closed，不从晚到期反推原因。context身份先安全可比较检查，不可比较/未知形状拒继承不panic。sql.go现有精确错误边界可复用，COMMIT Unknown保持。

新476d probe须逐字修后闭环，保原2aceacc与frozen观察；补同caller相邻正例、不同caller/op、后来到期、Force/internal、独立closed、初始化边界、不可比较context与COMMIT Unknown负例，保原七路径/真实exactClosed对照。完成有意义定向真实检查和check-go，冻结新delta/完整输入/资源0后交独立V；不随机反复公平性跑绿，未归因历史错误保留；objects超时另待完整验收，B01仍未完成。

相邻SQL补证最终报告SHA `128f64839755740a4bd5a68359e429f0b088aa5a7ed666420d413ba014d32cf1`、15项索引 `745818bd4409722b566ec33b5202ab4edd2edac6b442785dbcc8150d785d2a1d`，root全文核读。执行453manifest `910b38cc22f5ea075df98756e1fc197ec062b717f8d8a7f733969c2359880d63`、原红日志 `acc775c1808d091777f33033e3411dfdb860708f8954a1305ead9dc74bec8208`、不可变观察 `832f57497f03c4113a38d71fc545fa678226fcc45cf3602015e3f4b49e9e0a82`、cleanup `d0c5cd7782921ee017e691ad00154c0d4df72f21e539ba6c3bb3580967ddf1bc`。首轮无取消报告 `ebe11c02526d40acbc341cc59631253132c48e5b580fb737dc1694275206c6ed`/12项索引 `f6dbce4204b40a4d1562501428a044e97d5a4328545245e7ab43357f11385e3a`亦已核读，两个诊断目录均停写，作者转已授权两源窄修。

相邻SQL窄修冻结：`/tmp/agenteam-d07-adjacent-repair-afjky7am`，90源 `46ff1a820a235d6484e6967613686ae5bda363e3d265fde032c6ccff504fff43`、361deps仍 `a0a0c24522ae9f9546a4db24dc5170532507021b65e453431f0ba385cd82384a`、451union `2daa8e6714f365fd09678792eee442390826f5fcb1245f1fe0eb3af68ff93878`、delta5 `1fa5045842023b0c95b7dfd2077996e9c70b6127117eb74e1d584e0a45e294c4`，root全部匹配且已审源码；446旧项未变。两原probe作者逐字green（2aceacc database2.069s；476d相邻组），新增相邻持久race4.117s，最终check-go/integrationvet全过，15本轮诊断/修复nonce资源0，命令全停，仅/tmp报告收束。首轮新增rows/othercaller在ACK完成前的准备断言失败已保留，移动同断言到actualjoin后过，但未冒称准确原失败布尔已证。

root下一独立完整运行改为显式包级串行`GOFLAGS=-p=1 sh scripts/test-objects.sh`，仍无过滤、原每包6m/测试内部并发/断言和生产预算不变：前两次默认跨包并行均在对象包累计6m中断，改编排以取得全部包的完整执行，而非再次同条件运行。保留默认调度失败，不能据串行通过反推负载根因已证明；若对象仍超时或新失败，保留分包/具体运行结果再定位，不自行放宽预算。先独立复验两取消原probe与直接正反，再只做该一次全套，不重跑同路由脚本。源码全部冻结，Docker交V。

相邻窄修最终作者报告SHA `2ea0f24ee14b6296b6e6de6e547363b8610f67e6925fdf34ccd05fb9b2b4d560`、34项索引 `4d0ffc3aceb21d1cea40a55f22e278d136b93db6d2bb0ca15cee27bb5c678272`，root全文核读；check-go `4bd9411cee028a47969f716b615ed1379eeb154895aba2a12a8773798f29c29a`、final-cleanup `3c757cbca65e709613c43287d50a78260802bb7ada25faf2567ed8e7152b9c08`。15nonce含2诊断+3修复，修复本身9，均0；作者报告亦全停。V稳定副本`/tmp/agenteam-d07-adjacent-verify-izjj__3a/repo`匹配451输入，独立diff `ed93d33f852ee04336a1b58534fc1cbe86942cecf89e31b5dfad9b33ce7c842e`；两原probe因helper同名分别观察副本执行，纯源全组无插桩。

相邻修复独立定向通过：476d原probe+5模式/七路径/真实exactClosed/旧PoolCancellation组合postgres2.047s/database19.110s，日志SHA `5a6ecc5678ee88b8f14ee607a42ab44e54ede95d73f8281d19545808fd617348`；2ace原probe database2.643s，日志 `38b8229099b33412fe4690fabda843b359d71ae3085571ca5155be2865b577ed`。局部race1.066s/vet过，两frozen旧红不变，6nonce资源0。V启动纯源455输入（451+4Account原probe），manifest `5f93d80990f954ca5161f00ddf1306d639fb1146d12a551d143bdd3d7d689938`；唯一`GOFLAGS=-p=1`无过滤日志`/tmp/agenteam-d07-adjacent-verify-izjj__3a/evidence/full-serial.log`，保原每包6m及内部并发。完整门槛仍待。


## B01完整验收采纳

root核读最终独立报告并采纳B01（不代表D07整模块完成）：`/tmp/agenteam-d07-adjacent-verify-izjj__3a/review-report.md` SHA `5f792a3b9e29ea18b5aa2e96fc6e65b52d5fadd7c06b50741e8510d088daf82b`，48项证据索引 `5ff98b8070886fbd1e8a45d43ab140a60eccccc827d2cf18605c40e2df32ebd7`。451正式输入及455纯源执行、两个观察副本末次全部匹配；六个原probe及两份冻结修前观察未变。两取消原probe逐字通过，四Account原probe随完整组通过；相同输入作者check-go普通/vet/integration-vet/race/双构建通过。

唯一包级串行无过滤`GOFLAGS=-p=1 sh scripts/test-objects.sh` exit0/wall998.911s，12包全过：postgres1.535、database60.616、app45.702、process112.646、security83.968、Outbox89.069、内部Outbox1.032/contract1.290、Account210.050、objects299.846、内部object2.291/contract1.029秒。日志SHA `64018fb35114a68fa56367d9f2b70fc138c95d1006c1d546101d12e4424f253a`；保原每包6m、内部并发与断言。默认并行历史两次累计超时/Outbox初始化错误仍保留，串行成功不证明负载或历史唯一根因，不宣称默认调度性能通过。

清理SHA `8faa47232b84c6c12266dc96037e74fb4b46994e5282c6041081dc45af3ace16`：9自有nonce容器/网络/runtime及owned进程0。作者与V全部停止源码/报告写入及命令，Docker交回root。root再核90源与diff-check通过，按精确manifest提交B01及必要D03归因修复。HTTP/app装配、真实挑战、邀请/reset、SMTP及资料仍属B02–B04，当前诊断ready=false；下一步为B02精确授权。GitHub认证既有阻塞未解除，本次仅本地提交，不声称推送。


## B02实施授权

B01已提交`8b0261b`，本块基线为该提交；设计修订3不变。root授权backend完整B02，必读设计§1/3/4/6/7/8/9及T04–T07、T10适用项，沿go-development技能，Vue harness另读vue-development、vue-testing-best-practices及可用playwright技能。不得再委派或Git写；root独占本卡/计划/台账，V暂全停。Docker/owned fixture和harness资源独占backend，不连接既有基础设施或真实凭据。

解冻新`internal/central/account/{invitation,reset,password_change,challenge,delivery_intent,delivery_handler,cleanup}.go`及对应私有辅助/测试，`account/contract/{challenge,invitation,recovery}.go`；已由B01建立的`account/events.go`、`contract/events.go`添加正式delivery-requested。为实际绑定B02允许同域`account/{service,repository,planning,authority,audit_authority,secret_authority,login,session,recovery,local_recovery,command_record,browser,response,logout}.go`及`contract/{types,commands,identity}.go`必要组合增量和对应测试；保留B01原回归/Unknown/lease/身份语义，不借此重写无关基础。新增`tests/account/`本块组合测试，可在既有common fixture作必要组合；新`tests/account-captcha-web/`独立锁定依赖与真实浏览器harness、平台自有生成素材。go.mod/go.sum仅引入设计§1固定GoCaptcha2.0.5、x/image0.45.0及指定freetype，保留旧模块版本。Vue/Playwright固定版本按设计核官方integrity和实际Chromium，不改产品web或根前端锁。

00001–00010迁移、D03/D04/D05/D06旧域、recoverylog、HTTP/app/config与B03accountmail仍冻结；如现有契约确实缺必要口或表约束缺口，先报具体证据/最小范围，root裁决后再改。真实Outbox handler只在同Tx从合法intent幂等建job与marker，并实现canonical补齐；SMTP发送/日志首写门禁适配留B03，不用成功stub代替。B02需提供正式跨后块门禁/当前合法性能力，只有确有本块调用才实现。

验收包含固定24h邀请/同链接重发不续期、管理员当前权限、并发兑换/撤销/到期与用户名邮箱唯一、reset公开202隐私与异步受理/限额/同链接、改密换当前Session撤他会话/reset全撤、Audit/Secret引用/Outbox/receipt原子性、未知提交查事实/异义拒绝/零材料、当前lease真实join与公平到期回收。挑战需真实GoCaptcha生成→官方Vue旋转/键盘→验证→登录一次消费，跨browser/email/key/replay/并发/重启拒绝与上限；不得用jsdom代替真实浏览器或声称视觉挑战完整无障碍。适用T10以真实Outbox晚注册/重启canonical/重复事件验证，不提前声称SMTP attempt已验。

作者完成有意义普通/race/真实PG与浏览器检查、check-go，冻结精确source/dependency manifests，原失败完整保留、资源清理且所有写入命令停止后交独立V。最终兼容沿已明确包级串行编排、每包原6m及内部并发不改；新增测试若导致累计超时，先报分组证据再决定，禁止静默放宽预算或重复跑到绿。B02完成前不推进B03，D07仍唯一活动模块。现有GitHub认证阻塞未解除，后续提交继续本地记录。


### B02真实浏览器有限替代

固定Playwright1.56.1已lock/ci；其配套Chromium141.0.7390.37/build1194官方安装在单次命令内返回403 Domain forbidden，原日志`/tmp/agenteam-d07-b02-wadjih16/evidence/browser-install.log`保留，不继续下载重试或修改访问限制。root批准使用已安装系统Chromium151.0.7922.173做真实harness，并在设计修订4的§1补精确工程例外：记录实际路径/版本/SHA/启动与交互结果，兼容失败则报告，不冒称发行配套验证，不降为jsdom。固定npm/Go依赖不变，产品规则与验收链路不变；这是实际浏览器组合替代，不代表已经通过。backend继续独占实现与测试。
