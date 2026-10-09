# 当前执行检查点

- 目标：从环境中断处恢复产品开发，完成 D01–D28 全部能力及 E01 平台内游戏复刻与真实试玩验收。
- 状态：进行中；产品恢复实施尚未验收，不能视为 D11/D27 或全平台完成。
- 当前分支：`ai/task-planning-recovery`，恢复基线为远端 `main` 的 `1b38f470`。
- 恢复核对：初始本地 `work` 为 `3add174d`、工作区干净；fetch 后保留并 fast-forward 远端三个协作流程提交。没有发现 `origin/ai/*` 活动任务分支，也无本地未推送独有提交。

## 当前工作与所有权

1. D11 Task Planning：按[正式规格](../docs/development/work-items/d11-task-planning.md)恢复缺失实施。负责人负责卡中产品/测试路径、迁移 `00022` 和局部 README，安排唯一写者及自测；未参与实现者独立验证后才正式交付。两 ID PG-only harness 旧源同样缺失，必要重建输入保存在 `.agent-state/task-planning-recovery/`。
2. D27 Model Settings：并行按[正式卡](../docs/development/work-items/d27-project-owner-model-settings-ui.md)重建四个缺失 Go/browser harness；此执行者仅写本卡四测试路径及 `.agent-state/model-ui-recovery/`，保留旧 FAIL，尚未运行业务或独立验收。
3. root 独占当前检查点、全局台账与 Git。构建、迁移和真实测试资源按唯一写者与隔离 fixture 协调；必要源码随检查点保存，可再生日志在忽略的 `output/ai/`。

## 环境实际核对

- Go：`/workspace/toolchains/go1.27.1/bin/go`，实际版本 `go1.27.1 linux/amd64`。
- Docker server：`28.4.0`；工作盘可用约 30 GiB。
- 已成功取得测试固定 PG 镜像 `pgvector/pgvector@sha256:99a149d3c84cfb0f32d8da7d72737e4643468787220af2223418730f8e9e9cdc`；尚未启动任务 fixture，镜像存在不代表业务验证。
- 现有 `scripts/test-postgres.sh` 默认会转 Object 套件，公共 PG fixture 会含 PG16 且未列 tests/work，不能直接当 Task 两 ID 测试 driver。
- MinIO：已按正式固定来源与 Go/CGO0/trimpath/版本 flags 构建 `output/ai/deps-minio/bin/minio`，version 与 commit 正确，SHA256 `dc5298474f0bc87a068f0b1135c583bb1278c17c11c512212ed7644a238c89c8` 与测试契约一致；二进制可重建，不纳入 Git。
- web/harness 均已按各自锁恢复依赖；本域4单元文件354测试实际PASS（8.25s）。浏览器 executable `/usr/bin/chromium` 存在，首个 data: 页面探针被 policy 拒绝（原exit1保留），后续 owned loopback HTTP 探针实际exit0，Playwright1.56.1/Chromium151.0.7922.173，browser/server Close均返回；不归因为业务失败。

## 未完成与后续

- Task DTO/typed event/六Fault/Project闭集4源已形成可构建片段：作者离线 contract+Foundation 旧pure实际exit0；Task新pure测试待补。Project追加离线检查首因固定依赖缺失FAIL，准确 go mod download 后三包实际PASS；Task runtime/reader/PG测试仍实施，尚未验收。
- Model harness首段3源已形成：同fd私有读取/严格JSON/IPC闭合和case配置；作者限定两个Go源race测试exit0，独立复核已完成，绑定 `02e3daaf`：四race测试、private/JSON边界及13个Node配置检查PASS；8个非法typed/union IPC参数仍获空错误真实复现，独立probe保留预期exit1；decode当前仅envelope/action键闭合，值类型/union与登记授权还需补齐，不能当完整IPC admission；完整后端fixture与浏览器业务仍未实现。
- 当前尚无新整卡产品测试通过结论。阶段源码完成后先作者自测、冻结限定输入，再按风险安排独立验证；每个完整结果及时交付 main 并普通 push、核实远端。
- Model UI 原恢复 FAIL、Work Structure 原 Unknown01 FAIL 与独立 B 外部工具终态缺口保留，不回填历史。
- Object runtime join、OpenAI tools 独立验收、SPA concurrent-publication、Jina/Image 来源沿台账停止边界保持；局部停止不妨碍 Task 规划与验收输入恢复。
- E01 未开始。游戏参考版本、完整内容分母、权重与可复现覆盖率须在平台前置完成后、游戏实施前冻结；最终需要平台内任务/协作/执行/审核/产物和真实试玩证据。
- root交付worktree `/workspace/agenteam-delivery` 的 `main` 仅带基线与4个Task contract源；三包race、vet及Central/Runner两入口build实际exit0。尚未commit/push main，等待新pure测试和独立契约审查，不把构建当完整验收。
- Model独立可复跑probe：`.agent-state/model-ui-recovery/independent-probe.py`；审查输入固定02e3daaf，未来新输入须另验。
- 当前保存必须遵守用户“不提交无法构建的中间状态”：仅保存独立闭合、可构建的明确片段，未闭合源码保留工作区待依赖完成。
- 工具全树当前 7 席位；子实例显式 Astra/Ultra，priority 实际生效未确认。不因配置 100 推断当前容量。
