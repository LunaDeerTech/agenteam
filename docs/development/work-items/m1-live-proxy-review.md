# M1 可信代理与真实模型补验：独立审查

审查者未参与产品、接线或测试实现。代理传输、装配、JSON tools 兼容、响应头及预算补验已限定接受；**第四轮真实模型工作流通过独立证据核验，满足当前既定 M1 正式交付出口**。本报告不恢复旧 Tools STOP 驱动，不涉及 M2。

## 固定输入与结论

- 代理核心初片 `e971ea92f13236b4a3b23bbbe09d7f4d01ac8e1f`，最终独验输入 `8eaed7b76fe894a6d8df218702a7476a6267bc3c`，树 `/workspace/agenteam-m1-outbound-proxy`。最终产品差量仅将 CONNECT 设置读期限失败按原 context 映射取消原因；新测试与正式信任边界说明同时纳入。
- App/Config/Model adapter/live 资格接缝静审绑定 `14ecc81303770bd8d4a2422da27f06895998d5cb`，树 `/workspace/agenteam-m1-live-proxy`。后续活动修改不在本结论内。
- `3766b1432309f31067c661e3ccfc8323eecc4c54` 相对 `3e3586c1` 的 JSON tools 兼容修复与预算 hook 抽取已作下述限定静审；parser/config 后在相同生产源码的 `ac678146` 完成独立定向 race。预算后续以 `caf6022c` 的真实 PG/App 结果完成独立证据核验，范围见下。
- 上述范围未发现剩余确定 must-fix。代理基础风险门已获独立真实本地 socket 验证，可用于继续用户已授权的真实 live 验收；不代表外部 Model 协议、动态工具选择或 M1 整阶段通过。

## 已核边界

精确 HTTPS Origin 名单和标准环境选路在组合根构造时固定；调用者不能改名单、代理或 TLS。默认空名单保持原 direct，NO_PROXY 按捕获选择走 direct；已选代理失败不回退。拒绝 proxy userinfo，不向 CONNECT 添加业务凭据、query 或 body；目标 TLS 保持原 hostname、系统/部署 CA 与最低 TLS 1.2。仅实际业务首写正字节标记 sent，CONNECT/TLS 不计 Model sent，已发送后不内部重发。

网关承担目标 DNS/远端访问控制，客户端验证实际网关 peer、精确目标授权与目标 TLS。`trusted_proxy` 明确区别于目标 IP 分类，不能声称验证了网关背后的完整 A/AAAA 或旧目标 CIDR 门。当前策略不可用仍拒绝业务首写；代理名单更新需重建组合。正式依据为 [出站说明](../backend/outbound.md#显式可信部署代理)及 [D04 契约](d04-security-design.md)。

`14ecc813` 将同一不可变 routes 传给 App client、chat adapter 及 embedding adapter 的 client 创建；没有普通 HTTP 旁路。live 仅授权本次配置 Origin，保留正式 Secret、真实模型输入和原动态成果/Owner/预算断言；代理资格预检不再错误要求本地目标 DNS，但也不声称已证明连通。Runner/浏览器原凭据排除边界保留。该静审不宣称 embedding 产品全链已绑定或验收。

## 实际独立验证

2026-10-14 在最终代理树使用 Go 1.27.1、`GOMAXPROCS=2`、`-p=2 -mod=readonly -race -count=1`，仅运行 `./internal/central/outbound` 的下列 9 个顶层用例。DEV_CHAT 三项与 live 选择从测试子进程环境剔除；测试仅用自身 loopback 网关、生成证书和标记凭据，不发外部 HTTP、不启 PG/native 栈。

| 定向用例 | 实际区分的行为 |
| --- | --- |
| `TestEnvironmentProxyRoutesExactOriginAndSnapshot`、`TestEnvironmentProxyRoutesRejectInvalidConfiguration` | 精确名单、环境快照不可变、非法/携凭据配置拒绝 |
| `TestProxyConnectTLSAndSameOriginCredentials` | 真实 CONNECT、目标 TLS/SNI、业务凭据仅目标可见、同源 redirect |
| `TestProxyTLSFailuresSendNoBusinessRequest` | 错 hostname/CA 拒绝，零业务发送、无 direct 回退、原 socket 关闭 |
| `TestProxyConnectFailureAndResponseBudget` | 407/畸形/超头拒绝，sent=false、仅一次网关连接、无额外数据或回退 |
| `TestProxyCancellationClosesConnectAndTLSAndDrains` | CONNECT/TLS barrier 下取消或 ForceClose，原 Do 返回 cancelled、sent=false、网关关闭及 Drain 完成 |
| `TestProxyFirstBusinessWriteRechecksCurrentPolicy` | 隧道完成后策略不可用仍阻止首笔业务写入 |
| `TestProxySentRequestDoesNotRetryAfterResponseDisconnect` | 服务器已收请求后断响应，sent=true 且实际请求仅一次 |
| `TestProxyNoProxyRetainsDirectAddressPolicy` | NO_PROXY 走原 DNS/策略，直连 loopback 仍拒绝且无代理连接 |

原工具 session `76607` **Wait/exit 0**，包耗时 **1.162s**；9 个顶层用例、20 个子例全部通过，0 fail、0 skip，无 race 报告。原始 JSON 日志：`/workspace/agenteam-m1-outbound-proxy/output/ai/m1-proxy-independent/targeted-race.jsonl`。测试清理等待自身 gateway listener/handler 与 client ForceClose/Drain；取消子例额外等待原 Do 和 server socket 关闭，不用事后无进程观察替代原返回。

复用作者同冻结输入的 HTTPS gateway 三子例、完整 outbound race（两包 7.785s/3.152s）及 vet；没有机械重复全矩阵。作者此前全包 `package-test.log` 中 CONNECT 取消分类 FAIL 保留：修复前设置读期限与取消竞争被错分 InternalError；后续 `cancel-regression.log` 的四子例各 30 次及 `package-race.log` 通过不回填旧失败。独验本次也实际覆盖该四子例。作者 HTTPS 证据只证明自身范围，不写成独验执行。

## 3766b143 兼容差量静审

`adapter/errors.go:424` 仅在非 streaming 且具有 tools policy 时允许已确认的 `reasoning_content`：缺省/null 或不超过原 `maxTextBytes` 的有效 UTF-8 字符串；值验证后丢弃，不作为可见内容、工具指令或诊断正文。外层重复 key/Unicode、其他未知键、assistant role、choice/tool/name/arguments/finish/usage 门保持；旧 text/SSE 范围不接受新键。诊断仍是闭集阶段码。新增定向测试源码核对无放宽，未发现确定 must-fix。

独立原命令 session `34414` **Wait/exit 0**：同 Go/环境限制，对 `./internal/central/model/adapter ./internal/central/config` 执行 `-race -p=2 -mod=readonly -count=1 -run '^(TestJSONTools.*|TestOutboundProxyOrigins)$'`。adapter 7 顶层/22 子例通过（2.156s），config 1 顶层/17 子例通过（1.146s），均 0 fail、0 skip；包括辅助值丢弃、非法类型/Unicode、其他未知键、旧 text 拒绝、原工具参数/usage 及配置不可变/非法项拒绝。日志为本树 `output/ai/m1-proxy-independent/parser-config-race.jsonl`；未发外部请求、未启 native。该解析器/配置限定动态 PASS 不代替实际 Provider 全链。

同提交 `m1_live_workflow_test.go` 抽取原 observer 判定及停止 hook，原 project 全部 sent SQL、1 秒观测期限、deadline/阈值/观察失败三种原因及 StopAdmission→30 秒 Drain→原根停止顺序不变。抽取本身不构成预算触发的动态证据。

预算单文件后续冻结 `ac67814634c08f72526a08d4ebae34c808f3533d`，限定静审无 must-fix：5 个纯子例调用原 observer；真实 PG 计数测试明确使用合成记账行，复用原 SQL 验 project/sent 过滤及 work/review/retry 累计，不冒充模型调用；真实 App 用例在原 command 启动 barrier 后调用同一 budget owner，要求 incomplete、原 owners/child/group 退出、pending bytes 保真、无新增 dispatch 及 Runner 原 Wait/identity 释放。作者纯测 `15984` Wait 0 及原日志中的 race 1.049s 仅作作者执行证据；本角色随后独核下述两条真实 PG/App 用例的原结果与断言，不重复起栈。

## 真实 M1 第三轮：未通过

固定 `ac678146`，证据 `output/ai/m1-app-integration/run.0aXTC5/native.log` 与 `owned-resources.json`。W2 原 session `46910` Wait 1，原日志记录测试 PID `512979` actual_wait code 1、174.80s。代理资格与 TLS 已通过；持久诊断共有 12 次真实 sent，其中 3 次 succeeded 且 usage 来自 provider、9 次 timeout/`wire_transport_error` 且 usage unknown。已产生 3 个成功 Tool/result、4 rounds，随后 `sent_stop_threshold` 使原 task/work Execution cancelled，live 明确 incomplete。

这不是合成 12 次记账，也不是 12 次成功生成。该轮未进入 browser/Owner 送审，动态最终产物与完整 v2 成功门未到；日志未给出三次工具的具体种类，不据此宣称 read/write/run 成果齐全。首轮协议失败及第二轮已知 `reasoning_content` 失败仍保留各轮原 FAIL；第三轮部分推进不回填旧结果。后续 header 超时修改尚未纳入本审查。

独立核对该轮原 Runner `513086` cleanup Wait 已返回；源码该失败清理路径等待原 `cmd.Wait`，日志不单独证明 Runner exit 0。原 D03/D04/D05 cleanup 记录齐；对 owned 清单逐个 inspect，3 containers、3 networks 均明确不存在，3 个临时目录及原测试/Runner PID 不存在，两处临时 modules link 均已去除。上述关闭事实不能代替成功工作流的原 Join/成果验收。

## 预算补验：限定接受

最终固定 `caf6022c`；仅在 `ac678146` 预算测试上增加失败清理时先 Drain 原 Runtime 的保障。证据 `output/ai/m1-live-proxy/budget-native.gUujHH/native.log` 与 `owned-resources.json`，W2 原 session `17521` Wait 0，日志原测试 PID `519724` actual_wait code 0。独立核对冻结源断言、原日志及资源，不将作者执行写成独验重跑。

- `TestM1LiveBudgetSentCount` 0.09s PASS：真实 PG 执行原 `m1LiveSentQuery`，输入是合成简化记账关系；从 work 5/review 4/retry 2 的 11 增至 review 5 的 12，排除其他 project 与 not_sent，实际原 observer 判定 threshold，关闭原查询连接后判定 observation_failed。**没有 12 次外部请求，不证明完整 Model schema 或调用链。**
- `TestM1AppLiveBudgetStop` 5.64s PASS：原 App、Runner 已有实际 command 运行及 pidfd barrier，向同一 budget owner 输入本地 deadline，实际经过 StopAdmission→Drain→原根返回。断言原 Runtime/Model/Tool/App owners Joined、原 child/group 消失、原 pending bytes 保真、cancelled/incomplete 与终态同 Tx、前后 Execution/dispatch/sent/tool 数量不变、peer 仅 1；原 Runner `519882` Wait 0、drained shutdown、WSS 退出及 identity lock 释放。使用受控 provider，**external_model=false**。

原五个纯子例区分 deadline、11→12/已在途超过阈值、观察失败与正常 stop，配合上述同 observer 的真实关闭，已满足原准备卡预算缺口的最小动态补验；无需为证明计数耗尽额外外部调用或硬等 300 秒。第三轮真实 live 又实际触发 sent 阈值并取消，但不把该失败当成功验收。预算仍为含 work/review/retry 的观察停止阈值，不是严格费用上限。

预算原三层 fixture cleanup 齐；独立逐个 inspect 该轮 3 containers、3 networks 均明确不存在，3 个目录及 PID `519724`/`519882` 不存在。原 `93306a74` 准备接受与未变受控/失败取消证据继续复用。截至该预算补验，真实 Model 动态工作、成果、Owner 送审/刷新及成功路径退出仍待通过；预算专项本身不构成 M1 PASS，后续结果见下。

## 响应头预算差量：限定接受

固定 outbound `e05393812c7556bdc1eaba91ed5eeabed2307b02` 及 W2 冻结的 `openai_chat.go`、`transport.go`、`tools_response_budget_test.go`、`m1_live_facts_test.go` 四文件，最终组合保存为 `2d76554dab983a7c9b91f3e367d6fea5a6fb5c94`。未发现确定 must-fix；本段不包括后继修改或真实 live 成功。

只有非流式 Model 显式大于 15s 的响应头配置可扩大到原显式 Overall/60s 以内；默认与其他 consumer/SSE/SMTP 保持原 15s，代理 CONNECT 另取最多 15s。origin 响应头的实际 deadline 仍取原 request context 较早值，原取消、socket 关闭、sent 后不重发及 Drain 路径不变。adapter 仅 JSON tools 且配置为零时补 `min(Overall,60s)`，保留显式短值；不扩原 attempt、retry 或 live 总预算。

`Decision.Phase` 仅允许空值或 `response_headers`，只有实际 origin `readHeaderBlock` 失败处赋后者，CONNECT/TLS/body 不借用；JSON tools 仅将该 phase 的 timeout 映射为闭集 `wire_response_headers_timeout`，不按耗时猜测阶段。新增 harness 诊断只投影持久 attempt/elapsed 数字，不带原错误、正文、目标路径或凭据。正式两份出站说明与此限制一致。作者新 5 组 owned socket 与其差量 race/vet 仅作作者证据，独立动态结果如下。

独立原 session `34863` **Wait/exit 0**：同 Go/环境限制，对 outbound 新 5 顶层/18 子例及 adapter `TestJSONToolsResponseHeaderBudget` 的 1 顶层/8 子例执行 `-race -p=2 -mod=readonly -count=1`，包耗时 17.323s/1.015s，0 fail、0 skip。真实自身 gateway/socket/目标 TLS 的同轮 16s 响应中，默认配置实际 15s 超时且 sent/phase 正确，显式 20s 完整成功；100ms parent/Overall 限制仍关闭原 socket 并 Drain，CONNECT 错误没有业务 phase、无 direct 回退或 sent 后重发。资格矩阵、闭集诊断与 adapter 短值/上限/非法构造门均通过。原日志：本树 `output/ai/m1-proxy-independent/response-headers-race.jsonl`；未启 native 或读取真实凭据，未发外部 HTTP。原代理、预算与未变生命周期证据复用；此通过允许继续真实 live，不能回填第三轮 timeout 的具体阶段或宣称 M1 成功。

## 第四轮真实 M1：正式出口接受

固定 `2d76554dab983a7c9b91f3e367d6fea5a6fb5c94`，证据位于 `output/ai/m1-app-integration/run.OtpjqE/`。W2 原 session `41322` Wait 0；独立核对 `native.log` 的原测试 PID `529185` actual_wait code 0、66.76s，冻结的 live factory/facts/browser 断言及实际联合证明文件。本角色只读消费本轮结果，没有重跑 native、模型或 Go。

- **真实工作与产物：** live 分支使用显式目标授权、配置的真实 Model/Secret 与原出站 owner，未装配预设 WireSequence/fixture resolver；TLS 验证保留。原 task/work 只有 1 个 Execution，5 次成功 Model 调用/5 rounds，均 attempt 1 且 provider Usage 与原响应身份一致。4 个原 Tool/result 精确配对，分别完成输入读取、模型生成 `stats.py` 的真实写入、Python 命令执行及结果回读；下一轮 Messages 等于此前模型响应与实际 ToolResult 的追加。实际 `result.json` 和 stdout 的 count/sum/mean 为 4/20/5，命令 exit 0、输出完整、原进程组已确认终止；无重复 operation 或副作用重发。最终 `durable_final_text_v2`、末轮和 Execution 的版本/完成时间/原 Tx 一致。
- **成果与 Owner 送审：** Node `529967` 原 Wait 成功；实际 observation 为 live、3 次输出读取、唯一 1 次 API 写入，初始进行中、成果查看不改变 Task、显式 Owner 确认送审、刷新保持 `in_review` 四阶段齐全。独立比较 3 份实际 v2 响应完全一致及唯一 POST receipt；原 Go 再读确认状态/版本恰好推进一次、既有完成输出不变。执行成功未被当作 Task 完成。
- **浏览器完整性：** `m1-complete-response-proofs.json` 的 11 份联合证明全部完成：10 个 `requestfinished`、**1 个 `requestfailed/net::ERR_ABORTED`，该标签保留**。后者与其他请求一样，须同 Request ID 的 native EOF、零 reader/stream cancel/abort、公有 consumer Promise 实际 fulfilled、Go 原 body EOF/Close/handler 返回、正文长度与 SHA-256 一致。独立逐项核对这些实际记录及 typed 摘要；相关 host/方法/URL 身份按原 helper 分别验证，不用 CDP 终态替代完整消费。wire observer 已 retired 且原 CDP detach 完成。
- **审核与收尾边界：** 送审后另有 1 个 reviewer Execution，真实 sent 1 次，在正常原 Runtime Stop/Drain 收尾时 cancelled；总 sent 6、retry 0、failed 0、cancelled 1，最终无 unknown/active 调用。它不代表自主审核决策、审核通过或 Task 完成。原 Runner `529291` Wait 0，原 Runtime/Model/Tool/App owners Joined，Node/原测试均已返回；实际 D03/D04/D05 nonce cleanup 齐。独立逐个 inspect owned 清单 3 containers/3 networks 均不存在，3 目录、原测试/Runner/Node PID 及两处临时 modules link 均不存在。

依据[当前 M1 定义](../development-plan.md#当前开发目标与里程碑)，本轮已满足真实 Model 多轮工具工作、至少两轮工具交互、产物可见且可追溯并进入已有评审入口。结合已接受的代理/parser/config/header、预算触发、原受控组合、失败取消与 direct-text 兼容证据，**无剩余确定 must-fix，可按当前 M1 范围正式交付**。前三轮真实 live FAIL 不回填；仅此冻结组合、当前 Provider 配置及任务场景获真实成功证据，不泛化所有 Provider/任务稳定性、M2 审核/恢复、M4 生产交付或全部平台完成。

本次唯一写入为本报告及忽略目录中的独验原始日志；产品、harness 和其他任务记录未修改。正式文档归位与 Git 集成由 root 按该明确范围执行。
