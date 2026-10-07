# D08 Owner update HTTP：18 技术路径独立有界 PASS

最终技术输入为 candidate06（manifest `e5f288f04c9ff061ebd70f6b974501c91a3aecdd8f53c33273c63e9e82aef27b`）。`sources.json` 列出精确 18 路径及 hash，当前仓库字节全部匹配。README #19 尚待独立末件审查；本结论不表示整个 D08/D09 或既有停止项完成。

## 接受依据

静审确认 strict presence、原 key/body/version、当前 Session/Owner 授权先于历史回执、仅 update 查证闭集和历史 ProjectRef；保留库中 planning/final Unknown 分类及最多一次原 WithoutCancel+3s 确认，HTTP 不另查证/重试。默认根使用同一 Authority、真实 Audit/Outbox producer 与 Project gate、正式 catalog 注册顺序和真实 Service。私有 work 只有实际 Drain 返回 nil 才 Joined，Force 不伪造 join；原 Usage/Owner read/Summary 路由及停止边界保留。

作者冻结证据组合包括 HTTP 原 20 top/110 nested 普通与 race（含自然 30s/2s、schema）加 06 受影响 5 top/34 nested，app 精确 13 top/69 nested 普通/race、必要编译/vet/两 cmd。未变 Project 库按相同实际源闭包复用既有接受证据，未重跑无关纯组。此为明确版本组合，不声称同一次全包覆盖。最终 06 native 三轮 3 top/7 sub、8 listener 通过；direct 3 与 adopted 3 均实际 wait 返回 0，每轮全部任务 TCP（含 TIME_WAIT）和 owned 进程双空。

独立 HTTP race 探针在 06 上实际通过：1 top、11 nested（9 leaf），验证缺 Flush 零业务调用、精确 64KiB PATCH/1KiB lookup 上限和 +1 拒绝，以及 Body.Close 与取消回调各自阻塞时的真实 join/零晚写。该证据不扩称旧 RequestID middleware 能容忍任意循环 writer。

作者五个 PG 轮新 4 top/6 sub、旧 9 top/20 sub 全通过。四类坏事实确实委派真实 Audit/Outbox validator 拒绝，不以人工返回装饰代替。独立 A01 补上真实完整锁正对照、缺 User/Project/Outbox registration 锁、另一实际 Event 配旧 AppendPlan、已提交 Logout 后的当前授权；并在同一原 writer pending final 后让 backend 关闭，等待 HTTP/Service 实际退役，再由串行原 key lookup 与 canonical/receipt/Audit/Event/Activity 未变证明 final 回滚，随后原 key/body/version 重试及重放。

独立 B02 使用默认 app.Run，无 Store/handler 注入；真实 8192-byte UTF-8 description、当前 v3 对原历史 v2、字节相同重放、update lookup 不泄露 Create receipt、名称 resolve 和实际 root Drain 均通过。B02 原 PATCH 8545B 与 lookup 8607B 经固定标准 Draft202012/FormatChecker、本地 refs 对同一原 bytes 验证通过；同时核 method/path/status/Content-Type/length/target/run/candidate/schema hash，未用合成响应或作者另一响应替代。

## 修订与原失败

保留初始 app 名义 ProcessID/测试 Store 接口编译红及私有同 guard typed-ID adapter 修订；readyz 测试假设和真实 gate/Unknown/root 证据缺口在 04 补闭。独立发现 U04：只有 deadline、缺 Flush 时原实现会先调用业务；05 增有界能力解析，但 Unwrap panic 前尚未建立尾部所有权；06 将空 adapter/budget/defer 放在解析前并保留逐能力实际接收者，生产缺口已闭合。05 循环 writer 测试在旧 WithRequestID 提前 stack overflow 的原红保留，06 测试只在正式 RequestID 之后注入，不改旧 middleware。

独立 probes01 首编译三处缺 nil 参数及前置错误 plan hash 门禁原件保留。B01 在辅助 SQL `SELECT key` 报 DATABASE_SQL_FAILED，产品此前默认根历史断言已过；probes03 唯一改为固定列 `command_key`，无产品/导入/文件集合变化。格式、受影响 race compile/vet、精确 B list 与输入门禁通过后仅 B02 重跑，原 A01 直接复用，B01 全部原件保留。

## 资源与结论边界

`pg-rounds.json` 逐项核对作者 5 + 独立 A01/B01/B02 共 8 轮：每轮实际 wait，精确 7 资源（总 56 个不同资源 ID）与 owned/runtime 两次清零、前后输入一致、无 forced tail/monitor error。失败 B01 也完整清理。新 top 的 120s 从 RUN 到结果包含 Cleanup；包 6m。当前无独立活动命令/owned 资源，窗口已归还。

按实际 PID/starttime 集合，8 轮各新增 4 个 daemon-side containerd-shim Z，共 32；首 baseline 217、末 250，额外 1 个轮间 git Z（676396/starttime2562836）单列，不归本任务。它们非 owned、未 task-wait；不声称全机清零。

held COMMIT 的 reached 只表示原帧收到但尚未转发，writer 未终局；释放原 COMMIT 并收到 server terminal 才证明该终态。作者 Logout 排队不冒确定早于确认，独立 A 的已提交 Logout 补互补顺序。正常 3s 与合法 100ms 耗尽关停分别验证；forced root 返回不证明内部全部 join，后续 proxy/backend 实际 wait 单列。30s 是发布/I/O 预算，保留原 3s 确认退役尾部及同步实际收尾，不承诺全 handler 必在 30s 返回。新主链使用正式 Bootstrap/Invitation/Redeem/Login；旧库 SQL 身份、Skills 测试前置与 liveProcess 辅助边界均不提升为生产绑定。
