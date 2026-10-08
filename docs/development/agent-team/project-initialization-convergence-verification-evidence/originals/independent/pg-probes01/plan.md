# 独立 PG 两 top 私有草稿

状态 PREPARED ONLY：仅源码和overlay草稿，未gofmt、未编译/list/vet、未执行test/资源，实际probe图为null。正式规格/先前product-prep01不变，候选依赖更新到作者candidate03（相对02仅Go格式空格）。父实际full20+dynamic图已给定，仅引用，不复制大清单。最终作者离线原件另审，不把准备引用当产品接受。

精确两top（每top后继120s含Cleanup/包6m，独占资源另授）：

- A `TestIndependentInitializationConvergenceTransactionBoundary`：真实other Store的live token交原Authority，先在caller持EX的原事务写本域creation.version标记，故意忽略gate错误，要求实际NotCommitted并完整快照复原；另SH缺EX拒绝/poison；最后原Tx先成功检查，再写局部caller marker、取消仅本次调用childctx，原未取消事务仍必须因gate poison回滚。三个代表区分fake Tx、嵌套Tx、setup先失败和取消caller自然回滚，不扩作者完整锁矩阵。childctx均由原callback在本次调用内部派生，WithinTx则从独立有界Background helper开始；foreign不会嵌套。
- B `TestIndependentInitializationConvergenceCanonicalFacts`：四状态各一项可存但内部不一致代表，accepted原request_description、initializing原owner、failed initialized、completed历史owner。每次alter必须实际Committed后才调gate，拒绝时无写；然后修回，new gate可观察。completed另合法当前改名/description/version9，历史name/version1仍不变。与作者四状态矩阵有意选择相同高风险代表独立执行，不算新增覆盖种类或复制全部27子例；SQL setup拒绝不得记gate PASS。

明确复用：冻结candidate03的newInitializationConvergencePG/seed/change/snapshot/convergenceLock/convergenceCommitted/convergenceActor，固定4089的ctxFor/cause/id/requireCode。这些是真实PG/Store/registeredService与同StoreAccount构造依赖，author helper不是独立实现；本探针自己的事务/破坏-修回顺序和最终断言独立。snapshot仅两行内容及已列副作用计数，不称全库逐字节。caller marker由测试写，不冒生产gate写入。

继承fixture的IC-PG-01最终Drain实际等待，不新增goroutine/holder；作者原EX存续物理pg_locks竞争及旧两selector原件组合复用，不为相同目标重复完整矩阵。旧Skills adapter只作兼容，不参与新主链；没有初始化Skills/ObjectRuntime或HTTP。

输入入口见input-delta.json：父input-build与actual-graph固定hash、私有单源hash、8-map overlay（5candidate03+2UI空+1private），只扩tests/project有效文件集合。没有读取UI源内容。后继执行方案需root另授：gofmt-d（保原稿，新格式版另冻）、仅tests/project race integration实际list图并核imports/新generated main/extra0、race-c、精确2top -test.list、race vet；不执行testbody。每命令45s/原subreaper实际wait/双owned空/输入一致；复用已验runner仅换自有BASE/overlay并冻结driver差量即可，不重跑父full20图或pure。新增依赖先补冻，禁止忽略extra。

真正资源后继另外冻结driver、实际动态/CGO0/runtime-read必要映射、fresh>=5GiB/live PID/starttime/精确7资源与两本地镜像。A终局PASS+actualwait/双清后才B，失败先退役停止，不自动重跑；不沿用Audit或其他任务旧daemon baseline。当前UI资源窗口不消费，任何Go/资源执行仍待root授权。
