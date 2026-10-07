# production01 六生产源：STATIC HOLD

冻结输入：manifest `0235de8044114239a752bbf81015fd5afb2e55df244401d87ed517c70670c998`，继承 lookup01 与接受的 `a0b012ce` 根。六快照 hash 全匹配；受审仅 #5/#6/#11/#13/#14/#16，未读活动测试、schema、集成源。唯一阻断 **CRD-PROD-01** 已由 root 采纳，详见 `finding01.json`；这不是编译或运行失败。

`executeMutation` 将 Metadata `err=nil` 的损坏 ref/错scope/无效 version 转成 unavailable，却继续晚 lookup；若该 lookup 命中，仍可 Execute 并返回成功。rev2 §2要求这种损坏正式结果直接 DependencyUnavailable/零候选。最小修复是立即拒绝该分支；Metadata 实际调用 error、合法其它 Purpose、合法 version 冲突仍保原有限 late-receipt 处理。反例预期 firstLookup1/Metadata1/lateLookup0/Execute0；不得改原库或禁止合法历史重放。

其余静态路径未见新增阻断：

- Create 直接唯一 Execute；Update/Delete 首次 observed 仅跳过当前 Metadata，不返回 observation 充当 mutation 成功。所有继续写分支使用原 Actor/Project scope/ref/expected/key/material，最终最多一次 Execute；原库同 Tx Mutate-before-receipt 和完整材料语义摘要未改。Unknown/error 不进入自动确认或重试，候选不发布。
- strict decode 复用固定 DecodeJSON 与安全 `httpCredentialValue`。Create 缺 value、null、空值和多字段拒绝；Update/Delete expected 原始 RawMessage 经 canonical Version 校验，1..MaxInt64-1；lookup Create 显式 ref/expected null也拒绝，Update/Delete 两者必需且同 Project。POST/PUT raw400KiB、DELETE/lookup1KiB，decoded value1..65536bytes，GET/HEAD 拒实际非空 body/query。读取不要求 Idempotency-Key（规格并非要求拒绝该 header）。
- Metadata/Mutation/Observation 逐 scope/ref/purpose/version/deleted 与请求动作绑定，坏 union/target 等不进入成功编码；absence 是 `false/null`。完整 json.Marshal 后检查≤1KiB及 ctx，再由统一发布。HEAD经过正式读取/校验/完整编码，成功 Content-Length与GET一致且无body；错误投影沿Account。合法非Model Metadata为NotFound。
- 400KiB常量及上限选择已静态核，**尚未实际编码最大材料或最大安全输出**，不能把算术当动态容量证据。该项按准备计划由作者 wire/HTTP 实测，独立核原件并以真实 root 同body schema补验。
- 材料 DTO 使用原安全 Format/JSON/slog type；严格decode全部成功后才分配 owned材料；临时 byte clear，所有 execute 返回/panic通过 defer Destroy。现有 Go string、decoder与runtime副本不宣称可靠擦除。没有材料/raw请求日志、材料返回或额外缓存路径。真实日志负证据和trace采集安全尚待作者/独立验收。
- capability adapter仅给ResponseController，业务writer为原 tracked writer 外透明 `meetingSummaryAbortWriter`（固定旧helper带Unwrap及短写/错误abort）。因此Account/DecodeJSON/Problem/业务写的stateOf链可达，不通过无Unwrap controller adapter发布。64层解析逐能力首接收者、FlushError优先Flusher，不做提前I/O；finish defer在prepare前已建立。
- deadline safe-call分别捕获每次read/write setter panic，不格式化panic值，前者失败仍执行后者。Body.Close调用前置closed、局部recover、至多一次。AfterFunc callback有done defer，finish通过stop成功或done实际等待后才消费callbackErr；abort/Close错误也不跳过join，正常reset在callback终局后且两setter都实际调用。Flush/Write/panic从外层defer完成同一收尾后统一ErrAbortHandler。并发/阻塞行为须受控及native实测，不以代码存在声称actualjoin。
- 根对同一个db依次取得Secret Store、构造真实Secret ProjectAuditAuthority、唯一ProjectAuthority、原Audit Service、带该Authority的Secret delegate。Model Authority.Projects及已接受Model read/Owner/Usage/System/Summary路由保持，新credentials仅精确分派六operations。无额外Authority、后置locator、Initialize、Secret Stop/Drain、Outbox event或迁移；原Secret→Models→Summary→Usage和根资源关闭逻辑未变。真正同Store witness/Txn拒绝与在途shutdown仍需真实场景，不以构造无I/O替代。

目前未运行 Go/Node/schema/native/PG。lookup阶段既有限定STATIC直接复用；作者编译结果不替代这里的独立动态结论。原production01保留，等待root协调最小修复的新freeze后仅差量复核；整卡及README23仍未验收。
