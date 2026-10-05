# D26 UI-review03 窄复审

**STATIC PASS：UI02 剩余 U2 接缝已由本次固定差量补齐，未发现新增必要阻断，建议采纳继续完整候选及正式浏览器验收。** U1/U3/U4、core02 及其他未变 UI 结论复用；UI01/UI02 原 BLOCKED 报告和失败日志保持原字节。本轮只读固定源码/原件，没有执行 npm、Vue、浏览器、Go、Docker 或网络。

固定目录 `/workspace/agenteam-d26-auth-author-jrfhr6h1/evidence/ui-review-03`，manifest SHA `d9b0472650e1705273cf0e5009dc70855e54893285779181905d0e563fa0ed0b`；基线仍为 `457b1979c9d6563740543b2011eedc06cce34c71`。精确 delta SHA `a2389ef3834099eb9bc311165123c74989f9fc14c2582120970e5ce0fb8c2966`，只改 LoginView 和 authentication.spec；其余八 UI 文件与 UI02 相同。core02 三生产、两 package 及其两测试的固定字节仍相同，没有 API、核心所有权或依赖变更。

`LoginView.vue:35–74` 使用当前题 ID 保存验证开始前的焦点归属，watch 明确 pre-flush，在子组件把控件 disabled 前取得该状态。清题时既接受焦点尚在组件内，也接受同一题拥有归属且当前落在 body；再按真实 passReady 选择登录或创建动作。nextTick 后校验同一个交接对象、没有新题且 challengeBusy 已结束，才允许实际 focus。即时返回、尚未发生子渲染的分支仍可直接检查原组件焦点，未为焦点修复放松 busy、30s 等核心约束。

外部 focusin 会清 verificationFocus 和待交接对象；即使用户随后又 blur 到 body，也不恢复旧题的焦点权。换题清旧 ID，mounted 注册监听、unmounted 移除并清两份状态；旧 nextTick 因对象不同或 form 已消失不能抢新页面焦点。原 U4 的一次 enabled 焦点逻辑没有改动，原 U1 程序性清表单保护和 U3 显式一次 Session 检查入口仍在。

作者证据按原始实际结果保留：

- `ui-disabled-focus-red-01`：exit1，3 FAIL / 10 skipped，全部在 `range.blur()` 后“已落到 body”的前置断言失败。jsdom 对 disabled 元素的 blur 不产生所需状态；这一轮没有执行到最终目标焦点断言，不能记为三个产品焦点失败。
- `ui-disabled-focus-red-02`：生产仍等于 UI02；测试只增加显式 body.focus 来模拟原生落点，未重新启用 range。exit1，3 FAIL / 10 skipped，拒绝、30000ms 超时、成功都实际走到目标焦点断言失败。这是 pure 页面/controller 反例，不冒称真实浏览器。
- `ui03-unit-01`：两文件 17 PASS，1.45s，exit0。上述三个反例保持原 disabled、目标 focus、passReady 与 30000ms 后迟到成功不得发布的断言；另增加外部 email 聚焦后再 blur 的分支，确认不会误夺回焦点。其他原 UI 测试未降低断言。实际 argv/cwd/exit 位于 manifest 指向的 metadata，原 metadata 没有环境明细，不补造。

另一个已独立冻结的原生 DOM 实测报告 `/workspace/agenteam-d26-dom-focus-v-7k_tkbe3/report.md`（SHA `438868537e2a455bdfced0ffc9195ed9cdfda98b7aca375e9164df7e1378490f`）证实系统 Chromium 对 range Enter/button click 的 disabled 处理会同步 blur 到 body。它为模拟的原生前提提供真实证据，但没有执行本 UI03/Vue/Account API；不能和作者 jsdom PASS 合并宣称正式页面浏览器已通过。该探针浏览器已关闭，12 个自有 PID 二次 absent，runtime 空，窗口已交还。

实际静态检查命令为 `python3 /workspace/agenteam-d26-ui-review03-v-6n0oywcn/verify_inputs.py`，exit0：前后各 10 源、6 个 raw/metadata、2 个原红 manifest/4 payload、7 个 core02 条目逐 SHA 匹配；独立重建的 unified diff 与作者 delta 字节相同。原红生产都为 UI02，测试红01→红02仅补模拟前置，红02→最终仅加编辑保护分支和格式调整，保留原三分支断言；对应两个小型 diff、实际命令和结果留本私有目录。

没有读取活动 fixture 或后续全量 web-check，也没有写仓库、业务、作者原件或既有归档。完整正式 dist、同源六 API/Cookie、成功/失败/超时的实际页面焦点、官方挑战交互和窄屏/主题仍待冻结候选与正式浏览器窗口验收，不扩大到完整 D26/D28。报告与索引冻结后 all-stop。
