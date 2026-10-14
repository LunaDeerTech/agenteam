# 当前工作：M1 live 验收入口准备

`tool-loop-v1` 受控实现增量已随 `3a00e5a6` 推送 main；上一轮六个临时分支、五个工作树及精确跟踪引用已清理，必要证据已保全。正式保留工作区为 `/workspace/agenteam`，恢复先核实际 Git 状态及活动会话，再读 [M1 集成卡](../docs/development/work-items/m1-app-integration.md)、[任务台账](../docs/development/agent-team/tasks.md#当前目标与工作包状态)与[开发计划](../docs/development/development-plan.md#当前开发目标与里程碑)，不恢复已结束的旧临时树。

- 本次验收范围：显式 `tool-loop-v1`、Model JSON 工具调用、同 Execution 多轮、真实 Runner read/write/run、最终 v2 及 UI 解码，复用原 Store/Authority/Registry/DeviceHandler 和停止生命周期。原 `direct-text-v1`、旧 Snapshot 保留，无新增 UI 配置。
- 正常实证 `4ca30370` / `run.EW1axd`：同 work E 五次 Model / 四次 Tool、真实文件/stdout/exit、三次原成果消费、Owner 唯一 transfer 200 → `in_review` / version 4、reload 一致，正常 Runtime Drain → Runner Wait 0 → App/连接 Joined 全部通过并获独立接受，outer Wait 0。独立 review E 两次 Model / 一次 read 成功不代表产物评审决策。
- 已独立接受并有效复用：`33009a96` / `run.Wxxzus` 的 read missing 失败与原 Runtime Stop/Drain 取消；`fee69b0d` / `direct-text.2tWIPp` 的旧 profile 真实单轮及同 DB 关闭 runtime 后重启历史读取。正常、失败/取消和旧兼容各自按固定输入记录，不称当前 HEAD 单次全套运行。
- 外部 DEV_CHAT 因本环境 A 成功 / AAAA NXDOMAIN 被既有 D04 拒绝，未发送外部 Model HTTP，**M1 整里程碑仍待外部模型实证**。不改 DNS、出站或授权策略；无工具 Agent、运行中重启接管、Human 持久取消、完整审批恢复及 M2 仍未完成。
- 正常 harness 的 tick 250ms / skip 240 只是显式约60秒 Owner 窗口，不是生产默认。旧 live-chat FAIL、完整 D09 tools wire/SSE 与 Object/tools/SPA 等 STOP 保留；第九轮 Playwright 正文观测失败不回填 PASS，也不推断 native 正文不完整。

## 当前 live 准备与下一步

现有 `tests/m1-app-workflow/run.sh` 两个 case 使用固定受控 Provider；正常场景固定五次 Model / 四次 Tool，旧 live 入口仅为单轮 no-tools，均不能验收真实模型自主选择的多轮工具链。受控实现已交付不表示本地 live 验收入口准备完成；现阶段仍须补显式 live opt-in 入口及动态事实核验。

新活动分支为 `ai/m1-live-acceptance-20261014`，工作树 `/workspace/agenteam-m1-live-acceptance`，基线 `3a00e5a6`。delivery_path（W5）负责新入口、组合与新树状态，product_recent 负责浏览器/动态事实接缝；按各自唯一写域实施，当前为 WIP，入口尚未完成或验收。新卡与新树 current 由 W5 维护，尚未进入 main；主树这两份状态文档由 documentation_worker 更新，Git 仍仅 root 负责。当前不读取真实 key、不发送外部 HTTP、不改 DNS 或出站策略。

DNS 阻断仅影响实际外部调用，不阻碍本地入口准备。先完成 live 入口与动态事实的必要实现和检查，再在原 DNS/策略条件满足后做有限真实模型实证；M1 整阶段仍待验。

运行入口、配置与固定 case 见 [M1 harness](../tests/m1-app-workflow/README.md)和[后端说明](../docs/development/backend/README.md#配置与命令)。跟踪源码、锁文件、迁移 00091 与这些命令是跨设备恢复来源。Go 为 `/workspace/toolchains/go1.27.1/bin/go`，工具缓存及固定 MinIO、锁定 browser/web 依赖在 `/workspace/shared/agenteam-m1-tools` 等外置目录复用；依赖位置不等于业务资源可复用，每轮仍创建新 nonce/new DB，不连接 onboarding 环境。

必要 native 日志、资源身份和浏览器消费证明/截图保全到主树 `output/ai/m1-delivery-evidence/app-integration/`，保留原轮次目录，排除二进制、缓存与私有材料；delivery_path 已完成逐文件 bytes/SHA 核对，原失败轮一并保留。后续审批/恢复/协作沿既定工作包推进；W4 本轮未实施。旧失败细节只维护于集成卡及 Git 文件历史。

## 已完成交接：统一页面布局

`6d16bdae` 已交付统一前端布局，正式恢复材料为 `web/src/`、[界面规范](../docs/frontend-design/interface-design.md)和 [interface-layout.config.js](../tests/account-captcha-web/interface-layout.config.js) 同前缀 browser 源码/锁文件。本轮主任务已转 M1，不继续旧 project-list 候选。

原 Chromium 55 项按整轮 54 项与修正测试定位后 1 项独立复验组合接受；类型、构建、project-audit 49/49 与限定变更文件格式通过。使用合成 HTTP fixture，只证明前端行为；旧整轮 FAIL 不回填。原31项请求生命周期 unit 失败与17个基线文件格式问题保留，不宣称全局 npm check 全绿。完整历史范围从该提交的 current 与原日志查询，不新增归档。

锁定 Node 24.19.0 / Playwright 1.56.1 / Chromium 151 的原复现入口为 `node tests/account-captcha-web/node_modules/@playwright/test/cli.js test --config=tests/account-captcha-web/interface-layout.config.js`；其余命令与恢复边界见 Git 文件历史。原主树 `output/ai/interface-layout/` 保留简短日志与六张 preview；全部原验证进程已退出，可再生输出不纳入 Git。
