# D08 B03 恢复首块：生命周期参与者注册与恢复解析

修订：rev1。状态：主线程已采纳首块范围，API 与验收规则待独立静审后下发实现。工作项：D08 / B03-R1。执行角色为 `backend_worker`，独立验收为未参与实现的 `verification_worker`，均按团队规则使用 `gpt-6-astra / max`，不得再委派。

## 基线与恢复边界

恢复基线为 `main=origin/main=8872110099c84cf0600bb5b62cdcd6c0c6c843e3`；本卡落笔前工作区干净。中断前未提交的 A/P 主体、Object fact checker 及相关 `/tmp` 输入和日志已经丢失。旧台账中的局部运行记录保留历史事实，不能作为重建源码的验收证据。

已提交并保留的直接基础为 Project B01 `199554b`、B02 `6319d03`、B03 C0 `16595ad`。实际代码中 `contract.RequiredManifest` 已实现深复制、规范排序、必要四参与者、清理依赖图、Outbox/Audit 顺序约束及摘要；真实 adapter registry 尚不存在。D05 `ObjectProjectStop` 契约在树上，Object/Artifact 的 `RequestProjectStop`、`InspectProjectStop` 和 `project_work` 运行实现仍不存在。

全局迁移实际连续为 `00001`–`00015`；`00014_object_artifact_project_stop.sql` 已由 `30f5c29` 验收提交，`00015_model_configuration.sql` 已随 D09 System 块提交。本卡不新增、修改或分配迁移号，不把四张技术表的存在视为停止功能已经实现。

## 来源与授权

首次必读 [AGENTS.md](../../../AGENTS.md)、[团队流程](../agent-team/README.md)、[Go 开发技能](../../../.agents/skills/agenteam-go-development/SKILL.md)；验收者另读[验证技能](../../../.agents/skills/agenteam-verification/SKILL.md)。正式行为沿用 [D08 设计 §8](d08-project-owner-design.md#8-生命周期编排)、[§9](d08-project-owner-design.md#9-既有依赖的真实适配与必需窄补)、[§12 T08/T16](d08-project-owner-design.md#12-验收矩阵)，以及 [D01 生命周期端口](d01-contracts/domain-lifecycle.md#project-生命周期端口)和 [Project 架构](../../architecture/project-work-management/README.md)。主卡的完整 B03 范围见 [D08 工作项](d08-project-owner.md#b03-生命周期与现有领域集成)。

本块交付一个供后续真实编排直接消费的、不可变的 adapter registry：新操作取得当前 required manifest；恢复只按已经持久化的原 manifest 解析原版本 adapter；得到确定性停止顺序及满足原清理依赖的顺序。注册与排序不调用停止/清理、不授予权限、不返回完成事实。

唯一实现者仅获新增两个文件的写权：

- `internal/central/project/participants.go`
- `internal/central/project/participants_test.go`

只读依赖为当前 Project 包及 `project/contract`、foundation、identity/event 契约和 Go module 文件；使用已有 `nilPort`、`fault` 等包内 helper。不得修改既有 Service、Authority、commands、共享 contract、D05/Secret/Audit/Outbox、迁移、脚本、fixture、依赖或共享文档。本卡由设计负责人独占，台账由主线程另行维护。无需 Docker、PG、MinIO、端口或后台进程。

## 冻结的包内装配 API

以下类型与方法位于 `project` 包，`c` 指既有 `project/contract`；这不是新增跨领域协议。

```go
type LifecycleParticipantBinding struct {
    Registration c.ParticipantRegistration
    Participant  c.ProjectLifecycleParticipant
}
type LifecycleRegistry struct { /* private immutable state */ }
type LifecyclePlan struct { /* private immutable state */ }

func NewLifecycleRegistry(
    current c.RequiredManifest,
    bindings []LifecycleParticipantBinding,
) (*LifecycleRegistry, error)
func (*LifecycleRegistry) Manifest() (c.RequiredManifest, error)
func (*LifecycleRegistry) Resolve(c.RequiredManifest) (LifecyclePlan, error)
func (LifecyclePlan) Manifest() (c.RequiredManifest, error)
func (LifecyclePlan) StopOrder() ([]c.ParticipantName, error)
func (LifecyclePlan) CleanupOrder() ([]c.ParticipantName, error)
func (LifecyclePlan) Participant(c.ParticipantName) (c.ProjectLifecycleParticipant, error)
```

1. `RequiredManifest` 是唯一完整清单验证、规范化及摘要实现。本块消费其 `Entries`、`Require`、`Digest`，不复制必要参与者集合、图合法性或摘要算法。零 manifest 不得变成空成功。绑定 Registration 仅做构造所需的字段有效性、集合去重校验和复制/规范比较，不建立第二个 manifest 验证器。
2. 构造时复制 bindings、每个 Registration 及其 `ReferenceKinds`、`CleanupAfter`；拒绝接口 nil 和 typed nil adapter，且在调用其 `Name()` 前检查。Name 必须等于 Registration.Name。所有版本一律按 `(Name, ContractVersion)` 唯一；同名不同版本允许，同一 tuple 即使字节相同也作为重复配置拒绝。已注册 adapter 是可信服务实例，其 Name 必须稳定；registry 不复制实例内部状态，也不增加运行时注册/替换接口。
3. 每个绑定的 Name、ContractVersion、OwnerModule 及集合元素必须合法；集合内重复项拒绝，不静默去重。构造必须成功解析 current 的每个 entry，缺少任何当前 required adapter 即失败。额外绑定仅用于原版本恢复，不自动进入 current manifest。启用 D10 初始化时把 Skills 纳入 current required manifest 仍由 B03-P/组合根负责；若 manifest 已含 Skills，其绑定不可缺少，本块不推测尚未传入的模块配置。
4. `Resolve(frozen)` 只枚举 frozen 的 entries，按精确 `(Name, ContractVersion)` 取 adapter；还须逐项核对 OwnerModule、ReferenceKinds 集合、CleanupAfter 集合。集合顺序无关，但成员、依赖及版本不能缩减、扩展或替换。合法原 manifest 缺版本返回 `DEPENDENCY_UNBOUND`，不得取最近/最新版本或回退当前集合。同 tuple 元数据不相容返回 `DEPENDENCY_UNAVAILABLE`，不输出部分计划。metadata 改变须提供匹配原记录的兼容绑定或经另项升级规格处理。
5. `LifecyclePlan.Manifest()` 保留传入 frozen 的全部规范元数据和原 digest；不能合并当前新增参与者，也不能改写原清理边。StopOrder 按 ParticipantName 升序；CleanupOrder 使用原 CleanupAfter 图，每一步从当前入度为零项中选名称最小者，因而输入排列不影响结果。拓扑排序用于产生执行顺序，图合法性仍由已有 RequiredManifest 保证；不能用名称排序替代依赖排序。
6. Manifest、顺序切片和注册元数据均不暴露可变内部引用；并发只读解析无需可变全局状态。`Participant(name)` 仅返回该计划中原版本的实例，计划外名称返回 `DEPENDENCY_UNBOUND`。nil/零 Registry、零 Plan 的所有方法返回 `DEPENDENCY_UNBOUND`，不返回可被误判为空完成的无错误空结果。
7. 畸形 binding、Name 不符、重复 tuple/集合返回 `INVALID_ARGUMENT`；nil/typed nil adapter 返回 `DEPENDENCY_UNBOUND`。本块不产生数据库事务，Fault execution state 使用 `not_started`。opaque registry/plan 不提供 JSON 反序列化装配入口；它们只是受信任组合根的运行时引用，不是 Session、Owner、cause、Tx 或完成证明。

后续 B03-P 必须在 Begin 的正式接受事务前解析当前 manifest，再与 gate/operation 同事务持久化原清单和 digest；恢复从受信任本域持久事实重建 RequiredManifest、核存储 digest 后调用 Resolve。缺绑定/不兼容不得改变 gate、缩清单或记录 stopped/completed。具体持久编排、当前 cause/phase 授权、adapter 调用和 checkpoint 仍在后续卡实现，本块不提前改 Service 以挂接空参与者。

## 验收与交付

风险为恢复与生命周期装配基础，须由未参与实现的实例独立审查。使用可观察调用次数的测试 adapter 仅验证注册/解析；构造、Resolve 和所有顺序/查找方法不得调用 RequestStop、InspectStop 或 Cleanup，不能据测试 adapter 宣称真实领域已绑定。

必须覆盖以下行为：

- 合法当前四参与者与包含 Skills 的扩展清单；构造时缺 required、接口 nil、typed nil、Name 不匹配、重复 tuple/元数据集合、零 Registry/Plan 均失败。
- 同名 v1/v2 同时注册：当前 v2 与旧 manifest v1 分别返回正确实例；只有 v2 时旧 v1 明确 unbound，构造/恢复均无自动升级或回退。
- 原版本的 OwnerModule、ReferenceKinds、CleanupAfter 任一漂移拒绝；集合仅换顺序可匹配。测试输入本身须先通过既有 RequiredManifest 构造，不用已被 contract 拒绝的坏图代替 registry 反例。
- 当前新增参与者不进入旧计划；原 manifest digest 不变；所有参与者各出现一次；清理顺序满足原图的全部边和 Outbox/Audit 屏障；独立节点以确定性规则排序。
- 构造后改变输入 slices、改变返回的顺序或 Manifest.Entries 不能影响 registry/原计划；并发 Resolve/读取无 race。测试证明绑定选择和元数据不可变，不声称 adapter 实例本身被深复制。
- 无 adapter 业务方法调用、无 SQL/外部 I/O/后台 goroutine；不改既有 B01/B02行为。

执行目录为仓库根；先核实际 Go 二进制为 `go1.27.1`，始终 `GOTOOLCHAIN=local`。本卡不安装/更换依赖。作者与验收者固定精确测试集合及源码输入后执行适用命令，路径按实际已核工具链替换：

```sh
GOTOOLCHAIN=local /path/to/go1.27.1/bin/go test -count=1 ./internal/central/project/...
GOTOOLCHAIN=local /path/to/go1.27.1/bin/go test -race -count=1 ./internal/central/project/...
GOTOOLCHAIN=local /path/to/go1.27.1/bin/go vet ./internal/central/project/...
git diff --check
```

作者交付两文件的固定指纹、相关编译依赖标识、实际命令/原始日志/结果和停止写入声明；独立验收基于该固定输入验证关键拒绝、版本恢复及不可变性，语义和依赖未变的证据可复用。本卡写作阶段仅完成只读现状核对与文档检查，以上 Go 检查尚未执行。

需要修改既有公共口、Service、任何第三个文件、迁移或注册产品规则时，暂停受影响部分向主线程报告；没有重新设计全局计划的授权。通过本卡只代表 registry 可用；B03-P 持久生命周期、D05 真停止/清理、Secret/Object Audit fact checker、真实 PG/MinIO/ProcessGuard/Outbox 集成、B04 HTTP/app 和 D10 初始化仍未验收。
