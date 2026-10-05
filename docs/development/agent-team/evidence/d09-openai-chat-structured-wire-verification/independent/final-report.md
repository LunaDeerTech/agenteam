D09 structured OpenAI Chat wire 独立验收结论（冻结）

结论：PASS，建议采纳精确最终 10 文件提供的 structured wire 库结果。未发现剩余阻断；不宣称完整 D09、Resolver/Usage/生产 consumer 或生产 root 已闭环。本报告只绑定 ac5b4c65e88ed0ec1813fbf341035c88ca9a38c7 + candidate-review-02/manifest.json（SHA256 53d31b380ed5c214cb9e87fbf5450454b0f2e5935e0b92615dd3eb0b0389236c）。该候选 4 生产与已审 review02 以及独立真实探针输入逐字节一致；不消费活动主树。无需迁移、依赖或 C0 接口变更。

独立审查发现并闭合两项问题。review01 的输出 Decoder.Token 会在限额判定前复制整个长标量，且扫描中取消检查不足；consumerJoin 已真实 joined 的旧分支可漏掉随后 caller/attempt 取消。前者为本次新实现，后者是 ac5 既有行为但违反本卡明确的成功终态要求。原静态 BLOCKED 报告保持原字节，未倒写成动态复现。作者随后以同一 2 顶层 6 子例实测先红后绿：4 MiB string/number 的验证阶段额外分配 20,972,048/20,972,128 B 降至 0/64 B；四个 JSON/SSE × caller/attempt 确定取消 barrier 从错误成功变为取消失败。其 F2 纯例使用本地 owner，不能单独证明 D04 网络 body join。review02 已改为原 string 游标词法验证（严格 UTF-8/escape/surrogate、256 B key/1024 B enum scratch、129th number byte 拒绝、每约 1024 B 检查 ctx、原深度/节点/内容限额），真实 waitJoined 后无条件复核两个 ctx；原 D04/slot 所有权不变。静态差量 PASS，最终纯/真实证据补足对应边界。

作者证据复用：author-evidence.json（ebaa38378213af6b5ec5955a94eed69b6520c3128a7312ced530e1bd35789231）的 87 文件、820,337 B 全部 SHA/长度匹配；final10、687 基线文件与正式 SDK 两份原 Git blob 均独立核实。最终真实一次 driver exit0，6 顶层 48 子例，tests/model 13.165s：新增 3/26，旧 text wire 3/22，无 FAIL/SKIP。纯 race-02 整体 exit1 必须保留：adapter 26/113（新增 11/75 + 旧 15/38）及 contract 24/13 已绿，Model 因固定快照漏带 ac5 OpenAPI 资产而有 1 顶层失败；只补该精确资产后 model-pure-race-03 为 26/20 全绿，没有生产/测试修改。这里按包复用，不能表述一次纯整组全绿。最终 integration vet/race compile、原 fixture 编译闭包与两 cmd 实际 build exit0；七份纯/构建元数据均精确绑定最终10。review01 纯结果仅作历史，不替代最终检查。

独立实际增量：固定 ac5 + review02 四生产 + 私有 probe（5bd36ca1b831a621fbfc53103c4d1212807dec8fe84bd6c836c4f5105016292f），先独立 race integration compile / vet exit0，再运行原 scripts/test-objects.sh。两顶层一次 PASS，无子例、无重试、无放宽预算，tests/model 3.795s，driver exit0。

- TestIndependentStructuredMismatchUsage：真实 Account/Audit/PG/D04 fixture，真实请求与已消费的 SSE 前缀；合法 JSON 在嵌套 enum 处违反 schema。精确断言 nonretryable wire_protocol_invalid、Dispatched/PartialOutput、完整 usage(7/0/9)、无成功 StreamEnd、后续 EOF、只有一次 POST、错误不泄露正文 canary，以及实际 close/join 后才销毁借用 material。
- TestIndependentStructuredCancelActualJoin：原正式 WroteRequest barrier 与受控 server hold，caller cancel 后短 Close 超时且 Joined=false、借用 material 可用；同 Budget 混合 revision 实际占满项目 8 slots，第 9 个无请求拒绝。取消 Drain 不伪称 join；释放原 callback 后实际 join 仅归还一个 slot，再准入一个真实请求；StopAdmission/Force 后所有 owner/body/parser 实际 join、服务器连接/handler 清零，才显式 Destroy material。没有新增网络故障或协议干预方法。

实际命令：sh scripts/test-objects.sh -run '^TestIndependentStructured(MismatchUsage|CancelActualJoin)$'。cwd 为本目录 tree；原 driver -race/-count=1/-timeout=6m，GOFLAGS='-mod=readonly -v'；AGENTEAM_GO=/workspace/toolchains/go1.27.1/bin/go，GOENV/GOWORK=off，GOTOOLCHAIN=local，GOPROXY/GOSUMDB=off，共享只读 module cache、独占私有 GOCACHE/TMPDIR，CGO_ENABLED=1，本机 socket/空私有 Docker config，固定 MinIO 和 PG17/PG16 镜像。完整 argv/env/toolchain/资源记录见核心索引。probe 实际在固定 tree 中参与 driver Go 调用，不是仅编译未运行的 overlay。其他包的 no tests to run 只计 fixture/编译，不计行为通过。

独立原始日志 SHA256 9030cec37e62eb174303a80a8948936419daf093b064d4ddb64e129f8a20d27a。cleanup01/02 分别 23c90685567c13a16fd79eea0f68e279547b0da8fdabeb7946e705290f86a921 / 58409a7af845b98a0316282c9f10d871454a1bd77c2759cc38fdb71925d82c34：owned 4 容器 3 网络有活体 exact ID/name/nonce-label 观测，二次 exact absence，原 2 容器 4 网络 ID/name/labels/状态一致，进程组与私有 runtime 空；823 固定输入末核不变。driver 和 observer 已退出，窗口已正式交回。准备阶段曾因请求不存在的 api/openapi.yaml 而 git archive 拒绝；发生在 Go/fixture 前、零资源，保留原记录，不计测试失败。独立真实轮无行为红例或重跑。

边界：这是保守闭集 JSON Schema subset 与 native wire/受控正式 D04 fixture 验收，不是所有 JSON Schema/外部真实 Provider 模型保证。SDK 两原文件仅离线来源证明，不运行 SDK；真实探针的 material/snapshot 为 fixture 构造，不能外推 Secret resolve grant、Resolver、Memory canonical schema、Invocation/Usage 或生产 root 已接通。已知 Object 共享 guard 阻断不在此范围，没有重开或宣称解决。原 F1/F2 红、纯 setup 红及准备拒绝均保留。推荐精确10文件可提交；提交事实由主线程核定。本独立实例 all-stop，不再运行 Go/Docker。

全部定位与 SHA 见同目录 final-core-index.json；归档保留原件，避免复制 tree/cache/binary。
