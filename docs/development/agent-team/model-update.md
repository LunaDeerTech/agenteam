# AT-0006：子 Agent 模型调整

修订：2。状态：已完成。

## 目标与授权

用户明确要求全部子 agent 使用 GPT-6 astra、思考强度 Max。统一正式配置为 `gpt-6-astra` / `max`，覆盖默认配置和 backend_worker、frontend_worker、verification_worker 三个角色。主线程配置与串行/权限边界继续按原规则执行。

开始基线为 `main / 71657b9`；工作区已有 AT-0005 讨论记录，不覆盖、不回退、不混入本工作项提交。旧任务记录是历史证据，不改写旧模型运行事实。

## 执行者与必读技能

角色 verification_worker，新任务实例显式使用 gpt-6-astra / max，fork_turns=none。用户新决定优先于尚未同步的旧模型规范。

必读 [AGENTS.md](../../../AGENTS.md)、[团队流程](README.md)、[验证技能](../../../.agents/skills/agenteam-verification/SKILL.md)。不得创建下级 agent 或执行 Git 写操作。

## 授权写入范围

- `.codex/config.toml`
- `.codex/agents/backend-worker.toml`
- `.codex/agents/frontend-worker.toml`
- `.codex/agents/verification-worker.toml`
- `AGENTS.md`
- `docs/development/agent-team/README.md`
- `docs/development/agent-team/task-template.md`
- `docs/development/agent-team/verification.md`（仅补充旧验证为历史及当前配置入口）
- `docs/development/development-plan.md`（仅当前团队模型策略）

本任务卡、tasks.md 和 d00 讨论文档由主线程维护，执行者只读。

## 执行步骤与接口

1. 检查 main/HEAD/工作区，保留三个讨论文件；读取规范与技能。长文档通过 rg 定位后按每次 80–100 行读取，各次输出独立返回，避免聚合导致输出截断；无需为模型字段变更重复读取无关业务章节。
2. 更新默认 `default_subagent_model`/`default_subagent_reasoning_effort`、每个角色的 `model`/`model_reasoning_effort`，以及角色指令中的模型文字。
3. 同步当前 AGENTS、团队 README（含普通 worker 回退说明）、任务模板和开发计划；三个项目技能若未硬编码模型则无需修改。
4. 初始化验证记录保留旧运行证据，在开头注明当前策略已调整并链接本任务卡；不要全局替换历史模型名称。
5. 枚举角色文件按 name 字段验证，检查有效规范无旧模型/强度；主线程独立审查后提交。

## 验收

- Python `tomllib` 解析项目与全部角色 TOML：默认及三个角色均精确为 gpt-6-astra/max，角色集合不变，并发上限仍为 1。
- rg 检查授权的当前配置/规范：不残留 gpt-6.1-sol 或旧 low 策略；历史 tasks/verification/D00 的旧运行事实允许保留。
- 检查变更 Markdown 的相对链接目标、UTF-8/LF、trailing whitespace 和 `git diff --check`。
- 当前实例以 gpt-6-astra/max 成功启动作为实际调用证据，不伪称三个角色都已各自重新运行。
- 若已安装 Codex 支持旧记录中的 strict config/app-server 配置读取接口，可只读验证项目配置加载，仅输出相关 agents 字段。不得输出全量用户配置或认证信息；命令不支持时记录限制，不改动全局配置。
- 仅 docs/config 修改，不运行前端、Go、浏览器、数据库或外部服务产品测试。

## 停止条件与交付

基线异常、需要扩大文件范围、模型不可用或同一步骤连续两次失败时停止受影响部分并返回主线程。不自动降级模型，不修改权限、并发或业务架构。

交付实际变更文件、执行命令/结果、模型运行证据与未验证边界。不得 git add/commit/push/reset/clean、切换分支/worktree。主线程更新本任务卡和台账，独立暂存当前项并本地提交；不推送。

## 验证结果

执行过程：读取开发计划时两次聚合输出截断，执行者按停止条件返回；修订 2 改为定位相关章节并小段读取后恢复。

- 九个授权配置/规范文件均已同步；三个角色及默认字段均为 `gpt-6-astra / max`。三个角色的指令文字、普通 worker 显式回退参数与当前模型规范一致。初始化验证页只增加历史说明，旧运行事实保留。
- 主线程独立审查九文件 `git diff`；Python `tomllib` 解析并枚举角色 name，默认与三个角色参数正确，enabled=true、并发上限仍为 1。现行策略不残留旧模型/low，三个项目技能没有需修改的模型字段。
- 主线程 Python 检查共 11 文件 UTF-8/LF、106 个 Markdown 相对链接目标，通过；`git diff --check` 通过。纯配置/文档变更，不运行 Go、前端、数据库、浏览器或外部服务产品测试。
- 当前工具调用证据：主线程以 `model=gpt-6-astra`、`reasoning_effort=max`、`fork_turns=none` 成功创建 `/root/verification_model_update`，作为 verification_worker 执行本卡。未声称三个自定义角色都已在本地 CLI 各自重新运行。
- CLI 为 `0.159.0-alpha.3`。执行者以 `codex --strict-config app-server` 初始化并调用 `config/read`；项目 agents 层准确包含 enabled=true、并发 1、gpt-6-astra/max，但该层因 trust 状态被禁用，effective agents 对应字段为空。**仅证明项目层可解析，不能声称本地 CLI 已自动采用项目配置。** 未修改全局配置或仓库信任状态；当前会话继续在每次新建子任务时显式指定模型与强度。
- 配置读取首次过滤脚本未兼容 agents=null 而报 AttributeError，修正后取得上述结果；两次命令均在 1 秒内退出，所启动 app-server 均已停止。输出仅限 agents 相关字段与加载状态，未输出认证信息或全量配置。
- Git 交付仅包含本项配置、规范、任务卡和台账中的 AT-0006；AT-0005 讨论改动留在工作区。提交通过本文件历史定位，不推送。

执行者已完成交付并停止写入；主线程审查通过，随本工作项创建本地提交。没有待完成的配置文件修改；CLI 自动加载限制按上文保留。
