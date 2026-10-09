# Knowledge Owner metadata HTTP：分段独立审查

第一段稳定输入：`/workspace/agenteam-knowledge-http`，`0b74f2da`。
范围仅新 `d12-knowledge-owner-read-http.md`、current 与
`internal/central/knowledge/http/{handler,query,io}.go`；新 wire/schema/tests
尚未加入此结论。未修改作者源、执行 Go 编译或启动 PG/socket/browser。

目前无明确 must-fix；这是合同和冻结接线的静态审查，不是可编译性、HTTP
行为、SQL 或原生 I/O 退役的证明。

- 正式依据为 B02 卡§5.11、D12 design§3/6，以及实际
  `knowledge/{read,query,repository}.go`、`contract/types.go`。
  Service 查询在同 Store 活 Tx 的 User/Project/tree SH 下重新核当前
  Session/Owner Read；归档可读、未初始化/Deleting 不旁路；每页重新授权。
  adapter 的公开构造只接具体 Service 和 Account HTTPBoundary，私有 reader
  接口不构成公共替身注入口。它不直接 SELECT 外域权限表。
- 游标保持 stable user/project/kind/parent/filter/order 绑定，不绑定 limit，
  沿 title C/ID 升序及字面子串；非根 parent 必须同 Project active，完整
  ancestors 在同次读 Tx 内查询。Read 的 COMMIT Unknown 返回原 Fault/cause，
  Get/list/search 等丢弃该次候选，不把后继 GET 当作原事务确认。
- 五路资源映射先识别 children/search-titles，再解析 doc ID；不接受静态
  路径的 ancestors 别名。Account 原边界拒 RawPath/非clean路径。
  query 源码拒未知或重复 decoded key、空值、裸问号、裸分号、坏转义、
  控制符/非法 UTF8、非规范 limit；根必须显式 parent_document_id=null。
  默认50/最大200沿 Foundation，query/cursor额外限额见短卡。
- HEAD 与 GET 走同授权、查询和完整投影路径，成功不写 body；错误仍使用
  Account 的原 HEAD-aware Problem。真实 browser/session 前置在 method
  判断之前，所以未认证/跨 Origin 可以先得到401/403；短卡“其它方法405”
  不能被解释为免除这些前置。
- 2s context 在 browser 检查前创建并继承更早 deadline。原 read/write
  deadline、取消 callback 与实际 callbackDone、原 Body.Close、编码后写入及
  Flush 均处于同次请求尾；不支持原生deadline的 writer安全abort。
  `e48d7a` 实际只读比较证实三个 Go 源逐字冻结 blob，io.go 除 package 行
  外逐字等于当前 `projectvariable/http/io.go`。未执行该 I/O 行为。

后继必须基于冻结 wire 和实际风险控制确认：显式12字段 DTO而非直接
DocumentRef JSON（后者含 object_id）；tombstone只4字段；Human/AgentRun
creator闭集；全页/完整祖先链/目标与Project/active/排序去重/过滤及隐藏
ObjectID验证；坏尾项和超5MiB不得发布部分表示；HEAD真实表示长度；原
Fault安全投影及deadline/cancel/Close/短写/实际callback尾。真实Account+
B02五API/权限撤销/读COMMIT Unknown及原生慢I/O仍需以后独占窗口。

本阶段不审正文、下载、D13或生产 root，不重做已接受B02内部算法验收，
也不将作者尚未完成的 wire/test/schema 当作通过证据。
