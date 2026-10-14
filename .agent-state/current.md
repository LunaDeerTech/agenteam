# 当前状态：M1 验收完成，M2 暂停

M1“单个Agent真正完成工作”的真实外部模型场景已通过。验证源码输入`2d76554dab983a7c9b91f3e367d6fea5a6fb5c94`；`run.OtpjqE`原41322 Wait0，66.76秒。模型自主完成同一work Execution的5轮Model、4次真实read/write/run/read，文件内容、stdout/exit、原Round/Call/Operation/Usage、最终v2同事务均验证；Owner在真实UI三次读取原成果、唯一送审并刷新为in_review。原Runner/Node/App/所有owner Join及精确资源退场完成；macro最终独核已接受，包含11条同请求联合消费证明。评审另有1个sent调用随原Runtime停止而取消，不代表自主评审决策；work成功、total sent6、retry0、failed0。

本轮接通了正式精确Origin可信环境代理（HTTP(S)_PROXY/NO_PROXY/TLS/CA保持），JSON tools仅接受并丢弃已知可选reasoning_content；JSON工具模型的响应头等待对齐原attempt剩余时间且最多60秒，代理CONNECT和其他默认仍15秒。live仍必须显式双opt-in，真实KEY仅经正式Secret且不进入日志/子进程。总300秒、单attempt≤60秒、MaxOutput4096、work/review/retry合计sent12软停止条件不变。此前本地DNS失败是历史直连观察，不能再写成当前代理场景的阻断。

预算动态门也已通过：纯observer五子例及race、合成work/review/retry SQL计数，以及真实controlled App在deadline触发后原Stop/Drain/根返回、Runner/命令子进程Join和无后继dispatch。合成计数不冒充真实调用；第三次真实live曾触发sent12并cancelled/incomplete，该FAIL与前两次协议FAIL均保留在[live卡](../docs/development/work-items/m1-live-acceptance.md)。代理、TLS/取消、parser/config及响应头窗口已完成作者和独立限定检查；旧受控/失败取消/direct-text兼容按各自固定输入证据复用，不称当前HEAD全仓测试。

恢复优先读[live卡](../docs/development/work-items/m1-live-acceptance.md)、[集成卡](../docs/development/work-items/m1-app-integration.md)、[运行说明](../tests/m1-app-workflow/README.md#显式真实模型入口)及[任务台账](../docs/development/agent-team/tasks.md#当前目标与工作包状态)。必要新证据保全在主树`output/ai/m1-delivery-evidence/proxy-live/`，逐文件bytes/SHA256已核，排除KEY、Session/Enrollment私有材料、原Provider响应、binary和cache；此前受控与准备证据保留在相邻app-integration/live-preparation目录。所有本轮原进程、精确容器/网络/临时目录与借用modules链接已退出；源码交由root最终保存和正式合入，不恢复已清理的旧M1分支。

## M2 暂停与接续

用户要求先补齐M1；M2目前仍暂停，本轮没有修改或运行M2。其已保存恢复入口：协调分支`ai/m2-coordination-20261014`（`c1473516`）、组合分支`ai/m2-review-20261014`（`70f02ad4`）。后续按用户目标另行恢复，不因M1通过便宣M2完成或自动继续其原native。review receipt不等于审核者消费worker成果并自主通过/退回；原审批、持久取消和cold恢复仍按M2卡的真实未完状态处理。

原direct-text-v1与旧Snapshot保持语义，没有新增产品UI profile开关；生产仍需显式配置Execution Runtime，验收使用tick250ms/skip240的Owner窗口不代表生产默认。无工具Agent、完整Project生命周期、平台ready以及原Object/Tools/SPA等停止项不由本轮结果扩大宣称。

## 已完成交接：统一页面布局

`6d16bdae` 已交付统一前端布局，正式恢复材料为 `web/src/`、[界面规范](../docs/frontend-design/interface-design.md)和 [interface-layout.config.js](../tests/account-captcha-web/interface-layout.config.js) 同前缀 browser 源码/锁文件。本轮主任务已转 M1，不继续旧 project-list 候选。

原 Chromium 55 项按整轮 54 项与修正测试定位后 1 项独立复验组合接受；类型、构建、project-audit 49/49 与限定变更文件格式通过。使用合成 HTTP fixture，只证明前端行为；旧整轮 FAIL 不回填。原31项请求生命周期 unit 失败与17个基线文件格式问题保留，不宣称全局 npm check 全绿。完整历史范围从该提交的 current 与原日志查询，不新增归档。

锁定 Node 24.19.0 / Playwright 1.56.1 / Chromium 151 的原复现入口为 `node tests/account-captcha-web/node_modules/@playwright/test/cli.js test --config=tests/account-captcha-web/interface-layout.config.js`；其余命令与恢复边界见 Git 文件历史。原主树 `output/ai/interface-layout/` 保留简短日志与六张 preview；全部原验证进程已退出，可再生输出不纳入 Git。
