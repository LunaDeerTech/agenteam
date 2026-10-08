# 剩余新三组顺序交接（仅准备，STOP）

复用 product367156d / browser-v5 / Go04 / UI19 / driver-v04 exact BB，以及 final04 原冻结的955仓库输入、66目录集合、1165显式SHA和固定工具闭包。此处只绑定既有冻结来源，不新建wholefreeze/大索引，也不运行任何检查或资源。edit03目前仅记录作者实际PASS并已退役，独立终局复核仍待决。

| 顺序 | group / 短名 | 精确 Go top | 每轮资源 |
| --- | --- | --- | --- |
| 1 | new-recovery / recovery01 | TestAccountProjectOwnerWebOriginalRecovery | 7 |
| 2 | new-identity / identity01 | TestAccountProjectOwnerWebIdentityAndOwnership | 7 |
| 3 | new-layouts / layouts01 | TestAccountProjectOwnerWebLayouts | 7 |

各轮使用全原fixture链、新nonce与7个独立ID（PG2容器1网络、outbound1容器1网络、object1容器1网络）；不共享上一轮资源。case45s/top120s含cleanup/package6m/TCPtail75s不变，每轮实际开窗再核fresh disk≥5GiB与完整baseline。

只有前轮精确top通过、外层actualexit0/accepted、direct/adopted实际wait和watchdogjoin、7ID双absent、owned/runtime双空、TCP双clear、source/inputsame及完整原件均完成，才可开始下一轮。任一失败或未完成，先实际退役并保原件，随后STOP整个batch；不启动后续组、不自动重试或调预算。

当前root_authorized_resources=false，没有资源授权或asset窗口。本文件中的命令只是待root另建明确truecopy后的顺序模板。root须先接收edit03独审与本selector交接，再提供仅允许上述三group的独占资源/53资产批次窗口。root保留原3资产、所有reader最终退役后恢复；本次未读取globaldist。

layouts沿原driver设置绝对AGENTEAM_AUTH_WEB_IMAGES路径，实际截图和视觉复核仍pending。原失败及edit03证据保持。本次只生成handoff.json/md，完成即停止写入。
