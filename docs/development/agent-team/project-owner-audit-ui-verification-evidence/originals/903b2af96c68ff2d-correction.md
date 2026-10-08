# aud-owner01 旧取证边界补正 — STOP

原report `feb11294…` 中“原native bytes/请求URL绑定由实际冻结checker在内存执行”过强，本补正撤回其中“浏览器native bytes已比较/独立EOF证明”的含义。原报告、manifest `405edc01…` 和run原件保持字节；actual PASS、schema17、公开client重放11及完整owned退役结论不变。

固定browser-v5源码 `3cb33cf8…` :197–226明确只监听Playwright response事件的URL/status/X-Request-ID与内存跟踪ownerID，不clone或读取浏览器response stream。:294–298注明fixture保存完整、预丢失safe上游bytes；:352–407以event request ID关联endpoint/status，并将磁盘原件注入新Response给公开Owner client重放，list/resolve参数取event URL，get/resolve使用所捕获ownerID。:421–425只落聚合schema/client计数。

正确表述为：17份完整safe上游原件通过schema；11次GET按浏览器response-event关联后进行公开Owner client重放（list2/resolve4/get4/problem1）。它验证该原件的客户端解析，不证明浏览器实际收齐这些bytes或独立native EOF。URL/owner的Map关联是实际代码事实，但没有逐请求持久source/owner/完整body transcript。manifest旧 `legacy_Owner.native_public_client` 字段名同属过强标签，应按本补正解释为 `response_event_matched_public_client_replays`，数值未变。

仅核冻结scratch副本上述必要片段（工具65cc20），未动态执行/读取当前业务源或重扫图。Project Audit新协议的53份完整EOF及独立原byte校验不受此补正影响。最终四轮组合另封修正版，原组合final01也保留。
