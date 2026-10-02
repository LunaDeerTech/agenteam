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

## 串行子 agent 开发团队

主线程负责用户讨论、规划、任务下发、复杂诊断、审查、必要集成和汇报；业务执行交给子 agent。固定使用 `backend_worker`（后端）、`frontend_worker`（前端）和 `verification_worker`（测试与文档）三个角色，全部使用 `gpt-6.1-sol`，推理级别为 `low`，权限与 sandbox 继承主线程，不使用 `luna_worker`。

在 `main` 上每次只推进一个工作项，最多一个活动子 agent；子 agent 不得创建下级 agent。每个工作项可以拆成多张小任务卡，按开发 → 验证 → 返修 → 主线程审查 → 本地提交顺序推进，完成后再开始下一工作项。

主线程维护完整任务卡，明确目标、授权文件范围、既定接口、执行步骤、必用 skill、验收命令及预期结果、停止条件和交付格式。子 agent 只执行完整任务卡，先检查 `main` 基线及用户改动，读取角色项目 skill，只修改授权文件；缺少决策、接口未定、需要未授权的新依赖或需要扩大范围时，停止受影响执行并返回主线程。

子 agent 不得执行 `git add`、`git commit`、`git push`、`git reset`、`git clean`，不得切换分支或创建、切换 worktree，不得覆盖或回退他人改动。主线程验收后仅暂存当前工作项文件，自动创建本地 Conventional Commit；推送需要用户明确指令。

用户调整需求时，暂停受影响执行并保留已有改动；主线程确认调整后更新任务卡。通过提问工具询问用户选择时等待答案，不设置自动超时。长时间工作中，主线程定期汇报进展、验证结果和阻塞。

项目角色 skill 分别为 `.agents/skills/agenteam-go-development/SKILL.md`、`.agents/skills/agenteam-vue-development/SKILL.md` 和 `.agents/skills/agenteam-verification/SKILL.md`。Vue 测试任务还必须读取 `vue-testing-best-practices` 技能；浏览器任务使用可用的 `playwright` 技能。详细流程见 [串行子 agent 开发团队](docs/development/agent-team/README.md)。
