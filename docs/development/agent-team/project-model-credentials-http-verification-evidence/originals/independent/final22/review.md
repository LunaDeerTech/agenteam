# Project Owner 模型凭据 HTTP：22 技术路径独立验收

**限定完整技术 PASS。** 有效输入为作者 candidate03 `a362e55ac9007d0a39dcbe885b7c73a46a05c1e6dc05ad7ad56ed20b022fed45`；22 路径中 21 项实际修改，既有 `project_usage_test.go` 原字节。冻结快照和仓库实际 22 项 hash 均匹配，精确名单在 [sources.json](sources.json)。README #23 另行窄审，不包含在本结论。

接受组合为 production02、unit02 和 candidate03 的受影响修订：完整 STATIC；独立 controlled 普通/race 各 1 top/18 nested；作者普通/race/vet、自然 30/2 秒预算和 schema；作者真实 native 3 top/13 sub/14 listener；作者真实 PG 新 5 top、旧 14 top；独立 A02、B01 及本轮四份原始安全响应的标准 schema 校验。原始命令、输入和结果索引见 [result.json](result.json)，没有把 compile/list 当作真实运行。

独立 A02 通过当前 Session、归档 Read 与 Mutate 差别、原查证事务三把 SH 锁、Command EX 阻塞不误报缺失、正式 Logout 后拒绝，以及真实 Audit 使用错误活跃 Tx/合法但不匹配 Resource 的拒绝和完整 canonical/receipt/Audit 回滚。A02 实际 50.189 秒。原 A01 因私有探针继承原事务 context 再开事务，触发既有 TransactionNested poison，实际得到 500/InternalError/not_committed；该失败和完整收尾保留。只修私有第二事务的独立两秒有界 context 与断言顺序，原 503、真实拒绝与回滚条件均保留。

独立 B01 实际 54.392 秒通过默认 app.Run、65536 字节材料原 key/body 重放、最后一字节变化冲突、HEAD 表示长度及安全查证。唯一目标后端 134 实际先完成原 COMMIT 并进入 idle，再丢失 ACK，使原 HTTP 返回 Unknown；唯一 Execute、零隐式确认、后续单独公开 lookup 和事务事实均有断言。该时序独立于作者的 held-pending→放行后终态，不使用结果装饰替代物理终态。

B01 的 create、metadata、lookup、terminal-lookup 原字节分别为 104、88、131、131 字节。四份 body 与实际 method/path/status/Content-Type/Content-Length、候选、run、schema 和来源 hash 一同冻结；标准 Draft202012Validator/FormatChecker 直接解析同一原字节全部通过，未 parse/stringify 后冒原响应。schema 命令实际 exit0/0.103 秒、actual wait、双自有进程空、输入一致。

原失败均保留：malformed nil-error Metadata 被 late receipt 掩盖的生产 STATIC 缺陷由 production02 修复；callback 失败清理缺实际等待由 unit02 修复；作者两次编译失败保留；作者 new2 三个 final 检查点失败由 candidate03 测试 SQL 独立 UUID/text 参数及安全诊断修复，真实重跑通过。原 new2 未记录 SQLSTATE，静态归因不改写为原轮动态观测。早期 plan-hash/闭包前置失败也保持原作用域，未冒作真实 runtime。

作者六个 PG 实轮（含原 new2 FAIL）与独立 A01/A02/B01 共九轮全部有 actual wait、七个精确资源两次消失、owned/runtime 两次为空及前后输入一致。三轮 native direct 3/adopted 3 均实际 wait，全部 TCP（含 TIME_WAIT）与 owned 进程双清。九轮 daemon/PID1 shim 实际 PID/starttime 集合从 248 增至 284，精确新增 36；它们非自有、未 task-wait，不操作 PID1，不声称全机清零。逐轮集合与原件见 [pg-rounds-and-daemons.json](pg-rounds-and-daemons.json)。

材料的安全投影、SecretMaterial destroy 和 byte clearing 不代表 Go 字符串内存擦除。默认根正常/耗尽关闭沿作者真实证据；forced return 不等于所有内部组件都已 join，代理/进程/资源终局另列。正式账户身份链成立，Project.Create 仍使用已接受的持久化测试 Skills fixture，不代表新增生产初始化。生产 Resolution/Invocations、D24 及完整 D08/D09/E01 不在此接受范围。

全部独立命令和资源窗口已结束，本技术报告冻结停止写入；后续仅审 README #23。
