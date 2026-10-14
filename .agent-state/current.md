# 当前工作：M1 真实外部模型验收入口准备

基线为正式 main `3a00e5a6`；当前隔离树 `/workspace/agenteam-m1-live-acceptance`、分支 `ai/m1-live-acceptance-20261014`，root 负责全部 Git。旧分支/工作树已清理，不恢复。当前仅准备源码，**未执行外部 Model HTTP，未读取真实 KEY**。

- delivery_path 唯一写 live 入口、公共 factory 必要参数化、run.sh/README 与本卡；live_facts 唯一写新动态事实文件；product_recent 唯一写原 browser helper/spec；macro 保持独立。
- 受控 workflow 与 failure-and-cancellation 的既有通过证据保留。新 live 显式双 opt-in，真实 SystemResolver/CA/D04/正式 Secret 路径；不使用 ownedIP 映射、预设响应或 synthetic credential。
- 本轮预算目标：总300秒、单attempt最多60秒、MaxOutput4096、work/review/retry 合计 sent 观察阈值12（含在途的软停止条件，不是硬计费上限）；超限停止原Runtime并收集原Join。
- 首轮真实上下文已源码确认：agentloop/projection.go 将完整捕获 Task.Data()（含Plan）投影至 user Message，Agent instructions/mount IDs 保留。新目标只提供任务/输入说明，不提供程序/答案。
- Product模式及安全诊断冻结；live入口/factory/script和动态facts已落，App integration编译+fake配置13例 Wait0，缺opt-in脚本拒绝exit1；新预算仅静审，真实外部未跑。最后attempt绑定增量随受控normal再编译；当前准备片待最终保存和独验，受控回归未跑。当前DNS仍是本环境A成功/AAAA NXDOMAIN导致原D04阻断；只阻碍外部实跑，不阻碍本地准备。

下一步：冻结准备片，独立复核并跑共享factory/browser的单次controlled正常回归；合格preflight及明确选定实跑后方可读取KEY并做外部实证。详见 [live准备卡](../docs/development/work-items/m1-live-acceptance.md)。

---

# 当前工作：工具执行增量交付与外部模型待验

本次 `tool-loop-v1` 受控实现验收完成，由 root 执行 main 推送与临时分支/worktree 清理；此记录不提前宣称推送成功。正式保留工作区为 `/workspace/agenteam`，恢复先核实际 Git 状态及活动会话，再读 [M1 集成卡](../docs/development/work-items/m1-app-integration.md)、[任务台账](../docs/development/agent-team/tasks.md#当前目标与工作包状态)与[开发计划](../docs/development/development-plan.md#当前开发目标与里程碑)，不依赖已结束执行者或旧临时树。

- 本次验收范围：显式 `tool-loop-v1`、Model JSON 工具调用、同 Execution 多轮、真实 Runner read/write/run、最终 v2 及 UI 解码，复用原 Store/Authority/Registry/DeviceHandler 和停止生命周期。原 `direct-text-v1`、旧 Snapshot 保留，无新增 UI 配置。
- 正常实证 `4ca30370` / `run.EW1axd`：同 work E 五次 Model / 四次 Tool、真实文件/stdout/exit、三次原成果消费、Owner 唯一 transfer 200 → `in_review` / version 4、reload 一致，正常 Runtime Drain → Runner Wait 0 → App/连接 Joined 全部通过并获独立接受，outer Wait 0。独立 review E 两次 Model / 一次 read 成功不代表产物评审决策。
- 已独立接受并有效复用：`33009a96` / `run.Wxxzus` 的 read missing 失败与原 Runtime Stop/Drain 取消；`fee69b0d` / `direct-text.2tWIPp` 的旧 profile 真实单轮及同 DB 关闭 runtime 后重启历史读取。正常、失败/取消和旧兼容各自按固定输入记录，不称当前 HEAD 单次全套运行。
- 外部 DEV_CHAT 因本环境 A 成功 / AAAA NXDOMAIN 被既有 D04 拒绝，未发送外部 Model HTTP，**M1 整里程碑仍待外部模型实证**。不改 DNS、出站或授权策略；无工具 Agent、运行中重启接管、Human 持久取消、完整审批恢复及 M2 仍未完成。
- 正常 harness 的 tick 250ms / skip 240 只是显式约60秒 Owner 窗口，不是生产默认。旧 live-chat FAIL、完整 D09 tools wire/SSE 与 Object/tools/SPA 等 STOP 保留；第九轮 Playwright 正文观测失败不回填 PASS，也不推断 native 正文不完整。

## 恢复与下一步

运行入口、配置与固定 case 见 [M1 harness](../tests/m1-app-workflow/README.md)和[后端说明](../docs/development/backend/README.md#配置与命令)。跟踪源码、锁文件、迁移 00091 与这些命令是跨设备恢复来源。Go 为 `/workspace/toolchains/go1.27.1/bin/go`，工具缓存及固定 MinIO、锁定 browser/web 依赖在 `/workspace/shared/agenteam-m1-tools` 等外置目录复用；依赖位置不等于业务资源可复用，每轮仍创建新 nonce/new DB，不连接 onboarding 环境。

必要 native 日志、资源身份和浏览器消费证明/截图保全到主树 `output/ai/m1-delivery-evidence/app-integration/`，保留原轮次目录，排除二进制、缓存与私有材料；delivery_path 已完成逐文件 bytes/SHA 核对，原失败轮一并保留。下一步恢复外部模型在原 DNS/策略条件下的可调用环境并做有限实证，再按既定工作包推进审批/恢复/协作；W4 本轮未实施。旧失败细节只维护于集成卡及 Git 文件历史。

## 已完成交接：统一页面布局

`6d16bdae` 已交付统一前端布局，正式恢复材料为 `web/src/`、[界面规范](../docs/frontend-design/interface-design.md)和 [interface-layout.config.js](../tests/account-captcha-web/interface-layout.config.js) 同前缀 browser 源码/锁文件。本轮主任务已转 M1，不继续旧 project-list 候选。

原 Chromium 55 项按整轮 54 项与修正测试定位后 1 项独立复验组合接受；类型、构建、project-audit 49/49 与限定变更文件格式通过。使用合成 HTTP fixture，只证明前端行为；旧整轮 FAIL 不回填。原31项请求生命周期 unit 失败与17个基线文件格式问题保留，不宣称全局 npm check 全绿。完整历史范围从该提交的 current 与原日志查询，不新增归档。

锁定 Node 24.19.0 / Playwright 1.56.1 / Chromium 151 的原复现入口为 `node tests/account-captcha-web/node_modules/@playwright/test/cli.js test --config=tests/account-captcha-web/interface-layout.config.js`；其余命令与恢复边界见 Git 文件历史。原主树 `output/ai/interface-layout/` 保留简短日志与六张 preview；全部原验证进程已退出，可再生输出不纳入 Git。
