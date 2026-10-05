D09 structured wire rev1 独立规格静审（冻结）

结论：PASS，建议主线程采纳此规格并另行授权 10 路径实施。未发现确定的接口、范围或可执行验收阻断。本结论仅覆盖规格和固定源码的可行性，不是新 revision 实现或运行验收通过。

固定输入：候选卡 SHA256 103ef5b0d3e7451510d55d4bbf99986db59639ac309ab47ba9b9a8f2189f6eff；代码 ac5b4c65e88ed0ec1813fbf341035c88ca9a38c7；设计输入清单 SHA256 e2b32523758f7058c6fc8f26fd92b2ae783e84326e2cdb500397daede9f79d38。13 份 Git 基线文件逐字节/长度/SHA 与清单一致。两份 SDK 副本另经原本地 objects.git 的 becc1d20eed83c1b8d85e15dc131a372d9dc7813:path 读取，commit 内路径、Git blob、SHA256 与全部原字节一致；无网络或 SDK 导入执行。检查原件为 input-check.json、sdk-object-check.json。

- 范围与依赖成立。C0 chat.go:238 的 ResponseFormat 与既有 Snapshot.AdapterRevision 足够；memory 的真实 capability 要求见 references.go:157，旧 Start/prepare 对 text revision 的拒绝点见 openai_chat.go:108 起。新常量加 3 旧生产接缝及 1 私有编译器可实现；errors.go 的原 native 解码、安全错误和预算文件可保持只读。卡 13–33、75–90 行未要求改变 C0、D04、迁移、依赖或 Resolver/Secret/consumer。旧 prepare/stream 测试接缝可以通过私有 helper 保持。
- 修订分派明确。旧 text-v1 仍拒绝 json_schema；新修订在包含 json_schema 能力时支持 text 与 schema 两条明确路径，text 不编码 response_format、不编译/累计结构化输出、不改旧 finish 语义。编码和验证使用同一私有不可变输入，准入之前完成校验，拒绝不会消耗 slot 或 Do。没有每次请求改写冻结 snapshot 的设计。
- Schema 和成本约束闭合。卡 39–55 行要求全部成员/嵌套节点验证，严格 object 的 required/AP 不被补写，未知关键字拒绝；nullable/enum/type 联合生效。固定 schema/输出节点、深度、字节和数字 token 上限，精确十进制整数判定不经 float64/int64，不按指数扩大计算。SSE 仅 schema 分支保留一个有界累计 buffer，可在原 parser 内直接遍历，未要求通用 DOM 或外部 validator。SDK 只作为字段及 strict 转换源码事实；本地闭集并未冒称真实供应商逐模型支持。
- 完成和安全边界明确。卡 59–65 行要求普通响应完整 object + stop + 全 schema 成功；SSE 仅合法 DONE、完整累计验证及实际 join 后可给 StreamEnd。length/filter/refusal、坏 JSON、schema 不匹配、成本超限均无成功终态；前缀交付事实、usage、安全错误与无重试要求保留。UnknownUsage/出站观察不是数据库 Unknown，卡没有混用提交语义。
- 所有权接缝可用。固定 transport.go:62 的 perform、waitJoined/consumerJoin 和 sse.go:72 的 stream 足以把验证纳入原 owner；具体 Budget 不变即能混用两 revision。卡 53、69–71、113 行已要求验证中取消、成功发布前取消检查、Body/Client/worker 真实终局和不提前退休 slot/材料。实现审查必须确认这些实际分支，不能把 schema 判定成功或 ioDone 单独当作 complete join；这属于现有验收义务，不需扩卡设计。
- 真实验收可执行。既有测试 fixture 使用正式 Account/Audit/PG policy 与 D04 Transport，WireScenario 的分块/hold/disconnect/release/请求及连接状态观察可覆盖所列 3 个新顶层和 3 个原 text 顶层。server 512 KiB 上限与 16 MiB parser 极限分开：超出 fixture 能力的成本反例明确交纯测试。原 driver/race/count1/6m、独立实际响应不匹配与取消 join、资源双清零要求均保留；没有要求新网络故障代理或已暂停探针。

仍待实施和动态证明：10 源冻结后的实际编码/解析、精确边界、取消及 mixed Budget 行为，全部规定纯/真实组与独立反例。本次未写或运行 probe，未执行 Go/Docker/npm/browser/网络，未改仓库或 Git。无生产 Provider 账号能力证明；不交付 Resolver/Memory canonical schema/消费者授权、Invocation/Usage、root 或完整 D09；Object/Artifact 已知共享 guard 阻塞保持原状。

本轮仅自有 tmp 文件写入；静审已结束，all-stop。
