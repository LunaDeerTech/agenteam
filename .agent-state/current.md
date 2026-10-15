# 当前状态：M3 明确范围已接受，分支收尾完成

本轮明确的 Knowledge 检索与引用、Memory、领域工具、MCP、Meeting、必要 Runner 扩展及共同接线已完成并正式交付 main。接受范围、原失败和未覆盖边界见 [M3 卡](../docs/development/work-items/m3-knowledge-memory-collaboration.md)、[独审报告](../docs/development/work-items/m3-core-independent-review.md)及[任务台账](../docs/development/agent-team/tasks.md)。清理不扩大产品验收，也不把旧失败或未测范围改为通过。

## 当前 Git 与接续入口

root 负责全部 Git，本文件是唯一协调索引。本机仅有 `/workspace/agenteam` 的 main 工作树和本地分支；所有 M3 临时工作树、本地/远端分支、对应跟踪引用均已清退，精确 fetchspec 仅保留 main。本地冗余 `work` 分支也已删除。

远端仅保留 main 和 `ai/task-comments-20261013`。后者保存尚未合入 main 的“不改变任务状态的独立追加评论”API、迁移、服务、UI 与测试，完整任务尚未交付；这不表示产品整体没有评论能力，也不把该旧分支视为已验收。

旧 `ai/live-chat-execution-e2e-20261013` 与 `ai/project-cleanup-validation-20261012` 的业务目的已由 M1/M2 后继替代，两个远端分支已清退。完整恢复 bundle 为 `/workspace/agenteam-legacy-recovery-20261015-cgkz1r1a/live-validation.bundle`，已在同目录 `restore-check.git` 空仓恢复并核对两 tip。该备份仅在本机，未上传；原直连、无工具外部单轮失败和未测范围仍是历史事实，旧 DNS 记录不代表当前接口状态。

## 材料与恢复

必要源码、固定输入及已审截图已收拢并推送 main。原协调树不再承载材料或未来消费者。

- Model 七份原 M3 测试及两个搭配 helper 保存在[恢复 harness](m3-model-runtime/reproduction/README.md)，其余共用 fixture 与产品 API 由 main 提供。唯一一次隔离 overlay compile-only 已成功编译 Model、retentionprobe 和 App 三包；未执行测试，也未把这七份测试直接整合为 main 默认测试集。具体恢复方法、原结果与限制见 [Model 恢复卡](../docs/development/work-items/m3-model-runtime.md)。
- 固定检索选型、排名、来源及 bootstrap 复现材料位于[检索恢复入口](m3-retrieval-selection/README.md)；安装脚本、bootstrap SQL 和共用 notices 复用 `deploy/retrieval/`。唯一公开向量继续使用 [runtime-embeddings-v2](m3-model-runtime/runtime-embeddings-v2/manifest.json) 的 gzip，不另保 raw 副本。
- Meeting 参与者/引用 SQL 测试已按原路径保存在 `tests/meeting/`；Memory、Meeting 和 MCP 已审截图已纳入 Git，实际位置见独审的材料说明。

必要原日志、owned 与退出记录位于 main 的忽略 `output/ai/`，仍是本机证据，未通过 Git 上传。Model 原 `output/m3-model-checks/`、`output/m3-model-cold/` 分别映射到 `output/ai/m3-model-checks/`、`output/ai/m3-model-cold/`，其余所保原件沿原相对路径；内容和 mtime 保持。原测试结果没有重跑或改写。

Memory 的可再生 Node 依赖实体已迁到 `/workspace/shared/agenteam-m3-memory-node_modules`；旧 MCP/UI 软链已解除。已有 `/tmp` 工具、共享 overlay 和原产物未删除；清理前已确认旧树无活跃消费者。

## 后续范围

本轮 M3 无剩余必达测试门。下一轮按真实缺口评估完整 Lifecycle/全平台 Runner、旧页体验、生产 SPA/部署恢复和完整 D24/D25 token 流；不自动进入 M4、E01 或生产发布。`update-meeting`、`comment-meeting` 的可写语义仍未定义、未注册、未接受，原产品问题保留。
