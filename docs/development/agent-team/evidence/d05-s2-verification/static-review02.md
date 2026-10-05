# S2 review02 固定修复 delta 静审

2026-10-05。基于原 b81c98af... manifest 的四生产文件修复；四项 before SHA 与独立 review01 副本相同，after SHA 与作者只读 review02 副本相同，见 checks.json。仅静审，未读活动修复、未运行 Go/Docker、未改仓库。

原 SR-1 trace 在修复上闭合：preparation.writers 与 discarding 均在 service mutex 保护下；UploadPrepared 的实际 writer 入场和 prepared map identity 检查串行于 DiscardPrepared。引用的 defer 在 verify、finishWriter 及其调用返回后才减计数；LIFO defer 保证已取消 operation 的 Discard 重试发生于引用归零之后。Discard 不再凭 spool.busy=false 结束 operation，也不能在其开始删除后放入新 writer。

原 SR-2 trace 在修复上闭合：Source 已 open 和未 open 两个 release 路径都向 finishProjectWorkContext 传原 cleanup context；原 release 已取消或预算耗尽时只记 ended，保留待 checkpoint 身份，不再申请新的活 Force 预算。cleanup 计数仍覆盖实际 release 和其后 join 尝试。未放宽 Force 预算或旧测试断言。

直接读取的作者原红 author-writer-red-01.log 记录 archive/delete/discard 三个 standalone writer 子例失败；archive/delete 观测 premature stopped，三个均观测 live standalone writer falsely checkpointed as joined，objects 包 5.109s。故原 SR-1 已有作者动态原红支撑；不是独立复跑结论。旧 SR-2 原红仍在 review01 的 author-compat-core-01.log。未读活动测试源码作正式验收，修后真实组及最终 28 文件尚待冻结。

本次 delta 未形成新的确定硬阻断。两项静态闭合不等于 S2 通过；后续需固定完整输入、独立真 fixture 和受影响旧回归，包括连续并发 Discard/UploadPrepared、真实 verify/回写尾部、Force/Source 的共享预算及原 Unknown writer。
