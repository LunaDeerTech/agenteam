# browser-v2：J1 IPC ack 修正

正式独审发现 browser-v1 对所有 ack 要求不存在的 ok=true。backend04 实际只返回 sequence 与 action-specific 安全字段，v1 首次 IPC 会失败；尚无真实资源运行。因此不能使用 v1 进行真实验收。v1 源副本、freeze、检查、原说明全部原字节保留。

本版只修改 #21：按冻结 Go 十种 action 精确校验顶层 ack keys 与 sequence；observe 验非负安全整数计数/布尔；observe/update/reuse-name 验实际使用的 Project id、Owner、description、十进制 version，前二还匹配目标 ID，避免把可能省略 archived_at 的服务 ProjectRef 当完整 HTTP 投影；hold-status 校验双布尔；archive 必须 fact_only=true。只取必要安全投影，未改 Go、协议、产品 UI 或其它场景。

#20 config SHA 与 v1 完全相同；imports.json/tools.json 原字节复用 v1，实际 source imports 闭包仍为4个 API源、3个固定 schema。schema checker/五场景正文无修改，原受控 schema 结果沿用；真实同 body/API、浏览器、PG、截图仍 pending。

最终格式、严格 TS/checkJs、五 mode各精确1项 --list 均PASS，见 checks.json：actualwait、前后输入同、双 owned空。format write01 自然 inputs_same=false，原记录保留。两源当前 STOP，供 root/runtime 绑定 browser-v2；无真实资源或 web/dist 访问。
