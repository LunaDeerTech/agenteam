# D07 账号、Session、SMTP 与个人资料

- 修订：2；状态：实现中；唯一活动模块D07，台账AT-0014。
- 基线：`main@57bfadb`，D06完整独立验收后工作区干净；D01–D06前置通过，最终证据见[D06主卡](d06-transactional-outbox.md)。GitHub认证既有阻塞未解除，不声称推送。
- 目标：实现正式人类身份/Session与System授权、初始化/邀请/密码恢复、内嵌挑战、SMTP持久投递及资料/头像/偏好；通过真实HTTP、数据库/SMTP/对象组合验证，不以生产stub代替后续领域绑定。
- 依据：[计划D07](../development-plan.md#d07-账号-session-smtp-与个人资料)、[账号](../../architecture/platform-infrastructure/authentication/account-lifecycle.md)、[SMTP](../../architecture/platform-infrastructure/authentication/smtp-delivery.md)、[D01契约](d01-contracts/README.md)、[账号页面](../../frontend-design/layouts/account-entry.md)、[个人设置](../../frontend-design/layouts/personal-settings.md)、[系统设置](../../frontend-design/layouts/system-settings.md)。已确认产品规则不重复询问。

## 所有权与任务

| 任务 | 角色 | 独占写入范围与资源 | 状态 |
| --- | --- | --- | --- |
| S01 完整可实施规格 | architecture_worker | 新`d07-account-session-smtp-design.md`；其余源码/契约只读；不使用Docker | 已完成 |
| R01 工程可行性与有界技术证据 | backend_worker，承担research_worker职责 | 仓库只读；只在任务自有`/tmp`作报告及隔离实验，不修改依赖锁/源码；不使用Docker | 已完成 |
| V01 独立规格审查 | verification_worker | 仅读停止写入规格/稳定来源与独立副本 | rev2独立通过 |
| B01 身份与安全基础 | backend_worker | 下述实施授权及设计§1 B01；独占owned Docker | 已授权 |
| B02–B04 后续完整结果 | backend_worker | 设计§1后续分块；当前不解冻 | 未开始 |

root独占本卡/计划/台账；设计规格独占给architecture，R01只交临时证据，不同写一文件。使用agenteam-design/go-development/verification/documentation技能，按AGENTS团队规则执行，子agent不得再委派或Git写。规格或活动实现未停写不审查；模块完成前不推进D08。

## S01交付要求

核对实际D03事务/锁、D04身份/Audit/Secret/cursor/outbound、D05对象头像组合与D06事件/真实Runtime/ProcessGuard，而非凭设计假设已有能力。明确数据表/索引/唯一性、Go公共端口/正式provider绑定、HTTP wire/OpenAPI、错误/权限/取消、事务/幂等/未知提交/重启恢复和具体测试场景。00010为下一个全局迁移候选，00001–00009及现有依赖冻结；新增依赖/旧文件改动逐项列来源、固定版本、必要性、授权作者与顺序。

先固定工程参数与兼容：Argon2id编码/升级/并发与实测参数、15–128 Unicode字符/不trim/弱密码来源；username规范/保留字/唯一并发；public origin/Cookie/CSRF/Origin/登录返回目标；空闲7天/绝对30天/重置30分钟/失败挑战5次等配置的范围与已签发边界。GoCaptcha + go-captcha-vue源码集成沿已定选择，固定版本、素材许可/存储/一次性与请求绑定/防重放/可访问性；不得增加Redis/独立验证服务/冷却登录/外部泄漏查询。

初始化admin@mail.com/admin幂等，不重启覆盖密码；首次仅建议改密；初始化unknown先核持久事实。邀请固定24小时、管理员授权、有效期内同链接重发，原子兑换/撤销/到期清除。重置统一公开回执不泄漏账号、同有效链接不续期；普通改密换发当前Session且撤销其他，重置撤销全部；HTTP立即失效，D25真实WS消费者未来绑定，不能虚称当前有WS产品。token不可逆验证、用于重发材料通过正式Secret引用加密，清理与未知提交可恢复。

SMTP三模式none/STARTTLS/TLS，复用正式非HTTP出站拨号/证书与策略，不自动降级。配置/保存/测试/业务投递分离，Secret凭据不回显；明确有限重试默认/上限、持久job与当前合法材料/配置校验、撤销/消费竞争、actual I/O join/ProcessGuard与unknown投递语义。无SMTP不阻断ready，只有未配置走正式受限后台日志；配置后失败不改渠道。初始密码/邀请/重置链接后台日志为已确认恢复渠道，需专用受限输出边界，不进入普通日志/Audit/前端/上下文；不能误套通用脱敏为完全禁止该渠道。

头像静态JPG/PNG/WebP真实解码、拒SVG/动画/炸弹、大小/像素/缩放重编码/元数据，正式对象引用替换和可恢复旧对象清理；email只读、本人username/display_name/avatar/theme，stable UserID与路径改名无alias。D07交付后具体哪些D04/D05/D06 System端口正式绑定、哪些Project接口留D08，必须逐项明确且组合验收。

遵循计划边界：D07完成后端事实、安全端口、HTTP和可消费协议；统一客户端/账号个人页面属于D26，系统设置页面属于D27。挑战包Vue兼容及真实浏览器可行性可用独立测试harness验证，不提前把临时页面作为生产账号UI。规格明确D26/D27接入责任，保留现有前端组件/Debug隔离。按完整结果拆后续B任务，避免仅按函数拆卡，所有公共载体尽量一次规划。

## R01有界核对

目标是给S01关闭已知技术未知：现有Session/System/Audit/Secret/对象/Outbox端口可组合性；Argon2id、GoCaptcha与Vue包、弱密码素材、静态图片编码及SMTP实现候选的Go1.27.1兼容、许可/固定版本/一手来源。只读仓库，不安装或改根依赖；可以下载公开源码或在独立/tmp module做有限编译/哈希基准，不读取真实凭据/连接既有基础设施，不使用Docker。记录精确输入、来源/日期/命令/结果，区分静态建议与实测；先把发现的端口/产品规则冲突交root与architecture，不自行定新产品范围。无需全面调研所有候选；选定可实现推荐并列未验证项后停止。

## 验收与完成门槛

S01须有稳定来源manifest、格式/本地链接检查、全部读写/命令停止声明，独立审查/root采纳后才实施。R01临时报告与实验须归档可引用的哈希/命令，不能把研究建议自动写成已定契约或已实现能力。

后续实现必须覆盖正常、无权限、真实并发撤销/兑换、unknown commit/SMTP未知结果、重启/关闭/资源join、公开隐私和日志安全，运行适用普通/race/vet/真实HTTP+PG+SMTP+MinIO组合。模块最终独立验证及无过滤兼容才标完成，单卡或纯接口不代表D07完成。工程细节在既定范围内自主决定，仅真正未决产品含义/实质范围变化报告root。


## R01研究交付

R01只读研究完成全停，报告`/tmp/agenteam-d07-r01-bnRjHRYP/report.md` SHA `65f8ec602bb4a67f10fcfba10838a53a51c94ecb6e98eecc9cef06ed8c03ae01`，67项索引`779309a97a2f19fbbcee06ebcdd2b7576acc1294cac179b56ed931bb872ecf2f`，39仓库输入`fff1f73bb24863138961afaecd0e657238bacd19a7eed5d80ce17d06198dfc77`，root核读与指纹匹配。未改仓库/依赖，无Docker/服务监听/凭据或运行命令。

推荐沿用x/crypto0.55，Argon2id64MiB/t3/p4并发2；当前4CPU限额环境短测单并发median83.727ms、并发2 median149.018ms，仅局部基准非生产SLO。GoCaptcha2.0.5/Apache、go-captcha-vue2.0.7/MIT、x/image0.45/BSD与既有依赖兼容，采用自有生成图而不引未逐项核许可素材。Go真实生成/角度校验/编码解码及现Vue/Vite临时构建、jsdom挂载通过，尚非真实浏览器/恶意媒体验证。官方MIT弱表language-common4.1.3固定49,233项，但≥15字符仅41条，覆盖有限；匹配/有限弱模式由S01明确，不改实际密码、不增组合规则。原组件无键盘/ARIA，须同题键盘包装与真实浏览器验证，视障可达未冒称已解决。

已确认正式补口：Secret受限account service-write、PreparedWrite安全Ref/RequiredLocks、真实delivery-attempt lease owner和完整早序锁规划；Audit typed本人/预认证动作授权；SMTP可信TLS配置口与D07worker纳入共享ProcessGuard实际join。Session/System现口可真实绑定。上述均为S01输入，未授权代码实现；R01未运行真实PG/SMTP/浏览器场景，在线403/timeout和准备错误均保留来源限定。


## S01首轮独立审查与窄修

设计rev1 SHA `9e4e658c19dcc942b2c247970a86a3a9c0ce1a8d5060679657df4cdcb89453f5`已冻结，290行/61196bytes；作者78仓库输入与3研究输入root核对匹配，证据`/tmp/agenteam-d07-s01-d3phzdtf`。V独立报告`/tmp/agenteam-d07-s01-verify-n0f7mvx8/review-report.md` SHA `9ef922a7d5436151af740b52d0b80c41ee902e395f7e6610103903966efdc2a4`，100项索引`d06e47a1fa63745251dba5e31f7c465f1c01611c65997b7bcf0ede575413abe4`，87唯一输入末次匹配；格式/6链接/1fragment通过。检查器误认inline regex的准备错误单独保留，不计产品失败；未运行产品测试，V已全停无资源。

唯一必修D07-S01-01：account_response的lease不能套SMTP attempt/config，也不能让并发登录回应读取共用LoginCommand owner而互相释放。root采纳，仅解冻同一设计给architecture修订2：补独立实际response-read attempt/process/fence，绑定已验证browser、原LoginCommand完整HMAC、User/Session/password_version/exactRef/System用途/5m；完整规划锁后当前核实、每次独立lease/实际join，unknown或Audit失败零材料，窗口结束/撤销不释放别人仍活lease。补T05/T07并发重放、绑定拒绝、expiry/revoke、unknown及Cancel/join。

一并明确recoverylog有界队列/串行实际writer及Write/Sync各自阻塞场景；Force不能在普通文件I/O/Close无界等候，共享1s到期保未join/guard并仍发起DB ForceClose；不重新输出初始化密码。Avatar cleaning可复用原DeleteUnreferenced，revoked已有先于committed的安全Lookup，不为此扩大旧域范围。其余规格与工程参数保持；修订2冻结独立复验通过前不授权任何源码。


## S01采纳与B01实施授权

设计[修订2](d07-account-session-smtp-design.md) SHA `4e968882be968f79b00556032571645860ef9656816dc0494e68cb0f09b50321`独立静态通过，唯一D07-S01-01及Sink澄清闭环。独立报告`/tmp/agenteam-d07-s01-rev2-verify-if_9ndbe/review-report.md` SHA `0b5c2ea3e48428f7914f94ec14849c0cb03c70a5eb4e39d1d991df5b5d1a6553`，110项证据索引`29ac3d4cdd0000638281d4bbd1979f35db2687b3132948b47150ee1657085c4a`，88输入末次manifest `70b81eb96ac0a1818f38c47c4349a3149b1b239d17b9016dd41039b38f7463ec`；root核读报告/diff且逐项匹配。作者/V全停，无资源，格式/6链接/1fragment/6表通过。仅静态设计门槛，不代表产品验收。

root采纳设计§1固定依赖及必要旧公共增量，授权backend完整B01：新account/contract的types/identity/settings/commands与相应测试；account的repository/authority/planning/keyring/password/bootstrap/session/login/audit_authority/secret_authority及同包必要私有辅助/测试、固定弱密码素材与许可；新recoverylog真实Sink/测试；00010一次声明全D07结构；tests/account基础/迁移/真实组合测试。Go依赖仅按设计固定版本且按实际使用引入，旧00001–00009字节冻结。

B01旧域范围严格为设计§1表：identity/contract/identity.go；audit/contract types/metadata、新account.go及audit/service.go；secret/contract types、新account.go、secret service/write/lease与新account_write.go；outbound/policy.go、新smtp_tls.go；新object/download_keyring_material.go；foundation/fault.go、httpapi/problem.go、api/openapi/common.json及各自对应测试。允许本块新增目录内必要私有拆分，不因此扩大旧域。Secret Store RequireHeldLocks结构要求按设计在其service定义落地。其他旧文件若发现真实必要改动，先报告最小证据和范围给root，不能静默扩展。

B02–B04源码、Avatar旧对象补口、HTTP/app/config装配及产品web均未解冻。B01可提供正式后块接口，未绑定能力明确DependencyUnbound，不能默认允许、返回成功占位或提前实现后块。登录在缺实际挑战provider时须安全拒绝需要挑战的请求；B02完成真实挑战组合后才能通过其完整场景。B01完成真实基础端口及受限读取/材料生命周期，不以只交DTO结束。

验收覆盖设计T01–T03及T05/T07/T11本块适用项：真实PG迁移/约束、并发bootstrap/初始日志unknown不重印、密码/keyring/有界hash实际join、当前Session/System与旧Human端口完整锁、本人/预认证Audit、受限Secret写及独立response lease完整绑定、原登录/Acquire/Read unknown零材料、并发读取独立释放、撤销与期限不复活、日志队列/阻塞Write/Sync/Close及有界force。真正HTTP/组合根/SMTP和浏览器留后块，不将未做项写成已验。

执行固定Go1.27.1 check-go和相应真实PG/race组合；沿用已验owned PG17.8/vector0.8.1、MinIO固定二进制与私有网络fixture，backend独占Docker，禁止既有基础设施/凭据/广域清理。测试需新共用fixture或旧迁移断言最小调整先报root。作者完整自测、冻结source/dependencies manifests、所有命令与资源停止后交独立V；根线程不读取仍活动实现。已有推送认证阻塞不重复尝试，无新产品待定。

### B01必要事件子集提前绑定

实现核对发现logout必须按设计§8与真实撤销同Tx写account.sessions-revoked，而原文件分块将events置于B02。root追加B01精确授权：新account/contract/events.go及account/events.go中仅sessions-revoked v1的typed codec、producer当前授权与同Tx append组合及对应测试；允许改同为B01所有的session/login/planning/authority以消费该正式口。事件payload/稳定UserID/auth_sequence/固定reason沿设计原规则，注册屏障纳入初始完整锁；无真实Outbox绑定时明确失败，不能先成功撤销再补事件。B02稍后接管同文件添加delivery-requested及相关实现，当前不实现其handler/邀请/reset。旧Outbox源码不解冻，D25消费者仍未绑定。仅调整实现分块，不改变公共契约或产品范围；B01真实logout验证包含撤销、Audit、事件、receipt原子性与unknown。

### B01 usage计划传递补口

基线91f1d87的secret/lease.go既有InTx reference/lease接口无UsageDependencies参数，不能在内部重新Discover或用context偷传完整计划。root采纳显式DiscoverUsage与ApplyUsageInTx闭集方案方向，暂冻结该新增公共形状/实现，architecture仅修设计rev3的§1/4/T07；只读git基线，不读B01活动源。其他无依赖基础由backend继续，Docker仍backend独占。新设计冻结经独立窄审采纳后再解冻补口，产品规则/原预算不变。

### B01既有真实测试最小适配

Human Prepared InTx按既定设计只核已持完整锁，原测试直接Apply的调用须在外层Tx首次AcquireAll。root追加backend独占tests/security/{secret_access_test.go,secret_fence_test.go,secret_rotation_test.go,secret_cleanup_test.go,secret_nonce_test.go}，仅相应调用按prepared.RequiredLocks()预收集（若同Tx已有其他锁先union为唯一次AcquireAll），不削弱nonce/rollback/epoch/cleanup或错误断言。追加tests/testsupport/postgres/cmd/fixture/main.go仅在现有包列表纳入./tests/account/...；无新依赖、不改预算/过滤语义，独立最终无过滤同时执行新旧组。无需改未发现的旧迁移断言。

设计rev3已独立静态通过，源SHA `df7338362a074b535710918863cf97b0c23425ec5497a15ab1c78c3c6a7f5312`，报告`/tmp/agenteam-d07-s01-rev3-verify-icr3atrp/review-report.md` SHA `5aad08cce449fa253386ec789e0a4552f54e73701e2f444537ef3a261757ddc4`，30项证据索引`9fffad6747c241df37591200da839d3a08bc8e44076ef41e550e197a5ab63ad8`。11语义输入末次匹配，Git blob标签由git show核验（直接sha256sum把标签误作路径的工具准备错误已纠正）；活动源码零读取，作者/V全停无资源。root核读采纳，正式解冻设计§1/4/T07新增UsageOperations/UsageRequest/UsageResult/Dependencies与secret/usage_plan.go等最小实现/测试给backend，旧域仍限定原secret service/write/lease及contract types/account，不扩迁移或产品规则。真实实现尚待冻结独立验收。
