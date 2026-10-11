# Agent 创建与编辑的 Runner 挂载声明

状态：后端候选 `c189c9e7` 已通过下述分段纯控与真实 PG 领域 HTTP / final 回滚链，并获独立有限接受；默认 App 配置 UI 后继已随 main `94a225a2` 正式交付，真实普通 Owner 创建、编辑及 Unknown 恢复的有限验收见 [UI 交付卡](d10-agent-runner-mounts-ui.md)与[原 native02 证据](../../../output/ai/retired-20261011/agent-runner-mounts-ui/native02/)；不外推物理 Runner 执行或完整 D10。基线 `3c7f820d`，承接 [D10 Agent 配置](d10-agent-configuration.md)、[D15 Runner 控制](d15-runner-control.md)、[D16 Workspace](d16-runner-workspace.md) 与 [Agent Workspace](../../architecture/runner/agent-workspace.md)。

## 已定输入与兼容

原 `agent.create` / `agent.update` 接受 `runner_mounts:[{runner_id,workspace}]`，沿原 Agent 当前权限、Session 和 CSRF，不增加 Admin 门或独立 Mount.Create 流程。与 `allowed_mount_ids` 互斥；Create 必须选择一个 profile，Update 省略保留、`[]` 清空、`null` 拒绝。最多 128 项，原每类 128/总 256 引用以及完整命令 JSON 512 KiB 预算不变。目录名逐字沿 `mount/contract.ValidWorkspaceName`：1–255 ASCII 单段、无尾点、无 Windows 保留名，不截断或归一化。相同 `(runner_id,workspace)` 重复拒绝；保存原数组顺序。

旧请求 digest、strict AgentConfig、旧 receipt/Lookup 编码不变。新 digest 仍包含原请求及 expected_version，不包含生成 ID。新 receipt 在原 AgentMutation 中条件新增 `runner_mounts:[{mount_id,workspace_id,runner_id,workspace}]`，显式空声明返回 `[]`，不允许 null。映射逐项匹配原声明，MountID 集合恰等 Agent.allowed_mount_ids，并与原持久 command plan 完整匹配；Lookup 仍提交原请求/expected_version/key。已完成历史 receipt 在原 Read 门后返回，不受当前 Runner 状态重新判定。

## 原子计划与提供方

在原 planned → freeze → final 路径内，prepare 首次 AcquireAll 前收齐 Runner/System、Command、User、Project、Agent 锁。真实同 Store Mount planner 只读并生成稳定 ID，原 command 记录持久保存映射，重试复用；旧 Tool freeze 可以推进 plan_revision，但不改变映射。final 依原完整 union/current 权限/preimage/私有 Agent canonical witness，单一事务落 Agent、Mount 定义、稳定 workspace 映射、双向引用、原 typed Agent Audit/Event 与 receipt。无新独立命令或提交边界。

新定义 Name 为规范 MountID 字符串；旧定义 Name 保留。同 Runner/Project/Agent/workspace 再加入复用既有映射，旧重复 tuple 不任意选择。清空仅去引用，不删除逻辑定义、稳定映射或物理目录。新增 00060；不改 00032/00035。配置不制造 Runner 物理目录或启动执行器。

Runner 新窄 provider 自查同 Store/held locks/当前 Project Owner Read，只返回安全 ID/name/description/version/status；offline/incompatible 不等于长期引用无效。禁止调用 Admin Reader 或公开 root_path/credentials/hello。

## 配套当前读面

- `GET/HEAD /api/v1/projects/{p}/available-runners`：无需 AgentID，当前 Owner Read，最多每页 50，签名 cursor 绑定 Owner/Project/resource/sort，每页重新授权。
- `GET/HEAD /api/v1/projects/{p}/agents/{a}/runner-mounts`：独立 DTO `{project_id,agent_id,agent_version,items:[{mount_id,runner_id,workspace,name}]}`。同 Tx 核当前 Agent/head/version/refs/定义，完整至多 128 项；缺失/错配安全失败，不漏项或假报空。消费者与原 GetConfig version 对齐后回填。初片仅既有 active 定义，无新增 disable writer。

两路 2s、no-store、原 GET/HEAD/request I/O 实际退出规则；旧 strict AgentConfig 不加字段。

公开形状见 [Agent 配置 OpenAPI](../../../api/openapi/agent-configuration.json) 与 [Runner 候选及挂载当前读取 OpenAPI](../../../api/openapi/agent-runner-mounts.json)。默认 App 已接同 Store 提供方与原 cursor keyring；此处源码接线不等于该 App 路径已经完成动态验收。

## 写域与验证门

父作者负责 Agent contract/command/authority/Runner 安全端口/HTTP/schema/App 与专属 projectvariable 集成测试；子作者唯一负责 mount/** 和整个 00060。Root 单独负责 Git；独审另派。

先验证 strict 输入/预算/兼容 digest 与 receipt、缺依赖/权限/映射一致性及回滚/Unknown。首真实小链使用正式 Agent/Runner 提供方：非 Admin Owner List → Create 声明 → 同 Tx Mount/refs → GetRunnerMounts / RunnerMountBindingsInTx 当前编辑回填 → Update no-op/换目录/清空 → 原 key Lookup。不得 SQL 假造 Mount。PG 方法、binary/输入、资源预算和完整 Wait/双尾须先冻结独审，再由 root 排独占窗口。不重跑无关 D06/Runtime 矩阵。

非空 execution snapshot、默认 Runner executor/dispatch、生产 Project initializer/ready/registry/Cleanup 等既有 STOP 不解除。

当前编辑回填不称 Execution Resolve：现存非空 ExecutionCapture 与 operation/dispatch 授权仍未绑定，不因本片目录事实而扩权。

## 有限验证与原失败

原件位于本任务树 `output/ai/agent-runner-mounts/`，以下为不同固定输入的分段结果，不称当前 HEAD 一次全量通过。

- `unit01` 固定 `6f609a4a`：1658 技术输入前后相等，新 7 top / 4 sub race 通过；Runner service、App 与共享 assembly 仅编译，未命中动态 App 测试。原工具 68344 actual 0。
- `5f48159f` 将新声明 planner 前的目标存在、版本和 active 检查恢复为原命令错误顺序；不是通过放宽 planner 接受 stale 输入。修后真实 PG 第二轮核当前 v4 / expected v3 返回 409 且零新增计划和业务事实。
- 公开 Lookup 原 completed 分支漏核冻结 mapping 与 EventIDs，`75320ba7` 复用统一 `commandReceipt`。`lookup-repro01` 仅覆盖旧 `5f48159f` 的 reader 文件，在真实公开 Service.Lookup 的受控 SQL / 授权 seam 下得 4 个坏回执 FAIL、2 个有效新旧 profile PASS；保留原 actual 1。无 overlay 的 `lookup-fixed01` 同一 1 top / 6 sub race 全过，`compatibility01` 原 strict / create / update / Lookup / schema 15 top / 9 sub 共 24 PASS。原工具 48963 actual 0，三段输入各自前后相等；这些不是 PG 事务或真实撤权竞争证据。
- `native01` 固定 `18daab3b`：两 sub 在正式 Runner seed 后的 fixture 复合断言失败，尚未进入 Mount 业务。原日志只有错误为 nil，不能逐条件还原当时值；源码存在与正式首次创建合同相反的 Material 空值期望。原工具 1608 / supervisor 445496 actual 1，七资源、后代、私有目录、runtime 与 TCP 差额双尾完整关闭，输入不变。原 FAIL 与二进制、日志保留。
- `c189c9e7` 只改该 PG fixture：分别要求正常 Execute、合法 receipt、Offline、首次 Material 非空、Token.Valid 和合法 expiry；诊断仅固定分类，不输出材料。`compile02` 真实生成新 race binary，1736 编译输入前后相等，较首编译仅此文件变化；精确 list 仅一个目标 top。原工具 79340 actual 0。
- `native02` 使用该新 binary、1737 运行输入，精确 1 top / 2 sub PASS 23.26 秒。普通非 Admin Owner 通过真实 TLS / Account / PG / MinIO 组装完成候选 GET/HEAD、255 字节目录声明创建、完整当前配置及 bindings 读取、no-op、改目录、清空、重新加入稳定映射、原 key / 原输入历史 Lookup 和权限拒绝。真实 final 事务正常回滚后，Agent / Mount / 双向引用 / typed Audit / Outbox 均未留下 final 事实，原 planned mapping 保留，同 key 后继成功复用身份；同 Tx 删除反向引用的读完整性反控通过原 Store 回滚恢复，未 SQL 假造 Mount 或 Runner。
- 第二轮原 Go 457153 与 driver 455301 actual Wait 0，七资源共 14 次 absence、后代 / private / runtime 双尾、75 秒原自动 baseline TCP 差额双空与输入不变均齐；原工具 58727 亲收 actual 0，supervisor 455296 actual 0 / 120.781 秒。完整原日志 `/tmp/arm02/pg-97e3e35204424ea88b886f0d2712f96b.log`，配方和终态保存在 `native02/`。没有改门、过滤连接或自动重试。

真实 PG 使用原 P2 / Account / Object / ProcessGuard 领域组装，Project setup 仍消费原测试 Skills receipt；不证明生产 Project 初始化。此结果也不包含默认 App / 浏览器、Runner 在线执行、物理目录创建、Execution Resolve / 非空 snapshot / dispatch 或真实网络 Unknown。默认 App 浏览器后继使用独立工作树和输入，不复写本轮历史。全部原进程、helper、资源及缓存 writer 已实际退出，本线无后台运行。
