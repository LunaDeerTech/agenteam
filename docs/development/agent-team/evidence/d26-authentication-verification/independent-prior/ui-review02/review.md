# D26 UI-review02 窄复审

**BLOCKED：U1、U3、U4 的固定差量可采纳；U2 尚缺验证期间原生 disabled 失焦后的交接。** 未发现其他新增必要阻断，其余 UI01/core02 已审结论复用。本轮只读固定文本和原日志，没有执行 npm、产品 JavaScript、浏览器、Go、Docker 或网络，也没有读取活动 fixture、完整 web-check 后续输出或修复稿。

固定基线 `457b1979c9d6563740543b2011eedc06cce34c71`；目录 `/workspace/agenteam-d26-auth-author-jrfhr6h1/evidence/ui-review-02`，manifest SHA `e9fab84272bef87ad3438733fb2190d88b82b7a6211abdbb9e0399f9b55686e0`，delta SHA `9f36d4eb0c9522940e5c79de06276419bb7aaf7fa08fba8ceca818f02eca257a`。以下行号均指固定副本。UI01 报告 `/tmp/agenteam-d26-ui-review01-v-vgj2lp5h/review.md`（SHA `cc1447263573e63b4f4add0abee1aa75b3f5c3ce51b24daddebd31b8d7935e89`）保持原字节。

## 唯一尚需补齐的接缝

`LoginView.vue:37–55` 只在 `question` 从非空变空时，用 `.rotate-challenge.contains(document.activeElement)` 判断是否拥有焦点。验证开始时，固定 core02 的 `runChallenge` 先令 `challengeBusy=true`，`RotateChallenge.vue:123,131` 随后把 range/验证按钮 disabled。若浏览器因禁用原聚焦控件先把焦点移到 body，稍后拒绝、30s 超时或成功清题时，`ownedFocus` 为 false；`challengeFocus` 不会设置，页面不会返回创建按钮或登录按钮。当前代码没有在禁用之前保存该题的焦点归属。

这是固定代码中可明确指出的遗漏路径；**本轮没有真实浏览器运行，未将原生 disabled 的具体浏览器行为或该反例冒称已实测**。作者 13 个 jsdom tests 通过不能排除此分支：拒绝/超时测试先手工聚焦 enabled range，再触发 Enter；它们没有模拟 range 在 pending disabled 后失焦到 body。成功测试也没有覆盖该变化。

最小修复建议仍在现 UI 路径：在验证导致控件禁用之前，保存当前题是否拥有焦点；清题后按真实 pass 状态交给登录或创建动作。用户已移动到外部输入等控件时不得抢焦点，旧题/卸载回调不得影响新题，不能为满足断言去掉 busy 或提前结束实际请求。最小 pure 反例为 held verify 进入且 range 实际 disabled 后显式 `.blur()`，再分别拒绝、保持原 30000ms 超时或成功，核目标焦点；该模拟不能标为真实浏览器证据。后续正式浏览器还须用原生 Enter/点击、真实 pending 状态验证禁用失焦及成功/失败交接；无需改 core/API 或扩大卡。

## 已闭合的修复

| 项 | 固定差量及原反例核对 |
| --- | --- |
| U1 | `LoginView.vue:21–34,59–64,79–82,90–92` 明确放弃、上下文失效、成功/卸载清可见 email/password；程序性清理用同步 watcher 抑制 `inputChanged`，不会意外改仍应保留的私有 Unknown 原意图。普通明确凭据失败仍保留输入。原两红及最终旧输入不再出现、新输入使用新 key、普通错误保留密码的断言都在。 |
| U3 | `LoginView.vue:104–110` 对 checking 且无 busy owner 提供一次明确 Session 检查入口；原 Cookie owner 未返回时仍只有等待，没有轮询或提前清 owner。真实 memory router/controller 原反例保留，旧尾部不发布身份，点击检查后实际 GET 总数 3、login 1。原登录/注销回归的 GET 3/4 精确断言未删。 |
| U4 | `RotateChallenge.vue:57–102` 为当前题保存一次 focusRequest；busy 时保留，post-flush 确认 enabled 后再聚焦，完成后不因同题再次 idle 抢焦点，新题/卸载有对应清理，并保护外部编辑焦点。新增真实 controller/page 展开断言没有手工 focus；另独立组件用 busy=true mount→false 证明该调度，并检查后续 busy 切换/换题不抢外部输入。此处可静态采纳，实际 Chromium 的展开交接仍归后续浏览器验收。 |

U2 新逻辑在题删除前焦点仍留在组件内的路径有改善，成功/手工关闭及不抢编辑焦点的作者断言也有价值；这些不能替代上面的 disabled 失焦分支。

## 原失败与有效作者证据

| 原件前缀 | 实际结果与边界 |
| --- | --- |
| ui-u1-red-01 | exit1，2 FAIL / 4 skipped；最先失败是 email 仍为旧值，未执行到后续 password 断言，不将原 raw 夸成两字段均已动态失败。静态两字段均缺清理，最终 tests 覆盖两者。 |
| ui-u1-fix-01 | exit0，6 PASS。 |
| ui-u2-red-01 | exit1，2 FAIL / 6 skipped；期待 rotate-angle，实际 login-title，在 verify 前失败。这是 U4 的真实 mount-disabled 初始焦点红例，不能仅当作测试前置问题而忽略。 |
| ui-u2-u3-red-02 | exit1，3 FAIL / 6 skipped；手工聚焦 enabled range 之后才隔离出 U2 拒绝/超时的两红，另真实 router 的 U3 检查入口缺失一红。该手工 focus 不证明初始展开功能正确。 |
| ui02-unit-01 | exit0，2 files / 12 PASS。 |
| ui02-unit-02 | exit0，2 files / 13 PASS，1.45s；新增一次 enabled 焦点及保持外部输入焦点用例。属于作者 jsdom/pure 执行，不是独立真实浏览器结果。 |

实际 argv/cwd/exit 来自各 `.result.json`，原 metadata 没有环境明细，不补造。原输入三份 manifest 和六个源码 payload 均核 SHA；U1 原红生产等于 UI01，后两组原红生产只先加入 U1 且彼此相同。所有原反例预算及核心断言保留。

## 实际检查与边界

本轮命令 `python3 /tmp/agenteam-d26-ui-review02-v-njf1nsfc/verify_inputs.py` exit0：前后各 10 个 UI 文件、12 个日志/metadata、3 个原红 manifest/6 个原红 payload、7 个已固定 core02 条目逐 SHA 匹配。从固定前后独立生成的 unified diff 与作者 `delta.patch` 字节相同，恰为 LoginView、RotateChallenge、两 tests 四路径；其余六 UI 文件相同。core02 三生产及两 package 仍为原固定字节，本轮无依赖升级。检查结果及实际命令在 `checks.json`、`check-command.json`，只写私有目录。

未运行或宣称完成真实六 API、Cookie jar、正式 dist、挑战消费、390px/200% 缩放与主题/实际拖动；未改变原 browser 矩阵、上游阻断或完整 D26/D28 范围。主线程已收到 U2 具体接缝及最小修复建议；修后只需冻结新的窄差量复审，实际浏览器证据另行授权。报告冻结后 all-stop。
