# 当前执行检查点

## 目标与本轮恢复

- 持续推进 D01–D28 产品、真实集成和 E01 平台内游戏复刻验收；整体仍在进行中，生产平台、完整 D11/D27 和 E01 均未完成。按架构清晰、接口稳定、便于维护的模块边界并行推进；WIP 保存不代表验收通过。
- 本协调树为 `/workspace/agenteam`、`ai/product-continuation`，本轮从远端 `32a4d8a2` 恢复，保留已有 Model 未验源码和验收材料。主线程已合并正式 `origin/main` `280a6431`，三文件冲突修复随合并提交 `cb1cdded` 推送。当前这份恢复记录另待 root checkpoint；不能把尚未保存的编辑称为已远端保存。
- 最新 main 已包含普通 Variables 后端、Knowledge B02/Owner 只读和树命令 HTTP、Runner Linux/amd64 限定能力/00026、Skills P2/00027。各自范围见[任务台账](../docs/development/agent-team/tasks.md)和正式卡；D05/00028、Secret 存储/00029 与 Owner Service/00030 仍是 WIP，正式迁移按连续前缀和各卡门槛交付。
- 旧代理 ID、进程、终端和资源授权不继承。当前六个子代理席位动态共享；本轮已恢复任务见下表，不为角色或子树设置永久名额。全部 Git 写操作、分支/worktree 和最终集成由 root 执行，源码/harness 及各树状态由获授权执行者保存。

## 当前实际任务与唯一写者

| 执行者 | 工作树 / 分支与恢复输入 | 当前任务和下一步 |
| --- | --- | --- |
| `/root/coordination` | 本树 `ai/product-continuation` / 远端 `32a4d8a2` + main `280a6431` | 修复上述三文件冲突并维护本 `current.md` 与全局台账；三文件已随 root 合并提交保存。独立只读评审 Skills HTTP `cc0e45ac`，产品/OpenAPI/tests/harness 已停写；复用已有有效 native PASS，给出必要 PG 风险补集，真实窗口由 root 调度。 |
| `/root/cleanup` | `/workspace/agenteam-object-metadata-cleanup` / `ai/object-metadata-cleanup` / `a9f01003` | 恢复 D05 有界清理与 00028；核对历史/Stop 和成本测试前置、已有失败及实际待验范围，再推进定向修复/验证。 |
| `/root/secret` | `/workspace/agenteam-secret-variable-owner-service` / `ai/secret-variable-owner-service` / `482c5ae5` | 恢复 Secret Owner 服务；D04 上游 `ai/secret-variable-storage` / `b724e397`。保留作者矩阵固定版本通过，核连续迁移前缀、独立验证与装配缺口；00029/00030 尚未正式交付。 |
| `/root/content` | `/workspace/agenteam-knowledge-content-http` / `ai/knowledge-content-http` / `ad829aab` | 恢复 Knowledge 有界正文 HTTP，核候选编译和真实 PG/native 待验范围；消费正式 B02/Account，零迁移，默认 root 尚未绑定。 |
| `/root/skills_http` | `/workspace/agenteam-skills-owner-http` / `ai/skills-owner-http` / `cc0e45ac` | 恢复 Skills GET/HEAD 目录详情，核候选编译与 PG 待验范围，已有效 native 结果复用；消费正式 P2/Account，零迁移，默认 root 尚未绑定。 |
| `/root/work_ui` | `/workspace/agenteam-work-ui` / `ai/work-owner-planning-ui` / `197e7cb0` | 恢复 Work 规划 UI 原消费者完成接缝与后继真实验证；recovery08 原 FAIL/缺尾保留，recovery09 尚待本轮确认。 |

以上分支在本轮只读 Git 核对时与对应远端输入一致；后续各树修改、检查点及实际资源终态以该树 `current.md` 和 root 的 Git 报告为准。本表不自动继承历史通过结论，也不声称其他分支已恢复为活动实例。

## 已保存但尚未恢复为活动线的工作

- Model UI `ai/model-ui-delivery` / `41cbf90f`：原组合新 6/6、旧 14/14 和独立 A/B 已按限定输入接受；新 main 两 Audit 已完整通过，新的 authority 验收仍未通过，首 FAIL 和旧 Wait/TCP/独占缺口保留。本协调树的相关未验材料不丢弃。
- 普通 Variables UI `ai/project-variables-owner-ui` / `60dbcee7`：CRUD03 限定通过；Authority03 整体 FAIL，尚欠原 detail GET 消费者证明及其余权限矩阵，不能由归档 409 局部后验提升整组。
- Skills Cleanup `ai/skills-cleanup` / `32740452`：已有真实 D05 两 top/五 sub、最后同 Tx/Unknown 的有限结果；old-attempt、成本和生产 root 仍待。
- Task Timeline `ai/task-timeline-reader` / `9ad6f1cd`：只读 Reader 有限独审，三组真实 PG 尚未验收。

## 环境、验证与共享资源

- 本轮实际确认 `/workspace/toolchains/go1.27.1/bin/go` 为 Go 1.27.1。旧 runner-control 模块缓存路径、当前默认 `/home/agent/go/pkg/mod` 与默认构建缓存目录均不存在，限定 Go 包测试未执行；不能沿用旧环境的“依赖已齐”结论。依赖恢复由 root 协调相应执行者，当前 cleanup 共享依赖网络窗口内不并发联网或真实测试。
- root 调度当前唯一真实 socket 窗口，以兼容既有 PG/browser/native harness 的宿主 TCP 观测；该窗口涵盖运行、原 Go/driver/outer 实际 Wait 及资源退出尾。窗口期间 Git 网络、下载和另一 socket 测试按 root 安排，离线无共享输入冲突的工作继续并行。未获本轮窗口前不启动真实资源。
- 原 Wait、资源 ID/nonce、runtime/private/desc/TCP 等适用终态必须由该轮本人实际结果证明；后验 current-clear 不补原 FAIL，不推测旧失败根因。各树保留必要源码/harness/独特失败输入，可重建日志留忽略的 output。
- 本轮冲突有限核对已通过：三路径无冲突标记、`git diff --check`、Go 测试文件 `gofmt -l` 为空；初始化 Audit 测试与 `origin/main` 一致；三份 Markdown 共 137 个本地链接/fragment 检查通过。缺现成依赖缓存，Go 包测试未执行；尚未运行真实资源，也不由合并或文档核对新增产品 PASS。

## 保留停止项与最终门槛

Object Runtime join、OpenAI tools 独立动态验收、Central SPA concurrent-publication、Jina/Image 来源停止项仍按[任务台账](../docs/development/agent-team/tasks.md)执行，不因恢复自动解停。`ready=false` / `/readyz` 503 的现有产品边界不变。

E01 尚未开始。实施前必须冻结确切游戏版本、完整内容分母、权重和关键门槛；最终须使用 agenteam 本身组织任务、Agent/Execution、审核和产物，并以可运行游戏、真实试玩及独立验收证明至少 50% 完整内容覆盖。
