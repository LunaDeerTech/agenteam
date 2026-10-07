# 系统会议 Summary 设置 S2 实现验证

2026-10-07：完整有界技术与两个 README 末件独审 PASS，root 已完整采纳并提交推送 `4e615c7da2875418e1fbd925a49f134f14feae6d`，且已确认远端一致。规格为 [S2 rev3](system-meeting-summary-settings-spec-verification.md) `fdfcffdbc5949006030c80e48833bfe3b49a86cc`。S2/S3 卡页首另补当前接受状态，旧技术正文与历史记录保留原字节；当前入口另行接续。

## 1. 固定范围与结果

[完整独审原报告](system-meeting-summary-settings-verification-evidence/objects/a49b532fdf5485c22ece7f4c43c3e17dd3b138cee7e75b7326035467ae635219)（SHA `a49b532fdf5485c22ece7f4c43c3e17dd3b138cee7e75b7326035467ae635219`）、[最终结果](system-meeting-summary-settings-verification-evidence/objects/1473974f50f8fab734a7172e0cdf3332aeea6103c39f010f06c91f97702e22f6)与[精确技术路径](system-meeting-summary-settings-verification-evidence/objects/becd3e17eb396f86ff52674a338b1b8f9f8c118835a7e16465e84231b11ce271)绑定 30 个技术路径：13 新、16 改、1 个未变旧浏览器 spec，共 29 个实际技术改动；两个 README 末件已由[最终闭合报告](system-meeting-summary-settings-verification-evidence/objects/07039d8c3afb995ff5e163fd86ea51a14172862b3fec3efc8b36cc8ec4e08864)和[32 路径结果](system-meeting-summary-settings-verification-evidence/objects/ca17b31e85cbe418a2c61886d1930e8458b73b453111c0b51a04defc2dd4cc85)闭合，共 32 scope /31 实际改变；唯一未变路径是旧 Selection 浏览器 spec。早先 technical final 的 pending-README 文字保留其历史时点。后端 candidate04 的生产源码与 candidate01 相同，后续限定修订属于测试/安全证据导出；前端 delivery01、web11、browserstatic05 及 f20 的 42 个构建文件固定。本档复用产品提交的源码，不复制源树；已逐项核本地提交内 32 路径与最终冻结指纹一致，实际提交 31 路径。以 S1 `c210d249` 为路径差量基线，依赖继承已接受 [S1](system-meeting-summary-selection-verification.md) 和 [S3](system-meeting-summary-resolution-verification.md)；不把不同阶段说成最终全套重跑。

S2 实现独立 Summary GET/HEAD/PUT、同页独立表单及默认 root 的 Secret→Model→Summary→Usage 同 context 初始化。它沿现有 Account/System 权限和命令命名空间，提供严格 wire/receipt 绑定、鉴权前开始的 3 秒读/30 秒写预算直至实际输出和 callback 结束。两块表单在原 Cookie owner 内保留独立 intent：当前观察不确认历史写，显式原 lookup/replay、本区 discard 与一次聚合应用导航确认各有独立语义；原生 beforeunload 仅保护 dirty 并集。

管理员统一选择会议 initial/update（含首轮标题）模型；没有猜测默认、清空操作、第五个旧 Purpose、Project override 或复制初值。compaction 沿 Execution snapshot，Execution Summary read model 不变。S3 库的实际消费、production Resolution/Invocations 与 D24 仍未绑定；ready503、D08–D28/E01 未完成及 E01 未开始不变。Object runtime join、OpenAI tools 独立验证和 SPA concurrent-publication 三项停止继续有效。

## 2. 实际验证及版本组合

| 证据 | 实际结果与范围 |
| --- | --- |
| [后端 author-final04](system-meeting-summary-settings-verification-evidence/objects/840882b95cae8b73008e5b73d1a922d1439f19b00a836c43a858041594ad7e8b) | 四个成功原 PG/root/旧组合计 8 个不同 top、35 sub；candidate04 只补 Recovery 实际响应头的 1 top/3 sub。六个真实轮含最初失败，不把通过组补写成统一重跑。compile/vet/pure/race 和每轮原 command/raw/终态沿来源映射保留。 |
| [native 原结果](system-meeting-summary-settings-verification-evidence/objects/38bc4c531427a20ed3a77679c0f8d8e61ee286a988fb6aaed06bf3d4fa8be3cd) | 3 top/12 sub，keepalive/slowbody/writeclose；自然 GET 约 3 秒、PUT 约 30 秒，实际 handler/Serve/connection tail 与 direct/adopted wait。TIME_WAIT 约 61 秒消退属于清理，最后两次全状态 TCP/进程观察分列。 |
| 前端 delivery01 与[六轮 browser 原报告](system-meeting-summary-settings-verification-evidence/objects/4884f218222f1c2a9d62b5681d801d56d1350b859a6218b74ec64997650c7fb3) | client98 + state96 + component27，另 owner57；type/format/build。6 个真实 browser 轮覆盖 4 新 + 6 旧 top，均用同 42 文件构建；轮次分别 56.185/65.008/65.084/64.655/88.455/103.289 秒。 |
| [独立 A](system-meeting-summary-settings-verification-evidence/objects/cfbd672b1c9629a1a7919b4924da2fb00cc80922190618d775ddd03402b40784) | A01 已过的正式 Logout-at-observed-transaction-gate 子例 2.54 秒，组合 A02 仅受影响的 default-root/history/replay/cross-selector 子例 3.68 秒；保留 A01 首红，不称最后整组重跑。 |
| [独立 B](system-meeting-summary-settings-verification-evidence/objects/fc342e5c6eacbe219aee8fefc37ec1e99dbfbf56e707a91bb11d11ae6c0f5554) | B02 完整 browser case PASS 9.5 秒、parent13.28 秒。两个 selector 各一次真实截断后历史 lookup；platform 本区取消/确认 discard 保留 Summary intent；显式原 Summary replay，观察到 Summary2/platform1 PUT，无额外 DB 效果。实际 held Summary GET 到 EOF 才释放 owner；原生刷新取消保持 URL/timeOrigin/两 draft；一次聚合导航取消后接受，焦点/overlay 收尾正常。 |
| [原 HTTP body 联验](system-meeting-summary-settings-verification-evidence/objects/39afe422631a11ca8cc07d2af2cb04992b71280f2d0b49854f88ac7ad8bea15c) | 8 类实际 payload，public composed client 8 次、Draft202012/FormatChecker schema 9 次执行通过。前 7 client/8 schema 沿 candidate03；candidate04 Unknown 的实际 X-Request-ID 与 body 同响应补充 1 client/1 schema。闭合本地 refs、网络 guard、actual wait 与前后输入指纹均有证据；不是新 HTTP/browser 请求。 |

body 包含 null/current configured/receipt/lookup true/lookup false/deletion-impact/replay/Unknown。旧 Unknown schema 检查仍保存；不得由 body.request_id 猜头或跨轮拼接。原 CSRF/key 没有导出，离线 transport 控制为 synthetic，实际公开 command id/version/model 仍绑定；lookup true 属于另一历史场景。原生 response-prefix loss 是客户端传输事件，不冒充服务器 Unknown。后端辅助身份 SQL seed 后正式 Login、正式 Session Logout 证明不等于管理员角色管理 E2E；浏览器 current403 使用恢复后的自有 SQL 角色故障准备。

## 3. 原始失败、资产恢复与视觉边界

原件保留前端 f03 cancel-read、f06 readonly-test/type、f12 focus、f14 lookup0、browser b03 DOM type 及 f00 Python 前置失败，分别沿固定源/测试/工具修正记录，不统一说成测试脚本问题。后端首次 trailing-slash 404 期望与既有 Account CheckRequest400 不符；candidate03 仅改旧期望并补 canonical unknown404。candidate04 仅补真实 Unknown 实际响应头导出。独立 A01 把业务安全头 helper 错用到诊断 readyz；私有修复检查原 503 dependency Problem 并实际 EOF/Close。

B01 的 Playwright 45 秒首红和 parent49.84 秒失败原样保存。B02 仅修私有等待：等待实际 CDP Page.reload dispatch、原生 dialog dismissal 并完成 detach，随后核 URL/timeOrigin；不再等待已取消的新 document 导航。锁定 Playwright 源码解释这一等待可能性，但没有 runtime pendingDocument trace，不能断言唯一底层竞态。产品、作者测试、原 B01 及 45 秒/top120 秒/package6m 预算未改。

资产 preflight 初始无法辨识的 /proc cwd 后续分为 108 个历史 Z 和 14 个既有 Docker/PG/MinIO 基础设施进程；只辨识基线，未操作它们。首次 asset apply 的 atomic exchange 已成功，v02 事后校验因原根目录 st_size67→4096 首红；保存失败与 applied journal。v03 只修目录 size 不变量并实际只读 verify，未重复 apply。v04 纳入完整 8 个 started 参与者（作者6 + B01 FAIL + B02 PASS），全部实际结束后由原作者还原一次；[还原原结果](system-meeting-summary-settings-verification-evidence/objects/8566ce3e1353362a8c12549c359a1409fca461b76050e96890e7486315966e13)与[独立只读还原复核](system-meeting-summary-settings-verification-evidence/objects/6607b2158851c1939b49778be7c7c27f5832a02de4d734cb1a19166b8f172119)绑定原3文件 inode/device/bytes/文件size/hash/mode/owner/mtime/atime/link/xattr。目录 size 例外为 `.` 67→4096、`assets` 86→4096，namespace ctime 实际改变；不宣称逐项 metadata 全不变。独审用已知路径和 O_NOATIME 核当前内容及全部 observed owned PID/starttime 缺失，未执行交换/还原；完整目录成员来自作者快照与 journal。

[截图观察原记录](system-meeting-summary-settings-verification-evidence/objects/c94f3ed261001e06020adc577311299ec20520937386ec52b1256cb2d4e238db)及 9 张原 PNG 保留。作者实际查看9张，root 实际查看 dark390/light1440 两张，归档者和独审不增添视觉接受。全部是 900px 高局部 viewport；8 个普通主题/宽度图的可见 Summary 区为作者观察。CSS zoom=2 那张只显示缩放后滚动位置的旧四项区域，不能作 Summary 区/native zoom/full-page 证据；截图也不替代独立键盘/overflow 断言或 BFCache 接受。

## 4. 资源与证据保存范围

16 个 fixture 轮（后端作者6、browser作者6、独立 A01/A02/B01/B02，含首红）各有7个 exact resource 的 actual wait 和两次 owned 清理；native三组保留各自 socket/进程证明。独立四轮 observed PID/starttime 为90/84/100/98，actual adopted waits 为0/0/5/5；作者6browser 合计652个轮内身份观察、46次 adopted wait，不当作跨轮去重总数。记录没有 forced-tail/monitor 错误。每个 fixture 轮新增4个 daemon/PID1 shim Z，合计64，均非任务 owned、未由任务 wait。这个 daemon 增量不与包含别种 Z 的 all-PID1-Z 基线混算，也不声称全机清零。

[source-map.json](system-meeting-summary-settings-verification-evidence/source-map.json) 将 562 个原始来源映射到 447 个本档去重对象（4,981,803 bytes），另 31 个来源复用已提交 S1/S3 相同字节对象。保存实际 command/raw/result、必要 probe/driver、首红/差量、输入绑定、actual wait/双清和安全 body；原 manifest 保持原样，但它列出的每个文件不都入仓。115 项重图/重复输入/中间观察/工具或产品 schema 仅保存来源、完整 SHA 与 byte count，完整树、binary/cache/module/dist、私密 runtime/认证材料不复制。[命令摘录](system-meeting-summary-settings-verification-evidence/offline-command-excerpts.json)是逐字段派生数据，明确列出省略字段，绝非原件。重图未入仓使本档不能独自重建当时整个环境；可核原件字节、精确 Git 产品与固定来源的有界结论。

归档仅读取证据和 Git，不运行产品、旧 driver、还原或资源命令；原件保留字节，原始 patch/raw 的尾空白作为原件例外保留，不为格式检查改写；既有报告的 pending/FAIL 均保留当时事实，由此页汇总最终已知状态。文档、JSON、相对链接/格式及 SHA 自查在交付冻结时记录；两个 README 仅做末件 STATIC，无产品重跑；frontend README 的初始 difflib 字符串比较因 Git hunk context/header 表达不同而拒绝，随后用原 Git hunk 重建核实相同字节，见原末件审查。这不是产品/测试首红。产品提交内 32 路径、原件 SHA/JSON、派生字段、报告链接及生成文件 UTF-8/LF/空白自查均通过；原始尾空白例外保留。
