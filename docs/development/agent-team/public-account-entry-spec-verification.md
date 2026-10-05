# D26 公开账号入口规格验收记录

公开账号入口 rev1 已独立 **STATIC PASS**，主线程完整读核后采纳并提交推送 `e5a5ccf5343fe9f17aced6e9ee0d633fbfe3c5aa`，远端一致由主线程确认。结论仅为精确 23 路径的规格可实施性，尚无本卡页面、pure 或真实浏览器验收结果。

主线程现已正式授权 `d08_registry_backend` 实施[卡片 §8](../work-items/d26-public-account-entry.md#8-精确实施路径与禁止范围)，固定业务 `c54f73f3324caa11608d84e5d207141985eb6074` 与被审 rev1 技术正文；`skill_verification` 独立验收。作者当前可做私有 offline/pure/type/build，**未获 Docker/browser 运行权**。本次归档者只写文档，未执行这些业务检查。

## 固定来源

| 阶段 | 原始证据与边界 |
| --- | --- |
| 可行性 | [原报告](evidence/public-account-entry-spec-verification/feasibility/report.md)，SHA-256 `814f5a368c802567d3eb9571120284493a7a0aad10fe941050b2a704e33da829`；当时固定认证 `9a710f2`，个人设置仍只是已采纳规格，所述待定接缝保留为历史 |
| 冻结 rev1 | [原卡](evidence/public-account-entry-spec-verification/spec/rev1.md)，`3f3e3a9624af7b73ed8a5082d74e85c6a7f3631286892352e209c2e0b05008d2`；[31 个 Git 输入](evidence/public-account-entry-spec-verification/spec/inputs.json)，`913cc43664f17c151f7a6fa4c58837858ef412494b17875eea3491f18d8a6c6f`，已按个人设置最终 c54 核定真实接缝 |
| 独立静审 | [原报告](evidence/public-account-entry-spec-verification/review/report.md)，`e69b4e4d70ba780b7deb7e10a41034690cf25da1eef63a93d34cd5966ae2c160`；[索引](evidence/public-account-entry-spec-verification/review/index.json)与原 checks 保留，未运行测试/资源 |
| 主线程采纳 | [行政后卡](evidence/public-account-entry-spec-verification/acceptance/after.md)，`47eaf681cb7f2190bd11c7e6a301dfefd660123502112111cc2a221e5af71f0c`；[原行政差量](evidence/public-account-entry-spec-verification/acceptance/administrative.patch)和 manifest 保留，§1–9 未改 |
| 独立后续计划 | [冻结计划](evidence/public-account-entry-spec-verification/plan/plan.md)，`fdbfbf05f9b482d2237ba058ea385468b4ac3022fab170d79c7164a431b72f6d`；[索引](evidence/public-account-entry-spec-verification/plan/index.json)，`c78fdc44afc76297f4d276236707b82225806e09917412e8e24d5a87c1285e3d`。这是计划，不是实际验收 |

原件中的待提交、待实施授权状态不倒写；当前状态以上文及[活卡行政段](../work-items/d26-public-account-entry.md#10-当前状态)为准。原绝对路径与持久文件的对应关系见[原件映射](evidence/public-account-entry-spec-verification/original-map.json)，没有复制整棵源码或依赖树。

## 已审边界与实施门槛

规格涵盖邀请校验/兑换、找回申请、重置校验/提交三条公开流程。五个真实 Account POST 已存在，使用匿名 Browser Cookie/CSRF；两个 inspect 无幂等 key，三个写保留原 key、上下文与完整原输入。23 路径是 12 生产、4 旧 mock 适配与 7 新测试，现存 11、新增 12；没有后端生产、SQL、包依赖或迁移增量。

独立静审核对同一个 Cookie owner、合法 Session 与私有匿名上下文共存、可见 30 秒与实际尾部分离；Unknown 不换上下文、不先 inspect 或将 consumed 410 当作命令回执。链接必须在 history/网络前清 URL，材料只在私有内存；显式 switch 不是自动注销授权。reset 204 已确认与后续当前 Session GET 分开，另一用户的有效 Session 不能被假清除，GET 失败不能重发 reset。具体接口、状态及验收仍只以卡 §1–9 为准。

固定 Account/app、Account/common OpenAPI 与 accountenv 相对已验认证 `9a710f2` 无差量。新增真实 fixture 可复用最终同源 Account root 与受限日志接缝，用正式 HTTP 建前置和只读 PG 核后态；不改表造身份、不创建产品诊断接口，不把 SMTP UI 分支或 pure Unknown 当作新的真实 SMTP/PG 故障证据。

独立计划优先在作者冻结覆盖上去重，默认最多两类 pure 风险组合及一条真实多身份组合；最终选择与运行仍须固定输入和单独授权。旧认证/个人设置与本卡共享 controller/App/router/Login，不能只因旧测试文件未改便复用历史动态通过；受影响回归须有当前证据。原 race/count1/6m、Go 顶层 2 分钟、PW 45 秒/worker 1/retry 0 与资源双清门槛不变。

## 本次归档检查与未验证

只读 Git/原字节核验：31 个 Git 对象的 blob、SHA-256、长度与原输入匹配；被审卡、采纳卡及活卡 §1–9 逐字一致。原件、声明引用、本地链接/fragment 和格式检查见[archive-checks](evidence/public-account-entry-spec-verification/archive-checks.json)；[精确格式例外](evidence/public-account-entry-spec-verification/whitespace-exceptions.json)只保存原行政 patch 的实际诊断，不修改原字节。

本次没有 Go/npm/browser/Docker、网络、SQL 或 Git 写操作。未验证新增页面、真实组合、生产 SPA hosting、完整 D26/D27，也不改变 Object 原修复停止、Artifact/Project 依赖阻断与 ready503。公开流程中的真实 backend_log、纯 SMTP 分支、过期/Unknown 模拟须在未来验收分别记录，不能提前称通过。
