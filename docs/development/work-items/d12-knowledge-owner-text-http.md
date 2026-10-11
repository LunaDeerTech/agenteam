# Knowledge Owner 文本命令 HTTP

当前为已实现并完成后端真实小链验证的有限后继，基于 main `29666627`。消费已交付的 [canonical 文档合同](d12-knowledge-documents-design.md)与 B02 `CreateDocument`、`UpdateDocument`、`LookupCommand`，不增加领域状态机、迁移或外部依赖。完整公开形状以 [OpenAPI](../../../api/openapi/knowledge-text-commands.json) 为准。

## 请求与恢复

`P=/api/v1/projects/{project_id}/knowledge/documents`，三个入口均 POST，当前 Human Owner、原 Account 来源/CSRF/Session 检查及 `Idempotency-Key` 必需。先认证，再处理路由/method。路径 ID 为规范 UUIDv7；拒 query、尾 slash、未知/重复 decoded JSON 字段、孤立 surrogate、坏 UTF-8 和 Content-Encoding。写入完整编码请求上限 1 MiB（1048576 B）；Lookup 外层上限为 1048660 B，嵌套原 request 仍至多 1048576 B。规范 command/UUID 的最小包装恰增加 84 B，因此每个已接受的写请求均能原样包装查证；多余空白仍计入各自 encoded 限额，不是纯文本字符上限。

- `P/{document_id}/create-text`：`{"parent_document_id":null,"title":"Notes","media_type":"text/plain","text":"原始正文\n"}`。客户端先固定 DocumentID 和 key；显式 null 为根，否则当前 active 父必须同 Project，且不是自身。服务在真实事务中复查。
- `P/{document_id}/replace-text`：`{"expected_version":"1","media_type":"text/markdown","text":"# Notes\n"}`。只替换 source，不改 title 或 parent。保留原 UTF-8、空字符串和换行；media 仅 plain/Markdown。实际变化按原服务推进 version，无变化可返回原版本。
- `P/text-commands/lookup`：`{"command":"create","document_id":"原ID","request":原完整create请求}`，替换用 `command:"update"` 及原 replace 请求。仍携原 key；服务端以当前 actor、原 Project/Document/版本/全部文本语义调用正式 digest。客户端不得传任意 digest。Lookup 本身不执行命令；明确重放仍使用原 key/原完整请求，不自动重试。

写成功为 200 `{"document":安全metadata}`；Lookup 为 `{"state":"committed|in_progress|not_observed","receipt":null或{"command":"create|update","document":安全metadata,"changed":布尔}}`。未 committed 必须 null；create receipt 为 changed=true/version1。安全 Document 沿既有字段，不包含 ObjectID、正文、key、source/upload/lease 或内部错误。历史 receipt 不授予正文访问；确认成功后沿既有 metadata GET 和 content GET 重新读取当前数据。

Unknown、版本冲突、失权和原 Service Fault 保持其执行状态，不因 HTTP 包装改判。成功 metadata 必须完整校验 Project/Document、source/media、create父与标题或update版本；提交后的坏投影直接 abort，不能输出误导性新成功或 not_started Problem。

## 生命周期与接入

沿相邻 command HTTP 的同一 2s 总预算，继承更早 parent deadline，涵盖 Account、input/body Close、真实 source publication/事务、编码、write/Flush 与取消 callback 实际 join。不能支持原生期限则 abort；SourceInput 仅有内存文本，仍沿原服务的资源关闭责任。所有正常/失败尾均须实际返回，不以取消信号推断 joined。

默认根通过现有 Knowledge Service/Account boundary 接入，保持既有 handler 返回签名。已有 initialized Project 是前置；测试只对该前置显式使用原 test-only initializer。生产 Project initializer 仍 unbound，Object runtime join STOP 未恢复；此 HTTP 不宣称完整 Project 生命周期、上传或索引能力。

## 验证与接续

写域为新 `knowledge/texthttp`、独立 OpenAPI、`tests/knowledge/owner_text_http*`、最小默认根注册和本域 App 测试。基础race检查覆盖原文字节/intent、输入拒绝、Unknown安全投影、Lookup不执行，以及写入与原Lookup的精确encoded边界；原失败与修复后实际结果均保留，不以编译或schema存在替代业务通过。

2026-10-11，固定 `d74df012` 源码经原 `root_chain_driver.py` / `pg_only_supervisor.py --root-chain` 执行两个精确选择器：`^TestKnowledgeTextHTTP(Publication|CurrentAuthority)$`、`^TestKnowledgeTextHTTPDefaultRootComposition$`。两者实际退出0，精确RUN/PASS、test/driver实际Wait、七项自有资源两轮清除、私有目录/runtime/descendants、host TCP尾门及输入未变化全部通过。真实Account/PG/MinIO验证plain/Markdown创建、正文替换、当前metadata/content读取、原命令Lookup/重放、无变化和版本/意图拒绝；当前身份场景验证其他Owner、管理员、已注销Session及失去Owner后的拒绝。

App验证在测试进程内调用默认 `run` 并通过真实listener访问正式服务，关闭刺激为signal channel中的SIGTERM，覆盖producer join、ProcessGuard释放及数据库关闭；不宣称外部Central子进程实际OS信号。真实响应丢失/Commit Unknown故障、撤权竞争与浏览器联调不在这两条后端验收范围。精确命令、原件路径与作者资源终态见[本树接续记录](../../../.agent-state/current.md)，独立审查及UI后继由收口owner另行记录。
