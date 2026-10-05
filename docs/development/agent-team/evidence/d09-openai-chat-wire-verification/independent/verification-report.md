# OpenAI Chat wire 独立验收结论

**建议采纳该受限 wire 库。** 固定生产静审未发现确定阻断；作者真实 13 顶层/36 子例证据与最终 16 源相符，独立补证 1 顶层/2 子例（其中 SSE 正反两分支）首轮全部通过。真实资源已二次清零并交回窗口。本结论仅为库级文本协议/资源所有权，不包含真实 Provider 账号、Model Runtime、消费者授权、Project Invocation、Secret Model resolve、持久 Usage 或 root 装配。

主线程随后逐 SHA 核对这 16 源并采纳，通报已提交推送 `9c72190fc1600f27c3607a3315854967ae008138`，远端一致。独立执行输入仍是下列固定 de00 基线加候选与 probe，不把随后主线程提交时间当作测试起点。

## 固定执行输入

- 基线：`de00c610da62cb77cc03efe7c3cc842cf81f1ba5`，由本次自有 snapshot 的完整 Git archive 恢复，未纳入活动 Artifact/Object/Project 修复。
- 候选 16 源：作者 `candidate-rev1/manifest.json` SHA `1ed6b19184fa2e3cec928ed01e3b651eadc1704718b8d0f7547e1949dbf03f80`；原字节位于 `fixed/`。其中 5 生产与独立静审 manifest `df11905c95ee6009011db6a3c7426ffe3be49d9bac61349137a5c5d092e09df9` 完全相同。
- 独立 probe：`tests/model/openai_chat_wire_independent_test.go` SHA `be61b134824ea022384c92834e53c0cd668484c1ebd182935b93c81e2f7565d4`，原字节 `probe.go.txt`。所有断言与 40ms 等待预算从编译到运行未改。
- 完整增量执行清单 `verification-input.json` SHA `eb84622d9afe9f1701df014ce8a834c4d2549d56a7eb4f996ee499f534c31857`，列 16+probe 全 SHA 与模块锁。真实执行前及资源清理后均逐项相符，模块锁未改。
- 5 生产静审报告原路径 `/tmp/agenteam-openai-wire-production-static-qy10y8yp/review.md`，SHA `786800ca930deffdf8c7a1a23551b85136e7340edbc51fa01a83f8e8a6cc9860`。已核完整 JSON/SSE/usage 闭集、refusal、默认 obfuscation、精确转义路径与最终 origin、原 D04 Do/body/parser/Drain 后才能退休、64/8 无队列及 unknown Decision 不冒充未发送。
- candidate provenance 17 个 SDK 文件的 SHA256/Git blob 已与此前固定官方 `becc1d20eed83c1b8d85e15dc131a372d9dc7813` 源再次逐项核对。仅静态来源核对，不执行 SDK、供应商账号或对外网络。

## 实际命令与独立结果

先以 Go1.27.1 离线 `go test -c -race -tags=integration -mod=readonly ... ./tests/model` 编译 probe，exit 0；仅编译，没有执行测试 body。精确命令/环境/二进制 SHA 在 `evidence/compile.json`。

真实唯一窗口执行：

```text
sh scripts/test-security.sh -run '^TestModelOpenAIChatWireIndependent$'
```

原 driver 的 race/count1/每包6m 未变。GOFLAGS=`-mod=readonly -v`，AGENTEAM_GO 精确 Go1.27.1，GOENV/GOWORK=off、GOTOOLCHAIN=local、GOPROXY/GOSUMDB=off，共享锁定 GOMODCACHE、自有 GOCACHE/TMPDIR/空 DOCKER_CONFIG。固定 MinIO SHA `dc5298474f0bc87a068f0b1135c583bb1278c17c11c512212ed7644a238c89c8`，PG17/PG16 与 outbound 均由原隔离 fixture 创建。没有读取部署凭据或操作既有资源。

2026-10-05 16:40:01.250989–16:41:10.508629 UTC，driver **exit 0**；`tests/model` **2.896s**，目标顶层 1.88s。1 顶层、2 子例全 PASS，无 FAIL/SKIP。其他 driver 包显示 `[no tests to run]`，这些不计行为覆盖。精确 argv/env/input/exit 在 `evidence/independent-real.json`；原日志 SHA `75298b0bc3fe902003fe57ed86f9dde12e8384d1c8cfa27601063e2898daf20a`。

| 独立子例 | 实際补证 |
| --- | --- |
| late_do_unknown_decision_preserves_project_slot | 正式 Resolver 的首次 Lookup 明确 entered/release，真实 D04.Do/Policy/Client 未替换。取消后 Close/Result 可返回安全取消，DoStarted=true/Decision=nil、Joined=false，借用材料仍可用；另外七个同 Project 真实 held response 已到 server，第九次立即 ResourceBusy、零新增 Lookup/请求。放行原 Do，真实 Decision 才出现且 Sent=false，原 Client.Drain 后才补入唯一替代。请求8/Lookup9，Force 后全部真 join，拒绝者未排队迟发。 |
| finish_usage_stays_live_until_done_or_consumer_deadline | 真实默认 obfuscation/文本→length finish→final empty choices usage（含两个明确0及原 total7），在 DONE 前持有 server。已消费文本与 usage，但 handler/parser/连接仍活，Joined=false。release 支路在实际 DONE 后仅发一次 End、完成真实 join、再 EOF；另一支在原40ms consumer wait 到期时返回 timeout/真实 Dispatched/实际 PartialOutput，后续只 EOF、无迟到 End。两支保留既有 usage，修改返值不污染 Observe。 |

该补证区分“Do 尚未返回、没有 Decision”的 unknown 窗口与作者已测“Do 已返回真实 Decision、底层 writer 尚未返回”的窗口。probe 不模拟 Client、不伪造 NetworkError，不读取私有 Client map；不把 server handler 退出单独当 client join。

## 作者证据复用与原失败

`evidence/author-reuse-checks.json` 记录 15 对作者 metadata/log 的实际 SHA/exit 核对；完整最终16相符的真实 `first-real` 原命令包含 **3 新 wire + 10 旧 D04 = 13 顶层/36 子例**，无 FAIL/SKIP，model 9.470s。其断言已静读，覆盖真实 RequestURI/Bearer/origin、policy 零发送/TLS/POST redirect、安全正文、refusal/PartialOutput、默认 obfuscation、usage、真实 WroteRequest writer 尾部、64全局/8Project/共享两个adapter及受影响旧 TLS/header/redirect/stream/cancel/旧连接路径。因此未重复另造 origin/refusal 或旧 fixture 测试。

作者最终 adapter unit/race 15 顶层/38 子例、适用原 D04 race、根 vet、integration compile、Central/Runner 构建通过；这是原命令的分段证据，不冒称全都在一次最终输入上首轮通过。根纯测首轮有快照缺 API 的两个旧包失败，其他包通过；恢复精确基线输入后二包复验通过。后续候选测试有更改的部分由最终 adapter/compile/vet/真实组以匹配输入覆盖。

原失败完整保留，未重命名为成功：

1. first-unit：新测试错将 C0 安全 Format 标签预期为 model_provider_error；修测试为正式 model_model_error，生产未因该失败修改。原测试 bytes 与首失败 input SHA 相符。
2. integration-compile-v1：新测试使用不存在的 Decision.Allowed；改用正式 Reason，原失败测试 bytes 与 input SHA 相符。
3. pure-root：私有快照缺 `api/openapi/account.json`/`common.json`，恢复固定 de00 文件后仅相关两包复验；没有改旧测试或产品。

这些原日志、metadata、两个首失败测试源与 API 恢复记录在 `evidence/author/`。独立 probe 本轮首编译及首真实执行均通过，没有修改断言、加预算或隐藏重跑。

## 资源与交接

独立原 docker 创建日志绑定 **4 容器/3 网络** 的精确 ID。driver 退出后两次逐 ID inspect 均为明确不存在；两次原 **2 容器/4 网络** 的全部 ID/name/labels 与采集基线相同，owned 进程0、runtime目录空。16+probe 与 go.mod/go.sum 末检不变。资源证据 `evidence/resource-final-checks.json` SHA `fb86f6253829aca86e6194d99fe0663acb23ff1e2de29baac36f6f18147a67f0`。窗口已立即归还主线程；没有因报告重启 Go/Docker/fixture。

可离线重建源：从固定 de00 Git archive 取基线，将 `fixed/` 16 文件覆盖，再放入 probe 原字节；核 `verification-input.json`。大 SDK checkout、Go 缓存、二进制、runtime、TLS私钥均不应进入持久证据。若把源文件归档到文档目录，使用 `.go.txt` 保留 bytes，避免让它们成为仓库 `go test ./...` 的新包。

最终建议仅采纳已验 wire 库；原 Runtime/Invocation/Secret 等未绑定状态与无 Provider smoke 限制保持。报告与精简证据索引冻结，作者/独立所有源码、Go、Docker all-stop。持久文档与状态页由主线程指定 d08_recovery_design 独占归档，本 verifier 不写仓库或 Git。
