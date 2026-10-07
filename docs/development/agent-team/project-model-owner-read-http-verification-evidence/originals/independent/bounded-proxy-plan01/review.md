私有代理方案：需一次窄断言修订（E1），其余限定 STATIC 通过。

DataRow 的 size 含四字节长度字段；仅服务端 D >1 MiB 且≤16 MiB 用固定32 KiB缓冲逐字转发，其余边界保持1 MiB。完整写入及实际 COMMIT+idle ACK 丢弃正确，日志不保留 payload。当前 fixture 的 Store ForceClose 后注册、先清理，配合代理关闭 listener/conns、双方向 join 与 wg.Wait，可复用已接受收尾模式；不外推任意不可达上游拨号保证。

E1：新断言仅在全局 snapshot 分别找大 D 和 idle 标记，没有要求与实际 drop 同连接、顺序一致。最小补充是在 later GET 前取唯一 drop 连接，并核该连接 forwarded-large-D→send-COMMIT→tag-COMMIT→ready-I→drop；原 hit/Unknown/cause/attempt 断言保持。此为本卡证明目标实际故障边界所需，不改生产、旧共享 helper、预算或500期望。

固定方案仍未应用；无 Go 或资源运行。旧失败及归因限制保留。
