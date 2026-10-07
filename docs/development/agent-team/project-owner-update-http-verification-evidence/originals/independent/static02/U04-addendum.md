# U04：Flush 能力前置缺失

**candidate04 有阻断；static02 原件保留，其“无阻断”结论由本补充纠正。** 卡与生产其余静态项不重判。

固定卡 §4 要求完整 Write/Flush 消费原预算，且“能力不支持就在业务调用前安全abort”。candidate04 update.go 在 execute 前只调用已有 requestIO.start；固定 901eb546 handler.go:245 的 start 只设置 read/write deadline。update.go 的 Flush 在 execute、Body.Close 与响应 WriteHeader/Write 之后。Go 1.27.1 ResponseController.Flush 独立选择 FlushError、Flusher、Unwrap；两种 deadline 方法成功并不能推出 Flush 能力存在。

因此实现 http.ResponseWriter 与 SetReadDeadline/SetWriteDeadline、却没有 FlushError/Flusher/可达 Unwrap 能力的 writer，将执行一次 PATCH 服务调用并尝试写 200 后才收到 ErrNotSupported。现有 PureActualIO 的 unsupported 只令 SetReadDeadline 返回 ErrNotSupported，不能覆盖此反例。这是源码控制流可确定的问题，本阶段没有用未运行的探针冒称动态首红。

建议仅在授权 update.go 私有前置里无副作用检查标准能力分派，置于业务前并保留原 Body.Close/callback 所有权；不得提前执行 Flush，避免发送成功头。补 PATCH/lookup 零服务调用、零写入与真实 Close 的无 Flush 反例，以及 FlushError/Flusher/正常 Unwrap 的有效代表。真实 Flush 调用仍位于完整编码、写入之后，其运行时错误继续原 abort 处理。旧 read handler/requestIO 不改。

已于本轮即时报告 root/作者；等待 root 解冻协调与新 candidate。独立 HTTP 离线尚未启动，未产生资源或测试进程。
