# 当前工作：Runner 00026 有限 Linux 交付装配

- 工作树 `/workspace/agenteam-runner-control-delivery`，分支 `ai/runner-control-delivery`；精确正式基线 `4c1db71cf0f86cb6d6330167577944b0466c8664`。本文件是 WIP 恢复入口，不列入最终正式 main 产品提交。Git/index/resolved/checkpoint 仍由 root 操作。
- 装配来源为作者基线 `f1c94ee520e8153e935fea7e7ed269e7e8b9adca` 到 `101a7b7cdc8a2615d02fc0556c128eb62a223003` 的有限 107 路径差异，另取独立树 `af51e339bcb1a8714c24dbfa05b8cdbe223454ff` 的 `tests/runnercontrol/independent_{fixture,management,unknown}_test.go`，合计 110 路径。精确分组清单在作者树 `.agent-state/runner-control/delivery-scope.json`；不复制 WIP supervisor/driver/review/probe、output/cache、UI 或其他业务。
- 只新增迁移 `00026_runner_control.sql`；00024/00025 与 main 逐字相同，作为既有依赖保留，不重复导入；不含后继 00027–00029。独立 Unknown 测试所用 `.agent-state/project-variables-independent/commitproxy/{proxy,proxy_test}.go` 已在 main 且与独验来源逐字相同。
- root 三方 apply 原退出 1，仅 `internal/central/app/account.go`、`internal/central/audit/contract/{types,metadata}.go` 三处冲突。作者现已在上述写域解除源码冲突并 gofmt，4f69ed 实际逆差异确认每个文件仅去 Runner 新增后全文等于 main4c1。Account 保留 Variables owner、事件/outbox producer 与 route，新增 Runner owner 在 Account/DB 前退出；Audit 保留普通/Secret Variable、Knowledge 原闭集与 digest/ordinal0，独立增加 Runner System-only、UUID/ordinal0..1。原 main Project/B02/SecretAudit 未回退。
- 同轮只核对并更新 `docs/development/backend/README.md` 的 Runner 有限状态；D15 卡将独立计数纠正为 Concurrent3 + CommitUnknown2 + LogoutOrder2 = 7。三共享源、README、卡已由root标记resolved并保存85832bd9。Vars独立92468→2e717e实际pure3top24child通过，组合六owner/原期限与Force/late install、11路由和11Audit动作闭集；只受控owner，不代真实根链。

## 实际状态与剩余门槛

交付树11个受影响原pure top已实际race通过：69752/32e354→2b36ac，app1.128s、audit/contract1.022s、audit1.019s，Runner构造/当前调用Force/routes、Variables对应三组与两Audit typed分支/解码/授权。固定Go1.27.1/local/off/readonly/-p1和原Runner独占热cache，未递归选native/PG。作者旧树证据仅按不变语义复用；新main尚无实际PG/native根链。

作者 migration/management/native/client/device/current-authority、协议/竞争/deadline/identity、B 分次 Crash 恢复与 C 默认 Central 失败原结果见本卡，历史 wholeFAIL 与缺失材料保持。独立 Management 原 Concurrent 三子与 Unknown 两子有限通过，修后 LogoutOrder02 两子完整通过，去重七子；原 Management01 wholeFAIL 不回填。

作者默认 Runner OS03 方法三格真实 PASS（40776→b86eb6）：同自有 PID/start/exe、fd0 blocking pipe 与双 SYS_read 见证；EOF 后 Wait0，TERM→INT 与原3s+1s期限 Wait1。强退两格 `read_joined=false`，实际 stdio/Wait/锁/private 尾齐；无 socket 方法不冒 TCP 双空。原 OS01/02 FAIL 保留。最小正式 `tests/process/runner_os_signals_linux_test.go` 已由 Work 有限源码独审接受，正式新Go三格已另获唯一窗口完整PASS：55855→bf18c5 actualouter0、Go1376402 Wait0；EOF1376538 Wait0，双信号1376546/期限1376554 Wait1且read_joined=false；原stdio/同inode锁/private/3PID absent/runtime empty并删除/candidate不变尾齐。作者结果已保存ed709367，不当新main运行。

作者树正式 OS 候选离线构建先有 bfee94 fresh disk 4,470,239,232B、exit78，未启动。随后 73001/3c03ef→83b799 编译 actual0（首 2026-10-10T00:10:48.361204Z，5,470,879,744B），候选 `output/ai/runner-control/runnercontrol-os-signals-race.test` 为 18,980,155B，SHA256 `6d68d868cd0ee3e5226b116f049087c8b6dc4a839a8fd7312ae36c98f52a6e9b`。首次 list 2da475 因作者误用仓库 root cwd，原 TestMain 的 `../..` 无 go.mod，actual1；这是 setupFAIL，不是测试业务失败。正确 cwd 为 `tests/process`，其 list 还会离线构建两默认 cmd；更正后的 bc2550 首 disk 5,361,852,416B 低于 5,368,709,120B，exit78，未 exec。随后正确cwd64137/f62d45→ed441a list actual0，只发现该top；原TestMain实际离线构建两个cmd，非纯发现。其后正式作者三格PASS见上。

作者 DefaultProcesses01 原 session14016→e0a797 wholeFAIL：业务2.65s PASS、Go1362342 Wait0、driver1360322 Wait0，7资源/3private/runtime/desc 双退役与 input same 齐；原75s TCP尾各有1 delta，outer1/118.333s。原保存 JSON 仅证 baseline 同tuple ESTAB/inode3124982→末两样本及 reread TIME_WAIT/inode0，无可证 PID 归属；不猜8080 owner，不降门槛。原件在作者 `output/ai/runner-control/root-default-processes-01` 保留。root选择TCP零方法改动的一次新main候选完整验证，替代另跑旧作者候选；未自动重跑，不新增INET_DIAG。

当前范围只为 Linux/amd64 identity/control 空 registry 的有限 26；macOS、其他架构、实际操作注册、Dispatch/Mount/D16/D17/D18 绑定仍未完成，不能标完整 D15。Object runtime join 等原停项不恢复。

## 新main DefaultProcesses 准备片段

稳定基线85832bd9后唯一业务差异是 `tests/process/runner_control_test.go`：删旧handler-only proxy，原case构造改`newRunnerFailureTransport`，三处退休调用改`controlRetired`，其余业务全文相同。C `runner_failure_test.go` 逐字未改；hold默认false，不调用换址或持有方法，实际Close/copy/半关闭尾仍同3s。旧Default handler0不证明异步101 Body.Close完成，只是原夹具证据缺口，不作为旧14016实际泄漏归因。Skills e8a67e 已独立有限接受，无 must-fix；复跑18 TCP＋config1正6负＋observer7×14控制，保留原C方法/3s/copy-close-half尾、原工具预算/input门，不代真实Default。

两WIP工具仅补原exact `^TestRunnerControlDefaultProcesses$` 和作者ed709的TCP失败证据方法，不改变baseline差集、75s、Go6m/root540+60+3、资源及输入观察。`python3 -B .agent-state/runner-control-delivery/preparation-controls.py` 8e823b actual0：原18TCP控制针对实际移植源，移除明确增量后两工具旧全文相同；旧configs/input_paths相同、精确配置1正6负、原observer7格每格14资源替身观察。控制不运行main/proc/socket/PG，MinIO配置正控是明示小文件替身；真正固定MinIO由root稍后硬链接，不据替身判实际资源ready。

静核根case：正确cwd本树tests/process→TestMain实际本树cmd build→pgfixture新空DB→默认App完整EmbeddedSource1..26→六owner正常装配；只有Runner连接在途。Shared owner竞争/Force以已独审组合pure及原不变域证据分别覆盖，不冒本case验证六owner同时held。

新main candidate拟 `output/ai/runner-control-delivery/runnercontrol-default-processes-main-race.test`，固定Go `test -mod=readonly -p=1 -tags=integration -race -c -o <candidate> ./tests/process`，原作者热cache。首8a06f2在2026-10-10T00:20:54.807228Z sameprocess可用5,289,971,712B<5GiB，实际exit78，未exec/未生成候选；无业务编译失败。正确cwd精确list还会构建两个cmd，后继须记其实际终态。空间经root回收后，新候选已完成：84543/210833→838883 race-c actual0（首2026-10-10T00:23:51.383531Z free5,945,798,656B），产物33,772,357B/SHA256 `eaec76bdb90f8f3d9539a93b3b8ad63329e625e7e78da22cd017e53ece0db455`。正确cwd本树tests/process的31065/489000→261250 exact-list actual0，只发现TestRunnerControlDefaultProcesses，原TestMain实际离线构建本树两默认cmd；未执行业务。7d0678原root --check actual0，真实固定MinIO校验通过，579个实际输入文件全存在/本树00026在列，fresh `output/ai/runner-control-delivery/root-default-processes-main-01` 尚不存在，没有启动或创建runtime。

唯一就绪命令（仍待root独占freshgrant，首sameprocess≥5GiB）：

```text
python3 -B .agent-state/task-planning-recovery/pg_only_supervisor.py --root-chain --driver /workspace/agenteam-runner-control-delivery/.agent-state/work-owner-http/root_chain_driver.py --binary /workspace/agenteam-runner-control-delivery/output/ai/runner-control-delivery/runnercontrol-default-processes-main-race.test --run '^TestRunnerControlDefaultProcesses$' --output /workspace/agenteam-runner-control-delivery/output/ai/runner-control-delivery/root-default-processes-main-01
```

cwd本树；保继承PATH前置Go1.27.1，AGENTEAM_GO固定该Go，GOTOOLCHAIN=local/GOENV=off/GOWORK=off/GOPROXY=off/GOSUMDB=off/GOTELEMETRY=off，GOMAXPROCS=2，外层GOFLAGS='-mod=readonly -p=1'（原root adapter按既定p2执行）。GOCACHE/GOMODCACHE/GOTMPDIR/TMPDIR/XDG_CONFIG_HOME复用作者 `/workspace/agenteam-runner-control/output/ai/runner-control/{gocache,go-mod,tmp,tmp,go-config}`，只有本人写cache；原adapter为fixture/TestMain覆写fresh runtime。不得删除仍由新main构建消费的这些既有缓存。全程原Go6m/root540+60+3/TCP75/七资源/private/runtime/desc/input尾不变，不改TCP身份或排除tuple。

当前没有本域在途命令或真实资源，源码与候选冻结待Skills窄审/保存及freshgrant；旧14016整体FAIL保留。

## 最小 CLI 回归入口准备（未实际）

545d2b33 后仅三个 WIP 技术路径冻结：`.agent-state/work-owner-http/native_driver.go`、`.agent-state/task-planning-recovery/pg_only_supervisor.py`、`.agent-state/runner-control-delivery/cli-controls.py`。唯一组合 selector 为 `^Test(CLIScopeAndSafeFailures|RunnerRealSIGTERMAndSIGINT|RunnerAndNeutralDependencyBoundaries)$`，复用现33,772,357B候选，不重编业务。真实 held TLS challenge 的 TERM/INT 与 callback 退休不能用 Default 在线停止或已过 blocking-stdin 强退代替；此组不是无 socket 方法。

复用既有 native driver 的 Go90s/driver105、非root supervisor123+3和TCP75；exact分支设正确tests/process cwd，让原TestMain实际构建默认双cmd，TMPDIR/GOTMPDIR同owned tmp。输入复用原root adapter动态cmd源码/embed/module/固定Go闭包，并补实际native driver源码与编译driver；原Default/root/其它native入口不变。新observer验RUN/PASS恰三及原manifest同child Wait0/runtime/private成功，再双查tmp与process私目录无遗留；失败仍原desc/reap/TCP/input尾。

`python3 -B .agent-state/runner-control-delivery/cli-controls.py` 的852f6d实际0：24 observer/实际main外部效果替身控制，缺失/重复/额外/Skip/坏UTF8/错Wait/manifest/private拒绝，source变更和driver失败仍到原完整尾；逆去增量两工具全文等于545d2b33，预算和Default原输入路径不变。没有启动child/socket/PG，不据替身称业务通过。三技术已交Vars独审；native小driver尚未编译，CLI真实组仍待独占freshgrant。Default候选和其业务产品字节未动，supervisor已停止写，可先按原窗口运行Default。

有限正式范围沿既有107＋独验3清单；只读0d1a71核545d2b33相对main4c1恰110正式路径＋原4WIP，missing/unexpected=0。此轮CLI工具/控制也仅WIP，不进入产品提交。剩余必要实际为new-main Default和上述三回归；正式OS三格、作者/独验限定业务及11个共享pure、Vars3top24child可复用。全部实际结束后一次归位card/README/后端文档及交全局writer的tasks单行，不为准备另扩正式范围。
