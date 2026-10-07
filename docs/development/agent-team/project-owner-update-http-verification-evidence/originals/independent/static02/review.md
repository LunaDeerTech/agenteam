# Candidate04 独立 STATIC

**有界 STATIC PASS；动态与完整产品验收仍待执行。** 输入 candidate04 `991dd9de130ad92e3e42fbfed372ae8972aab549c32c9ff6bba0395880386b0b`，18 快照逐项匹配。复用 static01 未变生产结论；新增差量为原 candidate02 两 app 文件与四个 integration 文件，未读取活动源码作为固定结论。

U01 已闭合：readyz 只核实际 503 和既有 Problem，不再要求 Account 路由专属安全头。02 ProcessID adapter 保持原 process 值与 guard，目标解析后委派原 Account adapter；不新增公共接口。

U03 补测会在有效 Entry/AppendKey/ProjectRequest 构造后，将坏 cause/changed_fields 或两侧 summary 交给同一真实 Project Authority。realRejected 只在委派返回错误后计数，HTTP 断言到达一次且 canonical/completed receipt/Audit/Event/Activity 集合不变。原 producer/project 人工拒绝只证明失败传播，不冒称真实事实拒绝。正常路径仍分别观察 CurrentAccess/NewFact。

U02 新 proxy 精确匹配 PostgreSQL BackendKeyData PID，只截目标连接原 COMMIT。reached 是“收到但未转发、client 已断、原 writer 仍持锁”；release 后发送同一帧并观察 COMMIT CommandComplete 与 idle ReadyForQuery 才关闭 completed。没有替代 Store 返回。root 按唯一 registry waiter、已持原 command 锁、确切 Project/update/key 的 durable planned row 定位 final；Command rank0 先于 registry rank1，歧义失败。仅配置数据库代理，app.Run 无 handler/Store 注入。

原确认 Store seam 只观察真实 context/result。parent 取消后，原 WithoutCancel deadline 仍在 2–3s 内；实际命令锁等待、Stop 后仍未返回、短 Drain 失败，release 后 HTTP abort/零 body 与 Drain nil 分别核对。Logout 代表证明 writer 持 UserEX 时注销排队、放行后两调用实际返回及后续 lookup401；它不证明确定由 Logout 抢先于确认事务获得锁。独立 A 将补已提交注销先于原 receipt/plan 访问的确定顺序。

正常 root 在原 writer held 时不退出，释放后必须实际 root.stop 且日志 drained；合法 100ms 配置必须 forced，等待 app.Run 返回后仍无 proxy terminal，再单独放行原 COMMIT。后者不是 root 全部内部组件 join 的证据；既有 forced 退出/内组件 join 限制继续保留。client、HTTP、Logout 都有正常及失败 defer 的实际 channel 等待；每 proxy serve 收齐两方向 done，cleanup 停 listener/连接后 WaitGroup 等待 accept/serve。正常 DB-last/join gate 仍复用固定 app/resources.go，不改产品关闭链。

剩余验收归属：作者新四个真实 top/native/旧精确回归尚待实际窗口；独立 A 补真正 AppendPlan/Event 在真实 Tx 下缺 User/Project/outbox-registration 锁（完整锁成功对照）、旧 plan 错配新 Event，以及已提交 Logout 的当前授权；原双 planned 竞争/物理 writer commit、rollback 可按固定证据复用。独立 B 承担默认 root/history/兼容及真实 PATCH/lookup 原字节标准 schema。四个 bad-fact 不等于全锁/旧 plan 矩阵，源码存在与编译不等于动态 PASS。

本阶段只读固定快照/Git 与自有 scratch 写入，未运行独立 Go、Node、listener、PG、容器或应用。
