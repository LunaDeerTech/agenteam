# D09 OpenAI Chat wire 验收记录

状态：**OpenAI Chat 文本 wire 库已独立验收并获主线程采纳。** 精确 16 源已提交推送 `9c72190fc1600f27c3607a3315854967ae008138`，主线程确认远端一致。结论由生产静审、作者固定输入结果及独立真实补证共同支撑，限于本卡库级能力；完整 D09 尚未完成。

## 固定输入与边界

规格为 `9d648575d07ce791dd623eb45eb6604cc6c908b5` 的 [OpenAI Chat wire rev2](../work-items/recovery-d09-openai-chat-wire.md)，生产基线为 `de00c610da62cb77cc03efe7c3cc842cf81f1ba5`。作者冻结 16 路径，包括 5 个新生产文件、5 个单元测试、1 个 profile manifest、3 个新集成测试及 2 个旧测试 fixture；[原 manifest](evidence/d09-openai-chat-wire-verification/candidate-manifest.json) SHA-256 为 `1ed6b19184fa2e3cec928ed01e3b651eadc1704718b8d0f7547e1949dbf03f80`。

[固定源码与重建说明](evidence/d09-openai-chat-wire-verification/README.md)以原 `de00c610` 基线加已提交 `9c72190` 的精确 16 个 Git blob 为主入口；[逐 SHA 核对](evidence/d09-openai-chat-wire-verification/accepted-source-check.json)全部匹配候选。原 candidate patch 仅保留一份，已在内存中应用到固定基线并得到同样 16 个 SHA。独立执行另加归档 probe，17 项输入和模块锁均可核对。本次归档者没有执行源码、编译或动态测试；未复制完整 snapshot、缓存、二进制或凭据。

库级结果限定文本 JSON/SSE、原生 usage、安全错误、受控出站和共享无队列 Budget。采用的官方 SDK 为 `openai/openai-python@becc1d20eed83c1b8d85e15dc131a372d9dc7813`；17 文件身份、字段与许可证复用[已提交来源证据](evidence/d09-openai-chat-wire-source/README.md)，不引入或执行 SDK。Model Runtime、消费者授权、真实 Secret Model resolve、Project Invocation、持久 Usage、HTTP/root 和真实 Provider 账号 smoke 均不在当前验收结果中；合成 canary 不代表业务授权。

## 作者检查及复用口径

[作者报告](evidence/d09-openai-chat-wire-verification/author/author-report.md) SHA-256 为 `8a22186eeeb3e8d5800cfdf47356574a6be3b912d4722fac8939b7c29cbe9d37`；[原证据索引](evidence/d09-openai-chat-wire-verification/author/author-evidence.json)为 `955d2588d894ada2511e223320a04b2ec9a47ba84038c87501829f4d3885c52d`。归档者逐项核对日志/元数据 SHA、argv/env/cwd、输入指纹和 exit，并直接解析原日志；没有以空日志补造成功状态。

| 来源 | 实际结果与限制 |
| --- | --- |
| 作者最终 adapter unit/race | `pure-adapter-final`：**15 顶层、38 子例 PASS**，最终 16 源指纹一致。 |
| 适用纯 race | `pure-final-race`：adapter/D04 共 **44 顶层、160 子例 PASS**；后来仅 adapter 测试及两个集成测试文件有变化，最终 adapter 另跑上述 15/38。fixture 包无测试，不计真实行为通过。 |
| 根纯测试 | `pure-root` **exit 1**：38 包通过，account/httpapi 两包因私有快照缺少基线 OpenAPI 输入而失败。恢复精确基线文件后，只复验这两包并通过；保留原失败，不称整根一次全绿。最终 adapter 单独覆盖最终单测输入。 |
| 纯工程门槛 | 最终根 vet、integration race compile、Central 与 Runner build 均有实际 exit 0 元数据。集成使用 `-run '^$'`，只证明编译，不是行为运行；未启动两个产物。 |
| 作者实际 fixture | `first-real`：**13 顶层、36 子例 PASS**，其中 3 新 wire/22 子例，10 旧 D04/14 子例；无 FAIL/SKIP，model 包 9.470 秒。未选中的包不计业务回归。 |

[实际 driver 元数据](evidence/d09-openai-chat-wire-verification/author/evidence/first-real.json)保留完整 selector、Go 1.27.1、原 `-race -count=1 -timeout=6m`、`GOFLAGS=-mod=readonly -v` 和隔离环境；实际为 `sh scripts/test-security.sh -run '<冻结 selector>'`，没有使用 driver 不支持的 CLI `-v`。[完整真实日志](evidence/d09-openai-chat-wire-verification/author/evidence/first-real.log) SHA-256 为 `ee79900d356214c5ac7780b4faf3e3c1144279b3e2889af69fcdb75aa8db918d`。最终运行的 16 源与候选 manifest 全等；早期检查的输入差异逐项保留在 [日志核对](evidence/d09-openai-chat-wire-verification/author-log-review.json)，不将早期阶段指纹冒充最终输入。

作者真实组合使用 Account Session/System 权限及 Audit 更新隔离 PG policy，owned TLS server 观察请求、路径、合成 Bearer canary 和发送计数。结果覆盖拒绝前零发送、TLS/redirect/HTTP 错误、SSE 分片/usage/refusal/默认 obfuscation、body cap 和慢消费取消，以及共享 64 全局/8 Project 的预算。作者记录真实 D04 writer 在标准 WroteRequest hook 尚未返回时，取消与 Close 超时不能退休 slot；实际放行后才可复用。独立验收者另补 Do 尚未返回且 Decision 未知，以及 DONE 前实际存活的窗口，详见下节。

## 原失败与输入修正

1. `first-unit` 的 `TestOpenAIChatNativeEnvelopeAndSafeErrors` 失败，直接触发文件原件为 [errors_test.go](evidence/d09-openai-chat-wire-verification/author/evidence/first-unit-input/errors_test.go.txt)。该版误把 C0 固定 Format 标签预期为 `model_provider_error`，正式值为 `model_model_error`；该次测试修正前后五生产输入未变。后续闭集/UTF-16 收紧属于冻结前迭代，不能与这条断言失败混为一项产品修复。
2. `integration-compile-v1` 因新测试引用不存在的 `Decision.Allowed` 而编译失败，[原 HTTP 测试](evidence/d09-openai-chat-wire-verification/author/evidence/integration-compile-v1-input/openai_chat_wire_http_test.go.txt)及元数据保留。修正使用正式 `Reason`；前后阶段还包含其它冻结前变化，不把两个阶段所有差量都归因于此编译错误。最终 16 源重新编译通过。
3. `pure-root` 原日志明确缺少 `api/openapi/account.json` 与 `api/openapi/common.json`。作者从固定 `de00c610` 恢复两文件，[原命令与 SHA](evidence/d09-openai-chat-wire-verification/author/evidence/snapshot-api-restoration.json)保留；旧源码和断言没有修改，受影响两包复验通过。

所有失败原日志、输入指纹、实际 argv/exit 及上述直接触发文件均按原字节归档。完整可重建范围是最终 16 源；早期阶段保留其实际元数据和直接失败文件，不宣称每个早期源码变体都保存了完整可重建树。作者真实首轮没有失败，没有通过修改预算或断言掩盖真实红例。

## 独立验证与采纳

[生产静审原报告](evidence/d09-openai-chat-wire-verification/static/review.md) SHA-256 为 `786800ca930deffdf8c7a1a23551b85136e7340edbc51fa01a83f8e8a6cc9860`，5 个生产源与最终候选一致，有限静审未发现确定硬阻断。已审请求/usage 闭集、真实 D04 Client 终局、Budget、SSE/refusal/obfuscation、origin credential binding 与安全值。

[独立最终报告](evidence/d09-openai-chat-wire-verification/independent/verification-report.md) SHA-256 为 `0a229c310fb5d8a45fe25037b5ff5bb1a95364131560168294ea80f215c2fd6b`。[17 项执行输入](evidence/d09-openai-chat-wire-verification/independent/verification-input.json)绑定原基线、最终 16 源、模块锁及 [probe 原件](evidence/d09-openai-chat-wire-verification/independent/probe.go.txt)，probe SHA-256 为 `be61b134824ea022384c92834e53c0cd668484c1ebd182935b93c81e2f7565d4`。先行 `go test -c -race -tags=integration -mod=readonly` 实际 exit 0，仅计编译；独立验证没有改产品、作者测试或等待预算。

[真实命令](evidence/d09-openai-chat-wire-verification/independent/evidence/independent-real.json)为 `sh scripts/test-security.sh -run '^TestModelOpenAIChatWireIndependent$'`，Go 1.27.1、离线固定依赖、自有 cache/runtime，沿原 race/count1/每包 6m，`GOFLAGS=-mod=readonly -v`。driver exit 0，**1 顶层、2 子例 PASS**，model 包 **2.896s**，无 FAIL/SKIP；其它包的 no-tests-to-run 不计覆盖。[原日志](evidence/d09-openai-chat-wire-verification/independent/evidence/independent-real.log) SHA-256 为 `75298b0bc3fe902003fe57ed86f9dde12e8384d1c8cfa27601063e2898daf20a`。

| 独立增量 | 实际证明 |
| --- | --- |
| Do 未返回且 Decision 未知 | 正式 Resolver 有明确 entered/release，真实 D04 Policy/Client/Do 未替换。取消及安全错误返回后仍 Joined=false、材料有效；另七个同 Project 请求真实到达，第九个被拒且零新增 Lookup/请求。放行原 Do 并完成真实 Client.Drain 后才可补位；未知 Decision 未被当作 Sent=false。 |
| finish/usage 后、DONE 前 | 两分支均先观测文本、明确 0 usage 与 total 7，且 handler/parser/连接仍活。放行 DONE 后只给一个 End，再真 join/EOF；原 40ms consumer wait 到期则得到 timeout/真实 Dispatched/PartialOutput，随后 EOF、无迟到 End。usage 保留且返回副本不污染 Observe。 |

独立者逐一复核作者 15 对 metadata/log、最终真实 16 源及原失败，复用作者的 escaped URI/Bearer/origin、refusal、真实 writer 尾部、64/8 预算和 10 个旧 D04 顶层；未机械重复这些已匹配输入的场景。Do 未返回与已返回 Decision 但 writer 尚未终局是不同窗口，两侧证据互补。归档者再次核对 [独立日志、输入与资源](evidence/d09-openai-chat-wire-verification/independent-log-resource-review.json)，没有把文件核对称为重新运行动态测试。

## 资源交还与未验边界

[作者资源原件](evidence/d09-openai-chat-wire-verification/author/evidence/resource-final-checks.json) SHA-256 为 `1c52778fe4b54ad1635427114162e8534e54ceb99385232697f8bf5b8071885e`。与原 operation 记录对照，4 容器/3 网络的创建 ID 均分别命中两次实际 absent exit/stderr；原记录声明基线 2 容器/4 网络 ID/name/labels 不变，owned 进程 0、runtime 空、driver exit 0、16 源末检一致。[归档复核](evidence/d09-openai-chat-wire-verification/author-resource-review.json)只核这些原始文件，没有重新操作 Docker 或数据库。

[独立资源原件](evidence/d09-openai-chat-wire-verification/independent/evidence/resource-final-checks.json) SHA-256 为 `fb86f6253829aca86e6194d99fe0663acb23ff1e2de29baac36f6f18147a67f0`。7 个创建 ID 与两次 inspect absent 完全对应，均有真实 exit/stderr；两次原基线 2 容器/4 网络的 ID/name/labels 精确一致，owned 进程 0、runtime 空，17 输入与模块锁末检不变。两侧资源均已清零交还；本次归档不重新访问 Docker、数据库或网络。

通过限于文本 wire 库，不含真实 Provider 账号 smoke、Model consumer/Runtime、Secret Model resolve、Project Invocation、持久 Usage 或生产 HTTP/root。Summary 创建初值/Settings 仍待用户决定。没有修改这些产品边界，也不将本库通过扩为完整 D09。

[完整轻量索引](evidence/d09-openai-chat-wire-verification/SHA256SUMS.json)绑定本报告、原件、固定输入、唯一 candidate patch 与独立 probe；原失败不删除，历史索引中的重复源可沿已提交 Git 恢复。本文与证据冻结交主线程提交，三状态页仅同步结果。本次归档没有 Go、Docker、SQL、网络或 Git 写操作。
