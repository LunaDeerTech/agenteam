# D10 Secret Variable Owner Service 当前检查点

- 树 `/workspace/agenteam-secret-variable-owner-service`，分支 `ai/secret-variable-owner-service`，正式基线 `ce65714aac6eb4995a43fc427a2c77e6497470a7`。root负责Git、迁移协调和实际资源窗口；本线不执行Git写操作。
- 完整目标见[本库工作项](../docs/development/work-items/d10-secret-variable-owner-service.md)：真实Human Owner的Secret Commands/Queries、D04专用authority、单final Tx中的两域事实/安全历史恢复、共享目录兼容及库调用退出。app/HTTP/defaultroot/UI和真实Agent F1/材料使用不在本轮。
- eb0731实际只读核31承接路径逐字等于冻结D04 b724e397：16 production、9 pure tests、00029、D04/D10两卡，加26–28三测试依赖。新树Project/Knowledge/app/Outbox相对正式ce657无差异。没有把旧D04树的Project/B02整体覆盖过来。
- D04的core17节点、maintenance单top和恢复四top/十格已在原树完整真实PASS；本树只承接已验子能力，不宣称重新执行或新main组合通过。D04生产/00029及26–28保持只读，缺口报告原域。正式main迁移仅至25，26–29为测试装配，不能据连续文件推断正式交付。
- D10 rev2顺序修订870b5030已获Variables有限独审b17cc0dd：真实D10 Audit返回AuditID后才completed，Outbox仍查完整事实。A合同及e940 Audit读兼容在正式基线，现存端口声明不等于真实Owner已实现。
- 00030由root预留本线唯一writer；先写本域SQL草案和库源，正式迁移/真实PG待有序依赖与fresh grant。不改前序SQL，不写占位迁移，不绕Migrator。
- 唯一共享例外：`project/audit_facts.go`、`project/projectvariable_event_authority.go`的exact Secret新分支与新增helper/test；原ordinary/Knowledge/Object/lifecycle分支保留。Outbox engine、app、HTTP和默认装配禁止扩域。
- 已复用并核AGENTS、团队流程、D10 rev2/D04卡、Go/database/security/design/test-engineering/verification技能。当前仅安全记录首片段与SQL草案，下节纯控制不代表authority/事务库完成；无自有真实资源，本线不PG/socket/network、不大编译。
- 缓存沿既有独占 `/workspace/agenteam/output/ai/model-ui-recovery/go-build`，mod只读 `/workspace/agenteam/output/ai/model-ui-recovery/go-mod`；未来小pure使用本树private TMP、Go1.27.1/local/offline/-mod=readonly/-p1，不建GBcache，运行前核本线没有冲突编译。
- 31承接＋2docs已root保存77de3469，后续从schema/真实authority与安全repository分片实现；每个可恢复片段约5分钟内交checkpoint，未编译WIP如实记录。原D04与Model交付树全部冻结。

## 安全持久记录首片段与00030草案

- 新`projectvariable/secret_records.go`只定义无材料/无裸semantic摘要/无Session的内部completed记录、闭集结果与两域版本/原command/presence对应验证，显式逐字段比较D04安全observation；不用其固定JSON标记判断相等。Lookup适配保当前完整Actor/Session，历史identity只按稳定User匹配；它不是权限provider或实际D04观察。
- 新`secret_records_test.go`三top覆盖五effect/presence、跨Project/target/User/key/未观察及结果时间拒绝，schema合法但不相等的opaque observation，换Session绑定与固定fmt/JSON/slog输出。a81bbe同进程fresh5828571136后，Go1.27.1原cache/offline/-p1定向race原session37317→3c0795 actual0（1.019s）；没有TestMain/PG/socket。目标为`^TestSecretOwner(StoredRecordsFiveEffectsAndCrossBindings|ObservationEqualityUsesActualFields|RecordLookupSessionSeparationAndSafeFormatting)$`。
- `.agent-state/secret-variable-owner-service/owner-storage.draft.sql`为00030必要草案，未置正式migration/未PG：共享type互斥payload、completed-only安全command、Secret history/generation/本域引用记录及exact新Audit guards。history→command FK延迟同Txcommit检查，使真实Audit返回ID后才complete；保旧跨type名称唯一及Project/ordinary/其它Audit predicates，不改前序SQL。
- 首5paths已root保存fc162052；1df33e限定gofmt/diffcheck0，原实际工具终态。Owner服务/事实路由/SQL与完整验收仍未完成，不外推本纯片段。

## 安全repository与D04 authority接缝

- 新`secret_repository.go`实现Secret目录/安全completed扫描与原observation重建、completed插入和独立generation读取；严格区分Variable与Credential版本、回执Event/Audit列和安全DTO，未知/损坏记录拒绝。没有直接读取D04表或材料。
- 新`secret_write_authority.go`不可变绑定同Store与真实Project端口；Discover在原command/User/Project锁与当前Owner Read后才看历史，新写再检查Mutate与当前前像。CheckPlan仅私有issuer/完整原Actor(Session)/request纯验真；CheckInTx不另开Tx/补锁，完整锁和当前门先行，历史ReceiptRead不借写权限，NewWrite不接受已有completed。合法新create候选不同导致历史锁变化时只发私有reprepare标记，后续Owner不得用于Unknown重试。
- 新`secret_authority_test.go`三top定向race：repository损坏列、私有/foreign计划与换Session、原锁/当前权限先于SQL、历史路径与发现Unknown。控制Store/Project/Rows明确为替身，不证明真实Session/SQL。首d859a3因测试API/不可比LockKey编译失败；修后57d02b与9d2dba分别发现测试非法key刺激和列下标错误，已修测试。最终711af2同进程fresh5650407424，原session49584→5e447a actual0（1.079s）；无活命令/实际资源。
- 本阶段3新Go＋本文已root保存54c02308。后续canonical/history/finalTx与真实facts/SQL仍待实现验收；D04/29/前缀和app/HTTP均未改。

## Owner调用、变更计划与查询首版

- 新`secret_service.go`固定注入同Store facts/write authority、同一Project authority、D04/Audit/Outbox/Activity/cursor；本库Stop仅取消自己的原调用与confirmation，Drain等实际done，不关闭共享依赖。新`secret_plan.go`从A材料callback产生独立D04 Intent，不经明文string/JSON/digest；safe metadata计划区别显式value覆盖、metadata-only/no-op、Variable与Credential版本，计划自身固定安全输出。
- 新`secret_plan_test.go`三top（语义含5子格）58156d fresh5598535680→原42864→a77c71 actual0/race1.021s：显式value/metadata/no-op/max版本、caller材料和Intent销毁隔离、原Session、Stop取消不冒Drain完成。构造/计划片段不代表Commands已实现。
- 新`secret_reader.go`首版Get/List/Lookup：类型隔离、当前Owner先于SQL、独立cursor/generation；无历史Lookup仅当前Read及原command锁，不要求Mutate/当前target；有历史再同原锁D04 Lookup并逐字段核真实observation与D10完整receipt。identity-only Lookup不代写重放的原意图Match。
- `secret_reader_test.go`三个定向控制已写，尚未编译：e9d9e7同进程fresh5336195072低于5GiB，实际exit1停在预飞，没有启动Go。已限定gofmt/diffcheck，无资源。本阶段5新Go＋本文6paths已在ce713687保存；实际Rows与SQL仍待验。旧D04/Model产物冻结，磁盘回收仅向root提供可替代旧candidate的精确只读信息，未自行删除。

## 单final Tx源码首版（已编译，业务未验）

- 新`secret_commands.go`实现A三写入口的原Intent准备、短只读发现/Match、无durable planned的Outbox准备、完整union锁及一次final Tx；真实D04 Apply→D10后像/history→私有Audit见证→实际AuditID→completed→Outbox/Activity。replay/no-op分支不发新D10 Audit/Event/Activity；原Unknown保留一次3s确认且复用原prepared Match，不能用identity-only Lookup认领他方异语义完成，Stop仍跟踪确认实际返回。private reprepare仅NotCommitted且最多一次。
- 新`secret_mutation.go`负责共享名称/Secret容量/本域引用/独立generation检查、精确D04 preparation与实际observation各字段/effect/两域版本、同Tx canonical/history写及后像重读；新`secret_facts.go`区分prepare discovery与sameStore/liveTx mutation见证，Outbox NewFact必须完整completed及真实Audit返回ID。事实实现尚未接旧Authority/Project两个闭集路由，不能称可用成品。
- 自查将`secret_write_authority.go`创建ID预检从全局EXISTS改为仅取所属Project：本Project重复Busy，跨ProjectNotFound，沿普通API隐私行为；`secret_authority_test.go`补对应闭集负控。两源较54c的小差异尚未重跑。
- root精确回收五个有完整保留备份的旧Model重复candidate后恢复空间；53c304同进程fresh5742374912→原session11787→1d767a actual0/race1.028s：先前未跑读层三个top＋受跨Project ID修正影响的authority一个top，共4top。新commands/facts/mutation也参与实际编译无错，但其写事务业务控制与真实SQL未运行，不能把读层通过外推写功能。
- 本轮5技术路径＋本文6paths已root保存87838e2f。限定gofmt/diffcheck0，全部自有工具终态；无PG/socket/network，D04/29/前缀/app/HTTP不变。

## 精确事实路由与共享type兼容

- 本域`authority.go`只新增exact Secret Event/Audit三处分发；Project `audit_facts.go`、`projectvariable_event_authority.go`只新增精确Secret路由至新`secret_variable_facts.go`。实际逆移除新增分发后，三个旧文件全文逐字等于正式ce65714a；未扩ordinary predicate，Knowledge/Object/lifecycle旧分支保留。
- ordinary repository Get/读列表/容量/UPDATE四处SQL增`type='variable'`；Secret独立4096容量和generation，跨两type名称唯一检查保持。包说明同步实际职责；完整SQL未执行。
- 经root明确授权，旧`audit_facts_test.go`只更新只读阶段的Secret无事实负控：不再用always-allow替身证明“无分发”，改实际D10 Authority拒缺sameTx私有proof，且当前Session/Archived门先行；普通与其它action测试未改。新增Project测试核两Outbox stage、生命周期/Session/foreignTx、错闭集event、ordinary依赖隔离。
- 2e051d fresh5708410880→原90611→8e3a2a actual1：projectvariable定向SecretOwner/Variable组已race1.128s通过，但Project新测试误写三返回值为二导致编译失败。只修该测试赋值后a3903a fresh5622099968→原8262→e07f9c actual0/race1.032s，四个精确Project新旧top通过。没有PG/实际Session/真实Rows或Owner写事务验收；私有写事实/Unknown仍待纯负控和真实组合。
- 本轮9技术路径＋current/库卡共11paths冻结供root checkpoint与独审；全部工具实际终态，原D04/前缀/Outbox engine/app/HTTP未写。
