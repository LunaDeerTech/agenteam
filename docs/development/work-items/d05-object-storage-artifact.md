# D05 对象存储与 Artifact

- 修订：1；状态：B01实施中；唯一活动模块D05；基线 `main@abf5c37`，开工工作区干净。
- 前置：[D04](d04-security-foundation.md)全部独立验收和真实完整suite通过，入口本地提交abf5c37；GitHub认证失效，ee8ddb7起5个本地提交待恢复后补推，不冒称远端同步。
- 目标：按[计划D05](../development-plan.md#d05-对象存储与-artifact)实现StoredObject/引用与lease、流式MinIO读写、跨DB/对象存储的一致性和恢复、Artifact服务、受控预览/下载及短期传输授权。

## 任务与所有权

| 卡 | 依赖 | 角色 | 独占范围 | 状态 |
| --- | --- | --- | --- | --- |
| S01 工程规格 | D04完成 | architecture_worker | 新增d05-object-storage-design.md；已验收代码/契约只读 | 修订2独立静态通过，root采纳 |
| R01 依赖与隔离环境核验 | D04完成 | research_worker | 有界探针及新增d05-object-storage-research.md报告，不写实现源码 | R01/R02修订2完成并停写 |
| B01–B03 实现分块 | S01确认 | backend_worker | 按实施规格§1与本卡B01授权，串行移交 | B01实施中，B02/B03未开始 |
| V01–V03 独立验证 | 对应冻结范围 | verification_worker | 停写实现的独立副本及任务owned资源 | 未开始 |

root独占本卡、开发计划和任务台账；架构作者仅新建实施规格。角色遵循AGENTS与agenteam-design/go-development/verification/documentation技能，禁止子agent再委派和Git写操作。常态至多两个活动子任务，Docker和测试资源顺序明确移交。设计稳定且root确认后才写实现，不提前D06+。

## 已确认规则与设计任务

读取[对象存储](../../architecture/platform-infrastructure/object-storage.md)、[Artifact](../../architecture/tool-system/artifact-tools.md)、[D01资源契约](d01-contracts/resources-skills.md)、[生命周期](d01-contracts/domain-lifecycle.md)及[基础事务/幂等](d01-contracts/foundation.md)。此前已确认决定不重复询问。

StoredObject只拥有payload存储事实，Attachment/Avatar/Knowledge/Skill/MCP/Execution/Meeting/Artifact保留各自业务身份。显式owner reference和active lease必须真实保护删除；不能仅凭object_id授权。业务模块不能访问MinIO SDK/bucket/key，Agent不取得签名URL、存储凭据或无业务引用对象枚举。Artifact from source首版沿既有复制行为，D01引用保护不是擅自改为共享payload的许可。

上传先持久pending，外部stream写入/校验在DB Tx外，只有payload与metadata可信后发布available/reference；中断、payload已写但事务失败、COMMIT unknown、缺失payload、引用/删除竞争、崩溃恢复均落实为可观察领域事实，不造万能outcome表。长度/大小/digest/媒体类型、range、取消和内存边界须有明确平台上限。未引用对象清理为显式可恢复流程，不能因TTL猜测无活跃传输或业务引用。

明确短期Signed URL和TransferGrant的单对象/单操作/runner/完整性绑定、过期/重放/撤销实际可实现边界及安全投影。Runner直连部署声明的可达受保护端点，不可达明确失败，不暗中Central中转；D17绑定真实Runner通道，D07/D08绑定身份/Owner，D21绑定Artifact Tools。当前正式授权端口未绑定应拒绝，不开放匿名业务HTTP或成功stub。

复用已验收Go1.27.1/local、D03 Store/Tx/全局锁序、D04identity/Audit/cursor/Secret/安全配置与生命周期。设计须核对真实代码端口，新增Audit action/metadata等公共契约若必要，先列明确改动与所有权，不直接修改冻结D04。迁移只追加00005起全局序列，禁止改00001–00004。MinIO SDK/镜像/协议选择须固定精确版本与可验证来源；存储是可信部署通道，区分运行时受控出站规则，不引入业务任意endpoint开关。

S01需明确表与约束、Go接口/安全闭包、状态机、授权和Project gate、幂等/unknown/锁序、引用与lease、上传及远程直传完成校验、签名授权、Artifact复制/列表/读取/preview服务、清理/重启恢复、配置启动健康关闭和真实fixture验收。范围大的内部步骤按完整结果分块，核心能力不得留后续模块补齐。

## 环境与验收

Linux/amd64、Docker28.4.0、精确Go1.27.1/local、既有PG17.8/vector0.8.1正例与PG16.12拒绝fixture继续复用。MinIO版本/镜像及SDK仍待R01核验；当前未创建对象存储资源，不访问现有服务或真实凭据。

验收至少实际覆盖中断上传、外部已写DB失败/unknown、缺payload明确错误、引用/lease阻止删除、跨scope拒绝、stream不全量缓冲、签名材料安全及有效期/完整性、恢复/清理幂等与Central资源关闭。只用nonce/标签/exact ID任务owned PG/MinIO/临时目录，缺真实fixture在专用套件中失败，不能skip充当通过。

下一步S01+R01，冻结后独立静态审查并由root采纳；当前无新产品待定。D05及D06–D28/E01均未完成。


## S01/R01 进展

D05开工卡本地提交552287d。root已向S01明确两个完整性工程检查点：预签名PUT在到期前通常可重放覆盖，发布available不得复用仍可被旧grant覆盖的可变key；需要可验证的staging/不可变发布或固定版本方案，HEAD用户metadata与ETag不能伪充全文SHA。PutObject的可选expected_sha256须与D01同key不同body冲突语义兼容，不能只用MIME/length作为payload语义；临时spool/两阶段及恢复上限须明确，外部IO不持DB Tx。

R01初步来源核验：GitHub release API和Docker Hub网页被环境代理403，未获得其在线发布资料；Go module proxy可用，返回minio-go/v7 v7.3.0（2026-08-15，go1.25.0）为候选。官方Quay匿名registry可查询，镜像实际version/digest与API行为尚待核验；不把旧可用tag描述成最新版或声称安全公告已完整核验。当前未引入仓库依赖。


R01来源进一步确认：官方Quay token取得200，但repository manifest仍401；CLI对多个tag报no such manifest，不能据此断言tag不存在。Docker Hub registry匿名denied，dl.min.io binary/archive410。合法Go module proxy取得官方RELEASE.2025-10-15源码commit `9e49d5e7a648f00e26f2246f4dc28e6b07f8c84a`，README明确source-only发布及legacy二进制不再维护。root批准固定官方源码/module校验和+Go1.27.1/local构建，记录flags/实际version/binary SHA，再在既有固定基础镜像的owned隔离容器内探测；不伪称拉取到官方MinIO镜像。当前编译/运行可行性未完成，D28自建部署镜像责任须记录。

S01工程方案已初步采纳：PutObject先当前授权/限额预检，再受限owned临时磁盘流式准备并计算实际SHA，无论expected_sha256是否提供都按真实内容做命令语义；正式Tx仍重授权→幂等→gate/version。Runner PUT只签staging，平台验证同一字节流后写未签过写权限的独立immutable key并发布，不能先校验再无条件Copy可变源。

浏览器短期签名业务下载URL由Central在每次使用时重新校验Session/Owner并ReadObject流式返回，签名不代替授权，实际reader lease可跟踪；D05定义grant/验证/stream服务，HTTP真实绑定留D07/D27且未绑定不开放匿名路由。Runner依D01直连MinIO，不回退Central；raw S3 bearer不能承诺即时撤回，expiry只结束新传输资格，不证明已开始传输完成。D17可信完成/停止确认后释放lease，未知则保留pending/unknown，Project清理不伪报完成。


R01已用Go1.27.1/local编译minio-go/v7 v7.3.0临时module，运行依赖19模块（含SDK）/42外部packages；不含工具/linter依赖。源码核验PresignedPutObject不绑定额外头，需PresignHeader明确签校验/length/条件头；CopySrcOptions支持VersionID/MatchETag/时间条件，但CopyDestOptions无目标If-None-Match接口；高层multipart错误Abort复用原ctx，取消不能保证清理，须fresh有界cleanup与恢复。以上存储实际支持仍待真实probe，不只按SDK接口宣称通过。

官方固定源码已实际构建成功：`minio --version`为RELEASE.2025-10-15T17-29-55Z、commit9e49d5e7a648f00e26f2246f4dc28e6b07f8c84a、runtime Go1.27.1 linux/amd64；binary SHA `dc5298474f0bc87a068f0b1135c583bb1278c17c11c512212ed7644a238c89c8`，源码go.mod SHA `673f06144e90bc045f0a20050d2874c52e551be5bfbd70dff6e76b66da0db702`、go.sum SHA `86e062349c7abdce0465561bb409d410a95b00b0969ba05d5fb5f2e3550a5cd4`。owned internal network协议探针开始，尚未完成运行验收；非敏感源码/二进制可作为构建缓存保留，运行资源与凭据结束必须清理。


R01首轮真实协议证据：8MiB+3非seek stream写/GET/range/全文SHA通过；multipart6MiB的ETag与SHA响应都是带-2的composite，不等全文SHA。普通presign PUT只签host且重复200覆盖；PresignHeader签length+SHA+If-None-Match首传200/重放412，修改checksum或length403。Copy源ETag不符412，目标已存在仍被覆盖。首part实际200后取消留下1个incomplete upload，fresh有界RemoveIncompleteUpload后0。checksum错误响应的安全分类与HEAD无payload仍待补证，不伪报已完成。

首轮容器/网络精确删除；owned数据挂载因容器root创建导致host删除权限失败，R01正用仅挂载该nonce精确目录的隔离helper清理，后续以host UID运行。这是fixture权限清理问题，当前资源清理尚未整体完成，不冒称0残留。

S01另确认必要兼容扩展：Artifact read/list/download审计按Read intent且仍当前Session/Owner/CheckAppendInTx及归档只读gate；D04原动作语义不变，00005仅追加更新Audit CHECK白名单与对象schema。实施前列精确公共contract/authorizer/服务producer/cause所有权，不借其他服务身份或把Read动作当业务授权旁路。


R01补证：9MiB+7 DisableMultipart单PUT、预计算SHA头及If-None-Match:*实际成功，nonseek reader最大Read缓冲65536，HEAD全文SHA匹配。错误body为400 XAmzContentChecksumMismatch，HEAD NoSuchKey。SDK对seekable源8字节/声明3字节会截为前3字节成功，平台必须自行检查完整输入长度/尾随，不能依赖SDK。条件签名URL在key删除后重放再次200，因此条件写不等于一次性token；本轮资源报告已清理。

S01拟首版对象上限1GiB并显式DisableMultipart/条件创建，root认可方向。正在补最小慢条件PUT与零字节cleanup fence竞争证据，解决unknown+HEAD404不能证明旧上传结束的问题。fence删除后的迟到请求重建风险也须明确，无正向停止/完成证据则保持pending/unknown，不能把TTL或一次HEAD/fence响应当作全部传输结束。该方案尚待实测后冻结。


R01慢写竞争事实：两个owned key先实际写140000/280000字节并暂停，零字节If-None-Match fence在500ms内未完成；释放余body后旧PUT200、fence PreconditionFailed，HEAD280000且全文SHA匹配。因fence从未成功，预设删除分支未执行，不能据此推断“成功fence后删除安全”。运行资源已报告清理，清理协议须据此收束，不能用Delete间隙忽略旧写重建风险。

SDK v7.3.0默认MaxRetry10，可seek body可在网络错误后重试，nonseek仅1；首次真实提交丢响应后条件重试可返回412，412不证明未提交，应核实预期length/fullSHA。root认可生产显式MaxRetries1交领域恢复的方向。追加授权R01只新增d05-object-storage-research.md持久报告（版本/校验和/可复现构建/协议事实/限制/资源清理），停止额外实验，S01如需单项能力证明再单独安排。


R01修订1已冻结报告SHA1f14db3777fc3da3eb620c5a944a28345765c3e14d635f6a6c84d21659ac10b0，nonce3b818b801c22b067572d551d18ae5255容器/网络0，data/env/config均不存在，非敏感源码/bin缓存按授权保留。随后root仅追加必要补证并解冻研究报告至修订2：慢条件PUT与无条件零字节tombstone竞争及marker后原条件重放，另只读核验bucket versioning/ObjectLock/retention/lifecycle为空的实际接口结果。Docker顺序仍由R01独占，architecture暂不读活动研究报告。

清理工程方案方向由root采纳，完成分支仍等待补证：持久cleanup gate阻止新引用及旧上传发布后，对unknown/直传attempt的exact key以fresh bounded ctx写无条件零字节技术tombstone，成功响应且读回0字节/空SHA后确认旧payload消除。所有业务payload PUT继续If-None-Match:*；可能有迟到写或旧grant的marker首版不自动删除，随机key和marker无Project/Owner/名称/正文，不保业务内容。确定无外部写暴露且全部写有终局响应的key才允许实际DELETE。正式transfer lease未收敛时仍pending，不能因marker或expiry伪报Project完成。

专用bucket须versioning disabled（非suspended）、无ObjectLock/retention、无自动删除marker的lifecycle；启动查询失败不默认安全，不静默修改既有bucket设置。存储与凭据由平台独占部署管理，外部管理员篡改不属于正常业务操作。若真实零替换不能提供所需屏障，S01必须报告并调整，不把永久unknown当已完成核心恢复。


R02真实补证exit0：旧条件PUT写140000/280000字节暂停时，无条件zero marker等待；释放body后旧PUT先200，marker随后成功。HEAD与完整GET均为0字节/空SHA；原条件签名URL与SDK重放均412，重放后仍为空。技术marker不删除。nonce1e67a8f347f9b7a26a4d9c14f6db2ad4容器/网络/data/凭据已清；完整研究报告修订2待作者冻结。root据此采纳上述受前提约束的清理方案，业务lease结束仍需可信终局证据。

只读bucket核验实际为versioning200且Status空/Enabled=false、ObjectLock404 ObjectLockConfigurationNotFoundError、lifecycle404 NoSuchLifecycleConfiguration；未启ObjectLock时单对象retention为400 InvalidRequest，不能把任意400解释为安全禁用。S01须按这些精确响应定义启动检查和错误分类。


R01/R02最终研究修订2冻结SHA `b4a0462d255910443cbed31abbaeee6d17c133b74a2d997357e37d938242f038`；root核对完整报告、R02日志/探针指纹及原始协议结果，2文档8链接检查通过。S01实施规格修订1冻结SHA `afca3f1002dd3aecb31f7fa8cb580aa8b58d7c1a09d388d0c647e62d21dc7c02`，17链接/格式通过；作者停止写入，V01独立静态审查开始。尚未确认实施规格或修改生产代码，Docker当前空闲。


S01修订1独立静态审查暂不通过，4项规格澄清：prospective owner上传reservation在available但未Attach时的消费/显式撤销闭环；source型completed重放不依赖旧源仍存在而须当前目标结果可见与输入同义；Stat明确metadata-only而非reader lease；全文SHA流末尾失败采用有界末段holdback并区分已发送阶段。root全部采纳，验证者停读后仅解冻设计给原作者修订2；另补既有30s启动/共享额外1s force与健康陈旧规则。输入25项manifest SHA `e83d9513b09b6e7a69bd9e78b938216ddca539f7c6a59c1a57c324297ca3eb9a`，未运行产品/容器测试、无仓库实现改动。研究/进展本地提交08accd3，认证恢复后共7提交待推。


## B01 开工与验收基线

S01修订2独立静态门槛通过，4项问题全部闭环，root复核采纳。设计SHA `4b3241bb37246386ad56bd5f613b2c63aaae673e7d2fea2afd7f806793e65a30`，25输入manifest SHA `3a1ffc916c1a81cbfd5f4c9e36fd029e52b552e710daa426049d43bb49bc3f65`，其余24输入未变。只做静态规格验证，无产品测试；不能以此声称D05实现完成。

backend_worker独占B01：新增internal/central/object/及contract、00005_object_storage.sql、tests/objects对象场景、tests/testsupport/objectstore、scripts/test-objects.sh；依规格§1最小扩展identity/Audit/foundation/HTTP问题schema及相关测试；go.mod/go.sum仅固定已核验SDK及必要MVS依赖，记录旧依赖变化。可同步后端README的B01实际能力/命令；不提前实现Artifact、浏览器下载、Runner transfer或Central必填MinIO配置（B02/B03）。其他D03/D04生产代码冻结，新增共享改动先报告root授予所有权。root独占本卡/计划/台账，设计与研究保持验收冻结。

作者负责完整B01自测与真实owned PG/MinIO异常、并发、COMMIT unknown、reference/lease、reservation撤销、marker恢复及流式完整性证据；Docker现交B01作者独占。缓存可按研究指纹复用，凭据新生成且清理资源；无现有基础设施访问。先形成可独立验证的稳定子范围后停写该范围并交V01，独立审查期间不改审查输入。整体B01通过之前不进入B02。具体接口/边界/验收以实施规格修订2为准，不复制另一套规则。

- S01设计/开工实际本地提交：`c897653`，backend_worker已接管B01。GitHub认证未恢复，ee8ddb7起共8本地提交待补推；远端仍1898748。

B01作者初始切分获root认可：先完成object正式contract与identity/Audit/错误schema纯兼容，局部行为/race/schema通过后冻结给V；00005 Audit约束真实兼容仍须随PG验证，不提前声称完整验收。spool/MinIO/持久状态机共同实现完整恢复，不把正常路径当模块交付；后续若需调整冻结公共接口须解冻并重验。

B01a公共兼容13文件已冻结给独立V，manifest206054298969ef5c8f7a5bf1125cf9662eea20cfaec9048a1410cc3b62da9431；41未变依赖manifest608cc49dc7e873b0fc74f7cb72eb73da9ff0be44e43fb2179bf5b6a056c74db9。作者普通test/race/vet的identity/foundation/Audit/httpapi均exit0（race1.026–1.108s），00005真实兼容未验。V稳定副本/tmp/agenteam-d05-public-verify-f8x4ez8z已双重SHA核对54文件，作者只继续object活动范围并获准引入固定SDK；审查13禁写。

V01a公共兼容暂不通过：Transfer/Artifact审计phase与outcome矛盾8例被接受；MIME的name/filename进入Audit JSON共4例；Problem.status schema缺502/416，真实WriteProblem响应被JSON Schema拒绝。15授权场景与服务禁浏览race1.059s、HTTP/Foundation/Identity安全投影race1.122/1.034/1.016s、旧Audit回归race1.029s及vet/格式通过；不能掩盖3阻塞。证据manifest SHA a5d97535d5130f22dfc61585c7cae064bc72f3e3899aea3c677782283bb21c0f。V已停读且无owned命令/Docker，root仅解冻metadata/types/schema及相关测试交原作者返修，其余公共范围保持；原probe须闭环，不改弱断言。

B01a公共13返修独立通过：原3probe字节/断言未变，phase/outcome 8反例拒绝、MIME名称投影4反例闭环，真实503/502/416全部JSON Schema通过。完整相关race Audit1.026/contract1.037/HTTP1.103/Foundation1.037/Identity1.018s，vet/格式通过；原依赖无版本漂移。最终public manifest2b152004f06fc9d59248dbe40564835141f28d36d6b9920ae1295888f144f56a，依赖f6eb740c71164d32ab5ea8c7568d27d978be172914c17bc5ec6327cbe189298a，独立证据7ceb5be5d84717aa878233af846c46c28c8ba38cab02c79864935196f1a88c4f。V停读/命令0，root代码/schema/测试审查通过并精确提交公共13；活动object与SDK锁文件不纳入。00005真实DB兼容未验，B01仍实施中。
