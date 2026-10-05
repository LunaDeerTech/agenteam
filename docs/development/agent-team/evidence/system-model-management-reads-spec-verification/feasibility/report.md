# System Model 配置前端：有界可行性结论

固定业务输入：`c54f73f3324caa11608d84e5d207141985eb6074`。仅静态读取正式布局、已验后端和已验认证/个人设置源码；未读取活动公开账号入口、ledger、Artifact。本文是调度建议，不是规格采纳或实施授权。

## 1. 结论与建议边界

System Provider / Model CRUD、平台模型用途、Credential 命令及查证已经默认生产装配，可以支撑真实配置页面，不依赖未决 Project Summary，也不依赖暂停的 Object 修复。但**现有 17 个 HTTP 方法不足以直接完成正式布局规定的全部交互**：缺少 Credential 当前 metadata/version 读取与 Model 删除影响查询，且系统设置默认用户页尚不存在。不能用推算、空页面或刷新后失去编辑能力覆盖这些缺口。

建议后续完整卡名义为“System Model 配置管理页面及必要管理读口”，将这两个正式只读增量和前端行为一起验收；不做 Provider 调用、模型测试连接、Invocation、Usage、Project 设置或 Summary。既有 Agent/project_summary 替换 adapter 缺失必须呈现真实阻断，不能承诺已经支持跨域改绑。

还有两个开工门槛：① 公共账号入口验收提交后，重新核单 Cookie owner、App 和路由接缝；② 正常“系统设置”入口仍到“用户与邀请 → 用户”，需先交付真实默认用户页，或主线程明确将该已定页面纳入下一完整范围。建议独立交付系统账号管理入口后再开 Model 前端，避免本卡顺带扩成全部系统设置。指定 Model 叶子深链是已允许行为，但不能据此声称普通系统设置入口已经完整可用。

当前可并行做必要管理读口的正式规格；不能据本文先开前端或后端代码。以上是工程接口和任务范围选择，无需用户重新决定已经确认的布局。

## 2. 已具备的实际能力与边界

| 固定来源 | 已核事实与含义 |
| --- | --- |
| `internal/central/app/account.go:236–416`、`app/model.go:11–26` | 同一真实 Model/Secret/Account/Audit/Outbox 组合；Secret 后 Initialize Model；已有六个 URL 根及其子路径路由到 Model handler。两个建议新读口若落在既有根下，不需要另造 root 服务或后台任务。 |
| `internal/central/model/http.go:97–121`、`api/openapi/model-system.json` | 17 方法覆盖 Provider/Model CRUD、selection GET/PUT、Model command lookup、Credential create/update/delete/lookup；无 Credential metadata GET、无删除影响 GET。所有成功含 delete 为 200 JSON；GET 可 HEAD。 |
| `model/http_configuration.go`、`http_dto.go`、`query.go` | Provider/Model 当前 view 带版本；Model 列表按 provider_id 分页；selector 含 id/version/configured。可遍历真实分页构建选择候选，不必新增假“全模型目录”。不能只加载第一页后将其余合法当前项判失效。 |
| `model/configuration.go:45–84,104–146,312–320` | Provider protocol、Model type 创建后不可修改；Provider 有 Model 时 InvalidState 拒删，最终事务再次检查。布局“按领域规则处理”不授权前端伪造级联删除。 |
| `model/references.go`、`contract/references.go` | 平台用途有真实 canonical selector 与 references 核验/改绑；含 Agent 或 project_summary 引用时缺正式 adapter，删除拒绝 DependencyUnbound。现端口是准备/执行删除，不是安全管理预览。 |
| `model/references.go` 的 selectionCompatible | Embedding 必需；Memory 必须 enabled chat 且 json_schema 能力；Reranker/Image 可空。Memory 不是未决会议 Summary。初始 configured:null 是真实未配置状态，不能代选默认模型。 |
| `secret/contract/types.go`、`secret/write.go:458–467`、`model/http_credentials.go` | HumanWriteCommands 已包含 Metadata；返回 ref/scope/purpose/version，不含明文。现 HTTP 只内部用 metadata 校验写入边界，浏览器无法取得当前凭据版本。 |
| HTTP/root 验收卡与报告 | HTTP 已验 ac5b4c6，默认 root 已验 457b197；c54 实际源码包含它们。配置保存不等于五种 profile 均可实际调用；ready503、无生产 Invocation 及 Object 已知阻断保持。 |

Provider 的 credential_ref 只有 UUID；Provider.version 不能作 Secret.expected_version。Credential 历史命令 receipt 的 version 也不能当当前版本。刷新、其他管理员改动或旧绑定都需要真正 metadata 读取。通过总是创建新 Credential 并重新绑定来回避读取，会改变凭据语义与清理责任，不是可接受补丁。

## 3. 必要后端增量及事实所有权

**Credential 当前 metadata HTTP。** 建议在既有 `/api/v1/system/model-credentials/{id}` 增加 GET/HEAD；准确 URI/DTO 待正式卡冻结。复用 Account 当前 Human System admin Read 边界与 Secret 正式 Metadata，严格 System scope、Model purpose，只输出安全 ref/purpose/version。缺失或非 Model 凭据按边界拒绝，绝不回显 value、摘要、密文、nonce。读到的版本只是当前观察；更新/删除仍依原 expected_version 和完整写入事务校验。Secret 公共口已具备此能力，预计不需修改 Secret 契约/存储，更不需迁移。

**Model 删除影响管理读模型。** 新增 Model 自有只读 service/DTO 与 HTTP；真实读取已有 `agenteam_model.references`，按 kind/role 返回必要数量、当前 Model version 和确定的删除/替换约束。只给安全聚合，不暴露他人 Project/Agent 标识、名称或内容，不直接读取其他域私表。沿既有 `Service.read/readScope` 当前 Session/admin 与 Model/引用锁规则补齐所需 union；平台项核 canonical selector。外域引用可报告真实登记数量和“缺对应替换能力”阻断，不能将引用索引冒充其他域当前事实或可写许可。事务删除仍重新规划/核验所有引用及版本，预览不冻结引用、不授予权限，不将竞态当 UI 已经确认。

这两项依既定规则实现，属于需要正式化的技术 API。不需要新产品决定、SQL 表或迁移号。Provider 子 Model 信息已有真实分页；可以正确呈现“先处理子 Model”，禁止虚构事务级联。现 Model/Secret 两类命令不是同一 HTTP 事务：Credential 成功、Provider 绑定失败时保留准确分阶段结果及恢复入口，不能自动删除凭据或将整体标成已成功。

正式卡应固定 Unknown 行为：Model 服务内部 receipt 恢复成功可以 200；仍 Unknown 才按 Problem 呈现。两个 lookup 都使用原 key/kind/身份及必要 ref/expected，不重 Execute、不重交 Secret。`found/observed=false` 仅未观察到，不是 NotCommitted；true 是原命令历史 receipt，不替代当前配置 GET。完整原意图、身份变化清理和安全确认元数据的保留范围需明确，敏感材料不写 URL/storage/log；丢失原 key 时不得假称能恢复任意旧命令。

## 4. 前端接缝、导航与范围量级

c54 的 `web/src/api/client.ts` 是闭合 Account endpoint transport，`api/account.ts` 是 AccountAPI；不是已存在 Model API。`composables/useSession.ts` 私有 CSRF 和既有 personal facade 共享单 Cookie owner，已建立稳定身份与 actual-tail；Model GET、写入、lookup 同样可能因失效 Session 改 Cookie，必须加入同一 owner，不能旁建自由 fetch/第二 queue 或向页面暴露全局 CSRF。

`router/auth.ts` 的 safeReturnTarget 仅允许首页和个人三叶子；System 路由要增加精确安全解析与当前 admin 检查。`SystemNav.vue` 现按 meta.navigation 展示，没有 admin 过滤；简单注册新 nav 会造成身份未确认时闪现/越权入口。`SettingsShell.vue` 当前是个人栏目和注销的具体实现，不是现成任意菜单框架；新 System shell 可复用已验 UI 控件/样式与正确 dirty/focus 规则。App 中页面 owner 应跨同身份刷新保留草稿，真实身份或权限失效后关闭受限内容。

正式布局 `layouts/system-settings.md:3,22–28,78–82` 明定普通入口用户页，指定深链到对应叶子；`application-shell.md` 没有另一个“模型管理”顶栏入口；`system-pages.md` 首页仍是空 Dashboard。不能改默认 Providers、假用户页、在首页临时插 Model CTA 或把其他未交付栏目包装为已用功能。默认用户入口是实际范围缺口，非 Summary 产品问题。已有 D07 System users/invitations HTTP/root 可支持其后继规格；本文没有扩大审查或授权该页面。

估算路径供调度，**不是冻结清单/授权**：

- 前端旧 6 处：`web/src/api/client.ts`、`composables/useSession.ts`、`router/auth.ts`、`router/index.ts`、`App.vue`、`components/layout/SystemNav.vue`。
- 前端新增约 8 处：`api/model-system.ts`、`composables/useSystemModels.ts`、`components/layout/SystemSettingsShell.vue`，以及 `views/system/models/` 下容器、Provider 列表/详情、Model 详情/编辑、平台用途页。表单拆分可调整数量，不添加依赖包。默认用户页若并入，需另列真实额外 API/视图范围，不能藏在此估算中。
- 后端/契约约 6–7 处：`internal/central/model/{http.go,http_credentials.go,http_configuration.go,http_dto.go,query.go}`、必要新 `management_read.go`、`api/openapi/model-system.json`。这是 Model 自有服务/API 的正式扩充；不需要为管理 UI 擅改跨域 C0。预计 app/root、Secret、SQL 无生产改动；正式规格再核。
- 新纯测试约 4–5 处覆盖 typed transport、command owner/Unknown、编辑/选择/引用影响/权限；受 owner/router 变化影响的旧 authentication/session/personal 断言按实际 seam delta 限定。后端真实测试可用 `tests/model/system_http_*` 原 fixture 加两个新管理读口组；真实浏览器用同类 `tests/account/authentication_web_fixture_test.go` 的 app.Run fixture 复用或窄导出，新增 Model UI fixture/browser 驱动。预计合计约 30–35 路径，不含未决定纳入的用户默认页；不是小型纯 UI 卡。

公开入口正在修改上述 client/useSession/App/router 等共享源，也扩 AccountAPI mocks。应等其最终已验提交后只核这些接缝，不能预写基于 c54 的第二套 queue，再让合并解决语义。后端必要读口规格与该活动范围不冲突，可并行准备。

## 5. 完整验收与保留边界

1. 真实生产 dist + 同源服务 + 当前 app.Run/PG/MinIO：真实 admin、普通用户与匿名身份；不通过响应替换或改表造登录。验证正常入口/指定叶子、权限未确认不闪现、普通用户直接 URL 也无配置泄漏。既有 fixture 是否需窄扩以冻结接缝为准。
2. Credential 创建 → Provider 绑定 → 四类 Model 合法配置 → 四用途原子保存；五种协议按真实类型校验，protocol/type 编辑只读。刷新后 metadata/version 可用，其他管理员更新时保留草稿并正确冲突；取消、空敏感输入保留、独立解绑/删除及绑定失败后的部分结果可解释。Provider 含子项拒删，绝不伪造级联。
3. 真实删除影响数量/类型与删除事务一致：无引用删除，平台必需用途合法替换、可选用途清空；并发引用改变最终重新校验。外域绑定缺失用正式服务层事实 fixture 验聚合/拒绝，明确它不是生产 Agent/Project 工作流；前端不得因未绑定而显示 0 引用或可删。
4. 真正枚举完整分页候选；Memory 需 json_schema，enabled/model type 不匹配和失效当前引用有准确状态；selector 初始未配置可完成首次完整配置，无 Summary 默认。原版本与整数/JSON/安全 header 边界由现 typed DTO 验证，不把 JS 精度损失提交给后端。
5. 当前角色/Session、跨页面单 owner 与 actual-tail、原意图查证、found/observed false 和历史 receipt/current GET 区别分别验证。Unknown 的可控纯测试与真实正常命令查证分开报告；本文不授权任何网络故障注入。窄屏、主题、键盘、dirty 导航和焦点，以及受影响认证/个人/公开入口回归保持。

未决产品仍只有原 Project Summary 初值/Settings 等既有问题，本建议未作选择。暂停的 Object Runtime 缺陷不重启、不替代；现配置是同步管理操作，不创建后台 call/lease owner。配置 UI 验收不等于 Provider conformance、生产调用/Usage、项目域绑定、完整 D09 或 ready=true。

自查：所有结论基于上述固定 Git；仅生成本文，未修改仓库，未运行 Go/npm/browser/Docker/SQL/网络，未创建测试资源。预计路径和方案尚待主线程范围裁决及正式规格独立审查。
