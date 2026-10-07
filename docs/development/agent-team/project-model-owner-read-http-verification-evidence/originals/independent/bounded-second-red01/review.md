第二轮 bounded 整体 FAIL，三种超限 GET/HEAD 子例已通过。三份原始 GET 503 Problem 与实际 header/run/input 已分别绑定并通过标准 schema；其来源仍标为失败轮。

固定旧 readModelPGFrame 对全部 PostgreSQL 帧限 1 MiB，而复用代理的服务端 DataRow 也经过它。合法原 caps 超过 9 MiB，旧代理无法转发该行；当前 after-hook 仅在候选 callback 成功后才装载 COMMIT 丢 ACK，故旧接缝不足以构造目标 Unknown。快速 500/not_committed 与此容量冲突相符，但原日志没有记录具体超限帧，不补造动态观测，不归因为已证生产 Unknown 或旧超时问题。

后续应仅本卡私有 test 代理支持有界原 DataRow 透传，保留其余边界、真实 COMMIT+idle ACK-drop、cause/attempt、实际 join 与失败原件；待固定方案再审。当前未执行真实资源。
