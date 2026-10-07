# new2 首红与 candidate03 差量独审

candidate03 单测试源差量 STATIC PASS。manifest `a362e55ac9007d0a39dcbe885b7c73a46a05c1e6dc05ad7ad56ed20b022fed45` 的 22 个快照已核对；仅 `tests/model/project_credentials_http_unknown_test.go` 变化，其余 21 路径和原完整 STATIC 适用结果直接复用。没有运行 Go、数据库、监听或容器，没有改产品和私有探针。此结论不是重跑或整卡产品 PASS。

原 new2 实际 exit1：PassiveLookup 两子例与 Unknown/prepare_nonce 通过，三个 final mode 均在原第 121 行等待检查点失败。原 raw 没有 QueryRow 错误、SQLSTATE 或这些 final 响应状态，因此不能倒称原轮已动态证实 SQLSTATE 或已经到达 final backend。原轮已 actual wait、watchdog thread join、七个精确资源两次消失、自有进程两次为空、前后输入相同，无强制尾部；非自有 PID1 僵尸不计作任务 wait 或全机清零。

高置信静态归因是测试 hook 的 SQL：同一个 `$1` 同时比较 `project_id uuid` 与 `scope_key text`。接受基线 `a0b012ce` 的 migration、Postgres QueryRow 包装和原 Store after-hook 表明，这个错误可在 arm 前返回并使事务回滚；原测试随后先等 proxy.reached，会用五秒检查点错误掩盖原 HTTP 返回。prepare_nonce 不使用这条查询。Secret ExecuteWrite 的真实 CommandsCause、tagged context 与原 final 阶段匹配保持正确。

candidate03 改成独立的 `$1::uuid`、`$2::text`、`$3`，前两者仍传相同的规范 ProjectID。原 Command EX、User/Project SH、完整 receipt 条件、同事务 backend PID 与唯一 Command 锁拥有者匹配没有削弱。诊断只包含固定阶段、状态及 Postgres 已校验的五字符 SQLSTATE，不打印错误内容、SQL 参数、材料或协议帧；原 503/CommitUnknown 断言移到等待检查点之前。pending writer 放行、实际 terminal、Logout 等待和代理清理原字节保留。

独立 terminal-first 私有探针查询只把 `$1` 用于 `r.project_id`，没有同参数比较 scope_key，不需要同类修复。该探针及独立 A/B 仍未执行；作者 held proxy 仍只证明 pending→terminal 时序，不能取代 terminal-first ACK loss 代表。

原证据和固定 Git 来源逐项指纹见 [sources.json](sources.json)，机器可读结论见 [result.json](result.json)。原失败保留；受影响真实重跑由 root 单独授权。本文及本目录到此冻结。
