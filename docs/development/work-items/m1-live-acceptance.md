# M1 真实模型验收

## 当前结果：真实外部模型主链通过

2026-10-14，验证源码`2d76554dab983a7c9b91f3e367d6fea5a6fb5c94`、`output/ai/m1-app-integration/run.OtpjqE`，原session41322 Wait0，`TestM1AppLiveWorkflow` 66.76秒PASS。真实Provider沿正式App、环境代理、原Secret和TLS调用，模型自主选择工具并完成同一work Execution的5轮Model/4轮Tool：读取input.json、生成并写入stats.py、实际运行Python、读取result.json。独立oracle验证结果文件、真实stdout/exit与进程退出；原Call/Invocation/Operation/Attempt/result/Usage和后继输入逐项配对，最终v2与终态同事务。

真实浏览器三次消费原工作成果，Owner唯一送审并刷新为in_review，Node原Wait0；原Runner/App/Runtime/Model/Tool owner Join全到。final范围workE1、reviewE1，sent_work5/sent_review1/total6，retry0/failed0；review调用在原Stop/Drain时取消，不能称自主评审决策或M2。原test529185、Runner529291、Node529967与精确3容器/3网络/3目录、两个modules链接已退场。独立最终核验已接受：11条同请求联合消费证明（10条requestfinished，1条ERR_ABORTED但原native完整EOF/零cancel、consumer完成及Go同字节/EOF/Close/handler真实返回全部成立）、原持久事实和资源退出均核齐，见[本轮独验报告](m1-live-proxy-review.md)。

本轮此前三个FAIL保持原事实，不被最终成功覆盖：

| 输入/轮次 | 原Wait与结果 | 已确定边界 |
| --- | --- | --- |
| 14ecc813/run.Qw1kPI | 3906 Wait1，14.92秒 | 1Model sent/1Round/0Tool；通用协议拒绝，未保留原响应正文。 |
| 3e3586c1/run.oTwceC | 34213 Wait1，15.25秒 | 闭集解析诊断确认message唯一未知reasoning_content字符串，随后才做最窄兼容。 |
| ac678146/run.0aXTC5 | 46910 Wait1，174.80秒 | 12sent=3成功Provider Usage+9timeout，3Tool/result、4Rounds；sent12软阈值触发cancelled/incomplete。旧日志无phase/逐attempt耗时，不反推9次均为15秒头等待。 |

三次失败各自原fixture Wait及精确资源清理都已完成。新配置、兼容修复和响应头窗口分别有代码及受控门支持，未对同一无新信息输入盲重试。

## 本轮实现与必要验证

- 新`AGENTEAM_CENTRAL_OUTBOUND_PROXY_ORIGINS`是显式精确HTTPS Origin列表；未配置仍直连。App与Model共享不可变环境代理路由，保留HTTP(S)_PROXY/NO_PROXY/CA与目标TLS/SNI，不把网关IP伪称目标publicIP、不做代理失败直连fallback。直连仍原SystemResolver/地址门。真实KEY只在双opt-in且路由资格通过后读入本轮正式Secret，Runner/Browser不继承DEV_CHAT。
- JSON tools仅兼容可选null/有界UTF8字符串reasoning_content，校验后丢弃，不展示/持久/作为工具指令。其他未知字段、单工具数量、名称/参数、finish、Usage和旧text/SSE硬门保持。配置/解析器作者普通检查及独立race通过。
- owned真实TLS首包延迟16秒验证旧默认15秒timeout与新显式20秒成功；更短parent/Overall、原Close/Drain及CONNECT15秒仍成立。Model非stream显式header只能≤min(Overall,60秒)，JSON tools零header采用该窗口，已有更短显式值保留。其他默认15秒不变。闭集phase只来自真实origin头读取失败；live诊断仅增加attempt/毫秒数字。outbound新5组及adapter新8构造均获独立race接受；组合原67129 Wait0、App integration仅compile。
- 预算300秒/单attempt≤60秒/MaxOutput4096/合计sent12软停止条件保持。observer五纯子例普通/race通过；`caf6022c/output/ai/m1-live-proxy/budget-native.gUujHH`原17521 Wait0，合成计数SQL0.09秒与真实App提前deadline停止/原Join/无后继dispatch5.64秒通过，6资源3目录与原PID精确absent。合成12行不是12真实调用；第三live实际sent12触发另列。该预算动态范围已独立接受，未等满300秒不称真实300秒时钟等待已跑。

复用先前已接受controlled正常链、失败/取消和旧direct-text固定输入证据，不重复无关矩阵。M1真实出口已到达；M2继续暂停，review receipt不冒充审核Agent成果交接/决策，其他旧STOP不由本轮恢复。

## 可恢复运行与证据

运行入口与精确配置见[tests/m1-app-workflow](../../../tests/m1-app-workflow/README.md#显式真实模型入口)。验证使用Go1.27.1、锁定外置依赖、每轮新nonce/newDB及实际Runner；root负责所有Git，源码与必要harness已固定保存。正常部署仍需显式tool-loop-v1，产品UI无需选择内部profile。

必要原始日志、owned资源身份、safe计数、浏览器逐请求完整消费证明与截图按白名单保全到主树`output/ai/m1-delivery-evidence/proxy-live/`；`app/manifest.json`记录42文件417222bytes的来源/逐文件SHA256，outbound作者与独验日志另有子清单，总索引`manifest.json`。不复制binary/cache、Session/Enrollment/KEY或原Provider响应。此前受控/准备材料仍在相邻app-integration与live-preparation目录。当前无活动业务资源或消费者，M2树不在本轮清理范围。

## 先前准备交付

基线 main `3a00e5a6`；分支 `ai/m1-live-acceptance-20261014`。本卡交付显式 live 验收入口的本地准备能力，不回填已接受受控链为真实模型实证。

## 先前准备阶段的范围与归属

复用原 M1 App/Runner/Owner 浏览器链。delivery_path 编写新 live 入口、必要 factory 参数化和脚本；live_facts 独占新动态事实文件；product_recent 独占浏览器模式接缝。生产 Model/Execution/Tool/DNS 不变，root 负责 Git，macro 独立核验。

live 必须显式选择 case 和测试 opt-in；先核非秘密 metadata 与原 SystemResolver/D04 DNS，再允许实际选择的运行读取 KEY 并走正式 Secret。保持系统 CA、真实 resolver、原出站 policy，不设 ownedIP 替身、预设模型响应或合成凭据。准备阶段只用假值，不发外部 HTTP。

总300秒、单attempt最多60秒、MaxOutput4096；work/review/retry 全部 sent 观察阈值12是含在途软停止条件，不是硬调用/计费上限。超限停止原 Runtime、Drain、根取消并实际 Join；独立退出期限30秒。原 controlled 两个选择器保持其输入和语义。

## 先前准备阶段的动态验收目标

真实模型从捕获 Task 目标与文件说明自行决定调用，在同一 work Execution 至少两轮工具交互，真实读 input.json、写 stats.py、运行程序、读 result.json；独立核实际文件/JSON语义/stdout/exit、原 Round/Call/Operation/Usage/最终 v2，同原 Owner 入口送审并刷新。Task Plan 确实由 agentloop/projection.go 投影首轮 user Message，未以预设程序或最终答案替代目标。review receipt 不等于自主评审决策。

## 先前准备阶段的证据

源码准备已落：显式live入口、保留受控factory、独立动态facts、浏览器controlled/live预期、脚本双门。原 SystemResolver、CA、policy 不变；真实KEY读取仅存在于明确live且预检合格后的正式Secret种子。

作者检查：App integration整体编译及 `^TestM1LivePreparation$` 假值13子例实际 Wait0（0.010s），日志 `output/ai/m1-live-acceptance/preparation.log`；包括缺opt-in/不合格URL/model、DNS两族失败/混私网/空结果拒绝及MaxOutput正式TokenCount十进制字符串DTO。脚本缺opt-in以全部fake路径提前exit1，未启动构建/栈，日志 `script-opt-in-rejection.log`。精确Vite dist重建Wait0（1.22s），未重复前端矩阵，临时modules链接已清理。

当时新预算observer仅静态核计数/触发/Stop/Drain/根cancel顺序，未动态等到300秒或sent12触发；原Stop/Drain能力可引用既有33009证据，但二者不合称新observer动态PASS。动态facts尚未对真实Provider执行；准备通过不代表live通过。最后精确attempt绑定增量已随冻结 `93306a74` 的受控normal脚本再编译。该轮 `output/ai/m1-app-integration/run.OyaNNV` 实际原Wait0，`TestM1AppToolWorkflow` 15.32秒：原work E五Model/四Tool、实际文件/stdout/exit、三次原成果消费、唯一Owner transfer进入in_review并刷新、原Runtime/Runner/App Join和work5/review2 peer归属全部到达。真实review receipt不代表自主评审决策。

按本轮owned-resources精确核三容器/三网络均absent，三临时目录、原测试/Runner/Node PID及本轮两个node_modules链接均已消失。旧negative/directText输入保持原分支，不重跑已接受范围。独立准备检查也已实际Wait0：13fake子例通过，未启用live分支实际SKIP（明确不计live通过）；日志 `output/ai/m1-independent-preparation/preparation.log`。macro 已限定接受冻结 `93306a74` 准备门：新源码静审无剩余must-fix，13fake子例及未启live实际SKIP/compile0，受控 `run.OyaNNV` 源码/日志/同请求消费证明/精确资源退场独核通过。动态live facts与新预算触发仍只静审，未跑；不得写live或M1整阶段通过。

本轮必要25文件/266709bytes已按白名单保全到主树 `output/ai/m1-delivery-evidence/live-preparation/`，清单 `manifest.json` 保存来源及逐文件SHA256/bytes且复制后全匹配。排除binary/dist/cache和私有Session/凭据；该准备轮完成时无活动命令或业务资源。原受控/失败取消/旧directText的已接受证据见 [M1集成卡](m1-app-integration.md)，不重复运行无关矩阵。外部DNS仍待正常条件恢复，不改解析器/协议/策略绕过。

2026-10-14 05:50:30 UTC 仅复核连接资格：非秘密 BASE/MODEL 合法，本环境 `/etc/resolv.conf` 原唯一 resolver 的 A、AAAA 均为 RCODE 3 / NXDOMAIN / answer 0；此前 A 成功 / AAAA NXDOMAIN 是 05:14 的旧观察。原 D04 仍 blocked；KEY 从命令环境剔除且未读取值，未发 Model HTTP、未启栈、未改 DNS/Policy/配置文件，仅记录本环境 resolver 事实，不推断服务权威端点故障。
