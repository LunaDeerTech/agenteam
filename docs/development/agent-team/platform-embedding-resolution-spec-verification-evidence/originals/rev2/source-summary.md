# rev2 来源与差量摘要

root 仅授权原卡及作者 scratch，修正完整 rev1 独审的 D1/D2。rev1 完整独审为 CHANGES REQUIRED/STOP，原冻结卡、作者检查、freeze 与失败报告均原样保留；rev2 尚待独立差量及最终组合 STATIC 与 root 采纳。

- D1：按已接受 Resolver §3 与 C0 SelectionRequest/Consumer、现 policy 保留合法 agent/tool direct chat 用途（包括 ToolConsumer/ApprovalAuto）；在已列新 pure 的旧分支对照中加入该代表。
- D2：明确 InTx 先核同 Store、活 Tx、完整持锁与 opaque request/mapping 绑定，沿 caller 同一 Tx 进行必要 SQL 读写及 planned Secret Acquire；禁止补锁、内层 Tx、Provider/外部网络 I/O。没有要求产品重排或新增接口。
- 全差量只有修订状态及上述三处正文修改。8 技术路径加 README 末件、全部测试 top、命令/预算/资源门槛、后继未绑定、三硬停及 Jina 停止均保持。
- 原 49 项有界来源清单逐字复制，SHA 未变；不是 Go 可执行依赖图。仅复核本次必要的 5 个已列稳定来源并对照其原指纹，未重扫 49 项、Audit 活跃源或历史。其余来源复用 rev1 与完整独审结论。
- 本轮只执行静态文本读取、差量/指纹自查及本卡/scratch 写入；未执行 Go、Node、browser、SQL、网络、Git、测试或资源命令，未实施产品。
