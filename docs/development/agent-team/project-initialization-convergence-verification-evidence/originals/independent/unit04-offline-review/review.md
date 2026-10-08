# unit04 格式差量与作者离线原件独立审核

**限定版本组合 PASS：已接受三源 STATIC + unit04 格式差量 + 作者 Project/contract 离线实际证据。** 不是完整五源或真实 PG 产品接受。本次仅审核原件，没有重跑 Go/format/test，未读两份活动 integration 源，未安装仓库、操作 Git 或运行资源。

最终候选 unit04 manifest SHA256 `01d5d003a911a2959c574633776535ffb85921f58f952a865884ec151e2aab92`；作者 offline-summary01 SHA256 `889f0f8f9fa28e84b93732925863ce7248ddc4677207a828ac402603d59a798f`。两生产源仍是 unit01 原字节。unit03→04 diff `0c47bd64aab74a995a787201c502f80051fe99f3ffea9ccd603f65b86dd6373f` 仅测试 struct 对齐与两个闭包中相同语句的 gofmt 换行/分号排版，三源 STATIC 结论不变。IC-UNIT-01/02 的修正也已随最终源码实际编译和运行。

15 条 command/result/stdout/stderr 的固定指纹、输入前后完整字典相等、direct actual wait、subreaper、两次 owned 空、无 action/timeout/剩余后代均核对。每条 direct wait1、adopted0；这是计数，不将 wait exit0 与计数混淆。原 format-preview01 实际 exit1/有 diff/空 stderr 保留；format02 及其余13条命令实际0。没有为了变绿覆盖原失败。

| 作者实际范围 | 原 raw 独立复核 |
| --- | --- |
| Project 普通 / race | 各54 top、181 nested；其中本卡新2 top、70 nested，均PASS，无FAIL/SKIP/race告警 |
| contract 普通 / race | 各29 top、15 nested，均PASS |
| 两包普通/race test-c、race vet、format02 | 原result actual0，binary run精确复用编译输出hash |
| 预算与环境 | Go1.27.1、offline/modreadonly/-p1/GOMAXPROCS2、40s test、42s child/45s wrapper；编译与运行分离 |

actualgraph02 普通294包/1825文件、race296包/1831文件，各2个生成 testmain，extra0。按原 go-list JSON 核无 app/tests-model/web/UI包、无依赖错误；生成 main 指纹与代码对应普通 `testing.MainStart`/`m.Run`，不调用 custom TestMain。整个实际源图都在 build 输入及编译前 fingerprints 中；3-entry overlay 精确指向 unit04。第二次 graph 实际结束后才进入 compile，各 binary 也绑定 run 输入。

首次 actualgraph 的29个隐含 runtime/cgo源并未忽略：与已接受 Audit full-fixture actual graph 的原指纹一致；加4个接受工具，33项最小差量均核源/工具当前固定hash并纳 input03-cgo。后继 graph02 再验 extra0 后才生成 build 输入。pregraph01/02 的历史元数据缺口、补6个固定4089 contract test的路径/blob/hash、首次 actualgraph29extra与作者 collector AssertionError记录保留；它们不被算成构建通过。该纯图不是未来全20fixture的实际图。

可复核机器原件 `checks.json` 记录15组最小指针/统计、两个图、CGO来源与hash；`check.py` 只读原件作独立核对，实际exit0。先前完整三源审查及静态两问题仍位于 `../unit01-static/`、`../unit03-static/`，本次无需重写或重跑。

剩余范围：两新PG源完成并冻结、五源组合STATIC、integration实际图/编译/真实Store foreignTx与poison/锁/四状态及旧兼容、安装与README均仍待后续授权/验收。Object runtime join、Skills/root未绑定与三个停止保持。已停写。
