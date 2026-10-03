# Repository Guidelines

## Project Structure & Module Organization

agenteam is an AI Agent collaboration platform, currently comprising design documents, a backend directory skeleton, and a runnable Vue frontend with shared components and a development-only Debug page. The planned stack is Go, Vue 3, PostgreSQL/pgvector, and MinIO.

- `cmd/agenteam/` and `cmd/agenteam-runner/`: Central and Runner entry points.
- `internal/central/` and `internal/runner/`: respective implementations; `internal/runnerprotocol/`: shared communication contracts. Runner must not import Central business packages.
- `web/src/`: Vue application and shared component library; `db/migrations/`: one global migration sequence.
- `tests/`: integration and end-to-end tests; `deploy/` and `scripts/`: deployment and development tooling.
- `docs/architecture/`, `docs/frontend-design/`, and `docs/development/`: architecture, UI specifications, and repository organization. Shared controls live in `web/src/components/ui/`; Debug fixtures and presentation live in `web/src/views/debug/` and must not be imported by shared components.

Keep `.gitkeep` files until their directories contain real files.

## Build, Test, and Development Commands

The frontend is runnable independently; no Go module or backend build pipeline exists yet.

- `npm ci --prefix web`: install locked frontend dependencies.
- `npm run dev --prefix web`: serve development-only Debug at localhost:5173/debug.
- `npm run check --prefix web`: formatting check, unit tests, type check, and production build.
- `npm run preview --prefix web`: inspect production build at localhost:4173; Debug is excluded.
- `git diff --check`: check tracked changes for whitespace errors.

See `docs/development/frontend/README.md` for individual commands and component boundaries.

## Coding Style & Naming Conventions

Preserve surrounding HTML/CSS/JavaScript formatting; the frontend uses Prettier via its package scripts. Use UTF-8, LF endings, descriptive names, and kebab-case documentation filenames. Format future Go code with `gofmt` (tabs). For new Vue code, use two-space indentation and PascalCase component filenames.

Follow confirmed frontend style tokens and layout specifications. Keep business lifecycle rules in architecture documents, and update related links when moving documentation.

## Testing Guidelines

Frontend interaction tests use Vitest and Vue Test Utils; no coverage threshold is configured. Place future Go unit tests beside source as `*_test.go`, with `TestXxx` functions; put cross-module tests in `tests/`.

For showcase changes, verify light/dark themes, desktop/narrow widths, keyboard navigation, focus restoration, reduced motion, and overflow in a browser. Check Markdown links for documentation changes. Report actual validation and limitations.

## Commit & Pull Request Guidelines

History uses Conventional Commit prefixes such as `docs:` and `chore:`. Write concise, imperative subjects, for example `docs: clarify runner protocol boundaries`.

PRs should explain the problem, changed behavior, affected documents/modules, and validation performed. Link relevant issues; include screenshots for visual changes.

## Security & Agent Instructions

Keep credentials in ignored `.env*` files or root `secrets/`; commit sanitized example configurations only. When asking the user to choose through a question tool, wait for their answer without an automatic timeout.

## 基础先行的模块开发模式

正式开发遵循[开发计划](docs/development/development-plan.md)，按依赖顺序推进，每次只有一个活动模块。先确定整体模块边界与跨模块契约；模块开工前明确数据结构、接口、状态规则、错误、事务、幂等和验收场景，再实现、调试及验收。不以提前出现界面或演示效果作为早期交付目标。

一个模块可以拆成连续的小任务卡，但单张卡完成不代表模块完成。当前范围内的核心实现、异常处理及适用的并发和恢复验证全部通过后，才推进下一模块。未来模块的依赖使用已确定的正式端口，记录真实绑定与集成验收的责任工作项；不得以生产 stub、静默成功、演示数据或跨模块直接改表替代正式能力。

架构专题保存业务规则，开发计划保存模块顺序与门槛，工作项规格保存具体接口与实现决策。设计调整先评估影响并同步相关文档和任务卡，再修改实现，避免在下游复制补丁。

## 跨对话交接与恢复

交接以仓库文档、实际代码和 Git 证据为依据，不依赖聊天 Memory、临时 ToDo 或上一会话的完成声明。新会话开工前必须读取本文件、[开发计划](docs/development/development-plan.md)、[任务台账](docs/development/agent-team/tasks.md)和当前工作项规格/任务卡，核对分支、提交、未提交改动与前置依赖验收证据，从首个未完成项恢复。

模块完成或中途切换会话均须更新台账：工作项与计划编号、当前阶段、规格/任务卡路径及修订号、已完成与未完成内容、实际检查命令/结果及证据、未验证范围、阻塞与待定决策、未绑定端口、相关提交定位方式、未提交文件与明确的下一步。中途交接先确认子 agent 和命令已停止写入，保留已有改动；只有通过模块完成门槛才标记“已完成”，部分完成或未验证不得冒充已验收。

## 串行子 agent 开发团队

主线程负责用户讨论、规划、任务下发、复杂诊断、审查、必要集成和汇报；业务执行交给子 agent。固定使用 `backend_worker`（后端）、`frontend_worker`（前端）和 `verification_worker`（测试与文档）三个角色，全部使用 `gpt-6-astra`，推理级别为 `max`，权限与 sandbox 继承主线程，不使用 `luna_worker`。

在 `main` 上每次只推进一个工作项，最多一个活动子 agent；子 agent 不得创建下级 agent。每个工作项可以拆成多张小任务卡，按开发 → 验证 → 返修 → 主线程审查 → 本地提交顺序推进，完成后再开始下一工作项。

主线程维护完整任务卡，明确目标、授权文件范围、既定接口、执行步骤、必用 skill、验收命令及预期结果、停止条件和交付格式。子 agent 只执行完整任务卡，先检查 `main` 基线及用户改动，读取角色项目 skill，只修改授权文件；缺少决策、接口未定、需要未授权的新依赖或需要扩大范围时，停止受影响执行并返回主线程。

子 agent 不得执行 `git add`、`git commit`、`git push`、`git reset`、`git clean`，不得切换分支或创建、切换 worktree，不得覆盖或回退他人改动。主线程验收后仅暂存当前工作项文件，自动创建本地 Conventional Commit；推送需要用户明确指令。

用户调整需求时，暂停受影响执行并保留已有改动；主线程确认调整后更新任务卡。通过提问工具询问用户选择时等待答案，不设置自动超时。长时间工作中，主线程定期汇报进展、验证结果和阻塞。

项目角色 skill 分别为 `.agents/skills/agenteam-go-development/SKILL.md`、`.agents/skills/agenteam-vue-development/SKILL.md` 和 `.agents/skills/agenteam-verification/SKILL.md`。Vue 测试任务还必须读取 `vue-testing-best-practices` 技能；浏览器任务使用可用的 `playwright` 技能。详细流程见 [串行子 agent 开发团队](docs/development/agent-team/README.md)。
