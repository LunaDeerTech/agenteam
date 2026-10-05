# OpenAI Chat structured wire 作者交接

状态：作者实现/自测完成，10 源停止写入；等待独立动态验收与主线程采纳。未提交 Git。固定业务基线 `ac5b4c65e88ed0ec1813fbf341035c88ca9a38c7`，依据已采纳卡提交 `b6297143dce6a479d7c9ce7d463900288d5b0915`。本报告不声明完整 D09、Resolver、Memory consumer、Invocation、Usage 或生产调用装配完成。

## 最终输入与实现

唯一生产/测试范围是卡内 10 路径，见 [最终 manifest](candidate-review-02/manifest.json)，SHA-256 `53d31b380ed5c214cb9e87fbf5450454b0f2e5935e0b92615dd3eb0b0389236c`。`sources/` 原字节与仓库、轻量测试树逐 SHA 一致。4 个生产文件保持独立静审通过的 review02 原字节；真实测试期无代码差量。[完整差异](candidate-review-02/source.patch) 相对固定 ac5，SHA-256 `76d045760d3ddf3976ac8dd901d38a3e009b67007f735339eff3670addebdbef`。

同一个 OpenAIChat 实例增加独立 `openai-chat-structured-v1` 修订，C0/API 签名不变。Start 在原 Budget 接受前冻结/编译严格 schema 并生成同源 native 格式；普通响应和 SSE 在原 owner 内验证完整内容。text-v1 及新修订 text 分支保持原闭集/语义。实现有界私有 schema/词法遍历，拒绝不支持的约束；精确整数不转浮点、不展开指数，字符串无累计内容等量的第二校验副本。只有验证通过并实际 join 后才发布成功；既有 usage/安全错误/PartialOutput/借用材料所有权保持。

修复原授权 `transport.go` 的成功消费出口：实际 waitJoined 之后无条件复核 caller 和 attempt 取消，不能因底层 Drain 已完成而忽略取消。未修改 D04、Budget、旧测试、核心 Model、Secret、Object、Artifact、root、SQL/迁移、go.mod/go.sum。仅新增一个修订常量，不增加 schema 库依赖。

轻量测试树使用固定 ac5 的 687 项 Go 闭包/资产，加 10 源覆盖及一项 OpenAPI 补充资产；未复制完整仓库或 modcache。原 [baseline-source.json](evidence/baseline-source.json) 保留，补充文件记录在 [baseline-assets-supplement.json](evidence/baseline-assets-supplement.json)。最终全部非授权基线文件逐 SHA 核对未变。

## 实际检查与复用

所有实际命令的 argv、cwd、显式 environment、输入 SHA、开始/结束时间、exit 和原日志 SHA 均保存在对应 JSON。固定 `/workspace/toolchains/go1.27.1/bin/go`，`GOTOOLCHAIN=local`、`GOPROXY=off`、`GOSUMDB=off`、`GOFLAGS=-mod=readonly`；重用已有 modcache/本人私有 Go cache，TMPDIR 在自有 workspace。真实组额外 `GOFLAGS=-mod=readonly -v`，沿正式 driver 的 `-tags=integration -race -count=1 -timeout=6m`，没有改预算。

| 原记录 | 实际结果/使用边界 |
| --- | --- |
| `toolchain.json/.log` | 固定 Go 1.27.1 验证；早期 PATH 中另一个 `go` 命令只报 unknown option，未执行构建，单独记录 initial-path-go-observation.json |
| `pure-race-02.json/.log` | 整条 exit 1：旧 Model 包因轻量树缺 OpenAPI 资产 1 顶层失败；同条 adapter **26 顶层/113 子例 PASS**，contract **24/13 PASS**，可按包复用，不能称整条首绿 |
| `model-pure-race-03.json/.log` | 仅从 ac5 补原 OpenAPI 文件后，旧 Model 包 **26/20 PASS**；不改生产/测试、不重跑已过 adapter/contract |
| `integration-vet-02.json/.log` | `go vet -tags=integration ./internal/central/model/... ./tests/model/...`，exit 0 |
| `integration-compile-02.json/.log` | 固定最终输入、race integration `-run=^$` 的 Model/adapter/C0/tests/model，exit 0；编译记录不是真实行为通过 |
| `fixture-closure-compile-01.json/.log` | 正式 driver 其余所有包的 integration/race 编译闭包，exit 0，避免轻量树遗漏编译源；未执行业务测试 |
| `central-build-02.json/.log`、`runner-build-02.json/.log` | 两个真实 cmd build，均 exit 0；输出仅保存在自有 bin，不入证据归档 |
| `structured-real-01-input.json`、`structured-real-01.json/.log` | 最终 10 源一次真实运行，**6 顶层/48 子例全 PASS**，tests/model **13.165s**，driver exit 0；无真实首红或重跑 |

最后这些纯检查的 `owned_inputs` 全部等于最终 10 源。adapter 26/113 包括 **11 个新顶层/75 子例**及 **15 个旧顶层/38 子例**；没有修改旧断言。[按包计数](evidence/pure-package-results-02.json)、[纯检查索引](evidence/pure-summary-02.json) 保留失败原状态，不拼成一次整体首绿。前期 pure-unit-01、pure-race-01、pure-vet-01、integration-compile-01 是 review01 阶段证据，不能替代最终输入结果。

真实选择与逐项计数见 [selector](evidence/real-selection-01.json)、[结果明细](evidence/structured-real-01-results.json)：

| 真实顶层 | 子例 PASS | 实际行为 |
| --- | --- | --- |
| TestModelOpenAIChatStructuredHTTP | 14 | strict/native schema 与私有输入、嵌套/null/enum/大整数、先验零发送；合法 native envelope 内真实 schema 失败，安全错误/usage；新修订 text 兼容 |
| TestModelOpenAIChatStructuredStream | 10 | UTF-8/CRLF/转义/数字分段；final usage；DONE 后服务器 hold 不 Release 仍客户端实际关闭；mismatch/尾随/截断/refusal 等无成功 End；真实输出节点超限 |
| TestModelOpenAIChatStructuredCloseAndBudget | 2 | 实际背压 parser 持有 slot 与借用材料；超时 waiter 不退休，Close 后无迟到成功；混合新旧 revision 同一全局 64/项目 8 Budget |
| TestModelOpenAIChatWireHTTP | 6 | 原 text 普通响应及出站授权/拒绝/安全错误兼容 |
| TestModelOpenAIChatWireStream | 14 | 原 text SSE/usage/协议错误/实际 body cap/背压取消兼容 |
| TestModelOpenAIChatWireJoinAndBudget | 2 | 原真实 writer 迟到保留 slot、两个 adapter 共用配额 |

这是真实 PG17/PG16/MinIO、Account/Audit/PG policy、D04 controlled local service 组合；SecretMaterial/snapshot 是原 fixture 的合成协议输入，未宣称 Secret Resolve、实际供应商账号、生产 consumer 或 Invocation/Usage 绑定通过。16 MiB/1 MiB/深度/数值等全部本地极限由新纯测试验证；真实服务单场景 512 KiB 上限未变。

## 原失败、静审与窄修

[独立 review01](evidence/independent-production-review-01.md) SHA `de1f8913620aba6340e48381fe9ddf5debdc01396694451efacf8b29eaeef6b7` 是静态 BLOCKED，指出 Decoder.Token 的累计副本/不可取消扫描与 inherited consumerJoin 的取消成功出口。原 4 生产保留在 production-review-01；原红完整 10 源保留在 [red-f1-f2-01](red-f1-f2-01/manifest.json)，manifest SHA `91dae500a9a2514acba36721d1c9e962488e3d5a31f40fb72d8785c4e308479f`。

`pure-f1-f2-red-01` 在 review01 真实执行 **2 顶层/6 子例 FAIL**：4 MiB 字符串与长数值验证分别分配 20,972,048/20,972,128 bytes；JSON/SSE × caller/attempt 四个确定 barrier 在取消后得到 nil 错误。`pure-f1-f2-fixed-01` 对同测试与同预算 **2/6 PASS**；验证增量分配 0/64 bytes，四个取消出口均拒成功。这些 F2 单元只证明本地已完成 owner/slot 出口，不等价真实 D04 body/server join；后者由本轮正式真实组和后续独立动态验收证明。

review02 只窄改 structured_schema.go、transport.go，原 4 生产 manifest SHA `5d846a337a5b41b3e04621cfc51b6f2ebddf311f6974c98dd5b16264751f8351`，差异 SHA `e293b5d74550f0492686119d3d95e15b5063379d43d053ca02b8b7dba867a50a`。以原字符串上的有界词法游标替换 Token 验证：增量 number 限长、长字符串扫描内取消、小 key/enum 有界解码；实际 join 后双 ctx 复核。规格/旧分支/生命周期预算未减弱。

[独立 review02](evidence/independent-production-review-02.md) SHA `c433db64d206d1bf11575dad840fc7440a1fa66ccb48aa7747a3edf624c9ed6d` 窄复审 PASS，仅静审与原红/修后证据认可，不冒充独立动态通过。之后 4 生产未改变。

## 来源、资源与终态

新增 testdata manifest 含官方固定 `openai/openai-python@becc1d20eed83c1b8d85e15dc131a372d9dc7813` 两份 SDK 原件 base64、commit/blob/SHA 与旧 17 源 manifest 引用；新纯测试实际复原并验证 SHA-256/Git blob SHA-1，不安装/执行 SDK、不联网。该受限 schema 子集是本地明确工程能力，不是供应商全 JSON Schema 标准声明。

实际轮从 2026-10-05 19:45:01Z 到 19:45:35Z；真实 log SHA `a01719ccc27512d99320d33cc338590b9e6cc1595f6d25956ceb43fdf5b0111d`。run-real.py/resources.py/check-cleanup.py 在交窗前冻结，readiness 索引见 evidence/real-runner-ready-01.json。新资源每次创建后实际 inspect ID/name/nonce-label，包含网络；只记录安全资源身份，不归档生成凭据。

[双清理原记录](evidence/structured-real-01-cleanup.json) SHA `56e7d349f163368b19f62c720ff6315c473741ed22494fe4b69541cf74a3429a`：4 容器/3 网络，两次 exact ID 不存在；原 2 容器/4 网络 ID/name/labels 与最近 root 已验 trusted baseline 精确相同（内置 labels null 规范为空 map）；继承唯一 marker 的进程集合 0，runner 已退出，runtime 空。创建/活体检查见 structured-real-01-docker.jsonl，实际进程集合见 structured-real-01-processes.json。窗口已交回 root，不再使用 Docker。

保留原所有失败日志，不删除他人文件。当前 10 源停写，Go/Docker/后台命令均结束，只在自有工作根整理交接；无业务 Git 操作。待独立 V 动态结论后由主线程决定采纳/提交。Summary、serving snapshot、Model consumer/Resolver/Invocation、Project 领域装配、暂停 Object guard 缺陷和 ready503 均未改变。
