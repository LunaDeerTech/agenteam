# 环境恢复与开发接续

本页保留原路径供旧链接使用，记录接续时的输入缺口。**全局产品状态在[任务台账](tasks.md)，任务分支的最小恢复状态在[分支记录](../../../.agent-state/current.md)**；长期模块门槛见[开发计划](../development-plan.md)，自动保存与新设备操作见[团队流程](README.md#自动保存与跨设备恢复)。以下 2026-10-09 环境核对基于 `e55ad7d1`，当时初始工作树干净，仅查看 Git、必要路径、配置与工具版本，没有恢复产品执行或运行测试。

旧逐轮资源授权、代理 ACK、进程 ID 和清理结论是历史事实，不证明当前实例在运行、资源可用、旧权限仍适用或机器曾重启。后续只在当前任务范围内安排完整结果，不复刻逐轮 root 批准、全树哈希或永久证据包。

## 先确定实际输入

新设备先按团队流程 fetch origin，检查 `ai/*` 与所选分支的 `.agent-state/current.md`。能定位当前任务就自动接续；只有多个候选无法判断时才合并问一次。已有 dirty 改动先保存，不直接切换或覆盖。再在仓库根核对必要输入，检查结果不另建重复报告：

```sh
git branch --show-current
git status --short
git log -5 --oneline
# 将路径限定为接手任务的实际范围。
git diff e55ad7d1 -- docs/development/work-items/d11-task-planning.md
```

读台账目标行，再读对应正式卡和必要上游接口。工作区有其他改动时保留并确认文件所有者；旧报告中的固定版本组合只在对应输入仍适用时复用，不自动声明当前 HEAD 全测。阶段规格、STATIC、编译、PLAN、受控探针与真实业务验收要分别记录。

## 本次实际观察

以下是本次环境的有限观察，不是长期固定机器配置；只检查列出的位置，没有全盘搜索或完整依赖验收。

| 对象 | 实际观察 | 接手含义 |
| --- | --- | --- |
| Git | `e55ad7d1` 包含 `a64fb5e7` Structure 与 `97439ffd` Task规格；初始干净 | 后续以实际分支/差异为准，不自动切换 `main`。 |
| Go | `/workspace/toolchains/go1.27.1/bin/go` 存在，`go version` 为 `go1.27.1 linux/amd64` | 可作为 `AGENTEAM_GO`；未编译或运行产品测试。 |
| Node/npm | PATH 中 Node `v24.19.0`、npm `11.9.0` | 工具存在不表示项目依赖完整。 |
| web依赖 | `web/node_modules` 存在；Vue `3.5.43`、Vitest `4.1.11` 的包文件可读，但 `web/node_modules/go-captcha-vue` 缺失 | 不沿旧恢复报告称当前安装完整；按目标检查锁定依赖，只补实际缺项。 |
| 浏览器harness依赖 | `tests/account-captcha-web/package.json`/锁存在，锁定 `@playwright/test=1.56.1`；该目录 `node_modules` 不存在 | 需恢复锁定依赖并核浏览器可用性，不能以全局 Playwright 替代；本次未启动浏览器。 |
| MinIO | 旧测试注入路径 `/workspace/scratch/fixture-recovery-new/bin/minio` 缺失；onboarding 的 `/workspace/agenteam-onboarding/infra/minio --version` 为 `RELEASE.2025-09-07T16-13-09Z` / commit `01ce918d8279…` | 它不符合[后端指南](../backend/README.md)要求的 `RELEASE.2025-10-15T17-29-55Z` / `9e49d5e7…`，不能直接当测试固定二进制。取得正确产物后沿产品已有完整性要求验证。 |
| Docker/数据库 | Docker CLI 路径存在，PATH 无 `psql` | 未探测 daemon、镜像、现有容器或数据库；不据 CLI 存在认定测试环境就绪，不连接既有基础设施。 |
| scratch | `/workspace/scratch` 存在但直接子项为空 | 历史候选、driver、运行日志、私有 build 和资源文件不可直接续用；空目录不证明历史进程曾正常退出。 |

旧依赖恢复报告只解释当时安装过程，见[2026-10-08记录](environment-test-dependencies-2026-10-08.md)。不自动执行 onboarding 的安装/服务脚本，它包含旧版本及资源操作。纯文档接续不下载依赖、不启动 Docker/PG/MinIO。

## 缺失源码与恢复范围

- **Model Settings UI：** 当前25个已提交 web 源存在；下列4个验收 harness 路径缺失，无对应 Git 路径历史，也未在限定的 docs/tests 中找到源码副本。现有[规格记录](project-model-settings-spec-verification.md)证明工程契约，不包含可直接恢复的 Go/JS/TS 实现。后续按[正式卡](../work-items/d27-project-owner-model-settings-ui.md)及端点附件重建必要输入，保留原业务 FAIL 与未验范围。
- **Task planning：** [正式规格](../work-items/d11-task-planning.md)已接受，但15个新增技术路径（包括 `00022_task_planning.sql`）都不存在；6个已有共享/README文件不是新实现证据。旧 `/workspace/scratch/d11-task-planning-backend-preparation01` 等候选无法取得，后续基于现有 Structure 和规格重新实施，不宣称继续旧代理或已保存半成品。
- **Work Structure：** 18路径存在，限定技术域与 `a64fb5e7` 无差异，可消费已接受结果。原技术接受 receipt 的 scratch 路径缺失，但[永久报告](d11-work-structure-verification.md)保留组合接受及独立B外部工具终态缺口，不必为补历史文件重跑全卡。
- **其他前沿：** Skill 当前为 builtin/纯包，Knowledge 当前只有 contract；按台账相应行及正式规格恢复。旧 D08/D10/D12 卡的阶段派工和旧迁移号码不是本次分工，先核当前接缝与全局迁移序列。

Model缺失路径：

```text
tests/account/project_owner_models_web_fixture_test.go
tests/account/project_owner_models_web_test.go
tests/account-captcha-web/project-owner-models.config.js
tests/account-captcha-web/e2e/project-owner-models.spec.ts
```

`/workspace/scratch/mdn02`、`project-model-settings-ui/dist-ui01`、`dist-ui02`、`pmui` 及 Work原技术receipt均不在本次环境。重建源和新私有构建属于后续产品任务；不从旧 hash 文本、通过声明或测试二进制臆造原件。

## 恢复开发时的最小检查

先读当前任务授权，确认唯一文件写者、共享迁移/缓存/资产及真实测试资源的使用顺序。以下版本/依赖检查可按目标选择；它们不启动产品服务，也不安装依赖：

```sh
/workspace/toolchains/go1.27.1/bin/go version
node --version
npm --version
npm ls --prefix web --depth=0
npm ls --prefix tests/account-captcha-web --depth=0
```

本次实际运行了前三条版本命令；两条 `npm ls` 是接手示例，未在本次运行。若缺包，在后续依赖恢复范围内按现有锁补齐；不要默认删除并重装完整缓存树。Go脚本设 `AGENTEAM_GO=/workspace/toolchains/go1.27.1/bin/go` 且使用 `GOTOOLCHAIN=local`，避免隐式换工具链。

实现恢复后按任务卡选有意义的自测、必要浏览器/真实依赖检查和独立验证，命令入口见[后端指南](../backend/README.md)与[前端指南](../frontend/README.md)。已有输入未变时复用证据；缺失或重建部分验证其受影响行为，不为文档整理重跑全套。必要源、harness、probe 保存到正式仓库或 `.agent-state/<task>/`，不能只留 scratch；可由 Git、锁文件和命令再生的原始输出仍放忽略的 `output/ai/<task>/`，其余恢复必需材料必须入 Git。

Object runtime join、OpenAI tools独立验收、SPA concurrent-publication及Jina/Image来源保持台账所列停止。环境恢复和工具可用不是自动解停信号；原FAIL、缺失外部terminal/exit、未知业务因果均不得补写成PASS。

## 查询历史而不复制归档

旧逐轮日志仍可由基线读取，原验收证据目录未删除。按具体失败或任务定位即可，不默认加载全部：

```sh
git show e55ad7d1:docs/development/agent-team/recovery-2026-10-08-environment.md
git show e55ad7d1:docs/development/agent-team/tasks.md
git show e55ad7d1:docs/development/development-plan.md
git log --oneline -- docs/development/work-items/d27-project-owner-model-settings-ui.md
```

全局产品变化更新台账；任务目标、分支、精确路径、完成/未完、实际检查、失败限制、下一步和依赖恢复写入 `.agent-state/current.md`，本页不追加逐轮进度。主线程早建 `ai/<task>`，每个有意义结果、下级交付、长检查前、暂停或回用户前都自动 checkpoint；有改动时至多5分钟后的下一个安全边界，由下级停止相关写入并交出可恢复片段，主线程批次 wip commit 并 push 到 origin 同名分支，不等验收或提醒。保存不称 PASS，正式验收后的完整结果交付 `main`；具体命令以[团队流程](README.md#自动保存与跨设备恢复)为准。推送失败须明确尚未远端保存，保留本地提交并重试，不 force push 或覆盖他人工作。自动保存不会找回本页列出的缺失源码，也不改变原 FAIL 或停止边界。
