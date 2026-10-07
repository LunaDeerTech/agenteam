# 2026-10-07 环境恢复交接

本页只记录本次重新派工后的冻结恢复时点；后续结果以[任务台账](tasks.md)及各自固定证据为准。[上一接续 §39](recovery-2026-10-06-continuation.md#39-project-owner-usage只读http规格接受)与既有验收历史保持原文，不将旧临时工作区视为已恢复。

## 恢复输入

- 主线程初始核对 `work@bfdbd079`、工作区干净；实际 `git fetch` 取得 `6fa2ee721a75ea34a1ccd6523b8b25d68328c5b9`，相对初始提交仅六个 worker TOML 的 reasoning 从 `max` 改为 `ultra`。主线程执行 `git switch -c main --track origin/main`，全量保留远端成果；恢复基线 `main = origin/main = 6fa2ee72`，无未推送提交。文件配置变化不证明当前实例自动加载生效。
- `/tmp` 仅见 Codex 环境日志；旧私有 scratch 未恢复，不能复用旧临时候选或据其宣称当前实施通过。已提交规格、源码与验收证据仍是恢复依据。
- 主线程已读取 [AGENTS.md](../../../AGENTS.md)、[开发计划](../development-plan.md)、台账、上一接续 §39、当前卡及[规格证据](project-usage-read-http-spec-verification.md)，并核对实际代码与 Git，从首个未完成的阶段 A 恢复。

## 当前卡与实际派工

[D09 Project Owner Usage 只读 HTTP rev1](../work-items/d09-project-usage-read-http.md)已接受完整 STATIC 规格审查，实施未接受。三个 GET/HEAD 资源沿当前名称 resolve 与稳定 ProjectID 分工，每次独立完成当前 Session/Owner 授权；生产 `Invocations` 保持真正 nil。本次尚未执行产品测试或产品验收。

| 实例 | 已实际 ACK 的授权与状态 |
| --- | --- |
| `usage_backend` | 唯一阶段 A 作者，直接在 `main` 实施卡片 §6 的 #1/2/4/6/7 五路径；自有 scratch 为 `/workspace/scratch/usage-http-author`，已启动恢复实施 |
| `usage_verification` | 仅固定 `main` 依赖和独立计划，待 A 停写冻结后审查；当前没有独立实施验收结论 |
| `next_frontier` | 仅核对 D08/D10/D12 可执行依赖，不修改业务，不据此宣布模块完成 |

下一步由作者冻结 A 五路径及适用输入、自查证据，交独立审查；主线程核对结果后按正式卡继续授权后续阶段。当前授权不等于已启动 B/C/D 或真实资源运行。

## 保留边界

用户已明确 Summary 决定：“应该是系统管理员统一配置用于summary的模型”。范围是会议 Meeting Rolling Summary 的 initial/update（含首轮标题）；既定 Agent compaction 沿 Execution snapshot，Execution Summary read model 不改。主线程按项目消费系统配置理解，未将其解释为向每个新项目复制默认；已交 architecture 做会议 Summary 系统配置的影响分析，正式契约与实现尚未调整。

Object runtime join、OpenAI tools 独立验证、SPA concurrent-publication 三项原停止保持，不重试、不改写、不改派。D08–D28/E01 尚未完成，E01 未开始，ready503 不变；不宣称游戏或参考版已选定，也不宣称生产 Runtime Facts、Project 初始化/生命周期、Usage UI 或 Execution Summary 已交付。

本次持久交接仅新增本页及台账页首摘要，不重建大型证据索引，不修改旧历史。文档交付由这两条路径的 Git 历史定位；本页不预先声称已提交或推送。

## 测试依赖恢复结果

[测试依赖恢复报告](environment-test-dependencies-2026-10-07.md)记录固定 MinIO 二进制及两个 PG digest 镜像已恢复。MinIO SHA、release/commit、`AGENTEAM_MINIO_BINARY=/workspace/scratch/fixture-recovery/bin/minio` 与 16 条实际命令（含四次原失败）均可追溯；PG 17.8/16.12 仅为 image inspect 配置，未运行数据库。原 42 项 manifest 保持，仅保存其中 40 项小原件及 manifest 自身，binary/ZIP 只保留指纹。11 项仓库输入未变；未启动容器、listener、DB 或产品测试，不构成 Usage HTTP 或其他产品验收，三停止边界不变。以上原恢复时点正文保持原文。
