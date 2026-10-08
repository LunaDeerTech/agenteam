# Audit 旧回归第一批原件独核

**有限 PASS / STOP，仅 oldauthlife01 与 oldauthrevoke01。** 两个作者实际 top、4 条 RUN/PASS（含 revoke 父组）和 3 个浏览器用例通过，原件、资产门禁与 task-owned 退役证据核合。其余八个旧轮及独立实际 Audit 轮不在本结论中。

| 已封存轮 | 实际结果 | top / browser / package / fixture 秒 |
| --- | --- | --- |
| oldauthlife01 | Session lifecycle：真实登录、刷新、Session CSRF、登出；Go 原日志 logout=true、successful Session audit=2 | 6.68 / 3.6 / 7.711 / 58.996 |
| oldauthrevoke01 | revocation 与 expiry 两 sub：真实 GET/router 确认指定 Session 不可用 | 13.60；sub 7.97、5.63 / 5.0、3.2 / 14.631 / 64.599 |

expiry 原日志明确只把一条 owned Session 行移入过去，不解释为真实等待完整绝对过期时长。无 tests-to-run 的其它包不计行为覆盖。原浏览器断言及 Go 事实确实完成；没有额外 DOM/body/trace 或截图原件。

两轮各 24 个 run 原件，全 48 件合计 834,713 bytes 与原 manifest 核同。[evidence.json](evidence.json)保存 55 项必要原位指纹，不复制原件、源码或资产树。各轮前后 1,181 输入同 bytes/digest `0edbd0073d6efa6a0e6df506da994137b1ec359b141215c7633420fde03f26d6`，无 missing/mismatch/set 变化。driver `06cf03c3`、单轮 true grant、binding 及 exchange02/build-v02 的 private/global 59 文件（794,499 bytes）均按原固定记录绑定；legacy gate 实际 required/authorized/matched。没有读取当前 dist 或重建资产。

每轮 fixture direct actual wait 1，adopted actual wait 分别 4／8，exit 均 0；合计 direct 2、adopted 12。95／110 是观察数，不是 wait 数。两轮 watchdog complete/join，原 top 120 秒（revoke 两 sub 共用）、package 6 分钟预算保持。每轮 4 container＋3 network 精确 ID 两扫 absent，14 ID 只在此两轮去重；owned、fixture runtime 与 browser runtime 两空，baseline 未变，无强制 tail 动作／monitor error。TCP 尾分别 38.381052／38.371281 秒，在 75 秒内双清，未因 TCP 发信号。8 个新增 nonowned PID 1 shim 保留、不认领 wait/kill；不能称全主机清零。本验证者未亲自 wait 作者原工具 cell，driver 终局依已 STOP result／交接。

两轮 safe-http-evidence 与 screenshots 均为空；旧认证浏览器 PASS 不转换成新 Project Audit schema/client/sidecar 数量。本轮新增该类验证为 0。原 Audit FAIL、修正版本和其他范围限制继续保留。

本次两个只读 metadata helper 曾分别误把 `top_levels` 的 `[PASS,name]` 元组视为字符串、读取 revoke handoff 中不存在的 lifecycle 专用显示字段；原工具错误及精确归因记入 evidence。按实际结构核对后仅继续元数据检查，未修改业务断言或重跑浏览器；它们不是产品或原测试 FAIL。

无新业务、Go/Node/browser、资源、网络、Git 或仓库写入；未读未交接活动轮。原件复核停止，后继只按 root 的正式 STOP 输入另补组合。
