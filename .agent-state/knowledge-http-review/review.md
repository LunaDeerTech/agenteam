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

## 35b62908 wire/schema 增量

静核显式12字段投影、Human/AgentRun原闭集和同Project校验；隐藏ObjectID仍先由正式DocumentRef.Validate验证，不进入JSON。Get只允许active或最小4字段tombstone；page核limit、所有项/过滤/顺序/重复，坏尾项返回nil而不发布前缀。祖先按根起连续/去重/无自身逐项验证；Search另核末祖先与文档parent一致。单独ancestors的完整性来自真实Service同Tx路径，不声称仅靠target ID就能重建其父链。逐个有界DTO编码到私有5MiB表示，链超界返回错误，不把大链一次交json.Marshal或静默截断；handler在实际发布前另核原绝对deadline。

发现并返修一项schema must-fix：原5条HEAD的8种error都引用带content的Problem，实际Account HEAD不写body。独立 `head-schema.py` 原样执行abcfdd actual1明确40处；作者仅改专用无body HeadProblem后，cc7bba actual0全部清零。实际比较35b62908：GET/通用Problem/HEAD200/全部DTO schemas保持；90e24c另核10个唯一operation及GET/HEAD同身份/query与bodyless安全表示headers。原红保留，不是产品HTTP故障。

`risk_test.go` 与 `run-risk.py` 两个独立pure top已在根缓存协调后实际执行：真实handler的AgentRun字段/foreign creator拒绝、坏末项不得发布首项；原Body.Close与AfterFunc分别持有，需在实际原finish函数阻塞channel的栈证据上确认body退役后仍等callback，非用sleep或即时select猜测。采用只读overlay排除作者尚未稳定的native/schema tests，固定Go/local/off/本树独占cache，只选两个新top；75434→0db8c1 actualrace0，Go包耗时1.023s；首同进程UTC2026-10-09T23:06:08.805785Z可用5,993,074,688B，178048核实际终态后overlay临时目录为空、自有cache增加76,869,632B，23:07:06全局可用5,865,771,008B。没有TestMain/PG/native/浏览器/OS子项，未写作者源或作者cache。权限和domain均明确复用作者private doubles，不替真实Account/PG验收。

当前有限独审结论：冻结生产Go及修后Schema无剩余must-fix。原abcfdd HEAD schema缺陷、作者6157错误cause_id测试预期与修后67777真实终态分别保留。独立控制只证明安全投影和原取消/Close/callback接线；真实Account当前Owner/权限锁序/PG读COMMIT Unknown、自然socket期限与keepalive等动态门仍待后继验证，不能据private ports直接交付生产root。

## 四 top 最小 root 入口

固定 `bb98b6cd` 加作者冻结的两工具、控制及两文档增量。唯一新 selector 为 `^TestKnowledgeOwnerReadHTTP(Metadata|CurrentAuthority|Transactions|CommitUnknown)$`，映射到 tests/knowledge，supervisor expected 恰四 top。原工具预算、七资源、清理、TCP 和 input 尾不变；不移植其它树的 TCP 方法。

本人 `1f7561` 实际执行作者 `python3 -B .agent-state/knowledge-owner-read/root-selector-controls.py` exit0：新增精确行逆删后两工具逐字基线、config 1 正6负、observer 22格每格14精确资源替身观察。旧已通过产品/Schema/独立pure范围复用，未重复业务矩阵。

本人 `d2262e` 实际执行本目录 `root-input-controls.py` exit0。新 input_paths 是原集合加且仅加当前解释器、Schema脚本、knowledge-owner.json 和 common.json，均存在；旧 TARGETS 不变。直接执行原 main 的单个 env.update AST，确认继承的错误解释器值被覆盖为输入记录中的同一个 SCHEMA_PYTHON。实际 Go 使用该 env 值执行原 CommandContext/CombinedOutput、15s 子预算和本地脚本；脚本只从已记录两份本地 Schema 建 registry。四遗漏和错误解释器替换负控拒绝。没有执行 main/execve、业务 candidate、PG/socket 或 Go 编译；脚本所用既有 Python 包仍是本环境依赖，不声称额外冻结全部解释器安装。

结论：此最小入口有限接受，无 must-fix。候选四 top/14子的真实业务、原生 I/O 与实际七资源完整尾尚未运行，不由 selector 或替身观察升级为通过。
