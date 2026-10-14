# M1 真实模型验收入口准备

基线 main `3a00e5a6`；分支 `ai/m1-live-acceptance-20261014`。本卡交付显式 live 验收入口的本地准备能力，不回填已接受受控链为真实模型实证。

## 范围与归属

复用原 M1 App/Runner/Owner 浏览器链。delivery_path 编写新 live 入口、必要 factory 参数化和脚本；live_facts 独占新动态事实文件；product_recent 独占浏览器模式接缝。生产 Model/Execution/Tool/DNS 不变，root 负责 Git，macro 独立核验。

live 必须显式选择 case 和测试 opt-in；先核非秘密 metadata 与原 SystemResolver/D04 DNS，再允许实际选择的运行读取 KEY 并走正式 Secret。保持系统 CA、真实 resolver、原出站 policy，不设 ownedIP 替身、预设模型响应或合成凭据。准备阶段只用假值，不发外部 HTTP。

总300秒、单attempt最多60秒、MaxOutput4096；work/review/retry 全部 sent 观察阈值12是含在途软停止条件，不是硬调用/计费上限。超限停止原 Runtime、Drain、根取消并实际 Join；独立退出期限30秒。原 controlled 两个选择器保持其输入和语义。

## 动态验收目标

真实模型从捕获 Task 目标与文件说明自行决定调用，在同一 work Execution 至少两轮工具交互，真实读 input.json、写 stats.py、运行程序、读 result.json；独立核实际文件/JSON语义/stdout/exit、原 Round/Call/Operation/Usage/最终 v2，同原 Owner 入口送审并刷新。Task Plan 确实由 agentloop/projection.go 投影首轮 user Message，未以预设程序或最终答案替代目标。review receipt 不等于自主评审决策。

## 当前证据

源码准备已落：显式live入口、保留受控factory、独立动态facts、浏览器controlled/live预期、脚本双门。原 SystemResolver、CA、policy 不变；真实KEY读取仅存在于明确live且预检合格后的正式Secret种子。

作者检查：App integration整体编译及 `^TestM1LivePreparation$` 假值13子例实际 Wait0（0.010s），日志 `output/ai/m1-live-acceptance/preparation.log`；包括缺opt-in/不合格URL/model、DNS两族失败/混私网/空结果拒绝及MaxOutput正式TokenCount十进制字符串DTO。脚本缺opt-in以全部fake路径提前exit1，未启动构建/栈，日志 `script-opt-in-rejection.log`。精确Vite dist重建Wait0（1.22s），未重复前端矩阵，临时modules链接已清理。

新预算observer当前仅静态核计数/触发/Stop/Drain/根cancel顺序，未动态等到300秒或sent12触发；原Stop/Drain能力可引用既有33009证据，但二者不合称新observer动态PASS。动态facts尚未对真实Provider执行；准备通过不代表live通过。最后精确attempt绑定增量已随冻结 `93306a74` 的受控normal脚本再编译。该轮 `output/ai/m1-app-integration/run.OyaNNV` 实际原Wait0，`TestM1AppToolWorkflow` 15.32秒：原work E五Model/四Tool、实际文件/stdout/exit、三次原成果消费、唯一Owner transfer进入in_review并刷新、原Runtime/Runner/App Join和work5/review2 peer归属全部到达。真实review receipt不代表自主评审决策。

按本轮owned-resources精确核三容器/三网络均absent，三临时目录、原测试/Runner/Node PID及本轮两个node_modules链接均已消失。旧negative/directText输入保持原分支，不重跑已接受范围。独立准备检查也已实际Wait0：13fake子例通过，未启用live分支实际SKIP（明确不计live通过）；日志 `output/ai/m1-independent-preparation/preparation.log`。macro 已限定接受冻结 `93306a74` 准备门：新源码静审无剩余must-fix，13fake子例及未启live实际SKIP/compile0，受控 `run.OyaNNV` 源码/日志/同请求消费证明/精确资源退场独核通过。动态live facts与新预算触发仍只静审，未跑；不得写live或M1整阶段通过。

本轮必要25文件/266709bytes已按白名单保全到主树 `output/ai/m1-delivery-evidence/live-preparation/`，清单 `manifest.json` 保存来源及逐文件SHA256/bytes且复制后全匹配。排除binary/dist/cache和私有Session/凭据；当前无活动命令或业务资源。原受控/失败取消/旧directText的已接受证据见 [M1集成卡](m1-app-integration.md)，不重复运行无关矩阵。外部DNS仍待正常条件恢复，不改解析器/协议/策略绕过。

2026-10-14 05:50:30 UTC 仅复核连接资格：非秘密 BASE/MODEL 合法，本环境 `/etc/resolv.conf` 原唯一 resolver 的 A、AAAA 均为 RCODE 3 / NXDOMAIN / answer 0；此前 A 成功 / AAAA NXDOMAIN 是 05:14 的旧观察。原 D04 仍 blocked；KEY 从命令环境剔除且未读取值，未发 Model HTTP、未启栈、未改 DNS/Policy/配置文件，仅记录本环境 resolver 事实，不推断服务权威端点故障。
