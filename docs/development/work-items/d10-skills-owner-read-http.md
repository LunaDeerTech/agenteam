# D10 Skills 当前 Human Owner 目录与详情 HTTP

状态：独立 HTTP adapter 已实现，作者限定 pure/race 11top/47sub、Schema21/HEAD18 与 vet 实际通过；未参与者已有限静审产品。PG/native 源码已形成但尚未编译或实际运行，因此本结果未交付。基线为正式 main `3b7ed9da`，消费已接受的 [P2 初始化与当前 Owner 读取](d10-skills-initialization.md)及[正式设计](d10-skills-initialization-design.md)。本卡交付独立只读 HTTP adapter，不完成完整 D10。

## 范围与真实依赖

公开构造为 `NewHTTPHandler(service *skill.Service, boundary *account.HTTPBoundary) (http.Handler, error)`，两者必须非 nil；构造不启动 I/O 或恢复，生命周期归调用者。包内私有接口仅供测试。实际调用 P2 `ListSkills`、`GetSkill`，不调用 `OpenPackage`，不直接查询表或生成初始化事实。Account 负责浏览器来源与当前 Human Session；P2 在同 Store 活 Tx 的完整 User/Project/Skill 锁下重验当前 Session、Owner 与 Project Read gate，管理员没有跨 Owner 豁免。

当前 P2 目录只有已发布的 builtin Add Skills 一项，不开放外部安装分页、搜索或排序参数。未初始化 Project 沿 Project gate；Project 已初始化但本域没有 published 初始化时沿 P2 `INVALID_STATE`，不得转换为空目录成功。合法归档保持 Read，Deleting 或其它不准读取阶段沿现有 Project 错误。一次成功读取不缓存后续授权。

本结果不接默认 Central root，不改变 P2、Cleanup、Project 或 Account 实现；无迁移。不含包/正文流、安装、分配、Agent、UI 或新 Project 创建。Object Runtime join、OpenAI tools 独立动态、SPA concurrent-publication、Jina/Image 四停项保持。

## 路由、输入与响应

| 路径 | 方法 | 真实服务 | 成功表示 |
| --- | --- | --- | --- |
| `/api/v1/projects/{project_id}/skills` | GET、HEAD | `ListSkills` | `{"items":[SkillMetadata]}`，恰一项 |
| `/api/v1/projects/{project_id}/skills/{skill_id}` | GET、HEAD | `GetSkill` | `SkillMetadata` |

两 ID 必须是 foundation 可解析且与规范字符串逐字相同的 UUIDv7；不接受尾斜线、多余段或路径别名。两路由均不接受任何 query，包括空 `?`、重复/未知 key、cursor/limit、分号和非法转义。请求体必须为空：拒绝非零 Content-Length 或 Transfer-Encoding；未知实现的零长度 body 至多读取 1B 并要求实际 EOF，所有路径实际 Close。

先执行 Account 浏览器检查和 RequireHuman，再分派路由、方法及参数。已通过身份前置的其它方法返回 405 和 `Allow: GET, HEAD`；未认证或非法 Origin 仍沿原 401/403，unsafe 方法保留 Account 原 CSRF 检查。缺失/非法 request ID 不进入领域读取。非法已匹配路由参数为 400，未匹配路由为 404。

`SkillMetadata` 显式投影八字段：`id`、`project_id`、`name`、`normalized_name`、`description`、`protected`、`current_revision`、`version`。ID/版本沿正式标量；不输出 RevisionMetadata、ObjectID、对象 key、manifest、包摘要、正文、租约或创建私有事实。先完整 `Metadata.Validate`，核目标 Project、详情 SkillID 和目录恰一项，再完整编码；坏结果统一安全 `DEPENDENCY_UNAVAILABLE`，不发部分成功。最多 64KiB 表示，足以容纳当前正式字段的 JSON 最坏转义大小；未来扩目录须另改正式契约，不截断或假分页。

成功为 200、`Content-Type: application/json`、`Cache-Control: no-store` 和准确 Content-Length。HEAD 执行同授权、查询、完整校验与编码，成功长度与对应 GET 表示一致，但不写 body。错误原样交 Account HTTPBoundary：保 Fault、commit_state 和内部 cause；公开 Problem 不含原错误/SQL/原请求路径或 query，instance 沿正式 `/api/v1`。HEAD 所有错误同样无 body，但保留 Problem 表示类型/长度等 header。跨 Owner、未知 Skill 沿原 404；失效 Session 沿原身份错误；合法形状的未初始化/非法阶段沿原 409；未绑定/不可用及读事务 Unknown 沿原 503，不发布候选。不把后续 GET 当原 Unknown 提交确认。

## 总预算与资源责任

从浏览器检查前开始一次 2s 总预算，继承更早 parent deadline；包括身份、body 探测、真实 SQL/Rows/Tx 尾、完整编码、写入/Flush、body Close 与取消 callback 实际 join。复用已验 [Knowledge Owner HTTP](d12-knowledge-owner-read-http.md)的原 I/O 方法，在本包保留窄实现，不改共享库。原生 read/write deadline 必须可用，unsupported writer 安全 abort；取消/期限/短写/Flush/Close 错误不得改发成功或重复 Problem。原 callback 实际退出后才能清 deadline、复用连接。构造和请求均不增加后台恢复/worker。

GET/HEAD 不产生 Skill、Object、Audit、Event、receipt 或业务 Activity 新事实。只读事务的实际终态仍必须确认，读 COMMIT Unknown 不能发布候选。HTTP 只承诺原领域读取的事实与本请求 I/O 退役，不宣称 Project 全域停止或 Object lease 已清。

## 必要验收与写域

纯控制核严格路由/query/body、两 GET/HEAD 同体长度、安全八字段/坏结果/跨 Project 或目标、身份先行、原 Fault/Unknown 保留，以及 deadline/短写/Flush/body Close/原取消 callback join。Schema 检查覆盖成功和 Problem，HEAD 所有状态不声明 body。

真实 PG 组合消费正式 Account 登录/注销、P2 初始化与真实 Store/Project gate：目录/详情、未初始化/未知/跨 Owner/admin、归档可读/Deleting拒绝、每请求撤权及 Owner 变化、原 SQL 取消与 Tx 尾、读 COMMIT Unknown 零候选；独立连接核无新增业务事实。初始化/生命周期上游 fixture 必须说明是否真实命令或合法 seed，不冒 Project.Create/BeginDelete。native 组合覆盖原 2s/更早 parent、同连接 deadline 清除、真实写背压/断连及 callback/handler 退出。真实窗口另由 root 调度，纯控不代 PG/native。

唯一作者写域：新 `internal/central/skill/http/**`、`api/openapi/skill-owner.json`、新 `tests/skills/owner_http*`、本卡和本树 `.agent-state/current.md`。不改依赖锁文件、迁移或默认路由；必要运行入口按真实验证准备另交接。短规格由未参与实现者静审，产品与高风险验证另按稳定输入独审。
