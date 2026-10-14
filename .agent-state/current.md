# 当前工作：M1 App 工具链集成

- 分支 `ai/m1-app-integration-20261014`，工作树 `/workspace/agenteam-m1-app-integration`；起始基线 `c75e1bb8`，最近正常验收输入 `fee69b0d`。root 负责全部 Git；delivery_path 负责 App/harness，documentation_worker 当前只写移交文档。恢复先核 Git 差异、活动 owner 与命令，不继承旧会话写权。
- 目标、当前实际结果与限制见 [M1 集成卡](../docs/development/work-items/m1-app-integration.md)；完整里程碑见[开发计划](../docs/development/development-plan.md#当前开发目标与里程碑)。当前没有 M1 完成或 main 交付结论。
- 已实现显式 `tool-loop-v1`、Model JSON 工具调用、原 Ownership 内多轮循环、真实 Runner read/write/run、最终 v2 与 UI 解码。旧 `direct-text-v1`/Snapshot 保留，无新增 UI 配置；App 复用原 Store、Authority、Registry、DeviceHandler 和停止生命周期。
- 自有受控链已完成同一 work Execution 的 5 Model / 4 Tool、真实文件/stdout/exit 和 v2 持久事实。第九轮 `run.KkNSps` 已到 Owner transfer 200、持久 `in_review` / version 4 和独立 review 两 Model / 一 read；reload 第三次输出 `ERR_ABORTED` / 200 正文不完整，原 outer Wait 1。三层 cleanup 完成，正常末尾 App Join/peer 未到；整轮仍 FAIL，浏览器 owner 正修断点。
- `33009a96` / `run.Wxxzus` 的 read missing 与原 Runtime Stop/Drain 取消两个子例已 PASS 并独立接受；原 pending bytes、同事务终态、精确调用、Runner Wait 0 / App Joined 与资源清理已核。相关输入未变时复用，不重跑旧矩阵。
- 正常场景 tick 250ms / skip 240 是显式约60秒 Owner 窗口，不是生产默认；review 只读自有说明，不代表 worker 产物评审或 M2 完成。无工具 Agent、运行中重启接管、Human 持久取消及完整审批恢复仍待后继。
- 外部 DEV_CHAT 因本环境 A 成功 / AAAA NXDOMAIN 被原 D04 拒绝，未发送外部 Model HTTP；不改 DNS/权限策略。旧 live-chat FAIL 与 Object/tools/SPA 等 STOP 均保留。

## 恢复与下一步

运行入口和固定 case 见 [M1 harness](../tests/m1-app-workflow/README.md)。Go 为 `/workspace/toolchains/go1.27.1/bin/go`；共享模块 `/workspace/go/pkg/mod`、构建缓存 `/workspace/.cache/go-build`。固定 MinIO 位于 `/workspace/shared/agenteam-m1-tools/minio/minio`，SHA `dc5298474f0bc87a068f0b1135c583bb1278c17c11c512212ed7644a238c89c8`；锁定 browser/web 依赖位于该工具目录 `node-browser` / `node-web`。迁移包含 00091，每轮新 nonce/new DB；不连接 onboarding 环境。当前预算为两路 Go `-p=2`、一个 native 栈，由 delivery_path 协调。

原始证据位于 `output/ai/m1-app-integration/run.*`；前八轮失败及修复事实在集成卡与 Git 历史保留。本轮下一步是修正刷新输出读取并完成正常收尾和独立验证，再在原策略条件满足后验证真实外部模型。checkpoint 是可恢复实现，不表示产品 PASS；提交/推送状态由 root 实际报告。

旧 direct-text 兼容已在 `fee69b0d` / `direct-text.2tWIPp` 实际回归：1 top / 1 sub、Model HTTP 1 / Tool 0、v1 及同 DB 关闭 runtime 后重启历史读取、原完整 Join 均到达，outer Wait 0 / cleanup 完成，已获限定独立接受。正常全部门和旧兼容、失败/取消限定证据齐全后，才判断工具实现增量的受限 main 交付；M1 整里程碑仍须外部模型实证。

## 已完成交接：统一页面布局

`6d16bdae` 已交付统一前端布局，正式恢复材料为 `web/src/`、[界面规范](../docs/frontend-design/interface-design.md)和 [interface-layout.config.js](../tests/account-captcha-web/interface-layout.config.js) 同前缀 browser 源码/锁文件。本轮主任务已转 M1，不继续旧 project-list 候选。

原 Chromium 55 项按整轮 54 项与修正测试定位后 1 项独立复验组合接受；类型、构建、project-audit 49/49 与限定变更文件格式通过。使用合成 HTTP fixture，只证明前端行为；旧整轮 FAIL 不回填。原31项请求生命周期 unit 失败与17个基线文件格式问题保留，不宣称全局 npm check 全绿。完整历史范围从该提交的 current 与原日志查询，不新增归档。

锁定 Node 24.19.0 / Playwright 1.56.1 / Chromium 151 的原复现入口为 `node tests/account-captcha-web/node_modules/@playwright/test/cli.js test --config=tests/account-captcha-web/interface-layout.config.js`；其余命令与恢复边界见 Git 文件历史。原主树 `output/ai/interface-layout/` 保留简短日志与六张 preview；全部原验证进程已退出，可再生输出不纳入 Git。
