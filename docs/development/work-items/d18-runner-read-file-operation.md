# D18/D22 单 read-file Operation

本片把真实 Execution ToolCall 接到一次 Runner read-file Operation/Attempt，并持久保存可重读的完整结果。两组基础检查与compile01至04及精确list通过；native04已实际wholePASS，有限范围为显式装配的单次read-file持久结果、原Lookup恢复与当前撤权零发送。runtime_recovery已完成有限whole独验接受，主线整合由root收口；native01至03和codec原红全部保留。初始基线为 main `1b10c29c`；ToolFacts 先以已提交开发依赖接入，随后通过 main `95c092bc` 正式有限交付，原 native01/02 FAIL 与修后03通过事实均保持，不替代本片验证。

正式依据：[Model/Tool 契约](d01-contracts/model-tool.md)、[Execution 契约](d01-contracts/execution-orchestration.md)、[Runner read-file](d15-runner-read-file-adapter.md)。本片止于原 Operation 的持久结果，不包含下一轮 Model、Transcript 注入或自动重试。

## 兼容存储

- 新迁移 `00065_tool_read_file_operations.sql` 增加闭集 `operation_kind`。旧行为默认 `skill.install`，保留其非空 SkillID、原 backend key、input/terminal 编码、摘要及 32KiB metadata 上限；不修改 00037。新 `runner.read-file` 明确没有 SkillID/backend key，不伪造 install 目标或后端幂等承诺。
- read-file 的独立 private input codec 只保存原 ToolCall 身份、protected input 引用及摘要、实际目标的 Mount/Workspace/Runner 和配置版本；不另存模型原始参数或文件路径。指纹使用固定 operation/revision/protocol 与逻辑目标，当前配置版本、连接、credential、request/attempt 身份不改变逻辑指纹。
- 成功 payload 单独存入 `read_file_results`，绑定原 Operation/Attempt/Execution/Project/Agent，验证原 body SHA、长度、严格协议编码及原读参数。上限沿已有 `MaxReadFileResultBytes=512KiB`，完整合法 65536B 文件结果即使 JSON 转义也不截断。原 terminal/attempt metadata 仍不承载大 body。
- payload 不可修改；延迟约束要求 success、原 succeeded+retired Attempt 与唯一 payload 在同一事务内齐全，其余状态不得出现 payload。约束不证明实际 join，真实 owner 必须在原 call 退出后才写退休。没有新增公开 HTTP 读口、默认 JSON 输出或日志材料。

## 已对齐的真实接缝

1. Tool 消费 `ReadFileExecutionAuthority.DiscoverReadFileCall/WithReadFileCallInTx`。plan 是实际 Execution producer 的私有身份；`Binding()` 仅给安全值副本以发现稳定 Operation，不授予权限。`With` 在 same Store、原 Tx、完整已持锁下重验原 ToolCall/保护参数/current Execution/Agent/取消，私有 witness 在同步回调内有效，借用参数副本在退出时清零。Tool 只可保留已校验 typed intent，不保留借用 raw bytes。
2. 完整锁集包括 Registry SH、Project SH、ProjectSchedule EX、Agent SH、ToolSpec SH、Execution EX，以及 Tool 的 Operation/适用 Command/记录锁，入 Tx 前统一排序。正确调用方向为原 Mount Resolve/Claim Tx → Tool authority → Execution live callback → Mount 传入的原 callback；不能从 Execution callback 再开 Resolve/Claim 嵌套事务。
3. Runner `Prepare(ticket)` 只生成固定 RequestID，无 Claim、队列或后台工作；原 `ReadFileCall.Dispatch` 单消费并使用该同一 RID。Tool 只在自己原 Store 的 Attempt 事务已知提交且实际返回之后调用 Dispatch，数据库 `backend_request_id` 必须等于实际 wire RID。不能用调用方自报 Committed、公开 binding 或 call 存在代替授权。旧 Dispatch、Directory 与原 frame/connection 退休尾保持。
4. Attempt 提交 Unknown 时零发送，保留原 owner/claim，不换 RID、不重新 Prepare、不自动发送。已知提交后 Claim/队列失败还须按实际是否发送区别失败与 Unknown；回包后补写 RID 不满足本片。

## 写域与当前缺口

Tool lead `skill_package_review` 负责新 contract、private runtime、迁移65及本卡/current。Runner `6f87bfff`、Execution callback `4b73635e`、Mount 安全 metadata 投影 `0eeeea0e` 已保存并有限静审，三域19top race与适用vet已实际通过，真实666项依赖前后相同且不含Tool runtime；`runtime_recovery` 为独立 principal，root 负责 Git 与资源窗口。迁移65中曾误用 uuid 局部变量的静态缺陷已在 `86c298c9` 改为实际列 `%TYPE`，不称已 PG 复现。

显式装配顺序为 `NewReadFileAuthority` → 原 Mount resolver → 原 Runner dispatcher → `NewReadFileExecutor`。`ExecuteReadFile(ctx, ExecutionID)` 是唯一执行入口，不接 caller raw/body/target/RID；在原 Execution callback 内经真实 `CurrentTargetInTx` 读取六字段安全目标并同 Tx 写完整 created，实际已知提交返回后才 Resolve。ticket.Target 只作原目标精确比对，不替代 Claim。原 Attempt Tx 中写 prepared call 的同一 RID，实际已知提交并返回之后才 Dispatch。原 frame 与 terminal/connection 实际退出之后，一次有界最终 Tx 保存原终态、小 metadata 引用、完整 payload 及 retired。

`LookupReadFile` 只经当前 Execution 权限读取原 Operation/Attempt/历史 payload，不重新读当前目标替换历史、不发送或创建新 RID。提交 Unknown 保留原 owner、实际返回事件与有界结果候选；只有精确终态观测且原调用已返回才可解除最终提交的不确定性。显式 `ResolveReadFile` 只为这个仍持有的 owner 重查/保存原事实，绝不调用 Dispatcher；Attempt 提交 Unknown 且当时零派发，只能在查见同一原 Attempt 后记已知未发送，缺行不能证明 rollback。Stop 仅取消原调用；Drain/Joined 不把 Cancel、公开结果或未知提交当退休。首次构造均显式，默认生产装配未改。

独审指出的两处静态缺陷已窄修并通过适用基础控制：最终候选只由实际 instance schema mismatch 归一化，metadata/锁/取消错误不改原成功；NotCommitted保原候选，Unknown保本轮确证写意图。Lookup独立登记原调用，Stop禁止新查询，Drain等其原Store调用实际返回；Execute内部历史重读沿已登记owner，停止后只允许显式Resolve结算原保留事实。

Model JSON 为唯一明确端口替身，Execution/Tool authority、CurrentMount、ticket、WSS 与 Runner Manager 使用真实实现。默认生产 nil、全 registry、OpenAI tools A/B 和其他原 STOP 不变。

## 实际验证与保留失败

三接口19个top的race与三包vet通过，666项输入前后相同；Tool runtime/contract的16个top、34个sub及两包vet通过，509项输入前后相同。这些分段基础结果不等于真实整链通过。`compile01` 的PG包race编译及精确list均实际0，1827项输入前后相同，唯一top为 `TestRunnerReadFileOperation`；原二进制、命令与Wait保留于 `output/ai/runner-read-file-operation/compile01/`。

固定 `2d4f07ac` 的 native01 为 **wholeFAIL**。两子场景运行27.38s：完整结果场景在进入Operation前，被复用ToolFacts夹具硬编码的canonical length64断言拒绝；当前Mount清空场景到达新业务后验后，在旧 `requireJoined` 的Operation数必须为0断言失败。后者不记为通过的子场景。Go、driver及outer均实际返回1（原工具32554，supervisor700051，140.340s）；七资源两次absence、private/runtime/descendants双尾及1828项inputs_unchanged齐，但原75s HOST_TCP尾仍有1条TIME_WAIT差额，因此退出门也未全通过。原证据保留于 `output/ai/runner-read-file-operation/native01/`，监督原日志为 `/tmp/rfo01/pg-8c62651ea7e34711ac66556710e69e16.log`；不把事后连接消失改写为原成功。

夹具修正已固定 `2eadbbde` + `872ebdb3`：旧 `facts` 仍严格固定canonical64与Operation0，新增私有 `factsFor` 仅参数化这两项，保留其余SQL、原字节/摘要、身份、事务和引用断言。新PG按各场景独立预期64/65536，首次要求Operation0；原driver实际Stop/Drain后要求Operation1和原ToolCall身份不变，并再次核exact Operation/Attempt/完整结果及实际wire后验。原COMMIT barrier、当前撤权、Lookup先于Stop的次序、两子场景和所有运行门不变。修正经独审后，`compile02` race编译及精确list均实际0，1827项输入前后相同、相对compile01仅两份夹具变更，binary `f934ff93…13b475f2` 单独保留；此前基础结果未重复。独审确认原harness STOP仅使该轮失败，不是永久阻断；native01的TIME_WAIT归属仍未明，不改原FAIL。

固定 `b952cd59` 的 native02 为 **wholeFAIL**，原工具63642亲收actual1，supervisor709384实际1/120.850s。两子场景共30.35s：`current-mount-clear-rejects-unclaimed` PASS15.80s，包含原Attempt提交返回barrier后正式clear、旧未claim票据零wire、原Operation/Attempt身份与退休失败、拒绝新Execute/Lookup及原Join后验；`result-commit-loss-and-observation` FAIL14.56s，在已到Attempt barrier后，Execute返回err=nil而原断言要求COMMIT_UNKNOWN。该失败点尚未记录安全Outcome.Status、最终phase/terminal或hook命中状态，不能推断实际成功、failure或unknown，更不能倒填COMMIT丢回执已触发。原Go711250、driver709385均实际Wait1；七资源14absence、private/runtime/descendants双尾、原HOST_TCP两次delta_empty及1828项inputs_unchanged均通过，整轮仍因业务失败返回1。原件在 `output/ai/runner-read-file-operation/{compile02,native02}/`，监督日志为 `/tmp/rfo02/pg-b0e94d0c377f47909e923553ed067107.log`；本轮TCP通过不解释或覆盖native01差额。

后续只读发现确定的codec矛盾：实际Message解码会重排嵌套JSON对象键，而 `terminalReadFile` 使用完整Payload与struct定序编码的逐字相等作为成功条件，合法协议值可能被降为 `backend_contract_violation`。此为静态产品发现，不能替代native02未保存的分支事实。针对真实Message codec的反例 `d40fc527` 已实际复现：原工具24616 actual1，`codec-repro01` 原产品未改且509项输入前后相同。窄修 `67329a12` 保留严格协议解码、原请求参数和身份校验，移除传输JSON对象键序与struct编码逐字相等的错误条件，持久结果仍使用明确编码器生成的完整原值与稳定摘要。修后原工具30556 actual0，`codec-fixed01` 2个top/7个sub race与runtime `codec-vet01` 均实际0，各509项输入前后相同；原红及命令/Wait保留在 `output/ai/runner-read-file-operation/checks/codec-*`。这证明定向codec问题已修，不补造native02的未记录Status或动态首因。

`compile03` race编译与精确list实际0，1827输入前后相同，相对02仅已审codec产品/测试两源差额。固定 `8d0781bf` 的 native03 为 **wholeFAIL**：原工具84435亲收actual1，supervisor722019实际1/121.997s；两子场景33.08s，clear PASS16.62s，happy FAIL16.46s。happy已通过原COMMIT_UNKNOWN与Attempt/final实际Committed门，随后在成功terminal元数据解析处失败；尚未抵达完整结果后验、两次Lookup及正常Stop/Drain，cleanup因仍有未查证Unknown原owner而报告未退休，此FAIL也保留，不记作正常Join通过。Go723769/driver722040实际Wait1，七资源14absence/private/runtime/descendants双尾、原HOST_TCP两delta_empty、1828inputs_unchanged均齐，整轮业务仍失败。原证据为 `output/ai/runner-read-file-operation/{compile03,native03}/` 与 `/tmp/rfo03/pg-cdbc3822c3c04958a2ed65a2d509144f.log`。

静态核实成功结果引用的 `Bytes` 正式类型为 `f.Progress`，编码为十进制JSON字符串，PG误用int64导致解析失败。`b5532d77` 仅把测试字段改为f.Progress并在后验比较时显式转换int64，保留其余全部强断言，无产品变动；该两行修正已独审接受。`compile04` 原工具35958实际0，race编译与唯一top精确list均0，1827输入前后相同、对03仅此PG文件变化，binary `49f1fdf4…8cb4dfcc` 独立保留。compile01至03及各轮原FAIL均不覆盖，旧基础未重跑。native04沿原PG完整结果/Lookup/退休强门、预算、自动baseline与七ID双尾执行，无放宽或额外场景。


## native04 有限实际通过

固定技术 `b5532d77`（候选 `4ac043e0` 仅随后文档）使用compile04 binary `49f1fdf4a536109e109d4e4051b2292bcc50cc26d7e9d75b8d9209e28cb4dfcc`，1828项输入启动前逐SHA核等、结束inputs_unchanged。原工具71277亲收actual0；supervisor730036实际0/123.600s，原监督terminal0/122.171s。唯一top两sub均PASS31.94s：完整结果15.34s、当前Mount清除16.60s。

- 完整结果：真实ToolCall/保护参数与Registry/currentMount、WSS和Manager读出完整65536B；原Attempt已实际提交但Store返回held期间零wire，释放后实际wire RID与原Attempt一致。最终原Tx确实Committed后才投影Unknown，保原Operation/Attempt身份和未退休owner；独立SQL核完整原body/摘要/引用/原终态，两次当前授权Lookup均返回同一完整原结果且wire仍为1，随后原owner及driver实际Stop/Drain/Joined。
- 当前撤权：原Attempt提交后、旧票据尚未Claim前，经真实普通Owner Agent更新显式清空Mount。旧票据随后被当前门拒绝、wire0；持久结果为同一身份的已退休未发送failure，新的Execute/Lookup拒绝且没有第二Attempt/新RID，最终实际Join。
- Go731861与driver730084实际Wait0，七个原资源各两次absence、private/runtime/descendants双尾、75s原automatic HOST_TCP baseline差额两次空、metadata exact与inputs_unchanged齐。全部原预算及门不变；窗口已释放，没有native05。

原件在 `output/ai/runner-read-file-operation/native04/` 的invocation、input-hashes、process、outer与terminal；原监督日志 `/tmp/rfo04/pg-05e7687f0dfc4714bd201ba67768c6ed.log` 保留。该通过不覆盖前三轮FAIL、不解释native01 TIME_WAIT归属、不补造native02未记录Status；也不外推Transcript结果消费、下一次Model调用、生产默认绑定或完整D18/D22/原STOP。
