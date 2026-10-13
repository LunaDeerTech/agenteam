# Task Execution 的最终文本快照

本片在已有 Task Execution 历史读取上接入真实 schema1 DirectText producer 的最终文本。它不是 token stream，读取不启动新的模型调用、不产生 Artifact，也不把 Execution 成功解释为 Task 完成。真实默认 App、同数据库重启读取及浏览器展示验收已通过；模型响应来自任务自有的受控 HTTPS 服务。

## 正式接口

GET/HEAD `/api/v1/projects/{project_id}/tasks/{task_id}/executions/{execution_id}/runtime-view`，无 query、body、Range/If-Range 或 Content-Encoding。沿原 Account/current Human Owner、固定 Project/Task/Execution 与 Work 同 Store/live Tx 锁检查；归档按 Read 门，任何既往响应或已知 ID 均不代替当前授权。完整 wire 见 [Execution OpenAPI](../../../api/openapi/execution.json)。

响应为 `{execution, availability, snapshot}`。`execution` 是原安全 TaskExecution 表示，与快照由同一 SQL statement 读取。`availability` 只有三种：

- `pending`：当前 Execution 未终态，`snapshot=null`；它不是活动 Runtime View，也不宣称展示全部执行过程。
- `not_recorded`：Execution 已终态但没有原 producer 快照，`snapshot=null`。包括旧历史及未进入 DirectText 的准备失败；不伪造 generation 或在 GET 中补写。
- `available`：存在经完整绑定校验的原快照，Execution 必须已终态。成功恰有一个 text Item（content 可以为空）；failed/cancelled 恰有空 Item 数组。空数组也必须来自真实 producer 行，不能靠状态猜测。

`snapshot` 固定 `schema=durable_final_text_v1`、`generation=execution.snapshot_id`、`included_update_progress="1"`、`execution_version=execution.version`、完整 `items` 数组。每个 Item 含原 RoundID 的稳定 `id`、`execution_id`、独立 `seq="1"`、`type="text"`、`status="completed"`、`revision="1"`、原开始/完成时间、`source_reference={kind:"execution_round",id:RoundID}` 及 `data={content}`。时间沿 foundation.Instant 六位微秒 UTC。Item.updated_at 与 completed_at 都等于 canonical completed_at。

仅拼接原完整成功 ModelResponse 的显式 TextParts，不插入分隔符，不包含 prompt、私有 reasoning、Provider metadata、凭据、Usage 或原错误信息。保留文本原值；前端只作纯文本渲染，不执行 HTML。原完整响应 JSON 16 MiB 上限继续生效，文本 UTF-8 最多 16 MiB，完整 HTTP 表示最多 16 MiB + 64 KiB；超限为错误，不静默截断。单次完整快照没有分页或游标。失败、损坏、不匹配和权限拒绝仍为原 Problem，不转换成空结果。

## Producer、恢复及所有权

`saveDirectTextTerminal` 所在原事务先核原 session 实际 Joined，再同时提交 Round 终态、assistant Transcript、Execution 状态及不可变 `runtime_snapshots` 行。没有第二 Store、延后 worker 或公开 mint 接口；数据库门要求原 E/Round 同事务终态和完整公开投影逐字段吻合。失败/取消仅保存原安全终态，不写部分响应。

原 terminal COMMIT Unknown 保留原 run、attempt/cause 与候选，显式恢复须确认完整快照及原退休事实，不能凭一个可见 terminal 行提前结束原调用。独立 GET 只报告其自身原读取已确认的持久事实，不宣称另一调用或进程已经 Joined。重启读取相同 generation、progress、Item identity/revision 与正文；历史缺失不冒充完整快照。

读取复用同一 TaskExecutionReader 与 service lane 的 `get_task_execution_runtime_view` identity，enabled 与四服务共用原初始 pair，disabled 仍只有历史 reader。新 GET 沿原 2 秒入口与实际 Store、技术登记/关闭和 HTTP 返回尾；取消不替代真实返回。新表加入原 Execution Project retirement 的完整前像、逐表 exact delete 和 Unknown 确认，先删快照再删其 Round/Snapshot 父事实，沿 migration 84 原门。

前端在真实 Task Execution 详情中显式加载/刷新最终输出；上下文或权限改变时清理观察，不自动轮询。成功返回的 execution 与正文整体采用，读取失败不触发执行或修改 Task Unknown。当前持久代际不是 WebSocket epoch；[Runtime View §17–21](../../architecture/agent-executor/runtime-view.md) 的活动流、flush 覆盖、subscribe-first 和 resync 仍待后继真实 producer/transport 实现。ToolsHTTP 与原其它 STOP 保持。

## 必要验证范围

定向验证已通过 11 个顶层测试、41 个子测试及 3 个包的 vet，覆盖完整公开 TextParts、空文本与缺记录区别、终态候选构造与 Unknown 完整候选确认、当前权限/P/Task/E、原读 Store 返回尾及 exact retirement。构造接线修复后增量 App vet 也已通过，未重复未变的前端或领域检查。

`TestRuntimeFinalTextDefaultApp/original-producer-and-restarted-history` 在 `runtime-app-native03` 整轮通过。原默认 App 经 Coordinator/Handoff、一次 Associated.Advance、Preparation/DirectText 和真实 Model adapter，完成一次受控 HTTPS 请求与完整 JSON 响应。真实 Secret、当前出站 policy、CA 与私有地址解析参与调用；未 SQL 种植 Execution/Runtime 行。原 Execution、Round、assistant Transcript 与最终快照在同一终态事务保存，完整文本及绑定一致；Owner GET/HEAD、错 P/Task 和非 Owner 拒绝通过。原进程与 Guard 全部退出后，同数据库及密钥重启 Runtime disabled，读取的 generation/progress/正文与原响应字节保持一致。Model 调用为 1、Tool 操作为 0，Task 状态与版本保持非终态。

真实 `TestRuntimeFinalTextWeb/final-text-load-refresh-and-safe-render` 在 `runtime-web-native01` 整轮通过：页面 List、独立 Execution Get 及两次最终输出 Get 共 4 个目标 GET，页面阶段零写；连同辅助读取共 22 个原响应，reader/consumer、完整字节与 handler 返回均对齐。显式加载、刷新、HTML 字面纯文本及窄屏键盘滚动通过，视觉范围见[前端结果](d22-task-execution-output-ui.md)。原生和浏览器的原 Wait、Node、CDP 及所有任务自有资源均已退出，结果留在 `output/ai/execution-runtime-view`。

`runtime-app-native01`、`02` 原失败保留：首轮仅观测到完成条件超时，第二轮确认 Model 为 `network/not_sent`、受控服务零连接，原 producer 正确保存 failed 快照。夹具先前只替换普通出站 client，而 Model adapter 自行构造客户端；最窄修复将受控 resolver 接到原 Model 构造输入，并由现有配置加载 CA，默认 nil resolver 仍走系统解析。未放宽成功门、增加 Model 重试或改动宿主 DNS。

以上验证受控 HTTP 协议与真实系统装配，不证明公共模型网络可用，也未调用环境中的外部模型地址、密钥或模型 ID。ToolsHTTP、活动 Runtime 流及完整 Agent/Task 工作闭环仍未验收，既有 STOP 保持。
