# Knowledge 全项目标题查找 UI

全项目字面标题查找已获有限独立验收，专属 codec、观察控制器与组件接入生产页面和唯一 Session knowledgeRead owner。产品固定 `f09827a8`，原 App/浏览器方法固定 `9ec39517`；最终主线组合以 `34ea0b1a` 保留已交 Sprint 与 Secret/Audit 增量。

消费[既有 Owner metadata HTTP](d12-knowledge-owner-read-http.md)、[D12 查询规格](d12-knowledge-documents-design.md#6-查询删除与生命周期边界)与[知识库页面](../../frontend-design/layouts/knowledge-base.md)。默认 App 的 `bindAccounts` 已构造真实 Knowledge Service、Account boundary 并安装 `knowledgeSkillRoutes`；`SearchTitles` 在原 Store 事务中执行全项目 active 文档查询和逐项祖先读取。正文创建/替换及当前回读的实际依赖沿[已交文本 UI](d12-knowledge-text-ui.md)，不凭 Schema 假定接线。

用户输入标题文字后明确查找；大小写、空格、`%/_/!` 原样保留，不作通配符、不检索正文。接口 `GET /api/v1/projects/{project_id}/knowledge/documents/search-titles` 使用 `title_query`、`limit`、可选 `cursor`。默认页长 50，上限 200；完整编码 query ≤32 KiB、cursor ≤8192 B。空输入不发送查询，可清空返回目录。命中为安全 Document 加完整 root→parent 祖先；每页完整校验身份、路径、排序、唯一 ID 和字面匹配后才发布。跨页按 Document ID 合并，不承诺快照。

控制器捕获原查询、游标及当前身份/Project/read generation；编辑查询、页面失效或离开立即退役本次观察，不让迟到结果覆盖新上下文。取消仍归唯一 Session 请求 owner，原 fetch/read/cancel 尾未退出时不能另发请求。只读筛选属于页面实例，同 Session checking 卸载时清除输入/结果/游标，不新增 App 持久草稿。既有正文与改名命令草稿、Unknown 和未保存确认保持；选择命中在确认后重新核查询/身份，再交原文档选择器读取当前 metadata/ancestors/content。

首批专属20控/type通过，原件在 `output/ai/knowledge-title-search-ui/checks-01/`。接线10条定向控制在最终冻结输入 integration04 整轮通过，覆盖完整响应/坏UTF8/重复JSON/大小上限、真实Session取消原尾/换Project/同Session checking，以及实际App改名与正文未保存确认；旧Knowledge相关76控通过。`checks-02/` 保留两轮测试fixture原失败、最后定向修验与type/fresh dist-01构建实际0及输入前后相等。原 `TestKnowledgeTextWeb` 的默认 run/Account/PG/MinIO 方法已由本片真实链复用；旧输入两次fixture失败、局部排版前SHA未存原源码的证据边界及最终固定输入复验均保留。

本片不实现移动、上传、删除、Jina/Image 来源获取、语义检索、生产 initializer 或 Object Runtime join；既有 ready/STOP 边界不变。

真实链入口为 `TestKnowledgeTitleSearchWeb/literal-title-and-current-read`，原 App 使用正式文本创建 HTTP 准备父子文档。父正文含 `%_!` 而标题不含，子标题为 `操作 %_! 指南 中文🙂`；浏览器未加载子层即可通过字面查询命中唯一子文档并取得完整祖先，键盘选择后独立读取当前 metadata/ancestors/content，再查无命中并清空。Go 逐请求核原 handler、canonical DTO/正文、同RID/Host/EOF/Close/return及持久事实不变；PW核原 native reader、public consumer、完整响应与退休。没有浏览器写入或 Lookup。

`output/ai/knowledge-title-search-ui/native01/` 保存 invocation、2446项input-hashes、result、terminal和evidence；编译原件在 `compile-app01/`。原 outer工具31234由作者亲收exit0，141.189s；1top1sub19.38s，9GET全部200/finished，两次search且all_observed=true。两PNG为1440浅色当前正文与390深色文档树命中；非作者亲视并核业务/联合证据。Node/Go/driver/四Chrome原Wait0、七资源14次absence、private/runtime/desc及HOST_TCP双尾、inputs_unchanged与supervisor terminal0齐，有限whole接受。最初 `/tmp/ktw01` 重名预检FAIL保留且原目录未动，实际使用fresh `/tmp/ktsw01`，不回填预检终态。

正常首链不证明 failed GET 补证分支实际执行，也不外推真实多页查询/撤权/取消竞争或命令Unknown矩阵；对应受控基础验证与真实链分列。测试用已有Project和私有静态托管不证明生产initializer或SPA部署。原文本/读取/改名历史FAIL及既有STOP继续保留。
