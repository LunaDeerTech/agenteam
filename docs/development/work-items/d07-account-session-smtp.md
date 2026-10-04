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
| V01 独立规格及实现审查 | verification_worker | 全部停止，Docker已交回 | B01完整独立验收通过 |
| B01 身份与安全基础 | backend_worker | 90源/361依赖冻结，命令全停 | 已完成 |
| B02 邀请、恢复与挑战 | backend_worker | 下述精确授权；独占owned Docker与独立浏览器harness | 实现中 |
| B03–B04 后续完整结果 | backend_worker | 设计§1后续分块；当前不解冻 | 未开始 |

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

### B01 recoverylog独立子范围通过

冻结两源sink.go/sink_test.go独立通过，source manifest `c2fd8ad79232307d9cf2dc7c9df4313a40cd216e71f026e76a7bd81b55730688`、captured 28 deps `d69db7c080fbc233bdb8577eb9bc2226d91781da662b15d40716381f43165c36`。独立报告`/tmp/agenteam-d07-recoverylog-verify-5_21krbt/review-report.md` SHA `65402e2b4ea6ed054be773616228c49a8b5e4573df9704b29ce920eb80662aea`，47项索引`c8f2d6caf63687c04c6b46b2782c1043caf8db75e5c70b1f9ec3a4e111324b3b`，31执行输入`2f8fbdc9a478f43fdf7689232af881a31d8c4b0f7ca3fbb821b2f9b0e681b5c1`。普通0.043s、独立5probe0.058s、原+新10项race1.147s、vet/gofmt通过，真实私有文件及受控Write/Sync/Close验证；非实际永久磁盘阻塞。V全停无资源，root核读源码/报告且冻结源匹配。为独立提交另在`/tmp/agenteam-d07-sink-base-tweesysb`用git archive e34b96b+这两源运行固定Go、GOPROXY/GOSUMDB=off的`go test -count=1 -timeout=30s ./internal/central/recoverylog` exit0/0.042s，确认不依赖其它未提交新接口。作者最初错误25字节测试输入已修为既定24，不改变规则。

ctx取消返回Unknown不等于actual join；独立工作ctx+外层实际goroutine追踪的可行适配经正反probe验证，因此未强制新增公开handle。B03必须保持lease到实际任务结束，且验证内部队列与mailAdmission当前检查/首写≤1s的线性化，不能拿enqueue或整个Write+Sync当首写时刻。bootstrap持久不重印、当前token/config、共享ProcessGuard/DB最终force及B01完整依赖组合均尚未验收，子范围通过不代表B01完成。

另采纳账号内部response_plans（或auth_attempts planned阶段）作为可信预分配绑定细化：不是读授权，当前完整browser/command/User/Session/password_version/ref/System/process/fence与exactAttempt/LeaseID持久绑定，后续当前重验并同Tx真正attempt+lease；unknown先核，终态不复活，有界公平取消/到期/死亡清理。不新增迁移编号/公共接口。

### B01活动实现的作者阶段证据（尚未冻结）

作者第五轮真实`TestAccount`现有组通过18.322s（既有race fixture），日志`/tmp/agenteam-d07-b01-account-fifth.log`，3nonce精确清理。覆盖fresh/9→10/同checksum失败后恢复、并发bootstrap单用户单日志、登录Session/Secret/Audit同Tx与异义拒绝、两独立回应lease及Use实际阻塞时Close不假join、logout真实Outbox/Audit原子撤销、poll不touch和password_version变化拒绝重放。源码仍活动，root未审这些实现，不以作者结果冒充独立验收。公平材料恢复、真实unknown/授权竞争/公共安全负例及最终完整检查仍待完成。

保留前轮事实：首轮新增rollback测试换checksum被既有MIGRATION_HISTORY_DIVERGED正确拒绝，修为缺函数故障→补函数同字节重试并核pending journal/Goose；第二轮bootstrap确证SQLSTATE23514/audit_records_check2，新00010补资源ID闭集且保留全部旧分支，旧迁移未动；后续Account命令摘要与D04 canonical-v1不一致使正式Secret Audit拒绝整Tx，统一既有cursor.Digest；第四轮回应继承临时facade ctx导致返回即被defer cancel关闭，改继承原caller并保留独立真实工作跟踪。失败日志`/tmp/agenteam-d07-b01-account-{first,second,third,fourth}.log`与`account-bootstrap-diagnostic.log`按作者实际文件保留；第五轮过不抹去这些诊断。

后续作者恢复/unknown组`/tmp/agenteam-d07-b01-account-recovery-unknown3.log` exit0/account25.041s：login、response acquire、Secret resolve各真实COMMIT截断的commit/rollback共6场景、未知零材料、原writer终局后exact lease恢复/rollback不另造Session，以及活Use保payload/lease→join后清理、Force不假join均过，3nonce清理。此前清理delete owner误用cleanup ID已改为原LoginCommand owner+独立cleanup key；unknown收尾测试误用全服务Joined已改为Recover+exact尝试/lease终态观测，原writer/零材料断言保留，历史recovery1和recovery-unknown2失败日志保留。

作者`/tmp/agenteam-d07-b01-account-death-permissions1.log`真实SIGKILL+Wait通过bootstrap提交后/claim后/Write+Sync后checkpoint前三窗口及response Use中死亡、活进程拒回收→exact死亡后收敛且不重印；计划绕过/缺锁poison/User gate等待后撤权拒读通过。唯一失败为新增Secret resolve Audit错误测试期待Forbidden，而既有D04实际外层DependencyUnavailable/causeForbidden且零材料；仅修新测试预期，Account login Audit失败整Tx回滚已过。尚待该修正复验、完整check-go/账号及旧Secret受影响兼容、冻结独立验收；不以这些活动源作者结果宣告完成。

### B01完整冻结与独立验收启动

77源manifest `7093aa9afcab42ba2b9efa6be508ce3b639fc82254d3cfc9a59545f1d4de5123`、364依赖 `c493957c018260aa3a6e748e54c3337a0af821b80721802e6c9b0f346461b58f`、441并集 `b4a1c9cf8606bf5f096e899b7d0a9b98e02e20937eda0fd7567f81bfc49d67c0`均在`/tmp/agenteam-d07-b01-final/`，root逐项匹配。报告`author-report.md` SHA `eaec2307255277207542a95356a2637b2090406362f9251a193a5c2d99517bc1`，证据索引`21cc6d0e9d5cf4eee9609947eca0cbf4301ff3ec49b24a4c059ea1eefcb46e92`。最终check-go全过（日志SHA `231b9969d00832b03996daad338b24591e8afb1d37dc55d362f45c2d97383d98`），全部Account真实race140.120s（日志SHA `c77cac2529a10204dc206887a4baba59784ccb01d4dc10743dc4e2bdbddb341f`），integration vet过；42作者nonce资源全0/所有命令全停。旧00001–00009/go.sum逐字HEAD，go.mod仅既有x/crypto转direct，固定弱表保原字节无EOF换行例外，不任意重格式化。

root授权V独占Docker和稳定副本完整审查/风险probe及单次无过滤test-objects新旧兼容；不重复三个路由相同脚本。作者不再改源；root审相同冻结源并保存行政记录。B01仍未独立验收，B02–B04不解冻；已确认B03首写门禁/实际join、B04HTTP/app/ProcessGuard组合等限制继续保留。

### B01独立缺陷D07-B01-V01与窄修授权

V报告`/tmp/agenteam-d07-b01-verify-qx71a7d4/review-report.md` SHA `661e0c8a17367bc79f502c3201426f677e0b34d70967a9163970f875d1359bc4`，32项索引`9e23ed1521e986c63f2a9e96eb76a19c7e18cce4cc80b9e81226b2ee2c64f25a`。441源/副本末次一致、6nonce清零全停。唯一确认阻塞：response.go原plan COMMIT Unknown后，确认事务55P03未得原writer锁（caller仍活），代码依据确认结果NotCommitted提前删除st.responses；原writer晚COMMIT后同进程recoverResponse缺actual-join本地证据持续Busy，计划残留阻塞材料后续清理。零Cookie暴露与rollback对照通过。

原不可改v2 probe`/tmp/agenteam-d07-b01-verify-qx71a7d4/repo/tests/account/verify_response_plan_recovery_test.go` SHA `a43110d41d3447e46fd3efe47215fb0b38916f00e60419ce8b3a60be85661bc2`；真实race组account6.397s/exit1日志SHA `6c25b587107bcea9ce7214c32d14b3975a480953351f39809ced3ddc4419e0e0`，两轮Recover仍plan1/attempt0/lease0。原v1错误要求caller已取消仅探针准备错误，保留原文件与机械diff，不冒充产品复现。独立vet过，尚未跑无过滤全套。

root采纳，V全停后仅解冻backend的account/response.go及新增tests/account/response_unknown_recovery_test.go（如需本包私有状态测试可新account/response_unknown_test.go）。保留原Unknown provenance和local actual-join证据；确认自身失败/未获得原writer锁不证明原Tx回滚，不改成TTL/缺行/零lease猜join，不放宽ProcessAuthority。可信串行后的absence/terminal再沿现有recoverLocal/Recover退役；未知错误语义与原cause不能因确认失败丢失。其它76源/364deps冻结，旧测试/代理/预算/原probe不改。backend独占owned Docker，自测原v2逐字闭环+latecommit/rollback/原unknown及本地收敛相邻用例，受影响check-go后完整冻结证据/资源0交V。若必要修改越过上述范围先报精确理由；完整无过滤兼容留修后V最终执行，B02仍未授权。

V01窄修重新冻结：仅response.go+新response_unknown_recovery_test.go，delta `7c7e6305c6435a74cb2f92c74b82015f3185838b8222f01ce8e2922b3ce40161`。`/tmp/agenteam-d07-b01-response-repair-1000nx6x/author-report.md` SHA `2a26791399f368d0e67e9c868b9b6f423f9fcb68e91534e6d18592de45806a06`，索引`2329a1145e871e17924d86b5e28721edb432df70871cb4347250f2384f11633a`；78源 `ce17e54b5f5e621bb48c15e95e4a49d4c2627119deb87220e9ab790254b56ba4`、原364依赖不变、442union `73644f824ff09ea9aff0106668d35684d9b3fdd3655c993d362120c8b4fb45df`全部root匹配，440原项未变。原a43110 probe作者逐字green14.671s、新回归/相邻真实race92.712s、check-go/integrationvet/格式过，6nonce清零全停。root核读窄diff与新断言采纳方向，未提前验收；Docker交V最终独立原probe复验、剩余风险行为与单次无过滤完整兼容。

### B01独立缺陷D07-B01-V02与统一状态窄修

修后V报告`/tmp/agenteam-d07-b01-final-verify-yl7v8fug/review-report.md` SHA `a321e34b71b7d2979e3ef5f6289f8716f5811389fd04c7a299bb59d9a04445f2`，31项索引`5d63aafc2c9f454c728f1b318c75a24c6a03fe6200353efe94b17bad2d700cc0`。442源/副本一致，3nonce清零全停。工具内容风险标记曾中断agent回合；仅恢复既有会话/收束资源正常完成，没有绕过或新开测试。已有测试实际exit1/82.757s，Account30.543s。原a43110逐字probe及populated9→10（13旧表、21旧Audit动作等逐行保留）未失败，V01闭环；完整无过滤尚未启动。

新增V02分类probe `verify_unknown_classification_test.go` SHA `29448f3d07bed7aed76855cb1eb059ead2c82d5afc97063d67226ce5012cdb88`真实确认6处同类状态错误：login计划/最终登录、response Acquire、bootstrap创建/claim、logout，原writer仍持锁时确认55P03且caller仍活，对外InternalError/not_committed，原writer随后实际COMMIT事实1。response-plan/read两分支未失败，未观察未确认Cookie或bootstrap密码输出。日志SHA `67ae4749bb901bff30bd7268ebf6dfd24e69d685de514a79deaf87ac1088571f`。failLogin同模式仅静态定位，需本轮补实测；Logout两阶段auth_sequence并发可重试性只是未验候选，不混成确认缺陷。

root采纳V02，仅解冻backend的account/{bootstrap,login,logout,response,error}.go中原Unknown→确认失败的状态/cause传播与必要私有helper；新增tests/account/unknown_state_test.go及必要本包helper测试。现有commit_proxy_test.go只允许新增failed-login事务识别phase用于真实负例，原phase、延时、代理行为和旧断言完全不变；若无需该改动则不改。原Unknown未可信确认时必须仍CommitUnknown/Unknown，保原与确认非nil cause、材料零返回/当前授权/actualjoin不变，不能把所有业务失败无差别标Unknown。不得改Store/锁预算/迁移/公共契约或已验原probe。原a43110与29448f两probe须逐字复验，populated probe字节保留复用。backend独占owned Docker，补failed-login实际场景、相邻恢复和最终check-go后冻结/清理交V。Logout并发候选可只读分析或/tmp有界复现报告，未经确认不改序列策略。其余冻结源保持，B02未授权。

V02作者统一修复已冻结：`/tmp/agenteam-d07-b01-state-repair-h3eadtc8/author-report.md` SHA `7cb987f82d50d37a147ee627bff6dd057add603407910a6651cea751ccf49c91`，索引`b4e7ae10b3f038233ce8ad75cb2207e56c1e319d02550e24d15ed7beb939d8e3`；80源 `23733ee2ef7c41b7ebeb16125fedc157abe782eaede3233ff5c4fecc6895a39d`、原364依赖、444union `8a6155cdf0d7a9e96e2e683ebe8e833dc5911357d7263366ef9034423bab8082`全部root匹配。7增量delta `4bc5c7768d0db42fe4b8aceea12a3c23b81ac60f4ab111965c9ddcd131c38bcc`，437旧项未变。两原probe逐字+新12分类分支+failed-login两分支+相邻真实race162.684s/check-go过，failed-login旧实现先真实红（仅错误状态，持久原子事实正常）保留；旧测试字节全不改。9nonce0/全停；V02仍待最终独立复验。

### Logout内部序列竞争确认与窄修

同目录`logout-candidate-report.md`为作者临时隔离探针，不冒称独立实例验收。原probe `logout_sequence_probe.go.txt` SHA `674caee24adc7f742723d147bb92415346351bdf4ee86d1b70a7ab562791d0a0`，真实race3.809s/exit1日志SHA `7f2a737795e4de1bb55c347a387a64576b441c03150d45cc565dad20294f7173`。仅真实PrepareAppend前barrier：A计划Tx已提交，B另一合法Session先完整退出，A及同key两次恢复/重试仍Busy且A Session当前有效；User.auth_sequence=2、A持久event_sequence=2，event/Audit/revoked各1；新key控制成功变各2。root核读原probe、报告和冻结logout/events，采纳为内部计划不可恢复缺陷，非caller expected_version冲突。

V02全停后仅追加解冻account/logout.go及新tests/account/logout_sequence_test.go。最小方案在同一Command EX/User EX完整union锁后的当前Session→完整原命令语义→phase/receipt检查下，对尚未提交planned事件按当前auth_sequence重新规划；保持EventID、业务payload、原命令身份，只有必要header sequence/version更新，不能改已提交事件。重新走正式PrepareAppend，旧prepared摘要不匹配时安全拒绝；原writer锁确保unknown先终局，未知继续保V02语义。第一次合法竞争可以Busy，但同key重试须能推进，不要求无界自动重试。事件/Audit/撤销/receipt仍同Tx；不绕原Outbox provider或直接跨域写表。原674caee逐字修后复验，新增同key并发/陈旧prepared/当前撤权/unknown不重复/序列单调场景；原预算/旧测试不改，源码范围外必要性先报。作者独占owned Docker，完成自测check-go、delta+完整冻结资源0后交V对全部三项缺陷及最终无过滤验收，B02仍未授权。

Logout窄修最终81源 `b4b792b7cd5da07978e52d21d1e2f9eec90fd83f5be26573fb11d75f40eafec8`、364原依赖、445union `74182c101ea5a26f4bd9daf461b340dd5a8010291c3e08ed43d44c950dd8a2e2` 已由root逐项匹配；证据目录`/tmp/agenteam-d07-b01-logout-repair-074_ui4x`，delta两文件 `d9f129f5d3e44d3161b88c3de82edfac84583d80075a44ae9ef4f53231da97b2`。原674caee逐字作者真实green8.486s；新增/原Logout真实race42.817s日志SHA `86a780d55773386c97f084a571047e51f4b155768a5e493f2e20fff2d15322ee`，最终check-go全过。root核读精确diff及旧prepared/当前撤权/两阶段Unknown四分支断言；初次新测试缺括号编译准备错误修正保留，root误猜patch路径后读实际diff无代码影响。6nonce容器/网络/运行目录0，作者所有执行命令停止。Docker移交V，授权稳定副本复验三份原probe及单次无过滤完整兼容；尚不标B01通过。

Logout作者最终报告SHA `22019ad766fb63eb39127b5455e712352065794c5a0d72b6bff6bc9ada3a735d`、28项索引 `0347fe95d80d4a24d5bec218e31407ae22b38618218d69344cc195754d011695`、cleanup `b75d94cb92889b1f374f327efa0fb2c057b9bd0aebcb193ea79aaccc3d1fbd6d`，root核读。最终check-go日志 `beba7f3de084ce9ee1ca793327afc90871b27b94058d0b9eec9ff9677a29ed6d`。作者/tmp报告亦全部停写；V稳定副本`/tmp/agenteam-d07-b01-complete-verify-xn6_txkh`已核445输入及额外四份原probe字节一致。

最终V首个无过滤实际exit1/519.722s，日志SHA `22c4b153e3771795327ad0ed87f302171bf7b9e211a8b232760a96651af5355a`，目录`/tmp/agenteam-d07-b01-complete-verify-xn6_txkh`。Account真实race303.590s，三原缺陷probe及populated升级/全部正式回归通过；其它已过组含PG1.073s、database117.015s、app72.961s、process152.918s、security190.654s。未通过：Outbox TestOutboxRuntimeProtectedSixtyFourDoNotStarveTail 在startRuntime返回INTERNAL_ERROR（4.29s，包199.177s）；objects包原6m整体超时360.146s，栈在Transfer新fixture的Descriptor.Verify/owned docker inspect，不能据此断定产品或负载原因。完整原日志/栈保留，不以Account green标B01验收。root授权V先精确清理、在/tmp稳定副本仅增加失败cause日志作一次Outbox定向诊断，原源码/断言/ItemTimeout20ms/预算不变；对象尚不重跑，B02未授权。

Outbox单次诊断在保持原20ms/断言下再次失败（用例8.55s、包8.610s/exit1）；日志`/tmp/agenteam-d07-b01-outbox-diagnostic-mf9008qi/evidence/outbox-diagnostic.log` SHA `6c7de1b5347a320bc4eacf3f968ea9475defdecec5ee69128dcb79fe1f3eddc3`。phase=initialize，cause为Fault(INTERNAL_ERROR/not_committed)→postgres.Error→pgconn.connLockError→errorString，无SQLSTATE且Is(Canceled/DeadlineExceeded)false。类型本身不能区分conn closed/busy或EOF包装，尚不认定根因；V停止新增测试，先清理并只读定位最小上游范围。首轮完整资源清理SHA `51792928fa60a0bb3ce48efbdaada8c2838fc63bebb9beffb8bed272c806645b`。objects总包6m报警时该传输子用例只运行1s，不能误称单项卡6m。

### Outbox上游连接关闭诊断授权

V核pinned pgx5.11.0的connLockError.Unwrap，cause尾部errorString只对应ErrConnClosed，不能称conn busy；本次未采postgres.Error.Code，尚不能区分Begin/set_config或Tx SQL。静态已排除批次Rows未关/QueryRow未关的相邻路径。pool watcher取消后关闭data、driver仅返ErrConnClosed可能丢取消归因，为待验解释而非确认根因；不得无条件吞connclosed、放宽20ms或把hard错误都改Busy。第二批3nonce/进程0，V不再测试、仅整理/tmp报告。

root授权backend下一步仅只读冻结代码和自有/tmp诊断副本，不修改仓库/依赖/旧预算/断言。Docker独占移交backend；先对原Outbox公平性用例增加安全的postgres.Code及准确阶段/实际ctx与本地取消来源记录，有界复现以区分真实自有取消关闭、独立连接断开和提交后Unknown。必要时在/tmp增加受控真实PG/TCP探针取得确定性正反证，最多两轮诊断，仍无新证据则停止报告。原用例/日志/probe完整保留。当前没有授权生产修复；报告确认机制、最小文件范围、旧红与对照后由root下达窄修。对象整体超时仍未归因，暂不另跑对象/无过滤全组。B01与D07保持未验收，B02不解冻。

最终独立阶段报告已全部停写：`/tmp/agenteam-d07-b01-complete-verify-xn6_txkh/review-report.md` SHA `a22943edada819f582ee19e37190e972321ddb9c203b7dfbd72bcdab5f355f6f`，36项证据索引 `bab07a08e9a491d947ca1f1c98bcc2d6aa475cbb874ce5de2b388cbef39557d9`。root全文核读，445根/副本、449完整执行、450诊断执行均末次无漂移，6nonce及测试进程0。Account三修独立闭环但完整兼容失败，B01不验收；backend仅/tmp诊断继续。

诊断第一轮原公平性单项exit0/6.588s，64条正常保护记录，无watcher/connclosed，不能覆盖原失败。第二轮受控探针在Open前因临时MAX_CONNS=1低于冻结Config最小2而失败，未进入目标协议场景，属准备错误而非产品证据。root追加仅将/tmp探针MAX_CONNS改为2的机械修正并补跑原第二轮一次；不改生产/断言/时序/预算，不新增第三种场景，保留原失败日志。若修正后仍未达目标，停止汇报，不继续扩大。

### D03取消归因确认缺陷与窄修授权

纠正后第二轮真实旧红仅目标断言失败（database0.935s）：ownedPID129的20ms期限→实际watcher→真实CancelRequest ACK→Prepare阻在Unwatch→work关闭data→SQL owner discard→Exec返回ErrConnClosed/DATABASE_SQL_FAILED，却Is(DeadlineExceeded)=false、父ctx仍live，Outbox分类仍InternalError/not_committed。独立TCP先断开再到期对照保hard错误通过（该对照是EOF类，不冒称同ErrConnClosed）。日志SHA `ffebddafc0100878174dca4207cab3a3d09d6a9a9a40576fba3d2d99e9d2aeb1`，`/tmp/agenteam-d07-outbox-cancel-diagnostic-2l3l_tq3/evidence/round2-observations.json`保存真实顺序；原probe `round2-probe-frozen.go.txt` SHA `2aceacc33082fdddad7fe3785229782db71210a36539ae1c93243be3fb170320`。root核读原probe与完整观察，采纳为确认的上游取消原因丢失缺陷；并未证明它是历史公平性失败唯一原因。作者九nonce资源/进程0、445根输入不变、执行命令全停。

root仅解冻backend的`internal/central/postgres/{pool_cancellation,sql,transaction}.go`，新增同包取消归因测试与`tests/database/`实际回归。目标：在精确physical target、实际watched context/取消原因、非Force/非cleanup且SQL owner确认由该次本地discard关闭driver的证据下，仅对精确ErrConnClosed保留原cause并补取消原因；不能依据ctx后来到期、连接closed或存在work泛化归因。SQL/Rows/BEGIN/set_config/AcquireAll等适用pre-COMMIT边界使用一致私有规则；COMMIT已经调用后原Unknown分支不改，不把取消当远端已停止。原Outbox recoveryBudgetError、所有旧断言/20ms/20s/6m、D03实际join/Force共享预算与nonce安全保持不变。store.go或额外旧文件如必要先报告。

必须保留旧红探针逐字修后闭环（临时诊断helper仅作同语义观察），新增精确非watcher ErrConnClosed、无关watcher/后到期、Force与正常Rows清理不误归因等负例；不以原EOF对照冒充。运行适用普通/race/真实PG取消、unknown/原公平性与相邻恢复检查、check-go，冻结精确delta及扩大依赖manifest、命令全停/资源0后再独立验收。原公平性即使后续green也保留历史未采具体phase限制。objects累计超时另待组合验收处置，当前不修改其预算或断言。D07仍唯一活动模块，必要上游返修不代表D03整体重做；B01未验收、B02未授权。

诊断最终报告`/tmp/agenteam-d07-outbox-cancel-diagnostic-2l3l_tq3/diagnostic-report.md` SHA `3a35aa1b9514a18478c10b9b2f477ba46e34597b123e583d82f6ec69c08583ae`，索引 `9d92f90ca3676c194b9551c9e0791f29c696f4e8c0c2ccfd5b757900ce4c99cb`；root全文核读。原观察不变副本`round2-observations-frozen.json` SHA `10e820d9c4ecf436ab3c39177fa8b4e6b3e6ec26c6b0200e5b6f58ee63c498d4`，逐字probe复跑可覆盖其固定raw输出路径，不能覆盖-frozen证据。真实独立断开时driver也可能自行发CancelRequest，故单凭取消包不构成平台watcher归因。cleanup/root-inputs SHA `63d45f1746d3883a677f07b8bf8e6404b05812d7af75d72aa21c259770586d04`，9nonce0/445不变。诊断全停后进入另行已授权三文件窄修。

执行环境曾更新为starting，root wait_for_environment返回ready，HEAD9d8d343/原证据与三文件改动保留。随后作者会话停在pending_init而非测试运行；root interrupt确认该状态后followup恢复，作者明确running且无环境阻塞，从新增归因测试节点继续。该等待期间没有新测试通过，不重做已完成诊断。

D03窄修首轮普通postgres0.065s通过；新增真实回归首轮begin探针只匹配begin而真实SQL为`begin isolation level read committed`，属新测试前置未命中，原real-first日志保留；其余6正例和2真实同ErrConnClosed独立关闭负例通过。机械匹配修正后真正命中BEGIN→ReadyForQuery→Unwatch本地关闭→BeginTx成功→set_config新scope丢原因，real-begin-second.log保留。root采纳该连续初始化阶段证据，允许授权transaction.go内BEGIN+set_config同一私有初始化scope，进业务callback前结束，不能跨独立业务SQL或COMMIT传播；补旧初始化scope不能复用负例，不扩源/预算。作者证据目录`/tmp/agenteam-d07-cancel-attribution-repair-vdga3kll`，仍活动未冻结。

作者原2aceacc探针逐字修后green（overlay database9.982s，含原COMMIT unknown/相邻事务），但同组原公平性仍在startRuntime失败为DEPENDENCY_UNAVAILABLE（5.69s），未采cause不得推同根因或已修。日志`/tmp/agenteam-d07-cancel-attribution-repair-vdga3kll/evidence/repair-regression-first.log`保留、3nonce已清。该轮filter尾锚未包含新增七路径前缀，不计其通过。root授权先正确filter复核新增实际组，再最多一次/tmp原公平性失败安全cause/Code/SQLSTATE/phase诊断，原Outbox/预算/断言仍不改；超出三源的必要修复先报告。

作者正确prefix真实取消组通过：postgres2.170s/database10.145s，覆盖七路径、真实同ErrConnClosed独立关闭先/后取消及原PoolCancellation的Force/join/Rows/失败边界，real-all-cancellation.log保留，3nonce0。获准一次公平性诊断随后exit0/outbox11.735s，fairness-state.json记录64项，其中SQL/AcquireAll/Rows实际三次ErrConnClosed经精确归因保留Deadline，parent live/初始化成功；3nonce0。先前DEPENDENCY_UNAVAILABLE未复现、具体分支仍未知，不能被本次green覆盖。停止新增诊断，作者进入最终check-go/冻结；B01完整门槛待独立检查。

D03窄修正式冻结并交V：`/tmp/agenteam-d07-cancel-attribution-repair-vdga3kll`，89源 `fdaffb768d467c21cf5cd101b5c5968d87abc709a681ec302f6ab92f6e18b1b9`、361deps `a0a0c24522ae9f9546a4db24dc5170532507021b65e453431f0ba385cd82384a`、450union `23f17dc793f5c7bcebf54650c749e97475959104560107b32e6df74272d1f9b4`、delta8 `17f0bfb06a427da8e097dcd1e2a8c6e8e30e683190eaac52627cd56033ade274`，root逐项匹配并读三生产diff/真实独立关闭测试。仅三生产加五新测试，442原项未变；最终check-go exit0，15nonce容器/网络/runtime0，执行命令全停。Docker交V稳定副本独立归因正反、原2aceacc逐字及最终单次无过滤兼容；保留原历史未归因错误/对象累计超时，不提前验收。

D03窄修作者最终报告SHA `75dff8dd2ef1a155ffe87317f51d02bdcf2947dba14af5aa235b70ee57904c41`、38项索引 `89c58dcbf82abc044e8756a2abb4a6bd7085f3a9da12a30046c2e4166e4b95a8`，root全文核读；check-go `372519a93f94af7a23d8e11883c6002c26fe57bfed4ed7d6015a093860fb37bf`、cleanup `d7f4a1aa6679893284a676b18295bba85e7372a1ed11a05466120ef433b90538`。全部作者读写亦停止。独立副本`/tmp/agenteam-d07-cancel-verify-6b_flvvy`已匹配450输入，带四Account原probe纯源执行454manifest `36f14d74f0c8b2dcaa87f15777e5b4a0c9b814055496492246633ab1304e0081`，生产diff `546ee728d090f1ab5da4490b1e23047d695f38571f9bd0da573512c7889772c8`。

独立V定向真实组exit0（postgres4.223s/database16.239s），原2aceacc逐字通过；重建原453观察输入manifest `9988068c8a30f116a83ab08fea43ef376055e00d32999c4b694c9f74d3c28fa6`完全匹配。日志`/tmp/agenteam-d07-cancel-verify-6b_flvvy/evidence/targeted-real.log` SHA `c9d473901087af7ba9118a834ede7b6fdec0d63b98b269b2c23ff12b80a03c19`：实际watcher ErrConnClosed+Deadline→ResourceBusy/parent live，独立断开仍hard，原frozen观察不变。3nonce0，450根/副本、454纯源执行/453观察执行末次一致；V开始默认调度、原6m预算、纯源加四Account原probe的单次无过滤，完整门槛仍待。

上游修后独立纯源无过滤再次出现旧公平性startRuntime DEPENDENCY_UNAVAILABLE（outbox包208.171s），其它包尚运行，不能提前判整组成功。root已授权V收齐全组/精确清理后最多一次/tmp安全Code/cause增强定向诊断，不改生产/预算/断言；只读候选是store.borrow在pool.Acquire成功但late ctx取消时AdmissionStopped(nil)丢原因，尚未证实该次分支，不作为根因结论。当前三源保持冻结，暂无额外生产授权。

修后第二次独立无过滤完整exit1/525.343s，日志SHA `30889aa7e7aad43d18cecaf2c1d3f775554998feefabb09fcfbf6fcb25354c1d`。Outbox208.171s在启动DependencyUnavailable，objects360.077s原6m累计超时；其余10包通过，Account319.585s含四原probe。objects报警时TransferIssueWaitsForRealAuthorityAndProjectGates仅2s、operation子例1s，栈仍fixture→Descriptor.Client/Verify→owned docker inspect，不能推该子例卡6m或根因负载。3nonce全0/原命令及fixture/test PID消失，V继续既有一次安全cause诊断，不扩大预算或重跑full。

修后V唯一诊断再次真实失败（test9.40s/package9.502s，exit1/84.128s），`/tmp/agenteam-d07-late-checkout-diagnostic-msusku69/evidence/diagnostic.log` SHA `9cdb786796e3c269493313fe054627a3a72d66f4d5df26bbad02a03c7a7762bc`。phase initialize，Fault InternalError/not_committed→DATABASE_SQL_FAILED(hasCause,无SQLSTATE)→ErrConnClosed，IsCanceled/Deadline均false；此轮不是borrow AdmissionStopped(nil)候选，亦不能解释前次full的DependencyUnavailable。归因机制原probe通过但实际公平性仍未闭环，V停止追加测试/清理/报告，root等待完整交接再下有界精确顺序诊断。不得因此放宽一般closed规则或预算。

### 修后剩余ErrConnClosed有界诊断

V本轮9nonce资源0/执行命令全停，final-cleanup SHA `26bef2ee7cc21b1d97705fdd97a41d26fc210d0433bbcc0c7183366578001a23`；仅/tmp阶段报告整理。root将Docker独占交backend，所有450仓库技术输入继续冻结。授权最多两轮仅/tmp观察诊断：对原公平性同一代码/20ms/20s/6m/断言记录每次SQL私有上下文、actualwatcher选择/拒绝原因、exacttarget/work/Force/internal标志、SQL-owner Unwatch前后driver状态、首次错误阶段和原Code/ErrConnClosed/ctx状态；不记录SQL值或凭据、不从异步watcher读driver可变状态。每轮保完整原probe/diff/trace，出现明确失败再据实选择第二轮受控复现或正反；无新证据则收束，不反复跑绿。此次可同时记录late checkout拒绝分类，但不把它预设为根因。

不得在仓库修改/放宽归因规则、Outbox分类或预算；实际driver已先关闭、ctx不匹配、work已selected等只作候选，需真实时序。已通过原2aceacc及正反证据保留，不能用新机制否认旧修复有效，也不能用旧probe替代现实失败。取得确定机制与最小必要修复范围后再由root授权。objects累计超时不另跑，B01/D07未通过、B02不解冻。

修后独立阶段报告全部停止，`/tmp/agenteam-d07-cancel-verify-6b_flvvy/review-report.md` SHA `1f79ef656ea450a4aff5e4d5bf01c608cc7ab8b81e3c8f5efd57f9265aa5c660`，45项索引 `03dce372da0dc219f17ab96cc69fe84494381e7553683d44e82bea05b610283c`；root全文核读，450/454/453/451各输入末次匹配。静态五类遗漏候选另存late-checkout诊断目录static-candidates.md，未裁定，已转作者有界观察；全部历史失败和独立通过范围保留。

剩余路径首轮/tmp观察实际exit0/outbox7.554s，目录`/tmp/agenteam-d07-cancel-remaining-cf460kh_`，2099记录/65次BEGIN-set_config/339scope，但watcher/Unwatch/ErrConnClosed均0，因此无新失败顺序，不消除历史失败。作者遵守无进展收束停止随机原用例重跑，保持450输入冻结。root仅请求只读评估业务相邻SQL受控候选（SQL真实完成ReadyForQuery→取消Unwatch→第一SQL成功→同caller下一SQL ErrConnClosed），未经新授权不执行。

### 业务相邻SQL确定时序补证

作者只读pinned pgconn ResultReader.Close：真实ReadyForQuery分支先Unwatch再返回既有tag/err，可在本地取消关闭后仍返回nil；当前Exec仅err非nil提归因，defer结束scope后同caller下一Exec新scope可能失去原因。root采纳为值得一次确定时序检验的候选，追加仅/tmp一次受控探针授权，不重跑随机公平性：原代理放行Prepare第一个Z，在真实Execute第二个Z设置barrier，观察实际ResultReader.Close/Unwatch及CancelRequest ACK；同item/caller连续两Exec分别核第一nil/第二exactClosed及相同target。独立真实断开+当前ctx未取消/后来到期对照保持hard。不得伪造PG结果/扩大20ms等预算/修改生产，准备机械错误保留后可在同一目标范围修正；取得结果后冻结完整probe/trace/输入和资源0，报告机制与最小方案，不能冒称历史公平性唯一根因。

业务相邻SQL受控补证真实命中旧红（database1.121s，仅预期归因断言失败）：`/tmp/agenteam-d07-adjacent-sql-829y6xy0`，原probe SHA `476d960eeea5cdbb1988c32a7720defdc22bbd40a6853c6c8406573abfb10af1`。same target1/operation3/caller：scope4实际watcher→ACK→Unwatch open→closed/discardedtrue→第一Exec nil；scope6下一Exec raw exactClosed但无reason/discard证明，IsDeadlinefalse、InternalError/not_committed。独立真实断开先于item到期对照保hard（该对照非exactClosed）。root采纳此新确定机制，不称历史唯一根因；等待完整清理冻结报告与最小方案，当前未授权源码。建议若保留物理连接终局证明，必须sameoperation/same原caller/同取消原因，不跨新caller/Force/internal/独立close；任意context接口身份比较不得panic，未知形状安全拒绝。

### 业务相邻SQL终局关闭证明窄修

补证执行已结束，仅预期正例归因断言红，三nonce/进程0，450仓库与453执行末次相同。root采纳最小方案并授权backend：仅解冻`postgres/pool_cancellation.go`、`postgres/transaction.go`；在新`pool_attribution_test.go`增量归因测试、新`tests/database/`相邻SQL真实回归，允许本块新`cancellation_attribution_proxy_test.go`仅添加Execute第二Z阶段，已有阶段/旧断言原样。其它源特别sql.go/store/Outbox/依赖/迁移/预算冻结，额外必要文件先报告。

已证owner Unwatch open→closed时产生不可变本地终局证明，绑定exact physical target/operation/业务域原caller及原取消原因；后续业务scope只能在相同operation/caller与原取消仍匹配时对精确ErrConnClosed附原原因。它不是复活已结束scope权限，不跨初始化→业务、COMMIT、不同caller/operation、Force/internal清理或独立closed，不从晚到期反推原因。context身份先安全可比较检查，不可比较/未知形状拒继承不panic。sql.go现有精确错误边界可复用，COMMIT Unknown保持。

新476d probe须逐字修后闭环，保原2aceacc与frozen观察；补同caller相邻正例、不同caller/op、后来到期、Force/internal、独立closed、初始化边界、不可比较context与COMMIT Unknown负例，保原七路径/真实exactClosed对照。完成有意义定向真实检查和check-go，冻结新delta/完整输入/资源0后交独立V；不随机反复公平性跑绿，未归因历史错误保留；objects超时另待完整验收，B01仍未完成。

相邻SQL补证最终报告SHA `128f64839755740a4bd5a68359e429f0b088aa5a7ed666420d413ba014d32cf1`、15项索引 `745818bd4409722b566ec33b5202ab4edd2edac6b442785dbcc8150d785d2a1d`，root全文核读。执行453manifest `910b38cc22f5ea075df98756e1fc197ec062b717f8d8a7f733969c2359880d63`、原红日志 `acc775c1808d091777f33033e3411dfdb860708f8954a1305ead9dc74bec8208`、不可变观察 `832f57497f03c4113a38d71fc545fa678226fcc45cf3602015e3f4b49e9e0a82`、cleanup `d0c5cd7782921ee017e691ad00154c0d4df72f21e539ba6c3bb3580967ddf1bc`。首轮无取消报告 `ebe11c02526d40acbc341cc59631253132c48e5b580fb737dc1694275206c6ed`/12项索引 `f6dbce4204b40a4d1562501428a044e97d5a4328545245e7ab43357f11385e3a`亦已核读，两个诊断目录均停写，作者转已授权两源窄修。

相邻SQL窄修冻结：`/tmp/agenteam-d07-adjacent-repair-afjky7am`，90源 `46ff1a820a235d6484e6967613686ae5bda363e3d265fde032c6ccff504fff43`、361deps仍 `a0a0c24522ae9f9546a4db24dc5170532507021b65e453431f0ba385cd82384a`、451union `2daa8e6714f365fd09678792eee442390826f5fcb1245f1fe0eb3af68ff93878`、delta5 `1fa5045842023b0c95b7dfd2077996e9c70b6127117eb74e1d584e0a45e294c4`，root全部匹配且已审源码；446旧项未变。两原probe作者逐字green（2aceacc database2.069s；476d相邻组），新增相邻持久race4.117s，最终check-go/integrationvet全过，15本轮诊断/修复nonce资源0，命令全停，仅/tmp报告收束。首轮新增rows/othercaller在ACK完成前的准备断言失败已保留，移动同断言到actualjoin后过，但未冒称准确原失败布尔已证。

root下一独立完整运行改为显式包级串行`GOFLAGS=-p=1 sh scripts/test-objects.sh`，仍无过滤、原每包6m/测试内部并发/断言和生产预算不变：前两次默认跨包并行均在对象包累计6m中断，改编排以取得全部包的完整执行，而非再次同条件运行。保留默认调度失败，不能据串行通过反推负载根因已证明；若对象仍超时或新失败，保留分包/具体运行结果再定位，不自行放宽预算。先独立复验两取消原probe与直接正反，再只做该一次全套，不重跑同路由脚本。源码全部冻结，Docker交V。

相邻窄修最终作者报告SHA `2ea0f24ee14b6296b6e6de6e547363b8610f67e6925fdf34ccd05fb9b2b4d560`、34项索引 `4d0ffc3aceb21d1cea40a55f22e278d136b93db6d2bb0ca15cee27bb5c678272`，root全文核读；check-go `4bd9411cee028a47969f716b615ed1379eeb154895aba2a12a8773798f29c29a`、final-cleanup `3c757cbca65e709613c43287d50a78260802bb7ada25faf2567ed8e7152b9c08`。15nonce含2诊断+3修复，修复本身9，均0；作者报告亦全停。V稳定副本`/tmp/agenteam-d07-adjacent-verify-izjj__3a/repo`匹配451输入，独立diff `ed93d33f852ee04336a1b58534fc1cbe86942cecf89e31b5dfad9b33ce7c842e`；两原probe因helper同名分别观察副本执行，纯源全组无插桩。

相邻修复独立定向通过：476d原probe+5模式/七路径/真实exactClosed/旧PoolCancellation组合postgres2.047s/database19.110s，日志SHA `5a6ecc5678ee88b8f14ee607a42ab44e54ede95d73f8281d19545808fd617348`；2ace原probe database2.643s，日志 `38b8229099b33412fe4690fabda843b359d71ae3085571ca5155be2865b577ed`。局部race1.066s/vet过，两frozen旧红不变，6nonce资源0。V启动纯源455输入（451+4Account原probe），manifest `5f93d80990f954ca5161f00ddf1306d639fb1146d12a551d143bdd3d7d689938`；唯一`GOFLAGS=-p=1`无过滤日志`/tmp/agenteam-d07-adjacent-verify-izjj__3a/evidence/full-serial.log`，保原每包6m及内部并发。完整门槛仍待。


## B01完整验收采纳

root核读最终独立报告并采纳B01（不代表D07整模块完成）：`/tmp/agenteam-d07-adjacent-verify-izjj__3a/review-report.md` SHA `5f792a3b9e29ea18b5aa2e96fc6e65b52d5fadd7c06b50741e8510d088daf82b`，48项证据索引 `5ff98b8070886fbd1e8a45d43ab140a60eccccc827d2cf18605c40e2df32ebd7`。451正式输入及455纯源执行、两个观察副本末次全部匹配；六个原probe及两份冻结修前观察未变。两取消原probe逐字通过，四Account原probe随完整组通过；相同输入作者check-go普通/vet/integration-vet/race/双构建通过。

唯一包级串行无过滤`GOFLAGS=-p=1 sh scripts/test-objects.sh` exit0/wall998.911s，12包全过：postgres1.535、database60.616、app45.702、process112.646、security83.968、Outbox89.069、内部Outbox1.032/contract1.290、Account210.050、objects299.846、内部object2.291/contract1.029秒。日志SHA `64018fb35114a68fa56367d9f2b70fc138c95d1006c1d546101d12e4424f253a`；保原每包6m、内部并发与断言。默认并行历史两次累计超时/Outbox初始化错误仍保留，串行成功不证明负载或历史唯一根因，不宣称默认调度性能通过。

清理SHA `8faa47232b84c6c12266dc96037e74fb4b46994e5282c6041081dc45af3ace16`：9自有nonce容器/网络/runtime及owned进程0。作者与V全部停止源码/报告写入及命令，Docker交回root。root再核90源与diff-check通过，按精确manifest提交B01及必要D03归因修复。HTTP/app装配、真实挑战、邀请/reset、SMTP及资料仍属B02–B04，当前诊断ready=false；下一步为B02精确授权。GitHub认证既有阻塞未解除，本次仅本地提交，不声称推送。


## B02实施授权

B01已提交`8b0261b`，本块基线为该提交；设计修订3不变。root授权backend完整B02，必读设计§1/3/4/6/7/8/9及T04–T07、T10适用项，沿go-development技能，Vue harness另读vue-development、vue-testing-best-practices及可用playwright技能。不得再委派或Git写；root独占本卡/计划/台账，V暂全停。Docker/owned fixture和harness资源独占backend，不连接既有基础设施或真实凭据。

解冻新`internal/central/account/{invitation,reset,password_change,challenge,delivery_intent,delivery_handler,cleanup}.go`及对应私有辅助/测试，`account/contract/{challenge,invitation,recovery}.go`；已由B01建立的`account/events.go`、`contract/events.go`添加正式delivery-requested。为实际绑定B02允许同域`account/{service,repository,planning,authority,audit_authority,secret_authority,login,session,recovery,local_recovery,command_record,browser,response,logout}.go`及`contract/{types,commands,identity}.go`必要组合增量和对应测试；保留B01原回归/Unknown/lease/身份语义，不借此重写无关基础。新增`tests/account/`本块组合测试，可在既有common fixture作必要组合；新`tests/account-captcha-web/`独立锁定依赖与真实浏览器harness、平台自有生成素材。go.mod/go.sum仅引入设计§1固定GoCaptcha2.0.5、x/image0.45.0及指定freetype，保留旧模块版本。Vue/Playwright固定版本按设计核官方integrity和实际Chromium，不改产品web或根前端锁。

00001–00010迁移、D03/D04/D05/D06旧域、recoverylog、HTTP/app/config与B03accountmail仍冻结；如现有契约确实缺必要口或表约束缺口，先报具体证据/最小范围，root裁决后再改。真实Outbox handler只在同Tx从合法intent幂等建job与marker，并实现canonical补齐；SMTP发送/日志首写门禁适配留B03，不用成功stub代替。B02需提供正式跨后块门禁/当前合法性能力，只有确有本块调用才实现。

验收包含固定24h邀请/同链接重发不续期、管理员当前权限、并发兑换/撤销/到期与用户名邮箱唯一、reset公开202隐私与异步受理/限额/同链接、改密换当前Session撤他会话/reset全撤、Audit/Secret引用/Outbox/receipt原子性、未知提交查事实/异义拒绝/零材料、当前lease真实join与公平到期回收。挑战需真实GoCaptcha生成→官方Vue旋转/键盘→验证→登录一次消费，跨browser/email/key/replay/并发/重启拒绝与上限；不得用jsdom代替真实浏览器或声称视觉挑战完整无障碍。适用T10以真实Outbox晚注册/重启canonical/重复事件验证，不提前声称SMTP attempt已验。

作者完成有意义普通/race/真实PG与浏览器检查、check-go，冻结精确source/dependency manifests，原失败完整保留、资源清理且所有写入命令停止后交独立V。最终兼容沿已明确包级串行编排、每包原6m及内部并发不改；新增测试若导致累计超时，先报分组证据再决定，禁止静默放宽预算或重复跑到绿。B02完成前不推进B03，D07仍唯一活动模块。现有GitHub认证阻塞未解除，后续提交继续本地记录。
