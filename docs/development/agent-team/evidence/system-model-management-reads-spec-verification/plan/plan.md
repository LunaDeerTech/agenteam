# System Model 管理读口：独立验收准备

当前只有计划权。固定业务 `c54f73f3324caa11608d84e5d207141985eb6074`，消费已采纳规格 `bfae86b1b39d47c931d3be2071e76bd2b0e99a19`；复用既有 rev1 STATIC PASS 的 47 Git 输入及补读 1 个原端口。作者 13 路径尚未交冻结候选；本轮没有读取活动实现、编写 probe、运行 Go/PG/Docker/browser 或修改共享文件。下列均为待授权方案，不是已经执行的覆盖。

## 1. 最多两个定向真实组合

**A：当前身份与真实 Metadata 的结果发布。** 复用真实 Account/Secret/System HTTP fixture，不提供 allow authority。用语义 cause=`secret-metadata`、完整持锁与实际 SQL/callback 进入信号定位，不使用“第 N 个 Tx”或 sleep。

- 在 HTTP 原预授权完成、Secret 读事务尚未开始的明确边界，通过正式 Account 行为撤销该实际 Session，确认提交后放行。要求 Secret 同 Tx 再鉴权失败、零 metadata SELECT/零候选返回；记录实际 User/Credential SH 的同 Store/Tx，而非仅见到 HTTP 已鉴权。若作者已留完整同反例证据，独立不再重复此分支。
- 实际合法 Metadata 读取、当前授权、完整 union 和底层 Committed 已发生后，分开检查取消与 Unknown：取消原 caller 后仍返回零 metadata 与既有 DependencyUnavailable/committed；正式 Store 结果投影 Unknown 时公共 CommitUnknown/unknown、HTTP503、零 DTO、没有自动重读。两者不混称物理 Unknown，装饰器只能委托真实执行后改变返回结果，且不得放大预算或触碰网络/ROLLBACK。
- 校验只读取安全 metadata，没有 payload 密文/nonce/lease/业务记录增量；当前版本来自本次实际查询，失败不得留下候选。默认最多三个明确分支，作者同输入已充分证明的分支优先复用，独立只保留能补充信号的组合。

**B：preview 后引用变化，原 Delete 仍依据当前事实。** 使用真实 Provider/Model/平台 selector 命令，不插造外域引用。对尚无引用的 System Model 读到 count=0/none，保存真实 Model.version；随后正式更新 selector，使该模型成为 required embedding，确认其 Model.version 未被顺带改变。用原预览时期 expected_version、无 replacement 发正式 Delete，必须依据最新引用拒绝 InvalidState，模型/selector/引用及成功 receipt、Audit/Event 等业务事实不发生删除成功增量；再读取 impact 确認 count=1/required。必要时接一次正式合法 replacement 删除并核 affected_references、selector 和 target404，以证实合法执行路径；若作者已有同输入完整正链则直接复用，不机械再跑。此组合证明安全观察不是删除许可，不测试未绑定 Agent/project_summary 的活体事实。

最多两顶层；具体名字/分支及 selector 在作者冻结后、probe 实施授权前确定。A 的权利复验与失败发布、B 的实际新事实是优先信号；不为凑数量复制作者五新顶层和十二旧回归。

## 2. 作者核心冻结后的静审重点

1. Metadata 只改获准方法/helper：无效 ref 优先、Human/当前 Session/scope 权限先于存在性，typed-nil 不 panic；System/Project 只依赖各自 scope 端口。User+Credential（Project 再加 Project）一次完整 AcquireAll，InTx/RequireHeldLocks 与实际授权/SQL 同 Store/Tx。旧写前置不出现嵌套 Tx，历史 receipt 不被当前 metadata 替代。
2. Model 读 union 包含 refs EX 与 selection SH；不能沿旧 readScope 的 refs SH 或后续升级。平台全局 LIMIT5 与 canonical 全字段一致；目标反向输入先 LIMIT10001 再计数，最多七合法组，10001 全拒。外域只登记事实及未绑 blocker，不返回 owner/Project/Secret 材料。
3. 新两 HTTP 的 deadline 在 RequireSystem 前设置；库调用不延长更早 deadline。Committed 后同时检查原/派生 ctx，Unknown/失败零结果、不自动 retry；真实 D03 fault/SQLSTATE 链和清理责任不被改写。业务 3 秒不冒称所有物理清理也在 3 秒内终结。
4. 严格安全 DTO/decimal strings、query/body 拒绝、HEAD 与现 Problem、22→26 双向 OpenAPI 一致；仅新两 HTTP timeout，旧 17 方法保持。13 路径与固定 c54 逐项绑定，不以活动 main 或整项新 commit 冒充运行闭包。

## 3. 对上一静审报告的准确限定

原 `report.md`（SHA `a65a76885b8735e5c6dbf952d58adc623afe37dc831ac01818417b6482ae5c38`）将 Unknown“保留原 attempt/cause 的既有错误”写成总括，该句在此限定：固定 Secret `service.go:131–137` 的 commitError 仅提供既有 Secret/公共 CommitUnknown 投影，**没有公开 attempt/cause**；它们只能记录于真实 Store 观测。Model `store.go` 的 UnknownCommandError 才按原合同保留两项。原报告不改字节，已审卡本身正确，不增加 Secret 端口/字段或新的断言要求。

## 4. 去重、运行准入与交还

先逐固定输入读取作者五新真实、十二限定旧回归与纯/race/vet/compile 的实际 argv/env/exit/raw 和原失败；依据明确分支/终态/源版本建立小型复用表，不把一次父组失败内的 PASS 说成父组全绿。已有充分证据的 cap/七组、普通 CRUD、scope 矩阵不额外重跑。若冻结 diff 产生额外风险，只先向主线程报告，不自动扩大测试。

仅主线程再授权后才写私有 probe、离线编译并申请独占真实窗口。沿原脚本 race/count1/6m 与卡 3 秒预算，固定测试源码/输入/selector/观察器；首红停存，不自动重试、增宽限或修改断言。仅受控真实 fixture，无 Provider 账号、网络故障或暂停 Object 探针；live exact ID/labels、双次 absent、旧基线不变、所属进程及 runtime 清零后交还。最终只可报告本卡管理读口的结果，不涵盖前端、完整 D09、Summary 或生产模型调用。
