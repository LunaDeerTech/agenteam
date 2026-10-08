# Audit 作者旧十轮最终组合独核

**有限 PASS / STOP。** 作者十个具名旧回归轮均实际通过并完整退役，原件已分五批独核。共10 top、12 RUN/PASS（含撤销/过期父组及其两个sub）、11个浏览器用例。此结论限作者旧十轮，不替代仍待完成/交接的独立实际四轮、最终21源一次核对、README第22路径或整卡验收。

| 具名 v04 轮 | top / browser 秒 | 精确资源数 | 资产门禁 |
| --- | --- | --- | --- |
| oldauthlife01 | 6.68 / 3.6 | 7 | private＋global |
| oldauthrevoke01 | 13.6 / 5.0/3.2 | 7 | private＋global |
| oldpersonaltheme01 | 17.5 / 11.8 | 7 | private＋global |
| oldauditread01 | 13.11 / 7.9 | 7 | private＋global |
| oldauditauthority01 | 15.2 / 10.1 | 7 | private＋global |
| oldauditnav01 | 22.6 / 17.2 | 7 | private＋global |
| oldownerread01 | 17.02 / 8.5 | 7 | private only |
| oldowneridentity01 | 18.46 / 10.8 | 7 | private only |
| oldsummaryauth01 | 15.58 / 10.6 | 7 | private only |
| oldsmtpdelivery01 | 21.92 / 15.3 | 9 | private＋global |

本次第五批只新核 oldsummaryauth01 与 oldsmtpdelivery01 的48个原件，共1,175,902 bytes。Summary实际 package16.619／fixture68.183秒；SMTP为22.964／72.426秒。前四批[认证](../audit-old-regression-batch01/review.md)、[个人主题＋旧审计读取](../audit-old-regression-batch02/review.md)、[旧审计权限＋导航](../audit-old-regression-batch03/review.md)、[旧Owner读取＋身份](../audit-old-regression-batch04/review.md)的12个STOP报告/证据/manifest指纹核同后复用，没有重读其全部原件或源码图。

最终十轮363个run原件共5,926,307 bytes；[evidence.json](evidence.json)保存64项此次必要指纹、第五批实际核对与十行资源ID表，不复制原件。十轮固定文件内容digest均为`0edbd0073d6efa6a0e6df506da994137b1ec359b141215c7633420fde03f26d6`，各1181输入前后同，无missing/mismatch/set变化。build-v02/private59（794,499 bytes）全部要求并匹配；7轮另授权并实际要求exchange02 global59匹配，旧Owner两轮和Summary共3轮只要求private。不能把这些private-only轮的默认legacy match=true当作实际global读取证据；各轮输入记录整体SHA可因gate角色字段不同，固定文件digest未变。

Summary原日志确认正式bootstrap/invite/redeem/login/Logout，admin→user及user→admin SQL只是owned授权事实准备，不是生产角色管理API；无generation/Invocation/生产SPA接入主张。SMTP实际9个资源为5 container＋4 network，D07专有nonce的额外network/container与原exact-cleanup日志一致；5个新增nonowned PID1shim单列。原浏览器test/retry通过只证明**owned SMTP协议接收，不是外部邮箱送达**。两轮safe-http和PNG均0，不增加Project Audit验证。

十轮累计fixture direct actual wait10、adopted actual wait44、watchdog join10，原120秒/top及6分钟/package预算保持。只有oldauthrevoke含8次adopted，其余各4。观察进程数不当作wait数量，root工具70335／52296的actual exit0也不表述为本验证者亲自wait。第五批TCP尾38.385934／54.456614秒均在75秒内双清；十轮每个精确资源两扫absent，owned、fixture runtime、browser runtime双空、baseline未变，无强制tail或monitor error。72个ID只在这十个具名v04轮内去重。41个新nonowned shim保留，不认领wait/kill；不称全主机清零，TCP仍只是补充host轮询。

旧Owner保留63个sidecar/schema、27次client replay及48个body原件的限定接受：只有source_run，无逐请求setup/browser来源字段，依旧协议＋序号/endpoint/status＋aggregate receipt核分类。read准备28和IPC2、identity准备5及held010均不计浏览器完成client；不能外推新Project Audit EOF或来源证据。旧System Audit的8张PNG共692,161 bytes仅核存在/指纹，视觉查看0。**全部旧十轮新增Project Audit schema/client计数为0**，旧计数不迁入新契约。

原FAIL、修正版本、单独退役恢复与先前只读metadata助手错误的记录全部保持；不是追加一次最新HEAD fresh全套或生产验收。Owner的Skills/lifecycle辅助事实、Summary角色SQL与SMTP本地协议边界继续保留，未扩D10/D27、Provider/Invocation或生产host。本次未读在途独立aud-read01，无业务、Go/Node、schema/client、browser、资源、网络、Git、仓库写入或current dist/host扫描。旧十轮组合封存停止，后继独立与最终技术末件另授。
