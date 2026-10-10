# D12 Human Owner 文档树管理命令 HTTP

状态：**五个 Human Owner 文档树命令 POST、安全 DTO 与独立 Schema 已实现并完成限定独立验收**。作者 pure/race 10 top、86 sub、Schema 33 向量及 vet 通过；Runner 独立产品风险控制 2 top、9 sub 通过，Vars 入口与 Skills PG/native 方法有限接受。作者真实 PG 按未变 Authority 三子、Transactions 四子与修后 Mutations＋Unknown 六子组合通过，共 4 top、13 sub；native 3 top、6 sub 整轮通过，各实际命令与资源尾完整。

正式 main `3b7ed9da35844e3a367cc5e9da0cf424ab36499a` 装配已通过同包 integration race 编译与精确发现；未参与者在该主线的真实 00001–00027 迁移前缀运行新 Owner／旧 actor receipt 补集，唯一 top 整轮 PASS。新 Owner B 的新 key 写入与 Lookup、对 A 旧 key／原 Lookup 意图的拒绝及旧 A Session 拒绝均实际到达，原 Go/driver/outer Wait、七资源双退役、private/runtime/desc、TCP 双采与 input 全尾齐。原 PG01 整轮 FAIL 及未打印分项值的边界、独立首轮因标准 Problem title 误判而 FAIL／Owner B 未到达均保留；后者仅修八行测试 helper 判据后重验，不改产品。接受按固定版本组合，不称当前 HEAD 单次全量或完整 D12。

本结果直接消费已交付的 [B02 Service](d12-b02-knowledge-service.md)、[文档合同](d12-knowledge-documents-design.md)与 Account HTTP boundary，提供已有文档的改名、移动、删除范围确认与删除，以及原命令结果查证。作者基线为正式 main `ce65714aac6eb4995a43fc427a2c77e6497470a7`；装配保留主线已交付 read HTTP、Runner 与 Skills P2。创建、替换正文、上传、正文读取、下载、UI 和生产 root 不在本结果。

## 1. 独立包与装配

新增 `internal/central/knowledge/commandhttp`：公开 `NewHTTPHandler(*knowledge.Service, *account.HTTPBoundary) (http.Handler, error)` 和 `HandlesPath(string) bool`。公开构造只收真实服务，拒 nil；包内窄接口仅供单元测试。构造不启动 I/O、恢复或后台任务，两个服务的 Initialize/Shutdown 仍由装配者负责。

与五查询 read HTTP 负责人已核定：本卡使用独立子包、独立 DTO/IO 实现与 OpenAPI，不 import 已独立交付的 `knowledge/http`，不写其 handler/query/wire/io 或 `knowledge-owner.json`。下列五个精确路径均不被其现有 HandlesPath 捕获。未来组合根由唯一 writer 按精确路径分派；本卡不修改默认 app、共享路由框架或 read adapter。错误 method 仍归命中的本 handler，不转发到只读 handler。

## 2. HTTP 输入闭集

公共前缀 `P=/api/v1/projects/{project_id}/knowledge/documents`，全部仅 POST。先执行原 Account 的来源/CSRF/Session 前置与 RequireHuman，再判路径、method、参数；未认证/跨 Origin 不因本方法出现 405 旁路。命中路径的其它方法为 405、`Allow: POST`，未知路径 404。Project/Document 均为规范 UUIDv7，拒多余段、尾斜线、任何 query（包括裸问号）。路径日志只记录模板，不含 body、原 key 或 token。

| 路径 | 必需 JSON 对象 | 实际服务 |
| --- | --- | --- |
| `P/{document_id}/rename` | `{"expected_version":"…","title":"…"}` | `UpdateDocument`，原 Title 指针、ReplaceSource=false、source=nil |
| `P/{document_id}/move` | `{"expected_parent_id":null或ID,"target_parent_id":null或ID}` | `MoveDocument`；两个字段必须出现，null 明确表示根 |
| `P/{document_id}/delete-preview` | `{}` | `PrepareDeleteSubtree`；只读当前完整范围，非删除许可缓存 |
| `P/{document_id}/delete-subtree` | `{"confirmation_token":"…"}` | `DeleteSubtree`；提交原预览 token，不替用户刷新 |
| `P/commands/lookup` | `{"command":"update或move或delete-subtree","document_id":"…","request":原命令的完整对象}` | 先重算正式 digest，再 `LookupCommand` |

除 preview 外，单一 `Idempotency-Key` header 必需；不 trim、不转换、不生成新 key，按 Foundation 1..128 ASCII 标量验证，重复 header 拒绝。preview 不接受该 header，不生成 command。RequestID 只用原 httpapi middleware 的有效 transport ID，不能充当业务 key。rename 的 expected_version 是规范正十进制字符串；move/delete 不接受 expected_version。标题沿 B02 原文 UTF-8、1..512 scalar、无控制字符，不 trim/归一化。

仅 `application/json`（可带 UTF-8 charset）；Content-Encoding 拒绝。完整 body 上限 16 KiB，超限 413；拒空/null/多文档、未知或缺失字段、任何层级重复 decoded key、坏 UTF-8、孤立 surrogate、错误类型和额外字段。字面 null 只用于两个 parent 字段。token 只经正式 ParseConfirmationToken 校验形状，原最大 8 KiB；签名、当前 User/Project/root、时效及当前范围由 Service 复查。不得在 HTTP 层预验签名/时效而挡住合法完成重放。

Lookup 的 `command` 只允许上述三种。当前真实 actor、路径 Project、body Document、原 key、原 expected_version/parent/token 经正式 `UpdateDigest`（无 source）、`MoveDigest` 或 `DeleteDigest` 重算；客户端不得提交 user_id、semantic_digest 或删改原字段。用所得 `LookupRequest` 调正式服务，未完成/未观察都不自动执行命令、不自动重试。相同 User 新 Session 沿服务原语义；换 User 或原意图变更不借旧摘要恢复。Lookup 不接受 create/source replacement，不能假称整个 B02 命令 API 已暴露。

## 3. 当前权限与服务责任

每次调用由 B02 在同 Store 活 Tx 下重新核当前 Session/Project Owner、初始化与 Read/Mutate gate；HTTP 不查领域表、不缓存 Owner、不自行发后台 actor。管理员没有旁路。预览/Lookup 使用服务正式 Read 语义；新 rename/move/delete 使用 Mutate，归档可预览与查证但不能新写，合法完成重放沿服务次序；Deleting/未初始化不借历史 receipt 放行。

服务继续拥有原 command、锁 union、version/expected parent、防环、确认范围、Audit/Event/Activity 与 CommitResult。HTTP 不改 SQL、事件、事务、迁移或 cleanup 事实。delete 成功仅表示原子 tombstone 与可靠 cleanup 状态，不表示对象物理完成；服务的 cleanup_pending 如实输出。没有生产 root/Runtime 接入或 Object Runtime join 的新增结论。

## 4. 安全响应与错误

全部成功为 200 JSON、`Cache-Control: no-store`、精确 Content-Length。专用 Document DTO 仅有 id/project_id/parent_document_id/title/content_version/source_kind/media_type/status/indexing_status/created_by/created_at/updated_at；created_by 沿正式 Human/AgentRun 闭集，版本为字符串。只投影经过正式 Validate 且与当前请求 Project/Document 匹配的 active 结果，绝不直接 Marshal DocumentRef。

| 操作 | 精确成功形状 |
| --- | --- |
| rename | `{"document":安全Document}`；title 必须等于原输入，version 为 expected 或安全的 expected+1，不从该响应捏造 changed |
| move | `{"document":安全Document,"changed":bool}`；parent 必须等于原 target，changed 必须与原 expected/target 是否相同一致；内容版本仍由 B02 保持 |
| preview | `{"root_id":ID,"nodes":[安全Document…],"scope_digest":digest,"confirmation_token":token,"expires_at":instant}`；完整非空 active 子树，先校 Validate/根/全部 Project/唯一节点及 scope digest，再仅此 Human 响应调用 ForHumanResponse 输出确认材料 |
| delete | `{"root_id":ID,"deleted_ids":[ID…],"cleanup_pending":bool}`；根匹配，IDs 严格有序、无重且含根，不承诺物理删除 |
| lookup | `{"state":"committed或in_progress或not_observed","receipt":对象或null}`；仅 committed 有 receipt。update/move 为 `{"command":…, "document":安全Document,"changed":bool}`；delete 为 `{"command":"delete-subtree","root_id":ID,"changed":true,"deleted_ids":[…],"cleanup_pending":bool}` |

Lookup 必须同时检查正式 union、原 command、目标、Project 与相应原输入；update 的 changed/version/title、move 的 changed/parent、delete 根与 ID 集合都要校验。历史 metadata 不是当前正文读取许可。除了单独 preview 的确认材料，所有响应不包含 ObjectID/upload/lease/私有源/token/key/SQL/错误链。不得为“隐藏”坏字段而跳过其正式 Validate。

完整编码上限 5 MiB，超额或坏后项不得发布部分树。preview/Lookup 无本次 mutation，非法服务投影/超额返回安全 DependencyUnavailable；mutation 已成功后投影/编码失败必须原连接 abort，不改发 NotStarted 或 NotCommitted Problem。服务原 Fault/CommitState/Cause 分类保持，包括 Unknown；超时、写出或 flush/Close 失败也 abort，不伪造成功或回滚。未知输入 400，身份/权限/不存在/阶段与冲突等沿 Account 的正式 Problem 映射；字段错误只使用固定公开路径，不含请求原文。

## 5. 两秒与原资源尾

所有五路径从浏览器前置之前开始同一 2s 总预算，继承更早 deadline；包含读 body、认证、SQL/Rows/Tx 实际尾、编码、写出/Flush、body Close 与取消 callback 实际 join，不因 mutation/Lookup/错误新开时钟。原生 read/write deadline 不可用则安全 abort，不启动无界 fallback。先把完整请求消费/Close，再发布成功或 Problem；服务返回值必须晚于本身实际 Tx/Rows/call 退出。未实际 join 不因取消、timer fired 或 handler计数为零而记完成；取消 callback/原 Close 未返回的场景必须保留未完成事实。

正常完整结束才能清除原连接 deadline 供 keepalive 复用；过期/取消/短写/flush 或 Close 失败不能发布备用成功。单纯连接断开不能判 command 未提交，客户端必须保原意图进入 Lookup。沿既有 bounded HTTP IO 的已证方法做本包有限适配，不新增通用观察器或延长测试/fixture预算。

## 6. 写域与有限验收

唯一新增写域：`internal/central/knowledge/commandhttp/*.go`、`api/openapi/knowledge-tree-commands.json`、`tests/knowledge/owner_tree_commands_*_test.go`、本卡、current 与必要本域恢复源。不动 B02、Account、read HTTP、共享 OpenAPI/根路由、go.mod/go.sum 或迁移。标准 Schema 与真实 HTTP 输出交叉验证；独立实现的安全 DTO 不建立对 read DTO 的依赖。

验收分层：

1. pure/race：严格原字节/headers/五路与 read 路径不交叠；三个 digest 真函数与原 RequestID/key/expected/token；隐藏字段 canary、坏末项/union/版本/完整树、提交后坏投影 abort 与 Unknown 不降级；held body/Close/callback、short write、flush 与提前 deadline 的真实原尾控制。
2. 原生 HTTP：原 2s 的慢 body/慢响应、断开、keepalive 清 deadline/失败不复用、非法 method/CSRF顺序；使用原 net/http 与明确自有资源，所有真实 handler/请求/连接实际 join。
3. 真实 PG/服务：正式 Account 登录、Project Owner 与 B02 构造/内容产生；验证 rename/noop/version、move/root/expected parent、防环及同 key 异义，preview 后真实改变子树使旧确认失效，delete 原子安全输出、token 到期或 key轮换后完成重放、Lookup 三态。原 SQL/event/audit/Activity 最终事实与公开响应分开核；无手种 command/receipt。
4. 当前身份和 Unknown：真实 Session 撤销/Owner 两锁序、归档新写与原重放、同 key 两调用；实际 COMMIT ack 丢失与未提交两方向后同意图 Lookup，不把本次读的 Unknown 当作原命令确定状态。取消/锁等待必须走实际原服务和 Tx 尾。没有纯控代 PG 的结论。

固定 Go1.27.1/local/offline，复用已有 cache。作者与未参与者的上述 pure、原生 HTTP、PG 权限/事务/Unknown 和新主线独立补集已完成，原命令与各自资源尾实际闭合；首次测试类型名编译失败、磁盘不足未启动以及两次真实失败均保留。主线同包 integration 编译和精确发现已完成，已有 B02/read HTTP 及未变作者矩阵按固定输入复用，不冒主线所有业务单次重跑。此有限结果无迁移、不绑定默认 root；原四停止项与完整 D12/D08 未完成状态保持。
