# M3 Model Runtime：必要恢复上下文

M3 已接受范围以[任务卡](m3-knowledge-memory-collaboration.md)与[正式独审](m3-core-independent-review.md)为准。本页仅保留原 Model 测试与证据的恢复入口，不恢复旧分支 current 中已过时的待验任务，不增加产品验收结论。

原 Model 固定源码来自 `fbf5876229c4cd53113dd450e2b5af21f0c53c71`。与 main helper 不兼容的七份 M3 测试及两个原 helper 已保存为[恢复 harness](../../../.agent-state/m3-model-runtime/reproduction/README.md)，说明了差异及不依赖旧 ref 的隔离 overlay 编译方法。独立 retention DDL 测试、embedding 协议 probe 和 cold App 测试使用 main 原正式路径。旧卡的推进流水不复制进当前恢复入口。

- Runtime/真实向量：原 10276 整轮第三个 top 失败，80228 后续只验证 pre-admission 门；31270 验证 v2 受控门。2079 原 Wait0、136 条公开固定语料、三批 HTTP200/4096 维及原 Runtime/Usage/Secret/lease/Join 清理结果保存在[原 manifest](../../../.agent-state/m3-model-runtime/runtime-embeddings-v2/manifest.json)、result 和唯一 vectors gzip。响应 model alias 未保留；这不代替产品 Knowledge/Memory/Meeting 权限链，不重复外部请求。
- Serving retention：75483 原 Wait0、10.79 秒；91453 的 populated upgrade 8.37 秒和物理 COMMIT Unknown 12.07 秒，均按正式独审限定范围接受。原日志为 main 下 `output/ai/m3-model-checks/retention-native-01.log`、`retention-unknown-native-01.log`。
- OAuth Secret 窄写口：56569 原 Wait0，SealedWrite 5.47 秒、Unknown 10.01 秒；原 36560 Wait1 发生在 Unknown proxy arm 前，保留失败而不计成功。保存[原 result](../../../.agent-state/m3-model-runtime/oauth-secret/result.txt)及 main 下 `output/ai/m3-model-checks/oauth-native-{01,02}.log`。只覆盖受控 Secret sealing/CAS/receipt/Audit/事务 owner，不能据此接受实际 MCP OAuth 交换、刷新或冷恢复。
- Cold App：main 下 `output/ai/m3-model-cold/app02` 至 `app06` 各保留原 `native.log` 与 `owned.json`。app02 整轮失败但 authorized 子门通过；app03 sent 通过；app04 整轮失败但 retired 和 ModelCallOwner 子门通过；app05 后续 borrowed 定向通过；app06 覆盖原 COMMIT Unknown 两支。ModelCallOwner / Unknown 的后继进程只到 Model Initialize 后实际 kill/Wait，不称 Memory 业务恢复或完整 graceful Join；各原判断见正式独审及相应 harness。
- Retention 133：保留 `output/ai/m3-model-cold/retention-catalog02/native.log`，原 99703 的最小前置 DDL 修复门通过 0.11 秒及十子例；不能代替全量迁移或领域生命周期。StoppedResolution 保留 42231 原失败的 `output/ai/m3-model-cold/stopped-resolution02/{native.log,owned.json}`；96148 独立三态门 6.55 秒 / Wait0 的原输出已在 main `output/ai/m3-core-review/stopped-resolution03/`。91350 的原 fixture 失败仍见独审与 `stopped-resolution01/`。

上述17份 Model 本机原日志/owned已保存在 main 的忽略 `output/ai/` 下：原 `output/m3-model-checks/` 与 `output/m3-model-cold/` 分别映射为 `output/ai/m3-model-checks/` 与 `output/ai/m3-model-cold/`，后缀、内容与 mtime 保持；它们仍是忽略的本机证据，不声称已通过 Git 远端恢复。受跟踪的 harness、OAuth result、已有公开向量与正式独审保留必要跨设备恢复内容。此次不保存可再生旧 binary、raw vectors 或整份历史 output。
