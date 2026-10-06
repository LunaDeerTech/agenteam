# 2026-10-06 恢复续接

本记录固定本次从 `125e3c222ffca01ea0fd987d7dc47b8c30fa5f8b` 恢复时的 Git、源码、持久证据与派工事实。[此前恢复记录](recovery-2026-10-06.md)及[任务台账](tasks.md)旧段落保留当时状态；其中的实例名、临时路径和“实施中”不表示旧实例或未提交候选已经恢复。本页是恢复交接记录，不是新增产品接受报告。

## 1. 本次 Git 与运行目录

- 主线程初始核对为干净的 `work / 125e3c222ffca01ea0fd987d7dc47b8c30fa5f8b`；实际 `ls-remote` 返回同一哈希。主线程随后 fetch，创建跟踪 `origin/main` 的本地 `main`，没有未推送提交。
- 文档负责人只读复核：`HEAD = main = origin/main = work = 125e3c222ffca01ea0fd987d7dc47b8c30fa5f8b`，`git status --short --branch` 仅返回 `## main...origin/main`，`git rev-list --count origin/main..HEAD` 为 `0`。该干净状态是本次作者开始写入前的快照。
- `/workspace/scratch` 存在且初始目录为空。旧 scratch 中的 driver、运行资源或未提交源码没有随本次恢复出现；不能将旧清理记录当成本次 Docker、进程或运行目录检查。本轮文档任务没有启动业务进程或占用测试资源。

## 2. 已提交结果与本次离线复核

本次 `recovery_verification` / verification_worker 在固定 `125e3c2` 上读取归档核验脚本后执行只读检查，并将相关源码与固定 Git、当前工作树及原清单逐字节比较。以下结果复用原业务验收，没有重跑业务测试。

| 已提交完整结果 | 本次离线结果 | 保持的接受边界 |
| --- | --- | --- |
| [Invocation/Usage ledger rev1](invocation-usage-ledger-verification.md)，源码 `36e5ff1` | 核验脚本 exit 0：188 档案、185 原件、16 输入；20 交付源及 13 项组合上游匹配。原日志解析仍为 final01 的 11 顶层 PASS、combined01 的 3 顶层 PASS | 账本库及 00018 schema；生产 Facts/Runtime、发送重试、Usage HTTP/default root 未绑定，完整 D09 未完成 |
| [System Model 管理读口 rev1](system-model-management-reads-verification.md)，源码 `ecd7337` | 核验脚本 exit 0：77 档案、71 原件；input01–05 的源数依次为 9/9/13/13/13，可按证据重建。最终 13 源匹配；原独立日志为 2 顶层 PASS | 当前凭据 metadata 与有限精确删除影响读口；管理 UI、真实外域 reference adapter、Provider 调用未交付，预览不授予删除权限 |
| [公开 Account 入口 rev1–rev5](public-account-entry-verification.md)，源码 `787a5c7` | 核验脚本 exit 0：438 档案、854 逻辑原件、16 历史输入、333 映射；25 源匹配固定 Git 与当前源码。15 项 dist 仅核归档哈希 | 邀请兑换、找回申请、密码重置及卡内共享认证/设置结果；没有新增 SMTP 实投、原生浏览器缩放、生产 SPA 或真实 Vite proxy 结论，完整 D26/D27 未完成 |

复核所用最终清单 SHA-256：ledger input13 为 `b0dbeefe78312c2b2ca81f7e6de80fc8cde6b4f3354cfd3d6c7497a847b3009e`；管理读口 input05 为 `0208b2dcdce8623f65f2d7c2364a3daf083994f6271aeb63c02ca6fba7915a27`；公开入口 input13 为 `f824cf28b414a2b41276ae383342fd9bb861987784623929b0e35690ceff9fd4`。现行 dist 没有实测；旧失败、分版本证据组合及原资源观察限制均保留。

验证负责人实际命令如下，工作目录均为 `/workspace/agenteam`，三条均 exit 0；文档负责人没有重复执行：

```sh
env PYTHONDONTWRITEBYTECODE=1 GIT_NO_LAZY_FETCH=1 python3 docs/development/agent-team/invocation-usage-ledger-verification-evidence/verify-evidence.py
env PYTHONDONTWRITEBYTECODE=1 GIT_NO_LAZY_FETCH=1 python3 docs/development/agent-team/evidence/system-model-management-reads-verification/verify_inputs.py
env PYTHONDONTWRITEBYTECODE=1 GIT_NO_LAZY_FETCH=1 python3 docs/development/agent-team/public-account-entry-verification-evidence/verify_archive.py --candidate-root /workspace/agenteam
```

## 3. 旧 tools 与 Artifact 候选的精确路径核对

文档负责人按以下已提交清单逐项只读检查当前路径、`HEAD` Git 对象、原业务基线和候选文本哈希，没有将归档复制回源码树或运行保存的测试、driver、probe。

| 候选与精确清单 | 当前源码树与 HEAD | 持久归档 |
| --- | --- | --- |
| OpenAI tools：[14 路径清单](openai-chat-tools-wire-verification-evidence/candidate-files.json)，业务基线 `ecd733711caff5df46e423cadab52b32c34f785e` | 10 个新路径全部缺失；4 个旧路径逐字等于业务基线。14 项中没有当前源码匹配旧候选 | 14 份候选文本仍在，逐 SHA 与清单相同；清单 SHA-256 为 `1da8b486447a58f8f9ae5ffdc6ff81947a4f8f462ae98403f32c80c63c20084a` |
| Artifact：[16 路径清单](evidence/artifact-project-stop-verification/candidate/manifest.json)，业务基线 `6658a6cb1f29299521773bc8dc86b2f607b8c809` | 11 个新路径全部缺失；5 个旧路径逐字等于业务基线。16 项中没有当前源码匹配旧候选 | 16 份候选文本仍在，逐 SHA 与清单相同；清单 SHA-256 为 `79e5eb812c0fda2a42d390a7e2703b1ebca4ce581c055e1c2a99fda1ab76914e` |

tools 的旧四源是 `internal/central/model/adapter/` 下的 `openai_chat.go`、`errors.go`、`sse.go`、`transport.go`；缺失源为清单中 `baseline_git_blob = null` 的全部十项。Artifact 的旧五源是 `internal/central/artifact/` 下的 `service.go`、`create.go`、`upload.go`、`publish.go`、`commands.go`；缺失源对应[原卡 rev3](../work-items/recovery-artifact-project-stop.md)的三项新生产、三项新单测和五项新集成测试。精确路径以各清单为准，不把文档归档路径视为产品源码路径。

验证负责人另已审读并执行 `env PYTHONDONTWRITEBYTECODE=1 GIT_NO_LAZY_FETCH=1 python3 docs/development/agent-team/openai-chat-tools-wire-verification-evidence/verify_archive.py`，exit 0，核对 174 档案、167 原件、14 候选、9 历史输入及 117 映射。这只是[有限 tools 证据](openai-chat-tools-wire-verification.md)的离线完整性检查，独立动态仍为 **NOT RUN**，产品候选未恢复、未提交、未接受。[Artifact review09](artifact-project-stop-verification.md)继续只保留本域有限结论；共享 guard、更强恢复与完整卡仍 **BLOCKED**。

## 4. 保持停止、待定与未完成的范围

Object Runtime join 原修复任务及 `tools-independent-01` 原独立任务都曾被自动安全筛查以 **“possible cybersecurity risk”** 中断，继续保持停止；本次不重试、改派、换方式重建或执行其保存的探针。Object [已知 join 缺陷](object-runtime-join-regression.md)没有修复，Artifact 最终共享 guard/完整卡与 Project 领域绑定仍受阻；tools 原独立动态未运行，不能写成已执行失败或独立 PASS。

Summary 创建初值已由主线程重新询问，截至本记录没有用户答复，仍为待定。Anthropic Messages [规格接受](anthropic-messages-wire-spec-verification.md)仅限静态规格与来源，tools 产品前置未满足，其 19 条候选实施路径没有因恢复而获得写权。

生产 Project、执行与绑定、Artifact/download HTTP、Runner 传输授权/协议、Model Resolver/Invocation/Usage、真实 Provider/MCP 调用及其余后继能力没有因离线核验完成绑定。`/readyz` 503、`ready=false` 的既有边界保持；本轮没有启动进程重测健康接口。完整 **D08–D28/E01 尚未完成，E01 尚未开始**；E01 游戏、参考版本与完整清单尚未冻结。

## 5. 当前派工与接续

| 本轮实例 / 角色 | 唯一范围与阶段 |
| --- | --- |
| `recovery_architecture` / architecture_worker | 唯一编写 [D27 System 用户目录读取补口规格](../work-items/d27-system-user-directory-read.md)。rev1 已冻结并经独立 STATIC PASS、主线程采纳，规格已单独提交推送 `c11b512d5953501c4bb1e15c92e6cc8a84564b51`，主线程核远端一致 |
| `recovery_verification` / verification_worker | 完成已提交 ledger、管理读口与公开入口的离线持久证据复核、tools 归档只读核对，以及 D27 补口 rev1 独立静审；这些静态结论不代表新业务实现通过 |
| `directory_backend` / backend_worker | 已按主线程授权正式实施 D27 补口卡内五路径；本记录冻结时尚无该实现的业务验收结论 |
| `recovery_documentation` / documentation_worker | 唯一写本续接记录及台账页首短恢复入口；保留台账原正文和所有旧文档，文档自查后停止写入交主线程审查 |

D27 补口针对已经确认的需求与恢复基线输出缺口：`ListUsers` 查询并读取 `created_at` 用于分页，却没有将注册时间返回给用户列表；[系统设置布局](../../frontend-design/layouts/system-settings.md)要求显示注册时间。rev1 静审输入 SHA-256 为 `4edb37f8c7cd1f9aa492b2a797f348d6e8bb5a92c0a52b405c365eb40537d216`；采纳后仅更新页首，技术 §1–5 未变，提交稿 SHA-256 为 `b3d7c1b5da7a6cbca3a5c8b709adbdfe8cdf7d79eb5d96549e74445ad0650ec1`。规格已单独交付，五路径实施已启动；该状态不表示 System 用户页面、返回字段或完整 D27 已交付。后续实现、自测与独立业务验收另行记录，本页不新增业务写权或测试资源运行权。

## 6. 文档范围、检查与交付

本次遵循[仓库规则](../../../AGENTS.md)、[团队流程](README.md)、[文档技能](../../../.agents/skills/agenteam-documentation/SKILL.md)和[开发计划](../development-plan.md)。文档负责人实际执行 Git 状态/引用/未推送计数只读核对，使用 Python 按两个清单检查 30 个精确路径、原基线及候选文本 SHA；没有修改任何候选源码。

本次仅写 `docs/development/agent-team/recovery-2026-10-06-continuation.md` 与 `docs/development/agent-team/tasks.md` 的页首短恢复入口；后者原正文逐字保留。两文件的本地链接、Markdown 结构、UTF-8/LF、末尾换行、尾空格及限定文件 whitespace 检查通过；其余旧文档保留原内容。没有运行 Go、npm、浏览器、数据库、Docker 或产品测试，没有执行 Git 写操作。

文档范围完成自查后停止写入，无关联后台命令或自有运行资源；由主线程审查后按已有授权独立提交并推送，交付提交通过这两份文件的 Git 历史定位。本记录不预先声称已提交或推送，D27 规格及实施文件不属于本次文档交付。

## 7. D27 用户目录后端接受与 UI 前置交还

§1–6 保留本次恢复起点与当时派工。注册时间读口现按[rev2 六路径](../work-items/d27-system-user-directory-read.md)完成，由 `directory_backend` 实现、`recovery_verification` 独立 PASS，主线程采纳提交推送 `3affc0194214101cfa1e6fdc583afa5d60005db8`，实际远端一致。[正式报告与持久证据](system-user-directory-read-verification.md)固定 input02 SHA-256 `004ced3247661feca93ef7899dbc539f9f638a17daa824c30692881f26622c96`；六源、两项依赖锁与五项原边界末检匹配。

author01 首红为合法 admin HEAD 实际405、原断言要求200；rev1 规格/静审遗漏了 Account 严格 method 分派，不能用 serializer 的空 body 能力证明路由已通。原输入、driver exit1 和失败日志保留；rev2 新增 users 显式 HEAD、精确 query 分类和 OpenAPI 后，author02 原两组全 PASS、独立探针一组 PASS，两个 driver exit0。原 HEAD200 及旧权限/路由断言未放宽，no-tests/仅编译不计动态通过。

三轮均已 actual wait，各四容器/三网络 exact-ID 双次 absent，原两容器/四网络的 ID/name/labels 不变，所属进程与 runtime 为空，窗口已交还。归档只保留原报告、精确源码版本、命令/raw、静审纠正与资源终局；文档负责人只做离线原字节/Git/链接核查，没有重跑业务。

该结果只接受专用九字段目录与 GET/HEAD 读口，原共享八字段 User 保持。[系统用户目录 UI](../work-items/d27-system-user-directory-ui.md)的后端依赖现已满足，后续页面、同一 Cookie owner 组合及浏览器验收另行推进；没有前端、邀请投递补口或完整 D27 通过声明。Summary 待决、Object/tools 原任务停止、Artifact/Project 阻塞、生产未绑定、ready503、完整 D08–D28/E01 未完成与 E01 未开始保持。
