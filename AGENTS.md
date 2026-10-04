# Repository Guidelines

## Project Structure & Module Organization

agenteam is an AI Agent collaboration platform with design documents, a Go foundation, PostgreSQL/pgvector migrations and transactions, diagnostic Central/unconnected Runner processes, and a runnable Vue frontend with shared components and a development-only Debug page. Central requires its database, independent cursor/Secret/download keyrings, security initialization, DB outbound policy, verified MinIO runtime and the transactional Outbox before serving diagnostics. Typed Audit, signed cursors, encrypted Secret storage/leases, recoverable maintenance, controlled HTTP/SMTP dial ports, object/Artifact/download libraries and typed Runner transfer storage are implemented. Outbox uses same-transaction events/markers, bounded delivery, current-authorized redrive and typed diagnostics; production domain handlers and Project/Outbox authorization remain unbound. Identity, Project, execution/binding, object/transfer authorization, SMTP protocol and Provider/MCP consumers remain unbound; these processes do not indicate product readiness.

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
- `./bin/agenteam --check-config`: validate current D05 Central settings, including required database URL, independent cursor/Secret/download keyrings, object endpoint/bucket/explicit credentials, spool path and any explicit CA, without connecting or creating spool directories. Optional outbound CA appends deployment roots. `./bin/agenteam-runner --check-config` validates D02 Runner settings. Both report `ready=false`; help/version need no configuration.
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

正式开发遵循[开发计划](docs/development/development-plan.md)，按依赖顺序推进，每次只有一个活动模块。先确定整体模块边界与跨模块契约；模块开工前明确数据结构、接口、状态规则、错误、事务、幂等和验收场景，再实现、调试及验收。不以提前出现界面或演示效果作为早期交付目标。

一个模块可以拆成按依赖推进的完整结果任务，已定契约下的独立任务允许并行；单张卡完成不代表模块完成。当前范围内的核心实现、异常处理及适用的并发和恢复验证全部通过后，才推进下一模块。未来模块的依赖使用已确定的正式端口，记录真实绑定与集成验收的责任工作项；不得以生产 stub、静默成功、演示数据或跨模块直接改表替代正式能力。

架构专题保存业务规则，开发计划保存模块顺序与门槛，工作项规格保存具体接口与实现决策。设计调整先评估影响并同步相关文档和任务卡，再修改实现，避免在下游复制补丁。

## 跨对话交接与恢复

交接以仓库文档、实际代码和 Git 证据为依据，不依赖聊天 Memory、临时 ToDo 或上一会话的完成声明。新会话开工前必须读取本文件、[开发计划](docs/development/development-plan.md)、[任务台账](docs/development/agent-team/tasks.md)和当前工作项规格/任务卡，核对分支、提交、未提交改动与前置依赖验收证据，从首个未完成项恢复。

模块完成或中途切换会话均须更新台账：工作项与计划编号、当前阶段、规格/任务卡路径及修订号、已完成与未完成内容、实际检查命令/结果及证据、未验证范围、阻塞与待定决策、未绑定端口、相关提交定位方式、未提交文件与明确的下一步。中途交接先确认子 agent 和命令已停止写入，保留已有改动；只有通过模块完成门槛才标记“已完成”，部分完成或未验证不得冒充已验收。

## 主线程统筹的开发团队

用户只与主线程沟通产品目标、可见行为和取舍。主线程负责把意图整理为确认的产品规则、工程规格、任务和验收条件，并对技术选择、协调、返修、整合与交付负责；仅将未决产品含义、实质性范围变更或需用户授权的行动交回用户，不重复询问已确认决定。业务实现和成组文档修改委派给子 agent，主线程可做必要的小范围集成与修正。

按需使用六个角色：`architecture_worker`（影响分析与工程规格）、`research_worker`（有界技术调研）、`backend_worker`（后端实现与自测）、`frontend_worker`（前端实现与自测）、`documentation_worker`（已确认决定的文档归位）、`verification_worker`（独立审查与验证）。角色是可选能力，不是必须逐个经过的阶段；全部使用 `gpt-6-astra / max`，权限与 sandbox 继承主线程，子 agent 不得再委派。

在 `main` 上每次只有一个活动工作项。常态最多两个活动子任务，依赖、文件及共享运行资源可隔离时主线程可增加到三个；简单任务单人执行，不为用满名额拆卡。主线程在当前规格中维护任务依赖和所有权；同一文件同一时间只有一个写入者，共享契约、锁文件、迁移编号、生成产物、端口与测试库均须明确负责人或顺序。只读审查也须针对停止写入的范围；模块最终验收与提交前停止相关写入，不提前实现后续模块。

任务卡按完整结果拆分，引用共享规格、既定接口和验收场景，不复制全套架构或按单个函数机械拆分。卡片明确修订、基线、目标、授权文件/资源、依赖、必读技能、验收与升级条件即可，具体步骤仅在必要时约束。执行者核对现状并保留他人改动，可在授权范围内自行选择实现细节、修正工具使用和排查问题；缺少产品决定、公共接口未定、需要未授权依赖/扩大范围、发生所有权冲突或排查无新证据时，暂停受影响部分并报告主线程。

实现者负责有意义的自测及局部文档同步；主线程按风险安排独立验证。低影响文档/局部调整可由执行者自查加主线程审查；业务变更由独立实例审查，高风险权限、数据、事务、协议与恢复改动必须独立验证相关场景。证据记录实际命令、环境、输入版本或文件指纹、结果和限制；输入及依赖未变可复用，相关修改后重跑受影响检查。主线程核对实际差异、重点边界与证据，避免机械重复全部检查，仍对完整模块验收负责。

子 agent 不得执行 `git add`、`git commit`、`git push`、`git reset`、`git clean`，不得切换分支或创建、切换 worktree，不得覆盖或回退他人改动。主线程验收后仅暂存当前工作项文件，自动创建本地 Conventional Commit；推送需要用户明确指令。

用户调整需求时，主线程暂停受影响执行并保留已有改动，更新规格和任务修订后重新下发；无依赖的任务可继续。必须由用户确定的产品问题保持待定，不将沉默视为选择。子 agent 只向主线程报告，不直接向用户提问。主线程定期汇报结果、验证和阻塞，不把内部调度细节变成用户的管理负担。

角色与技能映射、调度、验收及运行时配置规则见[开发团队流程](docs/development/agent-team/README.md)，卡片使用[任务模板](docs/development/agent-team/task-template.md)。首次接任务读取仓库规则、相关技能和定位后的规格；同一任务后续只补读变化，避免全量继承聊天或反复遍历全部产品文档。Vue 测试任务还必须读取 `vue-testing-best-practices`；浏览器任务使用可用的 `playwright` 技能。
