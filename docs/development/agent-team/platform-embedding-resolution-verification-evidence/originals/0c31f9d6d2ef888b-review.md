# Embedding 两源纯阶段最终组合验收

结论：**#1/#2 纯阶段可接受，PASS，无未关闭必修；STOP。** 产品 `resolution_policy.go` 保持 `c8ccd0950f39015e2817c143ce224ef36956938dbeaf002e634f3ada5db2304a`；纯测试最终 `platform_embedding_resolution_test.go` 为 `dd1e60b71975fd83cc3ff33e6dbf4c8a8bbf3667e97791e95d3b2d62689b072d`。此结论只组合静态、作者 pure/race/vet 与独立 pure 补集，不是完整 Embedding 卡、PG 或生产验收。

#2 差量精确三 hunk、四处替换：负例前后只 Marshal p.Input/m.Input，两次错误均立即 Fatal，原 error code、空 revision 和输入不变性断言保留，关闭整体 record 零 Version 导致的空对空缺口。正例使用不可变 typed const 8192，后验显式拒 nil，调用者 copy 的 ContextLength 改为 1 后不会同时改变期望值。产品 Clone 未改。EntryGuards、两个 helper、imports/名字及此 top 其余内容均逐字保持。

实际只读核对 15 个冻结原件及两安装源：manifest fe6f4c9d、result a6c36e7d、diff 7c8ed0ad 匹配。作者受影响整 top 的原 raw 3d6680f3 实际 24 RUN/24 PASS（top＋两 purpose＋21 负例），-race、exit 0、6.602s；argv/env 保持 offline、p1、40s 内层/45s 外层。format 和该 test 均实际 direct wait，原记录两次 owned 为空；352 输入沿原清单仅 #2 差量，未重建闭包。

组合为最新 24 项＋原 74 项中未受影响的 50 项，共 74 个唯一作者 RUN 项；原 vet 按未改变产品/import/类型复用，并非最终源 fresh 全套重跑。独立 9f3768ef 的三组按 pure01/pure02 两轮实际结果继续适用：当前差量只修作者测试，所验证产品和契约没有变化，因此不重跑。作者原 a5fbc31e 的空对空缺口、本人首轮整体 Marshal 前提错误 FAIL、后继定点修复均保留历史，不覆盖原件。

本轮没有执行 Go/Node/Git/PG/网络/资源，未读取活动 PG 六源，未改产品/测试。未验证真实 Store/consumer/锁/Tx/lease/历史/Unknown；带 ref Forbidden 与前置全通过后的 KeyReused 仍待卡 rev3 对应真实 PG 及独立 A/B。只写本目录三小件，无 owned 命令或资源，Go/cache 窗口此前已释放。
