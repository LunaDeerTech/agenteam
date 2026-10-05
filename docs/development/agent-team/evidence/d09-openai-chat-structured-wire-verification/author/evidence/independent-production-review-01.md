D09 structured wire production-review-01 独立静审

结论：BLOCKED。已核全部 4 份冻结生产源码及相对 ac5 差量；F1 为新验证实现违反固定成本/取消契约的明确阻断，F2 为本卡继承的旧消费接缝尚未满足本卡明确成功终态要求。均为静态源码结论，未运行复现或测量分配/耗时。

固定输入：author manifest 50960486732000e7ad000db5f3b961ee861a11a21361b698639ea5e01b82bf68；source.patch f57bdf3324ec2423d3790c1e6fc9501dafb0d5da276e640f93193aa89e8f6f35；基线 ac5b4c65e88ed0ec1813fbf341035c88ca9a38c7；规格 b6297143 中卡 SHA 747c879b4d930de949570a72ad6f3418bac3870f1ea23d639e19ce8a456799a0。manifest.json/input-check.json 绑定 4 源原字节及 3 旧文件基线；副本在 sources/，不消费作者活动测试。

F1：输出 Decoder.Token 整 token 复制/扫描，未满足单累计 buffer 和长扫描取消。

定位 structured_schema.go:307–309、328–359、365–366；SSE 接入 sse.go:77–79、100–102。validator 在每个 value 入口检查 ctx，但随后把原完整 text 交给 json.Decoder(strings.NewReader(text))。合法且接近内容上限的单个 string，例如严格 object 中唯一 string 属性，会由 Decoder.Token 返回一个与输出内容同阶的完整 string；Decoder 自身也需要扩容保存整个 scalar。超长 number 则在完整读取、转换成 json.Number 后才执行 128 字节检查。Go1.27.1 标准库 stream.go:64–75 明确先 read whole value；103–182 的扫描与扩容不查调用 ctx；448–471 的 Token 走 Decode，decode.go:1130 起构造 scalar 结果。相应固定文件 SHA/原行摘录见 dependency-evidence/index.json。

structuredUnicode 的 4096 步检查发生在 Token 之前，不能取消后续同样长的 Decoder 扫描；其开头 utf8.ValidString 也是一次无 ctx 的整串扫描。字符串总量仍有 16 MiB 上限，故这里不称无限内存/无限循环；问题是卡 §3 已要求直接遍历累计内容、不构造第二份整体同阶内容，以及较长扫描内检查原 ctx，而当前代码不具备这些性质。仅 StringBuilder.String() 无复制或验证末尾查 ctx，不能证明此契约。

最小建议：在现有私有 structured_schema.go 内使用受节点/深度/token/原 ctx 约束的内容词法遍历；长字符串可验证而不整体复制，key/enum 只作既定有界比较，number 在扫描中执行 token 限额，Unicode 检查也应在有界步长内可取消。不改规格、公共接口、D04、Budget 或输出 16 MiB 上限。作者先固定窄修方案和差量，保持 review01 原件。

F2：消费端 actual join 成功会掩盖已发生的取消，仍可能交 Result/StreamEnd。

定位 transport.go:236–250、283–295、362–400；waitJoined 的 170–175、176–183。此部分与 ac5 原文相同，不能说由本次 diff 引入，也未动态证明旧 text 失败；但新卡 §3/§5/验收 113 行明确要求最终校验/取消不发布迟到成功。当前 consumerJoin 即使在 243 行看到 caller ctx 已取消，也只 cancel 派生 wait；waitJoined 在 joined=true 时直接 nil，正式 D04 Client.Drain 在实际集合为空时也先返回 nil（固定 ac5 client.go:156–172）。consumerJoin 只有 err!=nil 才返回 caller 的取消；Result 与 Next 在其 nil 后没有再检查 caller/attempt ctx。

可达顺序是：Result/Next 最后一次 ctx 检查通过，随后 caller 或 attempt 取消，再进入/完成 join；actual join 为真返回 nil，消费端直接给成功。这里不否认 work 已真实 join，问题仅是取消后仍发布成功。最小建议在成功消费出口或 consumerJoin 成功返回前无条件复核 caller 与 attempt ctx，保持 Joined()/waitJoined 的实际终局探测语义；无需修改 D04。用确定性的纯取消/已 join 反例验证成功出口即可，不需新网络方案。

其余有界审查结果：C0 Capabilities.Validate（types.go:286–288）已限制 modes 为 text/json_schema 并去重，ResponseFormat.Validate（chat.go:244–251）已限制格式，prepareWithSchema:138 先执行，未发现这些闭集遗漏。旧 text 分派和编码形状保持，新修订 text 的 schema 指针为 nil；schema raw 私有复制、required/AP、嵌套成员、nullable/enum、整数饱和指数判定、SSE 在 DONE 才验证及合法 usage 先观察等路径未发现另一确定语义阻断。此静态判断不替代完整分支/成本/旧 text 回归，也不把当前测试作者自报通过作为独立证据。

后续最小验证建议：先对 F1 大 scalar/超长 number 的扫描成本与过程中取消、F2 已取消但 join 成功出口补纯反例；源修复冻结后只核差量。作者按原卡完成 3 新真实组及 3 旧 text 组，独立真实增量仍按既定两类选择：合法 native 中 schema mismatch + usage/PartialOutput/无成功终态，以及原受控 hold/背压取消 actual join/slot/material。不要为本静审新增协议/网络故障方法或重跑无关全树。

本轮仅固定源码/标准库文本与私有证据写入；未运行 Go/Docker/npm/browser/网络，无仓库、业务或 Git 写。Object/Artifact 暂停问题不触碰；无 Resolver/生产账号/consumer/完整 D09 验收声明。审查已冻结，all-stop。
