# D04 Secret Variable Storage 当前检查点

- 树：`/workspace/agenteam-secret-variable-storage`，分支 `ai/secret-variable-storage`，正式 main 基线 `8cb0a95338dc417ff34be06c00086fbbc7159efa`；root 独占 Git/迁移编号/实际资源授权。
- 任务：落实已接受 D10 rev2 的 D04 专用 producer，保持旧 kind1/2、Model/Account/消费者边界。新 [工作项](../docs/development/work-items/d04-secret-variable-storage.md) rev1 形成 SPEC、端口、归属与 exact SQL/index needs，尚待独审收口。已新增无依赖 typed Request/Intent 纯合同及控制；Service/SQL/provider 仍未实现，未占迁移号；28 reserved Skills。
- 已读 AGENTS、团队 README、D10 卡、实际 Secret/ProjectVariable/迁移源码及 Go、database、security、verification、test-engineering、design skills。D10 A 的正式契约不等于专用 authority/provider 或 Owner 实现。
- 与 Knowledge 核对：现 store-only 事实 Authority 不反持 Project/Secret；独立专用 authority 在 Project 后、Secret Service 前构造。Knowledge 的新 Audit 树只做严格合同/读端兼容，不写 D04/private witness/Owner facts。
- 端口开放点：专用 prepared capability 的 contract 载体需要与 producer 接口一起固定；不以 any/公开 unwrap/摘要 getter 或 late setter 先行接线。Intent/request 校验及纯 kind3 编码可以先推进，旧 Service/SQL 接线待 SPEC/端口独审。
- 缓存：复用 `/workspace/agenteam/output/ai/model-ui-recovery/go-build` 的既有独占编译 cache；只读 `/workspace/agenteam/output/ai/model-ui-recovery/go-mod`。本树不另建 GB cache。实际 compile 前与 root 确认无本线 cache 在途，Go1.27.1、离线 env、独有 GOTMPDIR；本轮仅复用该 cache 执行下面两 contract 包的小范围离线 race，已结束。
- 独立 Model AuditAuthority 原 session62616 已实际 outer exit0（ee71a4），完整原尾齐并已释放唯一真实窗口；D04 未占 PG/browser/socket/网络。本树 SPEC/current 是新 recoverable 两路径，未执行动态产品验证。

## 无依赖纯合同片段

- 新增 `internal/central/secret/contract/project_variable.go` 与 `project_variable_test.go`：原 Human/Project/Variable/完整 D10 CommandIdentity/expected 的不可变无材料请求，create/update/delete 字段 presence，独立 owned SecretMaterial、metadata copy、safe fmt/JSON/slog 与拒反序列化。使用实际 D10 A identity factory/create material contract 作兼容控制；不提供 authority/basis/producer，不扩旧 Purpose.Valid。
- Go1.27.1 原离线 env、既有独占 GOCACHE/只读 GoMod、本树独有 GOTMPDIR，`go test -race -count=1 ./internal/central/secret/contract ./internal/central/projectvariable/contract`：session57406 → chunk477472 actual exit0，两包1.029s/1.189s。gofmt27414c、diffcheck415a62实际0；没有真实 PG/browser/socket/网络。
- Runner SPEC 初审提出 prepared contract 载体/producer signature、无 name 输入不能声称 authority 校验新名称、正式表名对齐 D10 `agenteam_secret.project_variable_receipts`。作者接受：name syntax/shared space 归同 final Tx 的 D10 Owner，专用 authority 只验其有输入的当前权限/前像；prepared 拟采用安全非空 interface，由真实 Secret 私有 concrete pointer＋同 Service issuer 严格验真，不导出 any/unwrap/digest。该修订仍待独审收口，卡暂保 rev1 冻结，不把方案算实现。
- 本轮四路径统一 freeze：本文、工作项卡、上述两 Go 源。root checkpoint 后再改必要 SPEC；现有 Service/SQL、旧 kind1/2、Model 验收输入与产物全部未改。

## SPEC rev2 接口闭合待复核

- root已保存四路径92aca721；两Go纯源保持原freeze。本文与卡只修Runner指出的三处SPEC缺口：prepared contract非空接口/四producer与两authority签名及生命周期；新name规则归同final Tx的D10 Owner；表名对齐正式agenteam_secret.project_variable_receipts。
- Prepared必须先精确私有concrete/typed-nil/同Service issuer/共用Destroy状态检查，再调用自有方法；拒外部实现/包装器/跨Service，安全Preparation给原Request/typed receiptID/ref，复制完整locks供D10 Outbox准备；无any/unwrap/digest/回调能力，不误销毁调用者材料。最小锁明确command EX/User EX/Project EX/write-key SH/CredentialRef EX，不增开Tx或补锁。
- rev2仅接口方案修订，待Runner有限delta复核；Service/SQL/provider均仍未实现，无新增动态控制，不重跑原纯组合。卡与本文重新freeze供review/root保存。
