# Audit 旧回归第三批原件独核

**有限 PASS / STOP，仅 oldauditauthority01 与 oldauditnav01。** 两个旧 System Audit 作者实际 top、两个浏览器用例、固定资产门禁和 task-owned 退役原件核合。没有读取正在准备或执行的 oldownerread01；其余四旧轮与独立实际 Audit 接受不包含在本结论。

| 已封存轮 | top / browser / package / fixture 秒 | 保留图片 |
| --- | --- | --- |
| oldauditauthority01 | 15.20 / 10.1 / 16.228 / 66.110 | 0 |
| oldauditnav01 | 22.60 / 17.2 / 23.627 / 74.151 | 8 张旧 System Audit PNG |

原 Go 日志分别确认 `group=authority` 与 `group=navigation` 完成，仅使用正式 Account/Profile/Outbound producers；不增加 Project/Tools、外部目标或生产 hosting 范围。无 tests-to-run 的其它包不计行为覆盖。

56 个 run 原件（两轮基础 48 件＋8 张 PNG）共 1,677,957 bytes，均与原 manifest 的 SHA/bytes 相同。[evidence.json](evidence.json)保留 63 项必要原位指纹及累计六轮的小 ID 表，不复制原件／源码／资产树。两轮前后 1,181 输入同 bytes/digest，与已接受[第一批](../audit-old-regression-batch01/review.md)、[第二批](../audit-old-regression-batch02/review.md)一致。单轮 true grant、driver `06cf03c3`、shared binding 与 exchange02/build-v02 的 private/global 59 文件（794,499 bytes）原记录绑定；实际 legacy gate required/authorized/matched。共用小 metadata 复用第一批，不读取当前 dist 或源图。

每轮 fixture actual direct wait 1、adopted actual wait 4、exit0；本批合计 direct2＋adopted8。99／98 为观察数，不是 wait 数。watchdog complete/join；原 top120 秒、package6 分钟保持。每轮 4 container＋3 network 两扫 exact absent，owned、fixture runtime 与 browser runtime 双空，baseline 未变，无强制 tail／monitor error。TCP 尾分别 38.378516／37.387605 秒，在 75 秒内双清，未因 TCP 发信号。8 个新 nonowned PID1 shim 原记录保留，不认领 wait/kill或全主机清零。当前 14 ID 与此前四具名轮无交集，42 ID 去重仅限这六个 v04 轮。root 所报 session70126／5372 exit0 是原交接，本验证者没有亲自 wait 原工具 cell。

导航 `images.json` SHA `93b9b109c5334417a62295762878443600cf4979d3bd277ba960b7049b4fc286` 与原 manifest 绑定。8 个 PNG 共 692,161 bytes，其独立路径、bytes、SHA 与该索引精确对应；**只核存在与指纹，视觉查看为 0**。没有据图像名称或原浏览器 PASS 推断图像可见内容。它们是旧 System Audit 图，不是新 Project Audit 截图。两轮 safe-http-evidence 都空，新 Project schema/client 计数为 0，不把旧 System 契约的通过转换为新 DTO 原生 body 验证。

累计六旧轮已分批原件独核；不提前宣布旧十轮或整卡通过。原 Audit FAIL、修正版本、非最新 HEAD 一次全套与生产限制保留。仅固定原件本地 bytes/SHA/JSON/raw/退役检查，无业务、Go/Node、schema/client、browser、资源、网络、Git、仓库写入或活动后继读取。本批封存停止。
