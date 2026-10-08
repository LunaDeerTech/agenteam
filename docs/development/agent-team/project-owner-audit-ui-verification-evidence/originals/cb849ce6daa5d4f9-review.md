# Audit auditnav01 独立复核 — STOP

原业务结果 **FAIL 保留**；本轮 owned 资源退役证据 **PASS**。30 件原件共 478292 B 的 SHA/长度与原 manifest 相符。复核范围仅原失败、退役及大小写直达/observer 语义；未执行检查、Go/Node、driver、filegate 或资源，未改仓库。

唯一 top `TestAccountProjectOwnerAuditWebNavigationAndLayouts` 于 16.06 s FAIL。首个未登录 uppercase dotted 直达已通过 HTML 200、规范化 return、登录后 ready/canonical URL 和 Resolve→Get→Audit 顺序。第二个已登录相同大写直达仍为 HTML 200，随后 frozen a91d spec:1713→588→541 的 current-document native fact 条件等待 5 s 超时。该等待在本次 heading/重新读取按钮断言之前；其后的 dotted_return 标记、导航/拒绝路径/共享草稿/焦点/pageshow、8 张 PNG、finish、schema/client 同 body 校验均未到达。PNG 为 0，没有失败 DOM/error-context 或最终 EOF 总表。

两个 browser list GET 均保存完整上游 200，request_id 不同、body 同为 1389 B/e53e60c4，不能归因为没有后端读取；也不能将两份上游 body 当成两次 browser EOF。proxy started/finished=61/61，held=0。

Owner 卡 §3.1 与 Audit 卡 §2/§7 明确保留合法大小写和 dotted 项目直达，现有场景有效。observer 每个 document 重建 UUID、facts 和 sequence=0；`facts(page)` 返回当前 window，`ready` 默认 start=0，`after` 不查询 Node archive。因此“reload 沿用旧 document 的高序号导致漏匹配”没有源码依据。

已读旧源码存在可疑时序：Workspace 接受 Get 后进行 canonical router.replace；Audit View 的 route-update guard 无条件 leave，可能退休已启动的 Audit 读。此处仅为静态可能路径；缺少当轮 DOM/native facts/调度记录，不能断言该顺序实际发生、判定唯一根因或接受修复。#10/#14 交给作者活动后未再读取；后续受控 RED/GREEN 应单独审查，原 FAIL 不回写。

fixture 68.15 s、exit 1，直接 actualwait、watchdog join 和 4 次 adopted wait 有原件；raw 记录 Node、proxy/body handlers、prep service、default root 的终局。7 个精确资源 ID 两次 absent，owned/runtime/browser-runtime 两次为空；补充 TCP 观察 39.377561 s 后两次 delta 空；无 forced action/monitor error。1179 项原输入前后记录逐字节相同。95 个进程观察不是 wait 数；4 个新 PID1 shim 非 owned，未 wait/join，227 个历史 zombie 未触碰。TCP 观察不宣称完整短连接追踪或所有权证明。

关键原件及有限源码定位见 evidence.json；这不是 Audit 整卡、后续导航场景或任何新候选的验收。
