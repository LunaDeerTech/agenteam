# M1 真实模型验收入口准备

基线 main `3a00e5a6`；分支 `ai/m1-live-acceptance-20261014`。本卡为显式 live 验收能力的 WIP，不回填已接受受控链为真实模型实证。

## 范围与归属

复用原 M1 App/Runner/Owner 浏览器链。delivery_path 编写新 live 入口、必要 factory 参数化和脚本；live_facts 独占新动态事实文件；product_recent 独占浏览器模式接缝。生产 Model/Execution/Tool/DNS 不变，root 负责 Git，macro 独立核验。

live 必须显式选择 case 和测试 opt-in；先核非秘密 metadata 与原 SystemResolver/D04 DNS，再允许实际选择的运行读取 KEY 并走正式 Secret。保持系统 CA、真实 resolver、原出站 policy，不设 ownedIP 替身、预设模型响应或合成凭据。准备阶段只用假值，不发外部 HTTP。

总300秒、单attempt最多60秒、MaxOutput4096；work/review/retry 全部 sent 观察阈值12是含在途软停止条件，不是硬调用/计费上限。超限停止原 Runtime、Drain、根取消并实际 Join；独立退出期限30秒。原 controlled 两个选择器保持其输入和语义。

## 动态验收目标

真实模型从捕获 Task 目标与文件说明自行决定调用，在同一 work Execution 至少两轮工具交互，真实读 input.json、写 stats.py、运行程序、读 result.json；独立核实际文件/JSON语义/stdout/exit、原 Round/Call/Operation/Usage/最终 v2，同原 Owner 入口送审并刷新。Task Plan 确实由 agentloop/projection.go 投影首轮 user Message，未以预设程序或最终答案替代目标。review receipt 不等于自主评审决策。

## 当前证据

浏览器受控/live模式片已静检并冻结。主入口/factory 与动态facts 尚在实现，未整体编译、未live实跑。原受控/失败取消/旧directText的已接受证据见 [M1集成卡](m1-app-integration.md)，不重复运行无关矩阵。外部DNS仍待正常条件恢复，不改解析器/协议/策略绕过。
