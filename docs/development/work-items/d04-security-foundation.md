# D04 Secret、出站访问与 Audit 基础

- 修订：1；状态：B03实施中；唯一活动模块D04；基线 `main@9beaa7f`，已推送origin/main，开工工作区干净。
- 前置：[D03](d03-postgresql-foundation.md) B01/B02真实数据库及进程独立验收完成；[D01契约](d01-contracts/README.md)已固定。
- 目标：按[计划D04](../development-plan.md#d04-secret-出站与-audit)依次完成Secret envelope encryption/版本化环境密钥环/可恢复数据密钥重保护、数据库权威动态出站策略与受控HTTP、append-oriented Audit写入及分页查询基础。

## 任务与所有权

| 卡 | 依赖 | 角色 | 独占范围 | 状态 |
| --- | --- | --- | --- | --- |
| S01 实施规格 | D03完成 | architecture_worker | 新增 `d04-security-design.md`；现有代码/根规格只读 | 已确认 |
| B01 Audit/签名cursor与正式授权端口 | S01确认 | backend_worker | 实施规格§1与本卡补充范围 | 已验收 |
| B02 Secret与密钥轮换 | B01及S01确认 | backend_worker | 实施规格§1/5/6及本卡补充范围 | 已验收 |
| B03 动态出站与入口整合 | B01/B02已验收 | backend_worker | 实施规格§1/7/8/9/10及本卡补充范围 | 进行中 |
| V01–V03 独立验证 | 对应冻结范围 | verification_worker | 只读实现，独立临时探针与隔离资源 | V01/V02已通过；V03纯边界/策略/DNS通过，HTTP待验收 |

任务拆分可由S01按完整结果的真实依赖调整，不机械分函数；安全日志/Audit写入需要避免以stub解决循环。root独占本规格、台账和开发计划；作者/验证者只在冻结的明确范围交接。遵守AGENTS、团队流程和对应design/go-development/verification技能，子agent不得再委派或Git写操作。

## 已授权边界与规格要求

读取正式[部署密钥](../../architecture/platform-infrastructure/deployment-runtime.md)、[出站策略](../../architecture/platform-infrastructure/outbound-network-policy.md)、[Audit](../../architecture/security-governance/audit.md)及D01对应Secret lease/Audit/权限/事务契约。既定产品规则不重问；工程编码与实现细节由主线程确认，真有产品歧义报告来源及影响。

Secret采用标准库AES-256-GCM envelope encryption，版本化主密钥只从环境注入；DB仅存受保护密钥与非敏感恢复元数据。明确AAD绑定、nonce生命周期、格式验证、密钥引用/轮换并发、旧版本缺失启动拒绝、分批重保护与中断恢复、lease安全生命周期和日志/DTO边界。自动迁移不能生成或回写部署主密钥。

出站DB策略为权威，保存后新请求/重试/每跳redirect立即用新规则，在途继续；连接复用不得绕过检查。明确URL/DNS全结果分类与pinning、固定禁止网段/metadata、private CIDR/端口/显式HTTP、TLS验证、凭据origin、响应/时间上限及取消。DB基础连接是可信部署通道，不套运行时出站策略；SMTP非HTTP协议适配留D07但本次正式安全端口需明确。真实网络测试只用任务专属隔离服务/网络，不能放宽生产loopback禁令来跑成功测试。

Audit只保存安全结构化摘要/ID，不复制ToolOperation/Attempt/Transcript或Secret；append-oriented不允许普通修改/删除，无自动过期。Project清理经正式端口，System记录不能复制项目正文绕过删除。明确typed event/action/metadata白名单、同事务写入、unknown、cursor签名/过滤范围和稳定排序。身份/系统授权D07、Owner查询授权D08才绑定，当前不开放无鉴权生产业务端点或以admin代Owner；测试替身只验证正式授权端口，无生产stub。

复用D03 Tx/锁/迁移/隔离PG17.8/vector0.8.1与精确Go1.27.1/local，新增迁移全局顺序明确唯一作者，不改已验收B01库除非先报告具体缺陷。优先标准库，新增依赖须有明确必要性/精确版本。补Central密钥mandatory初始化与健康/关闭，保留其余未来依赖unbound，不能伪报产品ready。

验收须实际覆盖密钥无效/解密篡改/轮换并发和中断恢复、敏感输出、出站策略并发更新/复用连接/DNS rebinding/redirect/TLS/凭据边界、Audit事务回滚与分页权限。每小块冻结后独立验证、小块提交推送；D04完整通过后才D05。当前无已知用户待定或环境阻塞，未开始产品实现。

## S01 依赖调整

主线程确认按真实依赖将实施顺序改为Audit/签名cursor及正式授权端口→Secret/轮换→动态出站/入口整合，使Secret和策略修改直接使用已验收的同事务Audit，避免stub或跨卡未绑定写入。Audit先提供服务/存储；身份D07、OwnerD08未真实绑定前不装业务HTTP。最小identity contract类型按D01消费需要建立，不能默认授予身份或替代后续授权实现。此为工程顺序调整，不改变D04范围与整体完成门槛。

## S01 静态审查

175行实施规格初稿已冻结，SHA-256 `bab770e7efaca71078866e1f3ef1621ed726941e5a958ed383e29cb8755906b2`；作者13本地链接/fragment和格式检查通过，19来源manifest `d0a7ce07aeb1a9074182ee1c85453488a2e7a37b09d6eceffa8996f8463de159`。独立静态审查与主线程走查采纳4处明确化：Audit去重须排除首次生成ID/时间与传输Session/trace并提供安全按键核实；密钥迁移失败明示组件unavailable/ready503，不能以degraded跳过启动门禁；固定Go Transport已发送GET也可能内部重试，必须跨借连接保留sent状态阻止二次发送并以服务端计数验证；旧策略命令重放不得发布旧规则覆盖新镜像。

独立审查已停止，无其他确定的nonce/epoch/lease/锁层序阻塞；仅解冻设计作者的实施规格集中修正，不开始实现。该静态结论不证明任何D04能力已运行。

## S01 确认

实施规格修订2已集中修正4项，主线程逐项复核：Audit首次生成ID/时间、稳定摘要/授权LookupAppend；运行中Secret失败明确unavailable/ready503且不放宽启动门禁；HTTP发送状态跨Transport重试保留，sent后不再发送；旧策略receipt不能回退镜像。独立初审无其他确定阻塞，修正文档仍只属静态设计。主线程确认其余数据/接口/权限/锁/nonce/轮换与真实验收方案，下一步B01实现Audit与cursor，不提前实现B02/B03。按已授权小块提交推送设计基线后开工。

## B01 开工

S01设计提交 `e34dfaf` 已成功推送origin/main（9beaa7f→e34dfaf），开工工作区干净；实施规格修订2最终179行SHA-256 `869f1fd0179dea0ecf2fe2099a47088af17e3fc90cd6514de121e171d1d55b98`。主线程4文档117链接/格式检查通过，设计只属静态证据。

backend_worker接管实施规格§1 B01新增及共享范围；补授权 `deploy/central.env.example`、新增 `docs/development/backend/audit.md`、`scripts/test-postgres.sh` 的必要suite接入。root独占本卡/实施规格/台账/计划。B01按§4完成真实Audit/cursor与必要配置诊断，不提前Secret/出站；既有postgres/foundation生产与00001冻结。全局新增00002若影响旧测试目标版本，先报告精确必要测试适配范围。无新运行依赖，沿既定Go/PG隔离机制，其他权限/业务端口未绑定，不能生产stub或未授权HTTP。

B01追加明确所有权：`cmd/agenteam/main.go`、`main_test.go` 的scope=d04/help/version与mandatory配置适配；`tests/database/migration_test.go`、`recovery_test.go`、`migration_cancel_test.go` 仅为追加正式00002所需，fixture复制完整embedded并在Target()+1追加测试迁移，替换固定2/Manifest()[1]及对应目标/洞断言，保留原恢复/历史/取消覆盖。生产postgres/foundation、00001、embed.go不变；backend_worker独占以上测试适配。

## V01a identity/cursor分段审查

作者冻结identity/cursor共6文件，manifest `/tmp/agenteam-d04-b01-identity-cursor.sha256` SHA-256 `10cee382712e8e31597ea00b2db4da53815fe697cd61c6b5196efc4e97c20b52`。依赖仅未变foundation/标准库；作者局部race（cursor1.058s/identity1.015s）与vet通过，使用独立Python生成的完整签名向量。Audit其余范围继续作者独占写入，不能将此子集当B01完成。

主线程与独立验证确认一项编码契约修正：order_generation虽Go语义类型int64，作为排序修订跨JSON仍需D01正十进制字符串，当前wire为number。不是Go内部精度或授权绕过问题；须改wire并增加超过2^53/MaxInt64向量及number/前导零/负数拒绝。等待独立探针结束后集中移交，当前冻结范围未解冻。

V01a独立探针已结束停读，仅上述generation编码需返修：超过2^53及MaxInt64实际失败exit1；其余逐字节token篡改、跨scope、typed bounds、身份反序列化/非法Service、嵌套fmt/JSON/slog安全race通过（1.187s），6指纹末次未变。临时副本 `/tmp/agenteam-d04-v01a-ye7hiv3g`。已解冻cursor.go与对应tests给作者修复，其他4文件继续冻结，修后定向独立复验；无容器/Git操作，无B01完整验收结论。

## B01a 公共identity/cursor小块完成

原6文件重新冻结，manifest `/tmp/agenteam-d04-b01-identity-cursor-v2.sha256` SHA-256 `18234b86451a5f843643cc25cbf0486bbe21232b4e40e26021bf6778e4d77bdc`。只cursor.go/test变化，wire generation改规范正十进制字符串，Go int64接口不变；作者race1.059s/vet通过。独立V01a复用原失败probe与Python MAC重算，两组>2^53/MaxInt64向量及number/前导零/0/负数/溢出/null/+1/1.0拒绝全部通过，定向race1.050s、格式通过；其余4文件指纹未变，复用前轮安全边界结论。

验证者已停读/命令，作者停止本6文件写入，主线程已读全部实现并核对证据。将此完整类型与签名库作为B01a小块精确提交推送；不包含仍在写入的Audit，也不改变CLI/数据库启动配置。当前签名不代替授权，D07/D08仍未绑定；B01真实Audit存储/配置诊断与整体验收仍在实施，D04未完成。

- B01a实际交付：`d623004`已推送origin/main（e34dfaf→d623004）。公共identity/cursor保持已验收冻结，作者继续B01 Audit/配置诊断/真实PG验收；活动Audit未纳入该提交。

B01文档补授权：`docs/development/repository-structure.md` 与根 `README.md` 仅同步Audit/cursor、Central必填CURSOR_KEYRING/D04检查范围和既有未绑定能力的现状/链接；数据库说明当前无必要变化。backend_worker独占此范围。

作者首轮真实TestAudit race4.348s通过同Tx回滚、12并发同义/异义、当前权限、同时间分页/过滤、EXPLAIN和501条分批清理/迟到append拒绝；第二轮5.887s通过COMMIT代理两个unknown方向与正式LookupAppend、cleanup unknown重读。nonce6b201157641349d0307d7fccc8fea18f、09b906c7a19ffc024235a558a65ff832已精确清理。配置/app/logging/cmd局部race通过；正在补真实入口及冻结，以上作者证据不代替独立验收。

## B01 Audit全量冻结与V01b

Audit核心15文件manifest `/tmp/agenteam-d04-b01-audit-core.sha256` SHA-256 `b5d2ddfc1a0f23f4a4f30f98b77e4e551b3ab98ee3dc3f055225d5a01a843147` 已冻结；全部B01含已提交B01a共52输入manifest `/tmp/agenteam-d04-b01-all.sha256` SHA-256 `002e0f365fada167133ca39571518e68e936b1fa177dd21a47bd8cb8bedd5fd1`。作者所有命令结束、源码停写。

作者check-go.sh普通test/两种vet/race/双bin构建exit0；完整test-postgres.sh最终exit0：postgres1.037s、database21.093s、app12.967s、process38.590s、security8.126s。首轮仅旧recovery fixture两处动态版本替换失败，已在授权测试范围精确修正后重跑；没有修改D03生产库。最终日志 `/tmp/agenteam-d04-b01-postgres-final.log` SHA-256 `b1bd930bdb19bfeb4852f53408ad0029c734c211a1266831551aa68fc47f82f9`，nonce7d0a73b12db8d34b85af74a61a9f31a7及此前失败fixture精确清理；PG170008/160012、vector0.8.1。

V01b已扩展独立整批审查，临时副本 `/tmp/agenteam-d04-v01b-v6hxfvcd`；其余61个依赖与d623004一致，无新增依赖。主线程已读生产核心/SQL/入口/脚本/说明，9文档147链接/格式通过，等待独立真实PG与进程结论。两候选不列当前阻塞：当前Human追加仍需Mutate，不凭denied放宽权限；后续D07/D19若要记录未获业务授权的Human动作，须在对应调用点明确trusted producer与安全Actor端口。audit_storage表示启动验证且随DB健康，security_stage仅初始化，不承诺运行时逐表DDL/权限变更巡检。以上限制不构成真实身份或后续模块验收。

V01b独立真实选定security7.684s/process5.242s及局部关键race通过；另新增公开cleanup失败探针发现唯一阻塞：真实PG已提交DELETE（独立查询剩余0）、COMMIT响应丢失且同Store停止admission后核实不可用，`errors.As`获得COMMIT_UNKNOWN却commit_state=not_started。根因audit/error.go对所有code固定NotStarted。探针 `/tmp/agenteam-d04-v01b-v6hxfvcd/tests/security/review_audit_unknown_test.go`，日志 `/tmp/agenteam-d04-v01b-unknown.log`，实际失败exit1/0.86s。

独立作者已停读和命令，52/15指纹末次未变；两nonce75eb1c790b239f4adb77ecf7ae71c052、6b0133ac2ed4b0b854513fb83f273296容器/网络/TMPDIR均确认0。主线程采纳并仅解冻audit/error.go及必要对应测试，要求COMMIT_UNKNOWN保留Unknown状态/安全cause，定向真实复验；其他范围冻结，B01仍未验收。

## B01 完成与交接

唯一unknown错误状态阻塞已闭环：仅audit/error.go、service_test.go、audit_cleanup_test.go变化，COMMIT_UNKNOWN明确保留foundation.Unknown及安全cause。作者定向race1.040s/integration vet、原独立probe原样纳入的真实cleanup3场景3.573s通过，nonce4ff49f7dc40b56abe0d9d95dd15beed5清理。独立再次使用未修改的原探针，真实提交删除+丢回包+核实不可用返回unknown，通过security1.904s；cause/As/Is/递归投影race1.018s通过。日志 `/tmp/agenteam-d04-v01b-unknown-recheck.log`，nonce725c5bcf2ac1e88959ba89a0330f4607两容器/网络/TMPDIR再次独立确认清理。

最终52文件manifest `/tmp/agenteam-d04-b01-all-v2.sha256` SHA-256 `3ec6ac44591b98ffb3e703727bcc82d6ccad7ede5f50f5fff306c84b95b978f4`；15core-v2 SHA-256 `0ccba2d0dfb0c1a3d12ae03b33a3afb2139b2783946597392fdd2e780585119c`。除授权3文件外其余49未变，复用原完整检查与独立真实证据，末次gofmt/空白/指纹通过。所有作者/验证者均停读写和命令，主线程审查通过，B01验收完成，按授权精确提交推送，通过本节Git历史定位。

当前Central新增必填cursor keyring与30s安全初始化，真实Audit存储可用、无业务HTTP；D07/D08的Session/System/Owner/gate适配仍未绑定并拒绝调用，不把fixture权限当生产授权。B02 Secret和B03出站尚未实施，D04整体未完成；下一步按已确认规格B02实现环境主密钥环、envelope/lease与可恢复轮换，无用户待定或环境阻塞。

## B02 开工

B01 `088185e`已成功推送origin/main（d623004→088185e），开工工作区干净。backend_worker接管实施规格§1 B02新增/共享范围：secret及contract、00003_secret.sql、tests/security/secret_*与必要config/app/cmd/logging/process/fixturehelper/部署示例/说明。补授权新增 `docs/development/backend/secret.md`，后端README、根README、AGENTS、仓库结构的必要当前状态；必要测试helper仅限B02用途，scripts/test-postgres.sh仅suite接入。

root独占主卡/实施规格/台账/计划；已验收Audit/cursor/identity与D03生产库保持冻结，若发现具体公共接口缺口或需返修先报告、移交再改。只追加00003，不修改00001/00002；旧数据库fixture已动态Target()+1，无需要不得改其既有覆盖。沿既定Go1.27.1/local与owned PG隔离机制，无新增运行依赖。

按规格§5/6实际实现env master keyring、AAD/DEK/nonce独立预留、registry canary与epoch fence、Secret metadata/mutation receipt及reference/lease、合法读和Audit、100条可恢复重保护、最终完整反查与退休、Project生命周期正式端口及启动/健康/同预算停机。区间预留须在业务Tx前确认提交，unknown烧掉；不能借InTx另开Tx。生产未绑定的System/Project/执行引用权限明确拒绝，无HTTP或stub；内部维护真实装配，出站留B03。候选/密钥敏感投影与DB故障/并发/重启必须真实验证，稳定独立子集可先冻结审查。无用户待定或环境阻塞。

## V02a 纯加密分段审查

作者冻结6文件：secret/error.go、keyring.go、envelope.go及test、contract/material.go及test；manifest `/tmp/agenteam-d04-b02-crypto.sha256` SHA-256 `06bec83b821afd4b8c7301335fd741d60d9fa42af9d3f3e36a3cbeff9e94caa4`。严格keyring/跨cursor材料隔离、AES-GCM封装/AAD/rewrap/canary、ATK1 nonce编码与可销毁SecretMaterial已编译并局部race（secret1.074s/contract1.037s）/vet通过，独立Python cryptography完整向量已作作者自测。

V02a只读此6文件及已验收依赖，使用临时独立module，不读仍在写的contract/types.go与SQL/轮换，不运行Docker。主线程已读此纯实现，尚未确定新缺陷；真实nonce预留/持久恢复/lease/轮换仍由作者实施，不能因纯加密通过标B02完成。

## B02a 纯加密小块完成

V02a独立通过，无阻塞。临时副本 `/tmp/agenteam-d04-v02a-fhcxltvb`，Python cryptography独立Project/receipt/AAD/fingerprint/GCM/canary及9007199254740993→MaxInt64重保护向量，证实业务ciphertext/data nonce不变；逐字节篡改、类型/长度/版本/counter/revision溢出拒绝；keyring16KiB/32-key与历史cursor材料隔离、材料panic/error清零及并发Use/Destroy、安全投影/cause身份通过。实际独立 `go test -race -count=1 -run '^TestReview' ./internal/central/secret ./internal/central/secret/contract` 1.066s/1.035s，原冻结测试也通过；精确Go1.27.1/local，13个已验收依赖未变。6文件末次指纹保持 `06bec83b821afd4b8c7301335fd741d60d9fa42af9d3f3e36a3cbeff9e94caa4`。

作者停止这6文件写入，验证者停读/命令，无Docker/Git操作。主线程全部纯实现审查通过，精确提交推送B02a；活动contract/types.go与SQL/lease/轮换未纳入，不把纯加密当nonce持久唯一性或B02整体验收。内部envelope是包内持久载体，未发现通用输出路径；后续prepared/错误/DTO可达性仍需整批审查，禁止敏感材料泄漏。

- B02a实际交付：`33a61db`已推送origin/main（088185e→33a61db）。纯加密6文件保持验收冻结，作者继续B02持久化/lease/轮换与入口，尚未整体验收。

B02持久化作者第二轮真实PG/race自测通过：原字节create/同义重放/异义冲突、lease读、更新后同lease读新值且已取material不变、lease阻止删除与原owner幂等释放；102 payload覆盖100条批次、覆盖后CAS丢弃、重复批次不重复计数、重启恢复/最终退休/移除旧key后值及receipt读取。损坏行测试报告failed/unavailable、拒绝新写且已授权无关值可读，日志 `/tmp/agenteam-d04-b02-core.log`。当前源码仍由作者写入，nonce/真实断连/kill/清理/入口未完整验证，未冻结指纹，以上仅作者阶段证据，不构成独立验收或B02完成。

作者追加nonce/恢复真实race自测9.214s通过：4实例并发区间唯一、业务回滚烧nonce、实际COMMIT回包丢失整段不消费、尾段及2^32−1上限、坏canary/历史材料重用/缺旧key拒绝；实际SIGKILL在prepared、100条更新未提交、checkpoint已提交三边界终止子进程，等待owned backend退出后重启完成104条恢复。作者报告fixture精确清理，正在补read-unknown和引用/Project清理；核心仍未冻结，等待完整指纹及独立验证。

## V02b 持久化核心冻结

B02核心25文件冻结：`internal/central/secret/**`（含已验收6文件）、`00003_secret.sql`、`tests/security/secret_*`；manifest `/tmp/agenteam-d04-b02-core.sha256` SHA-256 `e7c145a9f26217ba8f47fb6fb80e141a0fc060898c5d70e61b41fe3a83bd6ea7`，主线程逐文件核验全OK。作者全TestSecret真实PG/race13.402s，日志 `/tmp/agenteam-d04-b02-core-final.log`，nonce/kill细证据 `/tmp/agenteam-d04-b02-security4.log`；追加read COMMIT-unknown不交material且下一次独立resolution、Audit回滚、权限撤销/MCP retained拒绝、101条Project分批清理/refs+lease pending/迟到候选拒绝、旧writer等待fence、rotation unknown核实及核实不可用保持Unknown。作者局部test/race/vet/build与空白通过；纯6文件未变。最终fixture nonce42df8e38696ddefba4e3eabc1dc6f46f报告container/network清理，命令全停止。

V02b已接管冻结核心，在main@33a61db稳定临时副本覆盖精确25文件独立验证；既有Audit/cursor/identity/postgres/foundation与fixture依赖用已提交版本，不读取作者活动入口。Docker资源当前归验证者。作者仅继续未冻结app/config/logging/cmd/process及已授权说明，真实PG须顺序交接；主线程已开始核心代码/SQL审查。B02入口和整体验收仍未完成。


## B02b 持久化核心完成

V02b独立验证通过，无阻塞。临时副本 `/tmp/agenteam-d04-v02b-sqja0azv` 基于33a61db覆盖25冻结输入，实际 `AGENTEAM_GO=/workspace/toolchains/go1.27.1/bin/go sh scripts/test-postgres.sh -run '^Test(Secret|ReviewSecret)'`，PG17.8/vector0.8.1、security race18.846s、exit0；日志 `/tmp/agenteam-d04-v02b-independent.log`。新增独立探针证明12个同命令并发与更换有效Session重放仅一条Secret/receipt/create Audit，异义拒绝；canary Audit回滚留下pending登记，新实例从新确认区间补齐且不提前推进write version；cleanup在COMMIT前/后丢响应均unknown，重试收敛且迟到rewrap Applied=0；两个独立进程64次写入产生128个payload nonce加1个canary全部唯一，high-water3072。

冻结原有nonce unknown/SIGKILL、epoch fence、100条CAS/完整退休反查、lease当前值及Audit失败/unknown拒交材料、权限/清理用例亦独立通过；新增contract类型race1.020s。首次直接integration因缺fixture被门禁拒绝，专用脚本随后通过，不把缺fixture当成功。纯加密6文件未变复用V02a。最终25 manifest仍为 `e7c145a9f26217ba8f47fb6fb80e141a0fc060898c5d70e61b41fe3a83bd6ea7`，格式通过，nonce04a7bba599c7b9bf84ee35da74b04e6f的两个容器/网络/临时目录独立确认清理。验证者全部命令结束并停读；作者核心停写，Docker已交还作者完成入口真实验证。

主线程已读全部核心生产/SQL及关键故障测试，最终逐文件指纹一致；3份进度文档104链接/格式检查通过。B02b作为独立存储库小块精确提交推送，不含仍在修改的config/app/logging/cmd/process/操作说明。当前Central尚未由本小块装配Secret；System/Session/Project/Usage真实授权仍待D07/D08/D09/D20/D22绑定，无业务HTTP或生产权限stub。B02入口与D04整体仍未完成，继续当前模块。

- B02b实际交付：`6feda6b`已成功推送origin/main（33a61db→6feda6b）。持久化25核心文件继续冻结，作者正在验证Central入口/启动/信号与说明；本提交不包含活动入口，B02尚未整体验收。


## V02c 入口冻结与整合验收

作者全部B02范围停止写入及命令，入口27文件manifest `/tmp/agenteam-d04-b02-entry.sha256` SHA-256 `e6442c799793f3ddb96ff94f423d4c20d821d66b14c4ac9c86a85ca91f1d1234`；含核心52文件 `/tmp/agenteam-d04-b02-all.sha256` SHA-256 `f7a8c2dbe80dd50c1fb3ec58a7b42e03736ca6e33d35ca7137878c72a64c8415`。作者精确Go1.27.1/local完整check-go exit0（普通test、普通/integration vet、race、两个bin），日志 `/tmp/agenteam-d04-b02-check-go.log`；完整test-postgres exit0：postgres1.053s/database30.019s/app33.845s/process42.895s/security27.716s，日志 `/tmp/agenteam-d04-b02-postgres-full.log`，固定PG17.8/vector0.8.1及PG16.12拒绝fixture。最终nonced3b2e3bc92f9709bc20b772ca479d5a2报告清理。

局部入口真实PG app20.752s/process9.381s，日志 `/tmp/agenteam-d04-b02-entry-pg.log`：canary/缺旧key在HTTP bind前拒绝、1→2及最终退休移除旧key重启、registry advisory等待处SIGINT/SIGTERM取消和owned backend退出，真实SQL rewrap+HTTP在途drain/deadline/second-force与startup第二信号共用预算。未改scripts/check-go/test-postgres/fixturehelper、运行依赖或核心。

V02c已接管入口独立验证与Docker资源，基线6feda6b，复用V02a/V02b核心不变证据。主线程已读入口生产、配置、生命周期、诊断及操作说明，52文件指纹一致，8文档138链接/格式通过；尚无确定新阻塞。B02仍等待独立入口结论，B03未开工。


## B02 完成与交接

V02c独立入口/整合通过，无阻塞。定向race app4.184s/config1.029s/cmd1.024s；新增探针确认Audit/Secret同一30s deadline，bind失败停止并join已启动worker。实际 `AGENTEAM_GO=/workspace/toolchains/go1.27.1/bin/go sh scripts/test-postgres.sh -run '^Test(RealSecret|CentralSecret|ReviewCentralSecret|CentralRealHealthTimeoutAndRecovery$|CentralCheckAndCompiledRepairNeverConnect$|CLIScopeAndSafeFailures$)'`，app18.124s/process29.097s、exit0，日志 `/tmp/agenteam-d04-v02c-independent.log`。真实首停HTTP/SQL drain、timeout/第二信号共用额外1s、启动取消、坏canary/缺key拒绝、诊断及DB健康恢复通过；新增真实Central探针在启动抽样之外的payload损坏时确认worker failed/diagnostics及ready unavailable/旧key不退休，修复仅owned fixture损坏字节后实际重启恢复，再移除旧key重启通过。

独立5份Markdown34链接/4fragment检查通过，主线程8文档138链接/格式通过；27入口、52全批、25核心最终指纹均保持原值。nonce12ebe47977ec1b6f6dd1668ff88e7865两个容器/网络/临时目录确认清理，验证者全部命令结束停读，作者全部停写。主线程已读入口生产/核心/SQL/关键测试和说明，结合V02a/V02b与作者完整回归，确认B02本范围完成，按授权提交推送入口小块。

Central现强制独立Secret keyring，在30s安全阶段真实校验registry/canary/required DEK与epoch后启动维护worker，再监听HTTP；诊断状态真实，完整产品仍ready=false。Session/System/Project/Usage真实授权及Model/MCP/执行binding仍由D07/D08/D09/D20/D22后续绑定，无匿名业务HTTP或生产stub。D04整体未完成，下一步B03动态出站策略/受控HTTP及完整D04验收，无用户待定或环境阻塞。


## B03 开工

B02入口 `fffd0dc`已成功推送origin/main（6feda6b→fffd0dc），开工工作区干净。backend_worker接管已确认实施规格§1/7/8/9/10：新增`internal/central/outbound/`（含必要正式contract）、`00004_outbound.sql`、`tests/security/outbound_*`、`tests/testsupport/outbound/`、`scripts/test-security.sh`，及必要config/app/cmd/logging/process/shared fixture suite装配与测试。补授权新增`docs/development/backend/outbound.md`，后端README、根README、AGENTS、仓库结构和`deploy/central.env.example`只同步当前能力/配置/命令；既有test-postgres/fixturehelper仅必要suite接入。root独占主卡/实施规格/台账/计划。

已验收Audit/cursor/identity/Secret、D03生产及00001–00003冻结，不改既有加密或事务公共接口；具体缺陷/接口缺口先报主线程移交。只追加00004，不引入运行依赖，不提前D05+或生产未授权业务API。沿Go1.27.1/local和owned PG固定digest；真实网络fixture仅owned隔离服务/网络、生成临时CA，以实际private容器地址跑允许路径，不篡改生产loopback禁令、不连接既有基础设施。

按规格实现DB权威策略/同Tx Audit与receipt/旧重放不回退镜像，固定IP分类、全DNS结果/pinned dial、可取消writer优先准入门禁、真实HTTP首字节与policy提交线性化、跨Transport内部retry的sent身份/最多一次零写重试、H1 keep-alive逐次验证、TLS/credential origin/redirect/stream限额及安全输出。SMTP仅正式受控dial端口，协议留D07。装配启动/健康/停止和真实网络/PG/进程故障验收，不以stub替代安全能力。稳定完整子范围可先冻结独立验证并小块提交；D04整体完成后才D05。当前无用户待定或环境阻塞。

B03资料核验限制：主线程尝试读取IANA IPv4/IPv6 special-registry CSV时，环境代理返回HTTP CONNECT 403，未获得在线最新清单；没有调整网络或把该读取记为通过。实现沿已确认规格的保守分类和固定禁用段，并引用权威登记入口；本限制不阻塞既定实现与隔离fixture验收，最新在线登记完整性不得据此宣称已核验。


## V03a 纯出站边界冻结

作者冻结outbound的classify.go/error.go/target.go/rules.go/boundary_test.go共5文件，manifest `/tmp/agenteam-d04-b03-pure.sha256` SHA-256 `bc8027b62e564234df93a22ed3e3836e2e66e8ed71286f56ce19d0c55c5aad24`，主线程指纹核验一致。作者精确Go1.27.1/local的test/race1.023s/vet通过，覆盖URL/origin、规则规范化/严格JSON、固定IPv4/IPv6/metadata分类及安全错误。V03a在fffd0dc稳定副本只覆盖5文件独立验证，依赖为已提交Audit contract/foundation，无Docker；不读作者活动gate/policy/网络实现。主线程已读5文件，暂未确认缺陷；此子范围不证明DNS/HTTP/策略存储已通过，B03继续实施。

V03a发现origin候选：target.go对bracket IPv6执行Unmap，导致mapped IPv6与IPv4 origin被合并且URL authority改写。主线程复核D02已验收规则为保留mapped IPv6 origin身份；D04仅地址分类应Unmap，凭据origin/Host身份不得据此合并。等待独立实际探针与剩余审查停读后，集中移交target及对应test修正；当前5文件保持冻结，gate/policy无依赖部分可继续，不构成用户产品待定。

V03a独立probe已实际复现两项失败：mapped IPv6/IPv4 origin错误Equal且Target.URL authority被改写；numericLabel以ParseUint成功作为hex数字条件，令空`0x`与超uint64全hex串（含域名最终label）误收为DNS。临时副本 `/tmp/agenteam-d04-v03a-8h0_c206` 的review_boundary_test.go保留失败探针，实际exit1；Node URL对照空0x解释为0.0.0.0、溢出数字拒绝。主线程采纳两项修正：分类仍Unmap而origin保持IPv6；疑似hex label按字符语法判定，空0x/溢出拒绝，0xg等非数字DNS不误拒。待验证者其余边界结束停读后集中移交，当前冻结不变。

B03受控HTTP工程选择：作者采用包内专用HTTP/1.1 RoundTripper与受控keep-alive池，复用标准库Request.Write/ReadResponse语法，在实际conn.Write前实施门禁；主线程认可，规格不强制标准Transport。仍须验证同attempt sent跨借连接保持、已发GET/Idempotency-Key断响应只命中1次、零写至多1次并重DNS，以及1xx/final、chunked/trailer、Connection close、body EOF/Close、全限额/取消与并发连接所有权；不以换实现免除既定网络barrier验收。

V03a本轮已结束停读/命令：原冻结包独立race1.037s；其余fixed deny/mapped/metadata、完整重叠规则/canonical/复制隔离/256与257容量、递归fmt/JSON/slog/cause探针race1.075s，vet/格式通过。两项失败probe SHA-256 `8e87a27d74cbeef177fb59c2731bf9ee67ce495ccceba2a2a1e20e0f6818ab33`，原5文件末次指纹不变；无Docker/外部网络。主线程仅解冻target.go及boundary_test.go给作者返修，其他3文件保持冻结，修后定向复验，不把当前V03a标通过。

B03策略作者首轮真实PG/race security3.198s通过K1/K2/旧receipt不回退镜像、换Session重放、权限撤销/Audit回滚、真实COMMIT丢回复核实/不可用保持unknown等，日志 `/tmp/agenteam-d04-b03-policy-pg.log`；nonce35b539e8f6561f8c139f17a8f098a235报告精确清理。policy/gate仍活动，未冻结或独立验收；作者继续并发保存与门禁边界。

B03 DNS实现深化：主线程复读固定Go1.27.1的net/dnsclient_unix.go，StrictErrors仅在temporary family error时丢弃全结果，坏answer/其他错误仍可能返回另一family子集；独立LookupIP的IsNotFound亦不足以区分NODATA与NXDOMAIN，不能据此满足原规格。认可作者采用包内有界DNS wire适配，标准库、无新增依赖；读取部署resolv.conf nameserver作为可信基础DNS通道，明确search/NSS/hosts支持边界，不能静默回退。合法NOERROR/NODATA可为空，任一family失败整体拒绝；实现须验证ID/question/type/class/rcode、压缩指针与CNAME链/记录归属、UDP截断转TCP、TCP长度、包/记录/64地址上限、5s总预算与取消，并以owned真实UDP/TCP fixture独立验证解析器，不仅替换解析接口。此为落实原有严格完整结果门槛，不改变产品规则。


## B03a 纯出站边界完成

V03a两项阻塞已以原失败probe原样复验闭环：mapped IPv6保留规范hex IPv6 origin与authority、与IPv4不Equal，分类继续Unmap；空0x/任意长度全hex数字主机拒绝，0xg等DNS仍接受。仅target.go/boundary_test.go改变，其余3文件与19份稳定依赖不变。作者race1.032s/vet通过；独立副本完整 `go test -race -count=1 -v ./internal/central/outbound` 1.075s、vet/gofmt/空白通过，原失败probe SHA仍 `8e87a27d74cbeef177fb59c2731bf9ee67ce495ccceba2a2a1e20e0f6818ab33`。

最终5文件manifest `/tmp/agenteam-d04-b03-pure.sha256` SHA-256 `4993cd4047121664e58ce04e116a096d9619efb8ae79afa73876e3b811cfc166`，末次指纹一致。验证者命令结束停读，无Docker/外网；作者5文件停写，主线程已读修正，认可纯类型/分类/规范化小块，按授权精确提交推送。活动policy/gate/DNS/HTTP/fixture未纳入，尚无完整出站网络验收，B03/D04继续实施。

- B03a实际交付：`1898748`已成功推送origin/main（fffd0dc→1898748）。纯出站边界5文件保持验收冻结，作者继续活动policy/gate/DNS/H1与隔离网络fixture；B03尚未整体验收。


## V03 DNS分段冻结

作者冻结dns.go/dns_test.go共2文件，manifest `/tmp/agenteam-d04-b03-dns.sha256` SHA-256 `ee7563da811b54f9b7eb547403db2943f608e06d146559725ef214b52088ef10`，依赖仅1898748已验收pure；作者真实owned loopback UDP/TCP DNS测试/race1.054s/vet通过，覆盖双family/NODATA、TC转TCP、NXDOMAIN/SERVFAIL/FORMERR任一family拒绝、ID/question/type/class/包边界/压缩指针/owner/CNAME、64/65和取消。V03独立仅审此2文件及已提交依赖，不读活动policy/gate/H1，无Docker。主线程已读解析器，提出wire label内点可能被Join后误归一化的候选及NOERROR referral是否误作NODATA的语义候选，待独立证据；当前未解冻或标通过。

DNS两个候选经独立真实UDP probe确认并由root采纳：Question/answer owner/CNAME RDATA的单wire label内点被Join后误当多label；A正常而AAAA NOERROR/0answer/authority NS referral（RA有无两种）均错误返回A子集。原失败probe `/tmp/agenteam-d04-dns-review-qzp07n5g/internal/central/outbound/review_dns_test.go`，TestReviewDNSRejectCollapsedWireLabels/TestReviewDNSRejectReferralFamily。修正应逐label验证、保守拒绝显式未完成referral并保留合法NODATA/SOA；待最后独立协议探针停读后移交，不在活动审查中改源码。

B03 policy/gate另冻结6文件：gate.go/gate_test.go/policy.go/00004_outbound.sql/tests/security/outbound_policy_test.go及outbound_concurrent_test.go，manifest `/tmp/agenteam-d04-b03-policy.sha256` SHA-256 `27fc6f6032d4e1cdf6edee5fa69a48b98a832b7511cd8680406aaad357aa46cc`。作者并发保存真实PG security1.431s通过，同expected两命令恰好一提交一version_conflict、receipt1条；日志 `/tmp/agenteam-d04-b03-policy-concurrent.log`，noncea2ca89283f8a0717e7d4b0a5160bb354报告清理。root已读生产/SQL及gate/并发测试，指纹一致，暂未确认阻塞；独立验证待DNS本轮结束后串行接管，Docker当前空闲。

DNS本轮独立已停读后仅解冻2文件给作者，余下协议/组合64地址/mapped去重/5s总预算实测5.00s/兄弟取消/TCP剩余预算race6.182s，压缩CNAME/倒序/SOA及畸形拒绝race1.028s通过，无其他确认阻塞；probe最终SHA `07788e5a029b6e1a87172f9a8d9d0f8a1b641b1507e195473ebcf1af0a091850`。作者修正后2文件重新冻结，新manifest SHA `2d65bbe2d458bf0ecdc08242b653b985171731c4aad51f8d95ca5733aefb996d`，作者race1.063s/vet通过。原失败probe待定向复验；root复读diff提出新增SOA判定是否验证真实RDATA的修复完整性候选，仍待证据。

Policy/gate6文件已正式交独立验证及Docker资源，作者保持冻结仅继续DNS返修/HTTP无Docker范围。root读Reload时提出恢复竞态候选：仅进程门禁而无DB policy shared锁，可能在未知旧Tx仍持锁未结束时MVCC读旧row并提前恢复available；独立真实PG探针核实中，未先修改实现。

独立Policy真实probe已证实Reload候选：driver实际发送COMMIT frame到owned proxy后client EOF，proxy保留原backend Tx；UpdatePolicy核实等待锁至600ms返回Unknown并保留原cause。Reload在150ms预算内却立即恢复available=v2，随后释放原COMMIT后DB=v3而镜像仍available/v2。TestReviewOutboundReloadWaitsForOriginalUnknownWriter失败，日志 `/tmp/agenteam-d04-policy-review-pg.log`；同轮其他作者policy用例通过，nonce4f9d2d0b28ae3dc2332ab869b6496b45等待末次清理核验。root采纳：Reload在进程exclusive门禁内，用新短Tx取得DB policy shared锁并确认读取完成后才publish，失败保持unavailable，不能MVCC提前恢复。

DNS原两失败probe在返修上原样race1.027s通过，但修复完整性probe又实际复现畸形/无关SOA或NS被接受为negative：空SOA、少/多1字节、坏name、无关zone SOA、空NS RDATA都错误返回A子集。probe `/tmp/agenteam-d04-dns-review-qzp07n5g/internal/central/outbound/review_dns_soa_test.go`。合法SOA RNAME的hostmaster、host_master与单wire label host.master三正例通过，返修应保留。root采纳NS/SOA真实RDATA/zone结构校验，等待验证者全部停止后精确解冻，不把第一轮probe通过当DNS完成。

两块独立最终已停读/命令：policy作者场景与unknown/cause探针security4.312s，唯一失败为Reload提前发布；gate新增queued writer先于16个late reader/并发双release/计数归零race1.021s，vet/格式通过。原latecommit probe `/tmp/agenteam-d04-policy-review-1v2839un/tests/security/review_outbound_policy_test.go` SHA `01837162260cabbe541e9b7ed91542fe0477def1434bee590623e6c960fe68eb`；DNS新增SOA probe SHA `9b6dec2f47af0ee69fcc730fcde5b97300c3b6af83f6853ce1c3f18517cacdfc`，原DNSprobe未改。policy6/DNS2/pure5末次指纹一致，nonce4f9d2d0b28ae3dc2332ab869b6496b45两容器/网络/临时目录独立确认不存在。

主线程精确解冻dns.go/dns_test.go、policy.go及outbound_policy_test.go（必要可新增outbound_reload_test.go）给作者返修；gate/gate_test/00004/outbound_concurrent及pure5继续冻结。Docker交还作者自测，HTTP无依赖部分继续；修后分块冻结并用原失败probe复验。当前DNS/policy均不标通过。

B03 SDK工程边界已由root明确并同步实施规格修订3 §9：原生`*http.Client.Do`不可避免地将RoundTripper错误包为携原URL的`*url.Error`，导出Transport/CheckRedirect也并非类型不可变。主Client.Do保持安全输出；原生实例仅可信adapter/组合根使用，固定受控RT、拒自动redirect/无cookies，禁止替换、不提供业务拨号/TLS开关；原生SDK错误必须经SafeNetworkError映射才跨业务/日志边界，D09/D20真实adapter负责组合验收。作者补工厂/mapper敏感投影测试与说明；不把标准库原始error安全或未来SDK已绑定作为当前结论，无用户待定。


## B03b 策略存储与门禁完成

Policy Reload唯一阻塞已闭环：进程exclusive内新短recovery Tx取得policy shared DB锁，读canonical并确认commit后才publish；失败unavailable。新增outbound_reload_test.go纳入原真实故障回归；原probe仅初始化前不armed、目标Update前armed两处fixture适配，因Reload新增只读COMMIT，反向还原SHA匹配原 `01837162260cabbe541e9b7ed91542fe0477def1434bee590623e6c960fe68eb`，故障顺序/断言逐字不变。

作者受影响真实PG security4.989s通过，日志 `/tmp/agenteam-d04-b03-policy-repair.log`、noncec893c65935ae08ec90d55f90b78ea5db清理。独立原失败及replay/rollback/三unknown/同expected并发实际race security5.591s、exit0，日志 `/tmp/agenteam-d04-policy-review-recheck.log`；旧writer持锁时Reload不再提前available，原commit完成后DB/镜像同v3，unknown原cause保留。gate/00004/concurrent/pure5未变，复用原门禁独立race，vet/格式通过。

最终policy7文件manifest `/tmp/agenteam-d04-b03-policy.sha256` SHA-256 `39e6b55cba77b2d82d1e2e8087f5e9974c487b5c9f4682457478552306be4322`，source/copy一致。nonce928c77669ccdad099df921c4f9c127f8两容器/网络/精确TMPDIR独立确认清理；验证者停止policy读取和命令，作者7文件停写，root已读生产/SQL/返修并认可存储/门禁小块，按授权精确提交推送。Docker交还作者进行真实网络fixture；DNS2仍由验证者无Docker复验，HTTP/入口未整体验收，B03/D04继续实施。


- B03b实际提交：`ee8ddb7`已本地提交。此次push因HTTPS缺少可用认证失败，`gh auth status`报告当前GH_TOKEN无效；未输出凭据或修改认证配置，等待安全配置恢复后补推。远端仍为1898748，本地后续实施不受阻。

## B03c DNS完整结果解析完成

最终修正先解析并校验CNAME链，再以终局名称验证authority NS/SOA所属zone；合法跨zone CNAME与终局AAAA NODATA不再被误拒。SOA负响应可结束空family，畸形wire label、referral、不完整/无关SOA与NS继续拒绝，不返回另一family子集。

作者race1.077s/vet通过；独立第三轮定向race1.075s、vet/gofmt通过，涵盖双family/NODATA/TCP fallback、压缩CNAME、格式错误、合法SOA mailbox与跨zone终局NODATA。原三份probe字节不变：review_dns_test.go SHA `07788e5a029b6e1a87172f9a8d9d0f8a1b641b1507e195473ebcf1af0a091850`、review_dns_soa_test.go SHA `9b6dec2f47af0ee69fcc730fcde5b97300c3b6af83f6853ce1c3f18517cacdfc`、review_dns_cname_zone_test.go SHA `ff057686294b3b95021cb8821126b7b94b8c90e32a8d2b90746c4f5577bf81ac`。此前64/65地址、总预算5s、取消及TCP剩余预算证据复用。

最终2文件manifest `/tmp/agenteam-d04-b03-dns.sha256` SHA `238222cc10eed8d3e9d5c9c0349c4e058abca7ba5b05ef34a7a9d5e8f2537bef`，source/copy一致。独立验证曾被平台风险检测中断；确认原命令停止后，仅重跑用户授权仓库内自建127.0.0.1 UDP/TCP兼容性测试，未访问外部主机、未绕过安全限制，重跑正常完成。socket关闭/goroutine join及无残留进程已确认，验证者停读、作者2文件停写，root已审修正并精确提交该小块。

HTTP真实网络fixture及入口仍活动，尚未整体验收；当前Docker internal bridge不发布控制端口，作者改为核验owned private IP与nonce的隔离控制通道后重跑。B03/D04继续实施，不提前D05。

- B03c实际本地提交：`d6e92c8`。与ee8ddb7一并等待GitHub安全认证恢复后补推；作者继续HTTP真实网络及入口。

B03 SMTP端口细化已由root确认并同步实施规格修订4：可信D07 adapter在AUTH凭据及每封邮件前BeginSend，重新DNS/固定peer/首写门禁；初始TLS协商不计邮件sent，失败不沿用旧attempt，串行发送及并发边界必须明确。D04不实现SMTP协议、不套用HTTP allow_http。HTTP真实首批4.138s通过，barrier组合仍由作者验证；不是完整B03验收。
