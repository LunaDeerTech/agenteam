# D05 Human Artifact 默认 App 接线

状态：Human Service 片实现、独立审查、必要基础检查及默认 App 的真实 PostgreSQL/MinIO 验收完成；后继当前 Owner 只读 HTTP 与浏览器调用链的实际结果见下节，视觉评审由 UI 片另行记录。这些结果不代表 D05 全部入口、创建 UI 或 E01 完成。

依据：[D05 Artifact 组合服务](d05-object-storage-design.md#7-b02artifact-组合服务)、[D08 当前授权](d08-project-owner-design.md#4-身份端口与锁纪律)及原 Artifact Project-call ownership。

## 默认装配与授权

默认 App 在构造原 Object Service 前固定 Artifact OwnerProvider，并将 Artifact 事实检查器加入同一个 Project Authority 的初始 AuditFacts。业务 pair 复用原 Store、Object、ProcessGuard、Project Authority 和 Life，不另建实例或运行后替换端口。

Object Runtime.Initialize 会同步恢复，并可能调用预先固定的 Artifact owner；该 owner 只依赖原 Store 和 Project authority，不回指尚未构造的 Artifact 业务 pair。原 Object admission 和 root 资源链持有初始化实际返回；Initialize 成功返回后才构造并安装 pair，之后才启动周期维护和 Account.Start。这是实际装配与源码核查结论，不宣称所有遗留重启状态均已动态验证。

Human 内联创建沿原命令身份、Object 上传和发布事务执行，重放保持同一结果。List/Read 每次核当前 Session、Owner 与 Project；列表审计使用绑定原查询、分页、实际结果、AppendKey、Store 和 live Tx 的私有 page 见证。原 typed reader.Close、审计和 Store 外层返回均归同一调用所有者。

首次 `CancelCreation` 先核当前 Human 所有权，再在原锁内核完整命令、引用和 stable actor，拒绝已完成产物后写取消事实。后继 ObjectMaintenance 必须另行消费原 Artifact owner 的精确已取消 command/upload-intent 私见证，绑定 Project、owner、Object、upload、cause、Store 和 live Tx，回调结束即失效，只授 Converge。普通 Human/Service Converge 均限 active/archived；局部显式拒绝 archiving，保留全局 Read 规则和 deleting 的原 exact cleanup family。

Account Stop/Drain/Force 保留原 context，Artifact pair 实际 Joined 前，后继 Account provider、Guard 和数据库不得提前退出；晚安装也消费同一退出门。

## 实际验收

受测源 `ba54e297`、薄入口 SHA `9d440629…b0af08`。基础检查按测试名去重为 **22 top / 102 sub，3 包 vet 全通过**：Artifact 9/49、Project 8/39、App 5/14。复验不重复计数；[基础摘要](../../../output/ai/human-artifact-app-delivery/basis-summary.json)保留各轮原输入与结果。

默认 App 在 Runtime disabled 配置下运行 `TestHumanArtifactDefaultApp`，实际 **1 top / 3 sub 全通过**，Go 24.45s、外层 Wait 0 / 54.903s：

| 场景 | 已实际验证 |
| --- | --- |
| `inline-replay-audit-and-original-close` | 正式 Human 内联创建、同义重放、列表、UTF-8 读取和真实 Audit；typed reader 关闭事务及外层 Store 返回；技术返回跨 SIGTERM 时原 root/Guard/DB 等待；B 项目可用。 |
| `published-ack-unknown-and-readonly-replay` | 最终发布真实 COMMIT 后仅屏蔽本地 typed ACK，保原 cause/attempt；同 key 重放的原 Tx 写入增量为零，无重复审计，原 Store/调用尾真实返回，B 保留。 |
| `pending-creation-explicit-cancellation` | 正式上传已完成，在最终发布 Store/BEGIN/callback 前受控拒绝，形成真实 pending tuple；显式 CancelCreation 完成，原 Object deleted、attempt cleaned、无 active lease，B 保留，无业务 SQL 种子。 |

[实际结果](../../../output/ai/human-artifact-app-delivery/native01/result.json)、[原日志](../../../output/ai/human-artifact-app-delivery/native01/browser.log)、[退出复核](../../../output/ai/human-artifact-app-delivery/native01/post.json)和[总摘要](../../../output/ai/human-artifact-app-delivery/summary.json)共同记录：1,387 个输入及薄入口未变，编译/运行的 7 个原进程退出，6 项 owned 资源两次确认不存在，3 个私有目录与 runtime 退出，无剩余子进程。唯一 88,075,085B 测试 binary 在原调用链退出后按原指纹[精确处分](../../../output/ai/human-artifact-app-delivery/compile01/binary-disposition.json)。空间 1 秒采样最低 489,836,544B，不是连续峰值。

## 修复与保留失败

文档核查发现普通 Converge 借全局 Read 门会错误允许 archiving；独立契约核查后仅在两个 Artifact 局部门补 active/archived 闭集。Human 负控先证明 archiving 的 Read 仍合法，Service 负控经真实 OwnerProvider 私取消见证后命中当前 Project 门；两门及 active/archived 正向均已通过。

原始三个夹具失败保留，必要窄复验均通过：`project-basic01` 编译失败因直接比较含函数的 LockKey，改正式比较器并独立核 EX；`app-basic01` 的 foreign-process 复用固定 UUID，改真实不同 ID；`project-basic03` 漏掉原 OwnerProvider 成功授权后的 cleanup-completed 防复活只读查询，只补精确 SQL/P 返回。没有删除私见证、原返回或授权断言，也不以重试覆盖原失败记录。

## 后继：当前 Owner 只读 HTTP

默认 App 将原 Artifact pair 的同一个 Service 接入正式 Account HTTPBoundary；安装发生在 Object Runtime.Initialize 实际返回和原 pair 登记之后、Account.Start 之前，不替换 owner 或启动新 worker。公开形状见 [Artifact Owner OpenAPI](../../../api/openapi/artifact-owner.json)：

- `GET/HEAD /api/v1/projects/{project_id}/artifacts`：原分页目录和过滤，默认 50、最大 200；cursor 绑定原 Project、过滤与排序。
- `GET/HEAD /api/v1/projects/{project_id}/artifacts/{artifact_id}/files/{file_id}/content`：完整业务引用的有界读取，默认 8 KiB、最大 64 KiB，保持原 UTF-8 窗口和投影语义。

显式 DTO 只暴露业务引用与授权后的 metadata；`object` 仅含 `media_type/byte_size/sha256`，不含 StoredObjectID、地址或下载 URL。每请求生成独立业务 attempt；HEAD 仍执行原 Service、typed reader.Close、Read Audit 和编码，只抑制响应正文。2 秒入口预算不会替代原调用和技术返回的实际 join，读请求仍有真实 Audit 及 Project-call 技术写入。

新 HTTP 包基础检查 **11 top / 61 sub** 与既有 App 直接兼容 **3 top / 7 sub** 均通过 race，两个包 vet 通过，合计 **14 top / 68 sub**；[基础摘要](../../../output/ai/artifact-owner-http/basis-summary.json)保存各自原输入与进程退出，不与前述 Human Service 的基础计数合并成一次全量运行。

`http-native01` 使用受测源 `c581a6a`、Runtime disabled 的默认 App、真实 PostgreSQL/MinIO 和 listener，精确运行 `TestArtifactOwnerHTTPDefaultApp`：**1 top / 2 sub 全通过**，Go 38.45s，外层原 Wait 0 / 80.34s，未启动 Node：

| 场景 | 已实际验证 |
| --- | --- |
| `pagination-current-owner-and-utf8-head` | 正式 Human Service 准备 51 项，真实 HTTP 默认 50+1 页及排序、字面名称/类型/MIME 联合过滤、cursor 跨过滤/Project 拒绝；P/A/F 精确引用、有界 UTF-8 和错误边界；GET/HEAD 长度与真实 Read Audit；当前非 Owner 404 拒绝且不泄露 metadata，错误业务引用 403。 |
| `original-reader-handler-and-owner-return` | 原 typed reader.Close 的 Store 返回被持有时，HTTP 和 Artifact 调用未提前返回，Project B 仍可读；HEAD 的原技术 Store 返回跨停止时，即使 ledger 已关闭，pair/Account/HTTP/root、Guard 与数据库仍等待原返回；释放后原 HEAD 完成，退出链闭合。 |

[实际结果](../../../output/ai/artifact-owner-http/http-native01/result.json)、[原日志](../../../output/ai/artifact-owner-http/http-native01/browser.log)及[退出复核](../../../output/ai/artifact-owner-http/http-native01/post.json)记录：本轮四个原进程退出，六项 owned 容器/网络资源两次确认不存在，私有目录及 runtime 消失，无剩余子进程；受测输入和薄入口保持一致。[编译复核](../../../output/ai/artifact-owner-http/compile01/post.json)记录 85 包、1,394 个输入及 88,415,581 B 共用 binary，HTTP 完成时保留给后继浏览器消费者，不能据此宣称该 binary 已处分或浏览器通过。空间 1 秒采样最低 350,711,808 B，不是连续峰值。

本 HTTP 片没有实际产品测试 FAIL；编译准备脚本曾读错摘要字段，未运行 Go 即修复，不计作产品编译失败。Web 的独立结果与失败保留在其任务记录，不纳入以上 HTTP 通过范围。

### 浏览器读取调用链

`web-native06` 在上述默认 App 读取入口上运行 `TestArtifactOwnerWeb/catalog-and-bounded-read`，**1 Go top / 1 sub、1 个 Node 浏览器用例通过**：Go 38.01s，外层原 Wait 0 / 66.018s。浏览器实际发出 20 个 Artifact GET，包括 10 个目录、9 个内容读取和 1 个当前非 Owner 拒绝；20 个原响应都完成 EOF、Close、解析和对应 handler 返回，浏览器消费与退出记录均闭合，没有浏览器 Artifact 写请求。这里的创建种子仍来自正式 Human Service，不是创建 HTTP 或创建 UI。

[本轮结果](../../../output/ai/artifact-owner-http/web-native06/result.json)、[原日志](../../../output/ai/artifact-owner-http/web-native06/browser.log)和[退出复核](../../../output/ai/artifact-owner-http/web-native06/post.json)记录原 Node、App、proxy、Guard 与数据库退出；六项 owned 容器/网络资源再次确认不存在，私有目录、runtime 和原进程均已退出，受测源码及薄入口未变。原 `web-native01` 至 `web-native05` 的 FAIL 保留，不以后轮诊断倒填首轮未知原因，也不把中途 HTTP 200 当成整轮通过。四张本轮截图的视觉结论见 [Artifact Owner UI](d05-artifact-owner-ui.md)，本卡只据上述证据确认真实只读调用和返回链。

## 范围限制

原 Human Service 片覆盖默认已安装 Service 的 Create/List/Read/显式取消和原 owner 退出；本轮后继 HTTP 仅开放上述目录与预览。HTTP 创建、原意图 Lookup/取消及创建 UI 尚未交付，验收中的 Artifact 来自正式 Service 夹具，不是用户通过页面创建。Execution、Operation、Tool、ToolCall provenance 及未配置的 Source 通路仍未绑定；原 uploads/cleaner 库依赖保留，但不宣称上传、下载或 Source 对外入口交付。

不自动保存模型文本，不改变 D22 text-only，不启用全局 Lifecycle Registry，不新增迁移或维护 worker；不宣称完整 Project Delete、Runner→Artifact、全部重启状态或 E01。此次 Unknown 是真实提交后的本地 typed ACK 控制，不是外部网络故障。没有模型调用或 Runner Invoke，未恢复外部网络故障探针；所有既有 STOP 保持。
