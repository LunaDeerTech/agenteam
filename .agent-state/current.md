# 当前任务：D09 有限 text-only Model Runtime

- 工作树 `/workspace/agenteam-model-text-runtime`，分支 `ai/model-text-runtime`，起点 main `04455194`。本执行者唯一写本任务；全部 Git 操作由 root 负责。
- [有限 SPEC](../docs/development/work-items/d09-text-chat-runtime.md) 与 Object/Project 五技术路径经 cleanup 非作者有限审接受。首边界15top普通/race已实际通过。九个Runtime源、两基础test、00031与Secret router显式分派已落盘；Runtime首有限8top普通/race与model包vet已wholePASS，原命令/Wait/私有runtime尾齐。尚未PG/真实Secret/Usage/wire调用，不以pure通过代替组合接受。
- 目标：沿原 `mc.Chat/ModelStream`，一次真实 text-only attempt，串起持久 Call/attempt、00018 Usage、planned Secret read/release、原 OpenAI text adapter/D04、实际 Exchange join 与 terminal。ConsumerAuthority 必需但生产未绑定；契约 fixture 不证明 Agent/F1/Meeting 业务权限。
- v1 仅当前 Consumer plan 的 BoundedRetry、MaxAttempts=1、空 categories；AgentRetry、tools、execution-owned lease 等能力在 reservation/dispatch 前明确拒绝。重复/Unknown 保原 CallID，不自动 retry；不持久正文、不伪成功 replay。
- 全局迁移 `00031_model_logical_calls.sql` 已由 root 分配本任务独占，初稿在写尚未验；00001–30 不改。正式九个 Runtime 源及必要相邻测试、Secret router 显式新组合与真实隔离 fixture 写域见 SPEC §9。
- root已另授并已落盘：`object/process.go`+`process_current_test.go` 的纯当前ID读取；`project/audit_facts.go`+`model_access_audit.go`+`model_access_audit_test.go` 的 AccessProducer/currentGate 原Tx窄委派。它们不证明实际flock或Runtime事实provider已经完成；五路径停写可独立保存。
- SPEC §6 原 Model Secret read/release unbound 必须真实接入，不能仅注入 reader 冒闭环；新 Project provider 必须保护 authorized/sent 的原handoff到D04回调实际退出，不补consumer锁或另开Tx。
- cleanup实际core源窄审指出两项并发错误，现已窄修并获实际diff有限接受：活跃重复保原admission ctx至原当前授权/Tx返回；start首错误在原operation gate转交前仅首次发布，不被晚Close/确认错误覆盖。两项新增pure回归已在regression-03普通/race及model vet实际通过；旧core8/boundary15证据仅沿原输入复用。
- 下一步：regression-03 wholePASS后继续准备首条真实JSON成功与policy-deny隔离链，不重复已过pure；regression-01/02容量FAIL均保持。生产ConsumerAuthority仍unbound。真实资源窗口尚未授予；任何Go前私有telemetry off、去旁路、只读shared mods、自有cache及同进程fresh≥5GiB。
- app/defaultroot、旧 wire/标准 DTO、tools 独立 STOP、Object runtime join STOP 与 Agent F1 边界保持。旧任务记录留原 topic，不复制入本任务 current。

最近实际检查：`.agent-state/model-text-runtime/core-checks.py core-01`，session72425/outer532825 actual0；ordinary Go532828、race Go533336、vet Go534044实际Wait0。普通/race各8top，包0.029s/1.048s；三阶段fresh分别5529034752/5476339712/5416013824B，原组absent、无adopted、runtime/组双空。原件 `output/ai/model-text-runtime/core-01/{ordinary.jsonl,race.jsonl,vet.jsonl,result.json}`。boundary-01原15top证据保持，不重复；无源码编译返修或自动重跑。后续Go仍须新空间门。

`regression-01`于2026-10-10T09:24:13Z/outer551071在首阶段fresh4808167424B不足5GiB停止，exit1，零Go启动/零资源；`output/ai/model-text-runtime/regression-01/result.json`保原FAIL，不回填。

`regression-02`于2026-10-10T09:29:29Z/outer561025在首阶段fresh5057089536B不足5GiB停止，exit1，仍零Go/零资源。资源尾释放后另授`regression-03`，session59305→90bb38/outer567378 actual0，ordinary567381/race567521/vet567681各Wait0；三fresh5952544768/5937975296/5921067008B，每阶段原组absent/adopted[]、runtime/组尾双空。两个定向top普通/race与model vet通过，未重跑旧8/15；原件各自output目录分开保存。

首联调准备发现System凭据grant不可带Project OperationID，已窄修并获cleanup实际diff接受；System保原Secret proof Invocation RequestID，Project保OperationID。`scope-grant-01`原51600→7fbaad/outer572627 exit0，ordinary572630/race572724/vet572891全Wait0，fresh5867253760/5852631040/5835702272B，各原组与runtime双空。1top2sub普通/race及model vet通过，原regression03保旧输入证据。首真实fixture用正式platform MeetingSummary/System Model和凭据，D04仍Project scope；两新test文件与first-wire-method.md已初稿冻结，709行；固定candidate-01仅race-c+精确list已通过，未实际运行测试正文。

`candidate-01` source299f0138原首编即通过，79031→b9a3a2/outer5768900，race-c Go576893 Wait0、exactlist577560 Wait0；同processfresh5834768384/5635514368B，原组absent/adopted[]及runtime/组双空。唯一top `TestModelTextRuntimePersistentWire`，binary64796106B、SHA256 `7f2e41085a90d9860760d240a16bfde1849ed343bcd3cda460f1ad76ce04444b`。只候选就绪，尚无00031或真实链路PASS；cleanup方法审仍在途，真实入口与唯一窗口待授。

首真实候选/765输入闭包已获非作者方法及入口审接受，但`wire-01`于2026-10-10T10:15:00Z在supervisor之前的首Docker只读预飞失败：outer600305实际exit1，fresh5477285888B通过；启动器显式/usr/bin/docker不存在，真实Docker为/usr/local/bin/docker且冻结PATH也遗漏该目录。零Docker执行/Go/测试正文/7资源，/tmp/mrt01未创建；私有配置与原result保留，没有retry。此为launch FAIL，不冒业务/迁移FAIL或PASS；最小事实见model-text-runtime/wire-first-launch-failure.json。产品/fixture/candidate/765输入未变，待root授修后新launch。
