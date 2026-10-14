# 当前工作：M1 live 入口准备通过，实际模型验收待条件恢复

`tool-loop-v1` 受控主功能已随 `3a00e5a6` 推送 main，上一轮六个临时分支、五个工作树及跟踪引用已清理。本次交付包含已通过独立准备门的显式 live 验收入口；实际外部模型尚未调用，**M1 整目标未完成**。正式保留工作区为 `/workspace/agenteam`，恢复先核实际 Git 状态和活动会话，再读 [live 准备卡](../docs/development/work-items/m1-live-acceptance.md)、[M1 集成卡](../docs/development/work-items/m1-app-integration.md)和[任务台账](../docs/development/agent-team/tasks.md#当前目标与工作包状态)，不依赖已结束执行者或旧临时树。

## 已通过范围与限制

- 受控主功能：显式 `tool-loop-v1`、Model JSON 工具调用、同 Execution 多轮、真实 Runner read/write/run、v2 最终输出及 UI、Owner 唯一送审与刷新、原完整 Join 已限定接受。原 `direct-text-v1`/旧 Snapshot 保留，无新增 UI 配置；Execution 成功不自动完成 Task，review receipt 不代表自主评审。
- live 准备：`93306a74` 的显式双 opt-in 入口、动态事实和浏览器接缝已通过静审、13 项 fake 准备检查、未启用 live 的明确 SKIP 与编译；受控回归 `run.OyaNNV` 原 Wait 0 / 15.32 秒、三次成果消费/唯一送审/刷新、work5/review2 归属和全部原 Join、资源及链接清理获独立接受。SKIP、fake 检查和受控回归均不计 live PASS。
- 既有 `run.EW1axd` 正常链、`33009a96` 的失败/原 Runtime 取消与 `fee69b0d` 的旧 direct-text 兼容按固定输入有效复用，不称当前 HEAD 单次全套运行。第九轮 Playwright 观测 FAIL 保留，不推断 native 正文不完整。
- 准备已完成，但动态 live facts 尚未消费真实 Provider，新预算 observer 的300秒/合计 sent12触发只经静审，未动态验证。sent12包含在途观察，是软停止条件，不是硬调用/计费上限；配置与实际运行门见 [harness](../tests/m1-app-workflow/README.md#显式真实模型入口准备门通过真实调用未实跑)。
- 当前本环境 A 成功 / AAAA NXDOMAIN 被原 D04 拒绝；没有读取真实 KEY 或发送外部 Model HTTP。DNS 阻断只影响实际调用，已完成的本地入口准备不再记为 WIP。原 resolver、系统 CA、DNS/出站授权策略不改，无 ownedIP 或预设模型响应替代。

## 恢复与下一步

在原 DNS/策略条件满足、明确选择 live 双 opt-in 并通过非秘密预检后，沿正式 Secret 路径执行有限真实模型验收；核自主选工具、真实文件/命令结果、持久 Round/Call/Usage/v2、Owner 送审及完整退出。既定 M1 目标与门槛不变，详见[开发计划](../docs/development/development-plan.md#当前开发目标与里程碑)。无工具 Agent、运行中重启接管、Human 持久取消、完整审批恢复、M2 及 W4 仍按原工作包推进；旧 live-chat FAIL、完整 D09 wire/SSE 与 Object/tools/SPA 等 STOP 保留。

跟踪源码、锁文件、[harness 命令](../tests/m1-app-workflow/README.md)和[后端配置](../docs/development/backend/README.md#配置与命令)是跨设备恢复来源。固定 Go、MinIO 与锁定 browser/web 依赖可从外置工具缓存恢复；每次业务运行仍用新 nonce/new DB，不连接 onboarding。必要日志、资源身份、浏览器消费证明和截图已分别保全在主树 `output/ai/m1-delivery-evidence/app-integration/` 与 `live-preparation/`，逐文件 bytes/SHA 已核，排除二进制、缓存与私有材料；原失败记录保留。

## 已完成交接：统一页面布局

`6d16bdae` 已交付统一前端布局，正式恢复材料为 `web/src/`、[界面规范](../docs/frontend-design/interface-design.md)和 [interface-layout.config.js](../tests/account-captcha-web/interface-layout.config.js) 同前缀 browser 源码/锁文件。本轮主任务已转 M1，不继续旧 project-list 候选。

原 Chromium 55 项按整轮 54 项与修正测试定位后 1 项独立复验组合接受；类型、构建、project-audit 49/49 与限定变更文件格式通过。使用合成 HTTP fixture，只证明前端行为；旧整轮 FAIL 不回填。原31项请求生命周期 unit 失败与17个基线文件格式问题保留，不宣称全局 npm check 全绿。完整历史范围从该提交的 current 与原日志查询，不新增归档。

锁定 Node 24.19.0 / Playwright 1.56.1 / Chromium 151 的原复现入口为 `node tests/account-captcha-web/node_modules/@playwright/test/cli.js test --config=tests/account-captcha-web/interface-layout.config.js`；其余命令与恢复边界见 Git 文件历史。原主树 `output/ai/interface-layout/` 保留简短日志与六张 preview；全部原验证进程已退出，可再生输出不纳入 Git。
