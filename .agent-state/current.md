# 当前工作：Runner 00026 有限 Linux 交付装配

- 工作树 `/workspace/agenteam-runner-control-delivery`，分支 `ai/runner-control-delivery`；精确正式基线 `4c1db71cf0f86cb6d6330167577944b0466c8664`。本文件是 WIP 恢复入口，不列入最终正式 main 产品提交。Git/index/resolved/checkpoint 仍由 root 操作。
- 装配来源为作者基线 `f1c94ee520e8153e935fea7e7ed269e7e8b9adca` 到 `101a7b7cdc8a2615d02fc0556c128eb62a223003` 的有限 107 路径差异，另取独立树 `af51e339bcb1a8714c24dbfa05b8cdbe223454ff` 的 `tests/runnercontrol/independent_{fixture,management,unknown}_test.go`，合计 110 路径。精确分组清单在作者树 `.agent-state/runner-control/delivery-scope.json`；不复制 WIP supervisor/driver/review/probe、output/cache、UI 或其他业务。
- 只新增迁移 `00026_runner_control.sql`；00024/00025 与 main 逐字相同，作为既有依赖保留，不重复导入；不含后继 00027–00029。独立 Unknown 测试所用 `.agent-state/project-variables-independent/commitproxy/{proxy,proxy_test}.go` 已在 main 且与独验来源逐字相同。
- root 三方 apply 原退出 1，仅 `internal/central/app/account.go`、`internal/central/audit/contract/{types,metadata}.go` 三处冲突。作者现已在上述写域解除源码冲突并 gofmt，4f69ed 实际逆差异确认每个文件仅去 Runner 新增后全文等于 main4c1。Account 保留 Variables owner、事件/outbox producer 与 route，新增 Runner owner 在 Account/DB 前退出；Audit 保留普通/Secret Variable、Knowledge 原闭集与 digest/ordinal0，独立增加 Runner System-only、UUID/ordinal0..1。原 main Project/B02/SecretAudit 未回退。
- 同轮只核对并更新 `docs/development/backend/README.md` 的 Runner 有限状态；D15 卡将独立计数纠正为 Concurrent3 + CommitUnknown2 + LogoutOrder2 = 7。三共享源、README、卡五路径保持冻结，Vars 正做未参与者有限组合审；6187c4 diffcheck0。原 index UU 由 root 标记 resolved，不能以源码 markers 消失冒 Git 已解决。

## 实际状态与剩余门槛

交付树尚未执行 Go 编译、纯测试、native 或 PG；作者旧树证据只按不变语义复用，不能记为新 main 实际运行。下一步是共享 Account/Audit 受影响纯控制与正式 candidate 构建，然后按 root 排期验证必要根接线；无自行启动真实窗口授权。

作者 migration/management/native/client/device/current-authority、协议/竞争/deadline/identity、B 分次 Crash 恢复与 C 默认 Central 失败原结果见本卡，历史 wholeFAIL 与缺失材料保持。独立 Management 原 Concurrent 三子与 Unknown 两子有限通过，修后 LogoutOrder02 两子完整通过，去重七子；原 Management01 wholeFAIL 不回填。

作者默认 Runner OS03 方法三格真实 PASS（40776→b86eb6）：同自有 PID/start/exe、fd0 blocking pipe 与双 SYS_read 见证；EOF 后 Wait0，TERM→INT 与原3s+1s期限 Wait1。强退两格 `read_joined=false`，实际 stdio/Wait/锁/private 尾齐；无 socket 方法不冒 TCP 双空。原 OS01/02 FAIL 保留。最小正式 `tests/process/runner_os_signals_linux_test.go` 已由 Work 有限源码独审接受，正式新 Go 三格尚未运行。

作者树正式 OS 候选离线构建先有 bfee94 fresh disk 4,470,239,232B、exit78，未启动。随后 73001/3c03ef→83b799 编译 actual0（首 2026-10-10T00:10:48.361204Z，5,470,879,744B），候选 `output/ai/runner-control/runnercontrol-os-signals-race.test` 为 18,980,155B，SHA256 `6d68d868cd0ee3e5226b116f049087c8b6dc4a839a8fd7312ae36c98f52a6e9b`。首次 list 2da475 因作者误用仓库 root cwd，原 TestMain 的 `../..` 无 go.mod，actual1；这是 setupFAIL，不是测试业务失败。正确 cwd 为 `tests/process`，其 list 还会离线构建两默认 cmd；更正后的 bc2550 首 disk 5,361,852,416B 低于 5,368,709,120B，exit78，未 exec。尚无成功 list；等待 root 空间 ACK 再继续，不清未知缓存。

作者 DefaultProcesses01 原 session14016→e0a797 wholeFAIL：业务2.65s PASS、Go1362342 Wait0、driver1360322 Wait0，7资源/3private/runtime/desc 双退役与 input same 齐；原75s TCP尾各有1 delta，outer1/118.333s。原保存 JSON 仅证 baseline 同tuple ESTAB/inode3124982→末两样本及 reread TIME_WAIT/inode0，无可证 PID 归属；不猜8080 owner，不降门槛。原件在作者 `output/ai/runner-control/root-default-processes-01` 保留。root 选择零方法改动、另获一次 fresh 窗口同候选复验；未自动重跑，不新增 INET_DIAG。

当前范围只为 Linux/amd64 identity/control 空 registry 的有限 26；macOS、其他架构、实际操作注册、Dispatch/Mount/D16/D17/D18 绑定仍未完成，不能标完整 D15。Object runtime join 等原停项不恢复。
