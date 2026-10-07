# Cursor 过期措辞定点核对（仅拟差量）

结论：draft01 §3 的“过期 token”不准确，可能承诺不存在的时间 TTL。应改为 token 的签名 kid 已不在当前加载 keyring 中而不可验；仅切换 current_kid、仍保留旧 kid/key 不使旧 token 失效。本次不改冻结 draft01，不新增产品/公共契约或依赖。

固定来源均与已接受 Owner-read runtime closure 的对应指纹一致：

- `internal/central/cursor/cursor.go:57–77`：header 仅 signature_version/kid，payload 为 format/scope/query_digest/order/position/可选order_generation，无 issued_at/expiry/TTL。`:132–140` 以 current_kid 签发；`:147–200` 验证没有时钟/时间期限，`:169–177` 用 token kid 从当前加载 keyring 找 key 并验 HMAC，缺 kid 或签名不符为 CursorInvalid。
- `internal/central/cursor/keyring.go:15–19,28–60`：keyring 是固定捕获的 current + values，加载配置没有过期字段；旧 key 是否保留由实际加载的 keys 决定，不声称在线热轮换。
- `internal/central/model/project_query.go:43–60`：绑定稳定 User/Project/query kind 与固定过滤语义，不含时间有效期。
- `internal/central/model/query.go:215–251`：Model 验签后检查四 position scalar、无 generation、水位/after 的相对排序；instant 是分页水位/位置，不是 token 到期时刻。

拟差量只替换 §3 上述两句，精确文本见 proposed.patch。每页当前授权与 cursor 绑定仍必须通过；keyring 可验不等于有权。原 8 MiB/Unknown 等其余内容不变。

本次只有静态读取、SHA比对、生成未应用文字差量；没有执行 Go/test、Git、监听或资源。冻结 draft01 SHA仍为 9c0062bee024794f85b00f3581aa3782e4f0040ada3110989313c591b563fafe。
