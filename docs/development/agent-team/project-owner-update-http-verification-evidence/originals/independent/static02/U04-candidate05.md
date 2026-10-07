# U04 candidate05 增量核对

输入 manifest `d335ac3acb2547625e2c1398e09fde5d80461b3d1303675e29dc2eb737378ec7`，仅 update.go/update_test.go；生产其余不变。

64 层有界遍历、每能力首可达接收者、FlushError 优先 Flusher、跨不同层保留 read/write/flush 方法都符合 ResponseController 的能力分派；解析没有提前调用 Flush 或写响应。固定接收者 adapter 避免原 finish 再遍历恶意 Unwrap 环。

仍有阻断：prepareUpdateIO(w) 在 requestIO.finish defer 安装前调用。Unwrap() 是任意 writer 方法，若 panic，原 Body.Close 与 ErrAbort 转换尚未取得所有权，且部分已解析 deadline 接收者未返回。建议在空安全 adapter/requestIO/defer 建立后原位解析，使后续 panic 仍由同一 finish 按已解析方法同步退役。补 panic-Unwrap 的零 auth/service/header/body 与一次 Close 反例。

作者另报新增 cycle 测试通过旧 httpapi.WithRequestID 时先触发其 stateOf 递归，尚未进入本 handler；实际2原 raw 必须保留。这是旧中间件对人工环的边界，不改旧模块；后续 handler 单元应在真实 RequestID 已分配的接缝注入循环 writer，不能将 handler 有界检查说成外层整个 middleware 也防环。

未运行独立 Go/资源。上述已报 root，等待新冻结后差量复核。
