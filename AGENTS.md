# Repository Guidelines

本文件只保存长期有效的仓库规则，不追加逐轮进度、哈希、日志或验收流水。AI 工作流以[开发团队流程](docs/development/agent-team/README.md)为统一来源；旧任务卡和历史报告中的重复哈希、永久归档、逐层回主线程审批及禁止再委派要求不再作为日常流程。产品契约、真实验收门槛与未完成事实仍以对应工作项为准。

## 当前工作与按需读取

- 当前工作、阻塞和下一步见[任务台账](docs/development/agent-team/tasks.md)，模块依赖与完成门槛见[开发计划](docs/development/development-plan.md)。先读取当前摘要和目标工作项；使用标题或路径定位相关段落，不默认加载全部历史。
- 各工作项的阶段成果不等于整卡或生产接入完成；恢复时核对实际代码、Git 差异及相应任务记录，不按旧聊天或旧进度段落执行。
- Object runtime join、OpenAI tools 独立验收、Central SPA concurrent-publication 及 Jina/Image 来源获取的既有阻塞在任务记录中保留；工作流调整不自动恢复这些任务，也不改变生产未绑定和未验证范围。
- 原入口中的历史推进记录可通过 Git 文件历史和任务台账查询，不另复制成归档文件。只更新发生变化的正式来源，不在 AGENTS、计划和台账重复抄写同一进度。

## Project Structure & Module Organization

agenteam is an AI Agent collaboration platform with a Go backend, PostgreSQL/pgvector, Central and Runner processes, and a Vue frontend. See the relevant architecture and development guide for current contracts and supported behavior.

- `cmd/agenteam/` and `cmd/agenteam-runner/`: Central and Runner entry points.
- `internal/central/` and `internal/runner/`: respective implementations; `internal/runnerprotocol/`: shared communication contracts. Runner must not import Central business packages.
- `internal/platform/logging/` and `internal/platform/lifecycle/`: neutral process utilities; must not import Central or Runner business types. `api/openapi/common.json`: implemented shared HTTP schemas.
- `web/src/`: Vue application and shared component library; `db/migrations/`: one global migration sequence.
- `tests/`: integration and end-to-end tests; `deploy/` and `scripts/`: deployment and development tooling.
- `docs/architecture/`, `docs/frontend-design/`, and `docs/development/`: architecture, UI specifications, and repository organization. Shared controls live in `web/src/components/ui/`; Debug fixtures and presentation live in `web/src/views/debug/` and must not be imported by shared components.

Keep `.gitkeep` files until their directories contain real files.

## Build, Test, and Development Commands

The frontend remains independently runnable. The root Go module requires exactly Go 1.27.1; backend scripts set `GOTOOLCHAIN=local` and reject a different toolchain. Set `AGENTEAM_GO` to the binary path if PATH does not select it.

- `AGENTEAM_GO=/path/to/go1.27.1/bin/go sh scripts/check-go.sh`: Go tests, vet, race checks and both binary builds.
- `AGENTEAM_GO=/path/to/go1.27.1/bin/go sh scripts/test-postgres.sh`: real PostgreSQL/pgvector, MinIO and Central process integration tests using task-owned Docker fixtures; ordinary Go tests do not require Docker.
- `AGENTEAM_GO=/path/to/go1.27.1/bin/go sh scripts/test-security.sh`: database/process suite plus real controlled outbound private sockets, generated CA and nonce-owned internal Docker network fixtures.
- `AGENTEAM_GO=/path/to/go1.27.1/bin/go sh scripts/build-go.sh`: build `bin/agenteam` and `bin/agenteam-runner`.
- `AGENTEAM_GO=/path/to/go1.27.1/bin/go AGENTEAM_MINIO_BINARY=/task-owned/cache/minio sh scripts/test-objects.sh`: full database, security, object, Artifact, download, transfer, Outbox and real process suite. The other integration scripts also acquire mandatory MinIO; the exact source-built binary and SHA are documented in the backend guide.
- `./bin/agenteam --check-config`: validate current Central settings, including required database URL, independent cursor/Secret/download/account keyrings, `AGENTEAM_CENTRAL_ACCOUNT_RECOVERY_LOG`, safe `PUBLIC_ORIGIN`, object endpoint/bucket/explicit credentials, spool path and any explicit CA, without connecting, creating directories or opening the recovery log. Optional outbound CA appends deployment roots. Central still emits `scope=d05`; its help/version retain the D05 label. `./bin/agenteam-runner --check-config` validates D02 Runner settings. Both report `ready=false`; help/version need no configuration.
- `npm ci --prefix web`: install locked frontend dependencies.
- `npm run dev --prefix web`: serve development-only Debug at localhost:5173/debug.
- `npm run check --prefix web`: formatting check, unit tests, type check, and production build.
- `npm run preview --prefix web`: inspect production build at localhost:4173; Debug is excluded.
- `git diff --check`: check tracked changes for whitespace errors.

See `docs/development/backend/README.md` and `docs/development/frontend/README.md` for commands, configuration and boundaries. Backend process tests use temporary directories and loopback ephemeral ports; database and outbound fixtures verify nonce, labels and exact resource IDs before connecting. Outbox process tests share the real object ProcessGuard; it must remain held until callbacks and database cleanup actually join, or the owning process exits. Outbound success tests use owned private container sockets with exact policy rules; never replace the production loopback classifier with allow. Tests must not connect to existing infrastructure or read external credentials.

## Coding Style & Naming Conventions

Preserve surrounding HTML/CSS/JavaScript formatting; the frontend uses Prettier via its package scripts. Use UTF-8, LF endings, descriptive names, and kebab-case documentation filenames. Format Go code with `gofmt` (tabs). For new Vue code, use two-space indentation and PascalCase component filenames.

Follow confirmed frontend style tokens and layout specifications. Keep business lifecycle rules in architecture documents, and update related links when moving documentation.

## Testing Guidelines

Frontend interaction tests use Vitest and Vue Test Utils; no coverage threshold is configured. Go unit tests live beside source as `*_test.go`, with `TestXxx` functions; `tests/process/` builds and tests the real command binaries. Cross-module tests belong in `tests/`.

For showcase changes, verify light/dark themes, desktop/narrow widths, keyboard navigation, focus restoration, reduced motion, and overflow in a browser. Check Markdown links for documentation changes. Report actual validation and limitations.

## Commit & Pull Request Guidelines

History uses Conventional Commit prefixes such as `docs:` and `chore:`. Write concise, imperative subjects, for example `docs: clarify runner protocol boundaries`.

PRs should explain the problem, changed behavior, affected documents/modules, and validation performed. Link relevant issues; include screenshots for visual changes.

## Security & Agent Instructions

Keep credentials in ignored `.env*` files or root `secrets/`; commit sanitized example configurations only. Required product decisions stay pending until the user answers; silence is not approval.

## 基础先行的模块开发模式

正式开发遵循[开发计划](docs/development/development-plan.md)，按真实依赖推进；没有未满足依赖的模块和完整结果任务可以并行，不以模块编号或单一活动模块限制调度。先确定整体模块边界与跨模块契约；模块开工前明确数据结构、接口、状态规则、错误、事务、幂等和验收场景，再实现、调试及验收。不以提前出现界面或演示效果作为早期交付目标。

一个模块可以拆成按依赖推进的完整结果任务；跨模块并行同样要求正式契约稳定、所需前置能力已有验收证据、文件与迁移及共享资源所有权明确。只消费上游已验收子能力的任务可独立开工，必须逐项记录真实依赖与尚待集成的边界，不把整个上游模块视为已完成。单张卡完成不代表模块完成；模块当前范围内的核心实现、异常处理及适用的并发和恢复验证全部通过后，才能标记模块完成。未来模块的依赖使用已确定的正式端口，记录真实绑定与集成验收的责任工作项；不得以生产 stub、静默成功、演示数据或跨模块直接改表替代正式能力。

架构专题保存业务规则，开发计划保存模块顺序与门槛，工作项规格保存具体接口与实现决策。设计调整先评估影响并同步相关文档和任务卡，再修改实现，避免在下游复制补丁。

## 跨对话交接与恢复

交接以实际代码、Git 与当前任务记录为依据。新会话核对分支、基线和未提交改动，按需读取开发计划、台账当前摘要及目标规格；不反复遍历历史报告或全部产品文档。保留用户和其他任务的改动。

仅在完成一个有意义的结果或确需中途交接时，更新台账中该任务的简短状态：目标与规格、完成/未完成内容、实际检查及结论、阻塞、未提交文件和下一步。原始日志、命令输出、临时计划与证据放在忽略的 `output/ai/<task>/` 或外部临时目录；跨环境恢复依赖的必要结论须写入任务记录，不能只留临时路径。历史失败与未验证范围如实保留，不要求复制每轮原始文件入库。

## 主线程统筹的多层开发团队

用户只与主线程沟通。主线程负责全局目标、约束、一级子目标、跨任务契约与资源预算、最终整合和 Git 交付；不逐项接管局部拆解、工具排查和重复证据核对。一级负责人接收完整子目标，负责设计细化、向二级执行者拆分、局部协调、验证与汇总；二级及更深代理可在确有收益时继续拆分。简单任务直接执行，不为凑层级或用满席位创建代理。

六个专业角色及技能映射见团队流程。所有层级统一使用 `gpt-6-astra`、`ultra` 与 Fast Mode（配置 `service_tier = "priority"`），禁止静默降级为 `max` 或其他模型。支持显式创建参数时指定模型和思考强度，使用自包含任务和 `fork_turns=none`；Fast 的实际生效受运行时支持及配置加载影响，没有可确认信息时如实说明。各层继承权限与 sandbox；不修改全局 trust。

当前协作工具全树共享 7 个总席位（含主线程），以实时可用名额为准。主线程向一级负责人分配子树预算，父代理创建下级会消耗同一预算；为实际执行者预留名额，名额不足时由现有执行者完成或顺序推进，不能形成所有父代理等待未创建子代理的阻塞。深度按任务复杂度和运行时上限选择，允许多层，不固定仅一层。

每项任务交代完整结果、必要输入、文件/资源所有权、验收和升级条件即可；一般派工可在工具消息中完成，不为每个代理另建持久任务卡。一级负责人维护子树任务与局部所有权，主线程只跟踪跨任务依赖与交付状态。同一文件同时只有一个写入者；共享契约、锁文件、迁移编号、生成产物、端口、测试库和 Docker fixture 指定唯一负责人或使用顺序。

子代理向直接父代理报告完成结果、相关文件、实际验证、限制与阻塞；父代理先解决范围内问题，再汇总交给上级。跨子树冲突、公共契约变化、未决产品含义或实质性范围扩展才升级主线程；只有需用户决定的事项才交给用户。需求变化时暂停受影响写入、保留已有改动并更新简报，无依赖的工作可继续。

低影响文档/配置由执行者自查和父级审查；业务变更由未参与实现的实例独立审查，高风险权限、数据、事务、协议与恢复改动必须独立验证相关场景。负责实现的代理及参与实现决策的上级不能把自己的审查称为独立验证，应安排未参与实现的审查者。验证负责人可按同样预算继续委派，汇总有效结论；主线程评估关键边界、异常和结论，不重新扫描每份原件。

稳定输入默认以 Git 基线、限定文件差异和审查期间停止相关写入来确定。日常任务不生成全仓库或依赖闭包哈希、重复清单、源文件镜像、manifest 或校验报告；只有发布产物完整性、外部文件身份或具体故障确需时，对必要范围计算一次并复用。产品已有完整性检查及依赖锁文件按其真实用途维护。通过的检查在相关输入和依赖未变时复用，修复后只重跑受影响检查；原始失败不改写，命令与自有资源结束状态须实际确认。

续派已完成或停止的实例使用 `followup_task`，确认接收并实际启动后再标为进行中；`send_message` 仅用于沟通。实例完成不代表席位释放，按运行时实际状态调度。

子代理不得执行 `git add`、`git commit`、`git push`、`git reset`、`git clean`，不得切换分支或创建、切换 worktree。各级负责人确认相关写入与后台命令停止后，由主线程仅暂存本次已验收结果；一个完整结果默认一次 Conventional Commit，包含实现、测试、必要文档及简短台账更新。不把文档留作额外 `docs` / `docs archive` 收尾，不为回填提交哈希、整理报告或复制日志再开提交；独立的文档任务或有独立价值的阶段结果可以单独提交。推送沿用当前任务已有明确授权，不把历史其他任务的授权扩展到新任务。

首次接任务只读本文件、[团队流程](docs/development/agent-team/README.md)和目标规格及相关技能；后续补读变化。任务模板见[精简简报](docs/development/agent-team/task-template.md)。Vue 测试读取 `vue-testing-best-practices`，浏览器任务使用可用的 `playwright` 技能。工作流变更只需相关配置解析、链接/一致性及格式检查，不运行无关产品测试，也不新增验收归档。
