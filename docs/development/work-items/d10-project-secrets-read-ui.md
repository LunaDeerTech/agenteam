# D10 Project Secrets 安全目录与详情 UI

基线 main `43590d1f`；实施 variables_review，独审 recovery_lead，root 负责 Git。当前产品 `30f4b8e1`、方法 `e7466973` 与 native02 已有限独立接受，待 root 统一 Git 集成。native01 实际焦点 FAIL、早期控制/构建 FAIL 及全部生产 STOP 保留。

## 合同与范围

消费已实际绑定的 [Secret Owner HTTP](d10-secret-variables-owner-http.md) §§1–4、[正式 Schema](../../../api/openapi/secret-variables.json) 和 [项目设置布局](../../frontend-design/layouts/project-settings.md) §6。仅 GET `/api/v1/projects/{project_id}/secret-variables` 与 `/{variable_id}`；不新增后端、迁移或材料读取端口。

目录及详情都只接受八个必填非 null 字段：`id/project_id/type/name/description/version/created_at/updated_at`，`type` 固定 `secret`，拒所有额外字段。不能复用含 value 的普通 Variable DTO。严格 UUIDv7、正 int64 版本字符串、ASCII 环境名及保留前缀、Unicode/4096 UTF-8 字节描述、非零与有序时间；目录整页完整校验、同 Project、ID 唯一、C(name,id) 升序后发布。默认 limit 50、最大 100；opaque cursor ≤8192 字节、缺省不是 null；list 完整响应上限 5 MiB、detail 1 MiB。HTTP 原 read 2s 服务预算保持；前端沿唯一 Session 既有有界实际返回机制。

当前 Human Owner/Session 原领域事务授权，active/archiving/archived 可读，pending/deleting 拒；管理员没有旁路。页面仅展示名称、描述及安全身份/版本/时间。不显示未提供的有效性、授权 Agent、值、密文、CredentialRef、摘要、长度或掩码。不提供创建、改值、删除、授权或 Lookup。

## 页面与状态

精确路由 `/:username/:project_name/settings/secrets`，在普通变量旁增加设置菜单。页面实例持有目录和本页选中 ID，选中后独立真实 GET，刷新明确重新读取当前目录或详情。无草稿，不增加 App 全局编辑器。

独立 `secret-read` action/revision 经唯一 `runAuthorized` 串行 owner；读取身份绑定当前 Session/user/epoch、Project/generation/readGeneration 及当前路由。checking、失权、跨 Project/Session 或离页立即隐藏并退役读观察；迟到响应不得发布。取消仅终止可见观察，原 fetch/body/cancel 真正退出之前不能让后继请求抢占 Session。

当前 Secrets 的 403/404 清安全观察，不能污染 System denied 或清无关 Knowledge Unknown；503 保留明确重读入口。分页只接同一水位，重复/逆序/跨页 ID/失效游标不得混页，提供回第一页。只有确认合法完整页才追加。

## 写域与有限验证

新增专属 `api/project-secrets.ts`、`composables/useProjectSecrets.ts`、`views/projects/ProjectSecretsView.vue` 和专属 tests；共享仅 client 两 GET、useSession 独立 read owner、router index/auth 精确 allowlist、ProjectSettingsView 菜单。Move 共享 client/Session 增量由 root 最终精确并集，不能覆盖其已验内容。

受控验证涵盖严格 DTO 与材料额外字段拒绝、整页原子发布、实际 Session/Workspace 身份及 checking/迟到/失权/503、真实取消尾、过期游标与页面选择刷新。真实浏览器采用默认 App/自有材料 fixture，明文只由 Go 持有且不得送入浏览器或证据。前端 controls、静审和真实浏览器分别报告。生产 initializer、Agent F1/材料消费、完整 Secret CRUD UI 及全部既有 STOP 保持。

## 限定验证与真实方法

首产品 `f9c6c9fb`、测试 `45d4bf15` 的原离线结果如下；native01 后 View 焦点差额见末段。`checks/unit02` 整体 actual 1，但 API/state 两文件 20 控通过；页面 fixture 修补后 `page06` 四控 actual 0，属于分段 24 控，不是单次全绿。`regression01` 六既有共享文件 133 控 actual 0；`build02` type/build actual 0，327 输入前后相等，fresh `dist-01` 80 项与 web/dist 逐项相等。独审 recovery_lead 已接受这些有限结果。旧 unit01 的错误消息断言、unit02/page03–05 的页面 fixture FAIL、build01 的测试元素类型编译 FAIL 均保留。页面控的焦点只证明 jsdom；真实 Chromium 键盘与 disabled trigger 焦点由下述原链另证。

真实方法只含 `TestProjectSecretsWeb/metadata-pagination-and-detail`，沿默认 App run/listener、真实普通 Owner/Session 与 test-only Project 初始化 fixture。51 项由正式 Secret Create 服务产生；浏览器按 50+1 两个真实目录 GET、选中末项独立详情 GET、显式详情刷新 GET 执行，零写入/Lookup。每次动作都核当前 typed DTO、原响应同 Request-ID/Host/完整 EOF/Close/handler returned；第二页必须使用第一响应的真实 cursor。Go 内存保随机 Secret 值、受保护字段和前后 SQL 快照，不将它们或其摘要送入 Node、截图或证据。浏览器材料只有必要私有 Session Cookie/route 和安全八字段 DTO。

专属 PW/config/helpers 加共享观察器显式 `secretReads` 只读闭集；两原 harness 仅新增精确 App selector/1 sub/1 Node，保所有旧有效映射及原预算、Wait、七资源与双尾门。`methods/syntax01`、`list01` 原工具 78983 actual 0，分别 1.360s/1.825s，输入前后相等；唯一 list 项匹配该 sub，fresh dist 的 Session 单例绑定已解析。只读 profile 拒 21 种非法组合；Python `profile01.json` 证明去新增项后 AST 等基线、旧有效 maps 不变及结果门九种反例拒绝。方法已获 recovery_lead 独审接受；真实浏览器结果见下，不能以方法门通过代替产品行为。

`compile-app01` 固定 e7466973：原工具41020 actual0，App race-c70.855s、list1.077s唯一 TestProjectSecretsWeb，2461编译输入前后相同；runtime2462只增实际 binary，90 App Go/80资产。native01原工具74201 actual1，supervisor352208/123.242s；Go27.02s FAIL，Node success=false。两页与首次详情GET已到，原spec41真实Enter后标题焦点断言失败，未到刷新和两图。原View在ready后才创建标题、并以已禁用的触发按钮仍active为聚焦条件；原材料没有记录具体失焦元素，不补写它是body。七资源14absence、private/runtime/desc双尾、原HOST_TCP baseline+delta两次空及inputs unchanged均已闭合；原FAIL与dist-01保留。

授权最小修补仅View及专属page控：打开选中详情后即聚焦稳定h2，loading/失败使用通用标题，成功仅更新安全名称；异步完成不再安排聚焦，离页/失权仍清详情。原PW焦点与返回断言未改。page07加强通用loading标题/初次焦点、h2节点保持、迟到响应不抢外部焦点与离页无再聚焦，原工具37594实际0、四控4.294s；build03 type/build0、23.967s，327输入前后相同，新物理dist-02仍80项，旧失败原件不改。没有重复Go编译或其余已验矩阵；修补已独审，并经以下同一原方法复验。

native02 固定30f4b8e1、fresh dist-02，复用原binary `13b7f9d1…7f6a980`；1561原Go输入逐项不变，2462新runtime闭包除资产重绑定外仅View变化，原PW焦点/返回门未改，单Session绑定0。原工具98789亲Wait actual0、supervisor360517/148.227s；Go362517的1 top/1 sub38.03s PASS、Node362895 true、driver360581与4个adopted Chromium原Wait0。原日志`/tmp/psr02/ui-ff4ca809a9b645b7.log`收齐七资源14absence、private/runtime/desc双尾、HOST_TCP baseline+delta两次空及inputs unchanged/terminal0。原件在`output/ai/project-secrets-read-ui/native02/`，精确命令与输入见invocation/plan/input-hashes/runtime-delta。

7个GET为6 finished与seq1 Session GET实际200/ERR_ABORTED；该失败网络终态由同RID、原Go完整486B、native两次read返回/EOF/close/release及当前restore消费联合证明，不改写为finished。全部7份joint通过；四Secrets GET均200/finished，真实50+1原cursor分页、当前第51项独立详情与刷新、持久Project/D10/D04/AuditOutbox事实不变、零浏览器写入/Lookup通过。原Enter标题焦点与返回按钮回焦真实通过；1440浅色与390深色安全详情两PNG由recovery_lead本人查看，业务与资源尾已有限whole接受。该正常只读链不代真实撤权/跨Owner全矩阵或Secret写入/授权功能；生产Project initializer、F1/材料消费、完整D10/D27与既有STOP保持。当前无本轮后台、服务或cache writer，窗口已交回root。
