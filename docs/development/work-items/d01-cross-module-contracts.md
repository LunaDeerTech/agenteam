# D01 跨模块契约

- 修订：1；状态：已完成（静态契约验收）；基线：`19d300f`。
- 前置：D00 / AT-0004 及 AT-0005 决策归位已验收；本次核对 main 与远端 main 同提交，初始无未提交改动。
- 目标：落实开发计划 D01 的具体类型、端口、依赖、状态、事务、删除及验收场景；本项不创建生产实现或空包。
- 来源：[开发计划](../development-plan.md#d01-固定跨模块契约)、[基础契约](../../architecture/platform-infrastructure/foundation-contracts.md)、[架构索引](../../architecture/README.md)、[审计处置映射](d00-baseline-audit-report.md#7-at-0005-决策处置映射)。已确认决定不重新打开。

## 交付与所有权

| 卡 | 依赖 | 执行者 | 写入范围 | 状态 |
| --- | --- | --- | --- | --- |
| C01 契约设计 | 前置验收 | architecture_worker | 新增 `d01-contracts/` 下 Markdown；不修改本规格或其他文件 | 已验收 |
| G01 用户最终验收要求归位 | 用户明确目标；与 C01 文件独立 | documentation_worker | 开发计划、新增 `platform-game-acceptance.md` | 已验收 |
| V01a 基础/生命周期独立走查 | C01 明确冻结前三份 | verification_worker | 只读 `d01-contracts/README.md`、`foundation.md`、`domain-lifecycle.md` 及既有架构来源 | 已修复，经 V01c 复核 |
| V01b 全量独立接口走查 | C01 全目录停止写入 | 同一 verification_worker | 只读全目录，复用未变 V01a 证据 | 已修复，经 V01c 复核 |
| C02 必要架构归位 | 已采纳工程决定；与 C01 文件独立 | documentation_worker | architecture 基础契约、Project README、agent-skills 三份 | 已验收 |
| I01 整合、台账和提交 | V01 通过 | 主线程 | 本规格、台账、开发计划及必要入口链接 | 已完成，随本项提交 |

无共享服务、端口或测试库资源；不提前实现 D02。C01 必读 AGENTS、团队流程、agenteam-design 技能及相关架构；V01 必读 agenteam-verification 技能。不得再委派，不进行 Git 写操作。

## C01 验收要求

按稳定职责拆成少量文档，通过索引组织；明确提供方、消费方及 D02–D28 绑定责任。覆盖 D01 全部条目，包括具体字段/签名、错误、正常/冲突/拒绝/等待/unknown outcome、幂等及事务锁顺序。运行时调用依赖与代码依赖分开，说明通过契约与组合根消除循环。用接口走查场景验证恢复事实源、权限、生命周期、竞争和迟到结果。所有未实现端口显式标记，不能声明产品验收。

Model、Tool、Executor、Trigger、等待、Scheduler 只读 Launch 查询、Skills 下一轮固定版本、Meeting 身份及摘要输入、Runtime 快照水位/订阅竞态均须有可实现接口。D01 不要求提前写所有表或 UI API；领域内部细节关联责任模块。

作者自查 Markdown 链接、来源一致性、状态/端口覆盖及 whitespace；独立验证针对冻结文件，记录稳定输入指纹和实际命令。主线程确认后小块提交并按用户授权推送 main，不覆盖远端改动。

## 后续总体验收

用户要求完成全部既定产品模块后，通过本平台真实组织并执行游戏复刻：Minecraft 或 Terraria 至少 50% 内容。验收前固定游戏/参考版本、完整清单、权重与可复现覆盖率算法；保留任务、Agent 协作、执行、审核、产物及实际试玩/测试证据。不得将外部直接生成的游戏冒充平台执行，或将主观估计视为覆盖率。该端到端任务依赖 D28；平台发现的问题回归对应模块修复。

## V01a 局部审查记录

- 独立 verification_worker / gpt-6-astra / max 只读前三份冻结文档，初末 SHA-256 不变；其余五份未读，无产品测试或写入。
- 输入：README `0b1d1ef1b23efad103281fd4e7e06127d0646dcdc4f5f934b733c8f01367342f`；foundation `d9355737006aa1a35d6c846c290557225300433533c20f7ddd2617cc15970979`；domain-lifecycle `ea545cd233235af71b7d3d7b34d1dcfd8bb799860c07b77ab639b0ed40bcd1c1`。
- 发现：Project 最终删除响应丢失后缺完成查询凭据；Contribution 缺 pending/waiting_for_agent→cancelled；通用 Tx unknown 强制命令身份不适合后台事务；Cursor 提密钥轮换却无 kid 编码。主线程采纳返修，并补 Human 幂等主体使用 user_id 排除 session_id、MoveTask 目标不得 completed。
- 工程决定：删除保留最小安全 completion receipt，只含操作/旧 Project/原 Owner 身份、请求摘要、终态及完成时间，原 Owner 当前有效 Session 可查；不保名称/正文/项目 Audit，不占名称，不引入软删除流程。
- 验证：bash/Python 3，main@b369ad1；`sha256sum`、限定 `sed/rg` 场景核对、22 份既有来源与 19d300f 的 `git diff --exit-code`；18 个稳定来源本地链接/fragment、UTF-8/LF/末尾换行/尾空格、11 张表与代码围栏通过；11 个未冻结目标引用延后；新增文件 `git diff --no-index --check /dev/null <file>` 无 whitespace 输出（差异 exit 1）。
- 局部结论：待返修，未判 D01 通过。只读已停止，主线程恢复作者写入权；全量 V01b 重审修改部分并复用未变来源证据。

## V01b 全量审查记录

- 独立验证读取冻结的 8 份契约，确认 V01a 修复；全部文件初末指纹一致。作者 manifest 为 `66bf50638a8e352f1a2a8b193dd3545f86f6d35bd88ba5021b7bd8d5386ec697`，编码为按路径排序的 `仓库相对路径<TAB>SHA256<LF>`。
- 待修：Skill 域校验不得依赖尚未生成的 Execution binding；同 Skill 重分配须在下轮形成唯一有效绑定；删除命令返回类型包含完成 receipt；preparing 捕获所需 Model/Tool/MCP/credential 元数据须有显式 InTx 端口；归档只禁新业务活动、不阻断限定内部投递/投影收敛。另补同 aggregate 事件顺序文字。
- 主线程工程决定：执行域验证 pending/既有捕获引用，下一轮按最高有效 assignment_sequence 建立唯一 Skill 映射，旧轮历史不变；预收集资源后按全局锁序捕获，变化则回滚重采集，外部 I/O 在提交后。Model lease 固定 credential_ref 而不钉死 Secret value，轮换后后续 turn 取新值；MCP 沿自身租约规则。
- 验证：main@b369ad1，独立 Python 检查 90 个链接、10 个 fragment、38 个代码块、18 张表、W01–W43 连续唯一编号及字节格式通过；`git diff --no-index --check` 与限定目录 `git diff --check` 无 whitespace 错误。来源对照 19d300f 未变，复用 V01a 来源证据。
- 未运行产品/数据库/Provider/Runner/MCP/浏览器检查，端口仍未实现。全量审查停止，作者恢复必要文件写入，完成后只重审受影响差异及整体链接/依赖，不机械重跑未变来源。

## V01c 与最终交付

- V01c 通过，D01 静态契约门槛满足，无剩余阻塞；V01a/V01b 缺口全部闭合。独立验证者和两个作者均已停止写入、无后台命令；主线程完整读取契约并核对重点来源与实际差异。
- C01 最终 8 文件/1334 行、93 本地链接/11 fragment、39 代码块、18 张表、W01–W43 连续唯一；manifest `93b2ce8732e965aefa7b8f87e60a22e0477d537be88b53c3a11bb2ef07ea584b`（按仓库相对路径排序，`path<TAB>sha256<LF>`）。初末指纹一致，全文与范围检查通过。
- C02 三份架构 34 链接/6 fragment 及格式通过；独立验证实际 diff，只有已采纳的 receipt、归档内部收敛、技能版本与重分配边界，其他来源复用未变证据。主线程另执行 `/tmp/agenteam-check-markdown.py` 覆盖 11 份交付/架构文档 127 链接及编码格式，全部通过。
- 实查环境 main@b369ad1、bash/Python 3；执行只读 `git diff/status/rev-parse`、限定 `sed/rg`、Python 文档检查、逐新增文件 `git diff --no-index --check` 及 `git diff --check`。最终仅同步完成状态/入口与交接后再检查链接格式，无产品代码变化。
- 限制：全部端口未实现，数据库/Provider/Runner/MCP/浏览器/恢复行为未执行；所有绑定与 W 场景在契约索引归责 D02–D28，不能将静态走查解释为产品通过。
- 交付通过本规格/台账 Git 历史定位；按用户授权推送 main，不覆盖远端改动。下一模块 D02：Go 工程、配置、HTTP/基础类型、程序生命周期与真实构建/测试。E01 仍待平台模块全部完成后执行。
