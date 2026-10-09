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
- 当前3新Go＋本文4paths冻结供root checkpoint。后续Owner服务、canonical/history/finalTx与真实facts/SQL仍待实现验收；D04/29/前缀和app/HTTP均未改。
