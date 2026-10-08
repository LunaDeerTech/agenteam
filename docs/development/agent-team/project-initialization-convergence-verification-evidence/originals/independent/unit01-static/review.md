# unit01 生产两源与受控一源独立 STATIC

**组合 NEEDS_FIX；生产两源限定 STATIC PASS。** 冻结 unit01 manifest `58a72862cf2f0dcec744de757b9bb6b148d0df64f1dd4a76ace7b8839cb173cb`。完整读接口、109行生产实现及430行受控测试；未读作者两份活动 integration 源，未运行 gofmt/Go/资源、安装仓库、操作 Git 或再委派。

两个必修均限同一受控测试，不是产品语义缺陷，尚无实际编译/运行失败：

| 编号 | 定点与反例 | 最小修正 |
| --- | --- | --- |
| IC-UNIT-01 | test:38 用 reflect.DeepEqual 比较两个独立构造的 LockRequest。固定 foundation.LockKey 含非 nil `canonical func() string`，函数不能以 DeepEqual 得到真；合法 Project EX 也在 held helper 提前 Fatal，所有后继事实断言无法到达。 | 精确 len1、Key.Validate、CompareLockKeys==0、Mode=Exclusive；保留同 ctx/tx/order。 |
| IC-UNIT-02 | test:88 Query 声明 `(postgres.Rows,error)`；固定 SQLExecutor 要求 `(*Rows,error)`，Rows 是 struct。其 return nil 非法，测试 Store/返回 executor 不满足接口，不能编译。 | 只改返回类型为 `(*postgres.Rows,error)`，保留 nil+受控错误及 unexpected 计数。 |

固定补充依赖已按已接受 Audit full-input02→配置 full-input06 递归 fingerprint 核对：foundation/lock.go SHA `5f328d35a0bf313f589654c9e52b5b6b0b9f8f3f3b8502e9d1f612b588220384`；postgres/sql.go SHA `2a1357b60da8693124b0cdf3d863589604853b44beb5262e43995f0271f5484e`。未把活动源猜成接受输入。其余依赖沿规格 inputs01 固定快照。

生产两源与正式卡相符：新可选接口不扩 ProjectAuthority；零 Authority/无效输入先拒，registered service 的 exact Creation/Project cause 在任何事实访问前核对；只要求现有 Project EX，然后由同 Store InTx executor 扫描 Creation/Project，不开事务/补锁/调用 Session 或外域。合法不同前向目标/key 为 Forbidden，返回投影ID损坏或已确认归属后的反向指针/owner 矛盾为 Unavailable；非 active 拒绝。原 scanner 的 cause 不被替换，RequireHeldLocks 原 poison 由 Store 保有。

四状态及 NULL/历史规则完整：未完成必须无 initialized/Sprint/operation、version1、两 request 非NULL且合法匹配、protected 两字段均无、result无；reason 区分 accepted/initializing/failed。completed 必须 initialized、request全NULL/reason空、保护标识与revision合法、历史安全 result 为原ID/owner 的 active/version1/无Sprint/ArchivedAt；当前 Project 合法后续 name/description/version 不与历史强等。没有 UPDATE、Commit、confirm、后台或可复用 grant。

其余受控源码涵盖 actor/三 request/旧 gate 四状态、两次幂等观察、同ctx/tx/目标查询顺序、读依赖 cause、cancel、key与command_key区分、损坏投影/单边保护字段/历史快照等。已逐项核 expected order 与原 scanner 早退：accepted 的合法非空 reason 在新 helper 拒绝；failed 空 reason、completed缺字段等先被原 scanner 拒绝。计数/快照只证明受控零副作用，真实 foreignTx、poison与锁存续仍须实际 PG；生产同 executor 的使用是本次静态结论，不拿 fake Store 当真事务证据。

作者随后冻结 unit02，仅 IC-UNIT-01 语义比较单行差量，静态核其 len/key/mode/ctx/tx 都未削弱；IC-UNIT-02 仍待后继。原 unit01 和所有不变生产结论保留，后继只补受影响 delta。完整五源、格式/编译/普通race/vet/PG与旧回归尚未验收。一次只读路径发现先尝试未存在的 unit02/delta.diff，已按 manifest 改读正式 unit01-to-unit02.diff；这不是产品失败。
