# Embedding 独立 pure 补集

结论：**三个独立补集按两轮组合 PASS；STOP，Go/cache 窗口已释放。** 原首轮 FAIL 保留；不是一次整 top fresh PASS，也不代表 PG、真实授权/事务/lease/replay/Unknown 或生产绑定已验收。安装产品 policy `c8ccd095`、作者 pure `a5fbc31e`，规格 rev3 `f6535ef6`。

首轮 pure01 实际 exit 1 / 13.360s。两个 purpose 的 identity/semantic/Session binding 组，以及无 ref 不触 SQL 组通过。profile 组在调用 resolutionProfile 前，自有 probe:95 整体 Marshal 私有 record，被零 Version 拒绝；属于测试前提错误，不是产品 FAIL。原 probe/raw/result/退役均由 handoff `deace589` 固定。

root 另授 pure02 后，仅四处比较对象改为已先 Validate 的 `p.Input/m.Input`，既有 Marshal error 检查保留；整名 selector 只运行受影响 profile 子组。实际 exit 0 / 4.239s，6 种合法空对象/空数组表示通过且原类型和值/原 raw 空白未被改写；Options/Parameters/RequestOverwrite 各一个 public-valid null 成员负例均拒 CapabilityUnsupported，三例通过。其余两组复用首轮实际 PASS，没有重跑作者原 7 top。

两轮均 Go 1.27.1 固定路径、offline/read-only/p1、内 40s/外 45s；各实际 direct wait，合计 adopted 0 / forced 0、owned 进程组两次为空，352 已命名输入及各自 probe 前后相同。无异步 watchdog；同步监督器已返回，两个工具会话分别实际 exit 1/0。overlay 只增加虚拟自有测试，没有写仓库产品/测试，也未启动 PG、网络或其他外部资源。作者 7 top/74 RUN/PASS/race/vet 原件已核相同输入，按其有限范围复用。

发现作者测试必修：冻结 pure 第 41–42 行只设置 Input，私有 record.Version 为 0；128/131 行整体 Marshal 后忽略 error，因此 132 行的不变性比较是 nil 对 nil。原 21 个负例的 CapabilityUnsupported 和空 revision 检查仍有效，不能复用其配置不变性声明。建议作者只 Marshal Input 并显式检查 error；产品无需因此修改。此缺口待作者后继关闭，本报告不覆盖后继尚未冻结源。

无 ref pure 只证明 helper 的零查询早退；合法 Agent/Operation 的 pure 变化只证明 C0 结构、单位 identity 与 semantic 关系，不证明业务 facts 合法或真实错误优先序。带 ref canonical Forbidden 与其他前置全部成立后 KeyReused 仍须真实 PG。原失败未改写，当前只封本报告及证据，无后继执行。
