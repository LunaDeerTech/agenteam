# Project Owner Audit HTTP：独立验收准备 01

当前仅准备计划和已接受输入交接，不是产品 STATIC／动态 PASS。作者 `usage_verification` 独占卡 #1–14，审查者 `next_frontier` 不修改产品；#15–16 文档末件及真实资源待 root 另授。本阶段不读作者活动源码、不运行 Go／测试／资源，不再委派。

## 已核的新基线

产品 `cc850b2244cad771eb862a2c82d99887d5da7284` 的配置写入15路径逐件与独立 final15 的接受 manifest SHA256 相同，见 `accepted-handoff.json`。不用旧 e4 根覆盖已接受配置实现。

正式 Audit 卡在 cc850b22 的 §1–8 与已独审正式 rev2 字节相同，归档只增行政来源。规格页首的历史“未授权”不替代本次 root 对作者 #1–14 的明确授权；独立执行尚未获授。

- account.go：`89d01abf651ca247515ae8226c739469b86ed01ac6cb4a717cdd3091633e79e7`，配置写入未改该文件；现唯一写权由 root 交作者。
- project_models.go：`1efb3f0c95f05639db8f3da96cabb036d2a266859f425b418a031bebef57f5d0`，已经组合原 GET/HEAD 与新配置写入／查证，新增 Audit dispatch 不得盖掉这条真实链。
- security.go 仍 `0804ec5ab4c3b8566d3f933881415779bf354eaa89a1bb85e0fb4fb09bd0a7a4`；唯一 auditor／ProjectAuthority 构造与安全依赖顺序不改。
- 29 个 Audit／Project gate／foundation／identity／cursor／HTTP boundary 核心来源 Git blob 与规格基线同，可复用原固定审查快照；新基线仅保存8个必要来源，不复制全树。
- 原来标“未来”的 `TestProjectModelConfigurationHTTPComposition` 与 `TestModelProjectConfigurationWriteHTTPDefaultRoot` 已在固定新源码找到；目前只声明存在，不冒 `go -list` 或运行通过。
- 作者 `base01.json` SHA `7e80236c7e79b461b6ac2dbad5edb6806038b7689dd7e56e3dc8252eeee39e58` 的 commit、card／account／readonly roots 与本独立固定输入一致。不是实际执行闭包。

## 完整候选静态审重点

1. 精确14范围，未改原 query/service/SystemHTTP、公共 contract、common、共享 helper、迁移或根停止语义。允许作者先冻 query 单源再审，但不将子片 PASS 当整卡完成；最终14及传递输入全部固定后完整复核。
2. **User SH → Project SH**，同 Tx 当前 Session → Owner／Read gate → SQL；不是配置命令的 Command EX。查 current grant binding、真实 Store.InTx、授权先于 cursor／存在性观察；initialized active/archiving/archived 允许读，未初始化/deleting 拒绝，管理员非 Owner 仍 NotFound。前后 ctx、callback 完整、Rows.Close→Err、外部 nil／损坏返回都零候选。
3. 独立对照原 contract 枚举53合法 action filter／31 Project 输出／13 Service／14 Resource；不把 System-only 合法 filter 判400，也不准 System-only 输出。Actor三个分支、action metadata／resource／outcome／关联条件逐项交叉；Human wire无Session，Agent execution一致，Service cause安全闭集。严格缺省/null/unknown、原MIME及版本／年份边界；不凭 SafeRecord err=nil 就信任完整投影。
4. 全页（包括第201哨兵）、严格时间+ID降序／无重复／scope与每个filter绑定，第202异常行、Close后Err不得被前200成功掩盖。cursor保原签名/规范filter/scope/order绑定，改变limit合法；零items应[]，末页cursor null。
5. Unknown 保存真实原 CommitResult State/Cause/AttemptID；新 accessor 穿 errors.As 且只认新wrapper。无重试、无确认事务、无command receipt，零候选；wire仍原Problem无cause_id/attempt。NotCommitted及晚Committed取消分别按卡，不重写成错误的事务结局。
6. 3s发布/I/O预算含更早parent，实际尾部可跨deadline且同步join。能力预检在鉴权/业务前、无提前Flush；bounded Unwrap／每setter独立safe call／Close一次／AfterFunc终局／reset失败／panic；业务writer透明stateOf。HEAD仍完整查询、编码、实际Flush且错误也无实体。检查ctx race后不继续发布、不二写Problem、不将cancel等于组件已join。
7. 根只有同auditor＋原boundary＋一次middleware的精确dispatch；保留新配置读写、凭据、Owner read/update、Usage、System Audit/Summary等原链。readyz按原Problem503，不强加Account安全头；root正常/forced内层join与fixture自有proxy/backend退役分开记账。

## 有界独立动态代表（待作者冻结与 root 执行授权）

独立不镜像作者整套矩阵。先审作者固定原件及实际覆盖，再按以下风险组合私有探针；只在自有 scratch 使用最小 overlay，不写产品或旧 helper。

**受控窄组**：预计各一个 facade／HTTP 私有top，覆盖作者未同构组合：合法前200＋损坏201／额外202／最终Rows.Err，filter与scope错配；System-only filter合法与结果拒绝；Agent metadata/association互相矛盾；HEAD完整编码/零body；能力不同层、setter/Close panic和callback已运行的实际wait、tracked状态防二写。31合法工厂极值／200条实际编码原件主要由作者承担，独立核固定Go输出字节数/反解/1MiB无截断，不把静态1,037,420B算术当Go结果。必要补最大转义代表，避免再造无意义的全矩阵。

**真实 A（当前权限／分页）**：拟一top `TestIndependentProjectAuditOwnerAndPage`。正式身份与Project API沿已接受fixture，分别核Logout已提交先拒读、读取持有User SH使Logout真实排队；Project EX holder仅作对手证明等待，读取自身仍必须SH。同一事务中核Owner／归档Read，非Owner含admin隐匿；跨Project/改变filter cursor拒绝，合法改变limit续页无重复，坏隐藏哨兵零页。共享锁可共存代表用于区分错误EX或漏锁。注册无条件cancel/release/actual-done cleanup后才断言。

**真实 B（原终局／默认根）**：拟一top `TestIndependentProjectAuditTerminalAndRoot`。目标绑定本read RecoveryCause owner、Project、callback唯一backend及两SH/真实查询，歧义即FAIL；私有Store仅委派原真实WithinTx并记录其原返回供比对，不装饰Committed成Unknown。PG proxy观察同backend `C(COMMIT)`＋`Z(I)` 后丢客户端ACK；公开facade Unknown accessor须等于原result Cause/Attempt，HTTP503零候选，无cause/attempt wire。不记录原帧、SQL args或材料。默认app.Run的真实GET/HEAD原body与schema联验，并补root在途关闭互补时序；若作者已经精确承担物理终局，可按卡重组为相反权限竞争／根tail，不降低物理证据必须存在的门槛。

计划名是私有候选，当前未编写／编译／执行。最终按120s每top含Cleanup、6m包预算评估；不足必须在执行前重组窄轮，不能失败后加时或削断言。normal/forced root结果与所有自有资源终局分别记录，不回填innerjoin。

## 实际图、schema 与资源门禁

- 作者完整 normal/race GoFiles/CgoFiles/embed/TestMain、完整20 fixture、动态两cmd／三个helper及CGO0图冻结后，独立只补私有测试与工具delta。运行时 os.ReadFile/schema读取不能靠Go overlay假装隔离；不复制全树、不全app run。
- 标准Draft202012＋FormatChecker；按卡修正年0000日历checker而非收窄合法范围。冻结 common＋project-audit 本地refs、Python实际模块及20官方schema数据。读取原HTTP bytes不重编码，sidecar绑定method/path/status/真实Content-Type/Length/request-id、Project/AuditID、run/input/schema/body SHA；HEAD空body单验。
- 旧配置root selector实际调用 `AGENTEAM_PROJECT_MODEL_CONFIGURATION_SCHEMA_PYTHON`；凭据旧helper用 `AGENTEAM_PROJECT_CREDENTIAL_SCHEMA_PYTHON`；卡原Usage还需 `AGENTEAM_USAGE_SCHEMA_PYTHON`。执行图按实际选中helper设置，不遗漏已接受旧schema环境。
- 45s离线单命令／编译运行分离／真实subreaper direct+adoptedwait与两次owned空。当前仅准备，未获独立Go执行授权。
- native复核作者三个固定selector，40s binary／45s外层、15s owned退役／75s全TCP退役分记、安全10 syscall闭集无buffer；禁止仅凭受控Writer宣称socket事实。
- PG仅root独占窗：fresh5GiB、新PID/starttime/Docker基线、本地固定digest＋MinIO，不拉取；7资源fullMount/nonce两次清零、actualwait、流式120s/top含Cleanup watchdog／6m包。daemon/PID1差集另列非owned未wait，不沿用旧环境或旧计数。任何FAIL先清尾停后继，保首红／有效输入与最小受影响复测。

## 接入节点与当前状态

等作者 `query` 或完整候选冻结后只读固定副本，具体阻断立即报root，由唯一作者返修；冻结schema／测试／有效图就绪后再申请独立执行窗口。必须保作者原失败与来源、明确STATIC与动态差别。末件两文档在完整技术验收后另授、独立窄审。

本准备包已停止写入，无活动命令或自有资源。三停止、生产Resolution/Invocations/D24未绑定、管理员统一Summary无Project override、Project创建/Skills/生命周期未绑和ready503边界保持。
