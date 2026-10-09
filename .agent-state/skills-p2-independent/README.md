# Skills P2 有限独立补集

固定输入 eaad50fd；作者生产保持冻结。独立性针对 Skill 实现与业务判据，不把本测试作者的 harness 自查称未参与者验收。root 已批准以下范围；编译、精确发现、harness 接缝审查与真实授权分别完成。

| 场景 | 前置与刺激 | 可区分的断言 |
| --- | --- | --- |
| 纯确认 | 实际 Service 四口和正式 opaque plan，受控 Store/Project 返回当前权限变化及原 Unknown | 已有 plan 不缓存当前 grant；foreign issuer/原 request/弱锁不成功；不补锁、不开嵌套事务、不做 Object I/O；原 Fault/Unknown 不被改义 |
| 纯 Package | 实际 Service/P1 typed PackageReader 与显式 Object reader，分别 hold 原 Read 返回、Close 返回和取消回调 | 原调用实际返回前本地 Joined/工作不能提前完成；原 Close 错误保留；两个返回边界分列，不以 EOF 或 cancelled 代 join |
| 真实确认正负 | 一次真实 D05 发布；精确原 plan、同 Store/Tx/全锁确认，对照弱锁、重建 issuer | 原 Skill/revision/Object/canonical/Audit 恰一次；拒例无新增物理/持久发布 |
| 真实当前重验 | discovery 后用正式合法 fixture 状态使当前 gate 不再允许；不变原 plan/actor/request | 原计划仍拒，精确原物理身份不替换；规范 seed 恢复单列，不称生命周期命令 |
| 真实包当前权限 | ready seed 后当前 Owner OpenPackage 正向；错 Session/非 Owner 负向 | 拒绝不交付包、不新增 Object reader lease 或 Skill work；不复用初始化 Service 权限 |
| 真实包取消尾 | 同一个真实 D05 reader外层原 Read/Close 返回用 channel hold，原 context 取消 | held期间实际 Skill/P1 状态与SQL work、D05 lease分别观测；放尾后保原返回和错误、查实际记账，全部 goroutine actual join |

最后四行为一个新 exact top `TestSkillIndependentP2ConfirmationAndPackage`，复用既有七资源 fixture。held adapter 只延迟正式 reader 的原调用返回，仍转发原参数/字节/error，不制造 Object 成功或额外读；这种刺激不证明 MinIO 网络阻塞，也不把本地 Joined 当 D05 lease 持久释放。

不重复已通过作者全矩阵。真实 PG COMMIT Unknown、迁移和 Stop 的已有原范围证据按卡复用；本补集不会扩大它们。未实现的 Cleanup/root、D05后段清理/00028、完整participant、Runtime join停项和foreign进程退休保持未验证。

实际纯控：`python3 .agent-state/skills-p2-independent/pure.py`（21592→238565 actual0，race 1.068s）执行前两行2top/7sub；通过只说明真实Skill/P1代码在明确受控Store/Project/Object输入下的判据成立。独立候选 `output/ai/skills-p2-independent/skills-p2-independent-race-01.test` 为32,988,048B，session38872→43fd06离线race编译及精确list均actual0。

后四行随后在fresh独占七资源窗口实际运行：P2-01 Go业务top及4sub均PASS（2.32s），证实本矩阵有限范围的真实Skill/D05/SQL关系。整轮仍**FAIL**：原driver Wait0后监督记录owned PID1286231存活STOP，随后才actualWait0；没有该时点state/ppid/命令快照，不能推Z或回填成功。7ID/private/runtime/desc/TCP双尾及输入不变最终均齐，session39104→3d1e80 outer实际exit1，窗口释放。15行必要原件见 `p2-01-failure.txt`；不会因业务PASS声称完整P2、production root、MinIO网络阻塞、foreignguard或Cleanup通过，不自动重试。

既有 root driver/supervisor 仅增加 `^TestSkillIndependentP2ConfirmationAndPackage$`；该 selector 独有输入增量为 `tests/skills` 固定九个 Go 文件，覆盖新测试及完整编译包的复用 fixture。`python3 .agent-state/skills-p2-independent/harness-controls.py`（c223e6 actual0/42控）用实际 configuration、input_paths、原 supervisor main/manifest/observer，替换外部 child/Docker/TCP 边界检查正负尾；旧两工具去掉限定增量后逐字等于29b252c7。Skills未参与该接缝实现者aea89f重取42控并有限接受；这不替代实际Docker/Wait/TCP证明或其自有Skills产品独验。

离线候选构建使用如下固定环境（继承 PATH；原独占 cache，不另建 cache 副本）：

```sh
export PATH="/workspace/toolchains/go1.27.1/bin:$PATH"
export AGENTEAM_GO=/workspace/toolchains/go1.27.1/bin/go
export GOTOOLCHAIN=local GOENV=off GOWORK=off GOPROXY=off GOSUMDB=off GOTELEMETRY=off GOMAXPROCS=2
export GOFLAGS='-mod=readonly -p=1'
export GOMODCACHE=/workspace/agenteam/output/ai/model-ui-recovery/go-mod
export GOCACHE=/workspace/agenteam-project-variables-ui/output/ai/project-variables-ui/implementation/gocache
export GOTMPDIR=/workspace/agenteam-skills-p2-independent/output/ai/skills-p2-independent/tmp
```

P2-01原真实命令cwd为本树，保留上述Go环境并设置 `AGENTEAM_MINIO_BINARY=/workspace/agenteam-skills-p2-independent/output/ai/deps-minio/bin/minio`。root已普通复制，固定SHA `dc5298474f0bc87a068f0b1135c583bb1278c17c11c512212ed7644a238c89c8`，原启动同process再次核实。实际TMPDIR/GOTMPDIR指向本树 `output/ai/skills-p2-independent/runtime-env-01/tmp`，XDG三目录同属该私有父目录，原driver随后将fixture TMP覆盖到原owned runtime。每次真实启动须新授权、同process fresh statvfs≥5GiB及新输出检查；下列p2-01已用，不可依据本说明自动复用：

```sh
python3 .agent-state/task-planning-recovery/pg_only_supervisor.py \
  --root-chain \
  --driver /workspace/agenteam-skills-p2-independent/.agent-state/work-owner-http/root_chain_driver.py \
  --binary /workspace/agenteam-skills-p2-independent/output/ai/skills-p2-independent/skills-p2-independent-race-01.test \
  --run '^TestSkillIndependentP2ConfirmationAndPackage$' \
  --output /workspace/agenteam-skills-p2-independent/output/ai/skills-p2-independent/p2-01
```

原三层 fixture 的七资源、Go6m/root540+60+3/TCP75、实际 Wait/reap、两次资源与private/runtime/desc/TCP/input尾保持，不新增清理者或延长预算。此次整轮失败按原门保留；后继必须新输出和fresh grant。

原01之后的监督改动只适用于上述exact P2 root入口：原driver实际Wait后，在原失败判断点取得本任务owned descendant集合，对每个PID前后两次读取pid/starttime/ppid/state，父进程必须当前subreaper。安全exe只输出固定常见basename或null；Z缺exe不伪造，也不以名字授权。只有一致的owned Z且 `waitpid(精确PID,WNOHANG)` 实际同PID/status0才能接受；任何live/未知/消失/身份变化/未waitable/非0/ECHILD/部分失败或新PID都保失败并继续原清理尾，不加sleep、重试或预算。持久 `reaper-controls.py` 38eb3e actual0/64控和受影响 `harness-controls.py` 8b7e64 actual0/42控通过，后者保旧generic/非P2全文逆差异与完整受控尾。片段已冻结交Runner窄独审，尚无新真实运行；不会据此改变原01的STOP、身份缺失或wholeFAIL。
