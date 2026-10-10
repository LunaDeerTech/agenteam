# Project Variables 本实例精确停止：作者 PG 入口

本目录复用 [原 PG supervisor](../task-planning-recovery/pg_only_supervisor.py) 与 [PG driver](../task-planning-recovery/pg_only_driver.go)，每次在私有 namespace 绑定一个固定 selector：原提供方 `^TestProjectVariableLocalLifecycleStop$`，或下文单轮 phase `^TestProjectLifecycleLocalStopRound$`，各为1 top / 3 sub。没有修改共享源或预算，没有 native HTTP listener、MinIO 或浏览器。

- `archive-calls-and-project-isolation`：原 BEGIN 后、Project 锁前持有普通/Secret 写、合法读和另一 Project 写；真实 stopping phase fixture 提交后只取消目标写，原调用返回前不 joined。错误 acceptedVersion 与原回调导致的真实回滚均零取消。
- `delete-read-calls`：两个 Service 的 Get/List/Lookup 共六个原调用，取消之后仍等待真实 callback/Commit/call 退出。
- `confirmed-phase-and-rejection`：真实 Project EX 持未提交 phase，原 Stop 的 SH 等待以 exact PID/key/blocker 的 PostgreSQL 事实证明；COMMIT 前零取消，确认后取消并观察原调用退出。

fixture 复用正式 Account/Project 创建，原 Skills 初始化 receipt 明确是受控上游；stopping operation/manifest/participants 是规范阶段夹具，不冒生产 Archive/Delete worker、完整 participant、foreign join 或 cleanup。生产 initializer unbound 与 Object Runtime join STOP 不变。Unknown 的零取消仅由作者纯控制覆盖，本轮不冒真实 COMMIT 代理恢复矩阵。

## 固定调用与输入

先在本树以固定 Go1.27.1、private actual telemetry off、共享 module cache readonly 和自有 GOCACHE 离线编译两个产物；同进程 fresh >=5GiB。候选 `output/ai/project-variable-lifecycle/candidate-01/project-variable-lifecycle.test` 与 `pg-only-driver`。精确 list 必须只返回上述 top。

当前固定候选 37,225,874 B，SHA256 `73659a6572d3b67a3428cbf2c9ff3544d56be526b4e23f462fcf53f5ea6c2286`；原 driver 的本树 race 构建 19,371,402 B，SHA256 `abc413c74d5438a9c2a03978fc22afdfd54e15d5f7b4cd6684dba75361e464f1`。实际依赖闭包 435 路径。

首轮环境使用独有 `output/ai/project-variable-lifecycle/environment-pg-01`：`go-config/go/telemetry/mode` 内容为 `off`，私有空 `runtime`，`docker/config.json` 为 `{"auths":{}}` 且0600；`DOCKER_HOST=unix:///var/run/docker.sock`，移除 `DOCKER_CONTEXT/DOCKER_CERT_PATH/DOCKER_TLS_VERIFY` 与 telemetry child/test 覆盖变量。固定 `GOTOOLCHAIN=local/GOPROXY=off/GOSUMDB=off/GOTELEMETRY=off/GOMAXPROCS=2/GOFLAGS=-mod=readonly -p=2`，readonly module 路径 `/workspace/shared/agenteam-deps/go-mod`，本树私有 `output/ai/project-variable-lifecycle/go-build`。PG 固定原 driver 的 `pgvector/pgvector@sha256:99a149d3c84cfb0f32d8da7d72737e4643468787220af2223418730f8e9e9cdc`；不拉新镜像。

```sh
python3 -B .agent-state/project-variable-lifecycle/run.py \
  --driver "$PWD/output/ai/project-variable-lifecycle/candidate-01/pg-only-driver" \
  --binary "$PWD/output/ai/project-variable-lifecycle/candidate-01/project-variable-lifecycle.test" \
  --run '^TestProjectVariableLocalLifecycleStop$' \
  --output "$PWD/output/ai/project-variable-lifecycle/pg-author-01"
```

真实运行必须 root 独占资源 fresh grant；PG17/vector0.8.1 原 container/network 两资源、Go6m、driver105+cleanup15、sup123+TERM/KILL3、原资源/desc/runtime 双尾、host TCP75s 连续双空、原 Wait 与输入初末重新枚举全部保留。启动前私有 actual telemetry off、空 Docker config 和 >=5GiB；无自动重试。

生产依赖、完整同包 Go fixture、DDL/SQL、弱密码 embed、原 supervisor/driver 源及两个固定 binary 和本域三恢复源一并由 run.py 枚举；正规文件/无 symlink，缺项或新增输入、改变 bytes 均拒。只执行固定 selector，root/native/别名/重复 flag/未知 flag 在加载原 supervisor 前拒绝。

## 首轮准备证据（历史）

首纯测试编译因测试直接比较非 comparable LockKey 而 FAIL；窄修为 Mode+CompareLockKeys 后 9 top / 20 sub race actual0。candidate01 race-c / exact list 与原 PG driver race build actual0，原业务尚未运行。首次实际 input 枚举因复用时误改 commitproxy 目录字面值而 FAIL；已恢复 main 的真实原路径，未修改原 helper 或扩大依赖。`entry-controls.py` 使用显式 OS 边界替身调用真实 supervisor.main/observer 与输入双检查；其结果仅为离线方法控制，不能当作 PG 或独立验收。

## 首次真实 PG 结果（有限作者链）

冻结技术来源 `55a6e725`、上述 candidate01/driver；`pg-author-01` 单次运行 `TestProjectVariableLocalLifecycleStop` 1 top / 3 sub 全 PASS（top 9.20s，子例0.55/0.38/0.19s）。原 tool session59972 → 0590e9 exit0，outer/supervisor524357、driver524369、Go524928 均原实际 Wait0；driver17.323s，supervisor76.621s。两nonce PG资源各双退役、private闭集、desc/runtime双空、HOST_TCP连续双空、435 inputs unchanged、STOP0 全部原门成立，无重试。原日志位于 ignored `output/ai/project-variable-lifecycle/pg-author-01/pg-d77e6c6086274412855cbc89fb0d3bf2.log`；源码与本摘要可由 topic 恢复，不声称 ignored 原日志已进入 Git。

独立实例已对 SPEC、核心实际 diff、两PG源与私有namespace入口作有限只读接受；动态纯测试和本次 PG 均由作者运行。此结果只接受 ordinary+Secret 两个本实例服务的精确停止与实际call退出；不证明 foreign process join、真实 Unknown COMMIT代理、完整复合participant、cleanup、生产phase worker 或 production Project Create。原两次准备失败（首Go编译与首input路径枚举）保留，未回填成原轮 PASS。

## 后继单轮 phase：首轮整体 FAIL，测试钩子已窄修

首轮 `^TestProjectLifecycleLocalStopRound$` 使用独立固定产物 `output/ai/project-variable-lifecycle/candidate-phase-01/project-phase-stop.test`；原提供方 candidate01 不覆盖。首候选37,379,760 B，SHA256 `9983dab14e6415a6c5944fc6726c68dc73acf2aed1cdfd0d7cbe793542c761ae`。复用上文 driver，SHA不变；新实际输入439路径，额外显式要求两phase PG源存在，仍初末全闭包重新枚举。

- `committed-phase-and-local-return`：accepted 是明确上游规范夹具；真实driver在原Project EX事务推进stopping与claim。COMMIT前零provider，确认后才取消原业务调用；业务实际返回及held checkpoint均未退时，原Run/Drain不退出。另一Project隔离，全participant仍required。
- `rollback-and-frozen-manifest`：原事务真实rollback与缺冻结版本声明均零provider/零claim。
- `claim-fencing-and-terminal-attempt`：stale原BEGIN、EX之前持有；另一个真实driver完成fence2，旧轮取得EX后拒绝覆盖。claim terminal只表示本轮尝试退役，operation仍stopping。

新6top/2sub纯race与Project vet对修前核心实际通过；非作者审发现组合checkpoint错误路径可回显provider error，已仅安全包装该路径。修后首次启动fresh空间不足（2,301,857,792 B），原失败ca201c未启动Go，未改判通过。窗口释放后，唯一新privacy canary定向race及Project vet实际0（93546→7784ce）；物理Unknown/原cause保留且private canary不回显。PG候选race-c与精确list实际0（39670→e47133，Go567801/list567945原Wait0、runtime空），仅列出上述top，没有运行PG。

私有入口只增加selector/artifact/3sub的固定映射；旧6个控制方法AST未变，未重跑。新增3个phase方法控实际0（687f49）：真实sup.main、原PG observer与两次inputs均实际调用，OS资源边明确受控；拒绝错artifact/别名/root/native、缺或重复case、FAIL/SKIP、错PID/非零Wait、退役缺失、private残留、缺文件/symlink和输入漂移。共享sup/driver源码与a047efa3逐字相同。控制不是真实PG或独立动态验收。

以下为来源 `1ccbc56b` 的首轮历史命令；当前入口已按下段固定为 candidate02。

```sh
python3 -B .agent-state/project-variable-lifecycle/run.py \
  --driver "$PWD/output/ai/project-variable-lifecycle/candidate-01/pg-only-driver" \
  --binary "$PWD/output/ai/project-variable-lifecycle/candidate-phase-01/project-phase-stop.test" \
  --run '^TestProjectLifecycleLocalStopRound$' \
  --output "$PWD/output/ai/project-variable-lifecycle/pg-phase-01"
```

新轮必须重新创建独有environment/private actual telemetry off/空Docker config并在同进程核fresh>=5GiB；沿上文固定镜像与全部原预算/Wait/双尾，无重试。该准备阶段未运行phase；后续实际结果如下，不以候选编译或离线控替代联调，不覆盖完整registry、foreign业务join、cleanup或生产initializer。

首轮后来按root独占授权实际执行，来源`1ccbc56b`，439输入SHA256 `c26a8d79b20ccc9824d2d94f16e6e987d8bf67b7b2044ba898eee9871b79d6de`。`pg-phase-01` 整体FAIL：top12.07s，第一sub在phase原barrier未命中（5.23s），第二sub回滚断言未成立（0.11s），第三sub fencing PASS（0.14s）。session41634→d62a41实际1，outer/sup578847、driver578850、Go579413均原Wait1；driver19.945s/sup80.542s。两资源精确双退役、private仅owned.json、desc/TCP双空、输入初末一致、STOP0，原exact cases门False保持。环境runtime退出后两次观测空属于后续观测，不补写原门。原日志在ignored `output/ai/project-variable-lifecycle/pg-phase-01/pg-b04f0f5503a746a4a421b5443a5d48b0.log`。

有界源码定位：两个测试after hook错误筛选`CauseDetails.Owner`；实际driver使用`NewJobCause`，正式字段是`Kind=JobCause`、`JobType=project-lifecycle`及原JobID/JobAttemptID，Owner仅属于RecoveryCause。因此phase持有与回滚注入均未被触发。这是测试方法缺口，不能把原FAIL升级成产品PASS。修后状态见下段；原FAIL不追认，尚无修后动态结论。


### 修后 candidate02 已编译/list通过（真实PG未运行）

两处hook已窄修为原`Kind=JobCause`、`JobType=project-lifecycle`、`JobID=OperationID`及合法`JobAttemptID`；非作者实际diff有限接受，产品、全部断言、第三fencing子例与预算均未改。该单源修由root本地保存`6b91f717`，当时两次远端推送失败；后来该修复与入口准备已随`065598c2`保存远端，不改写原推送失败事实。为保留原FAIL产物，run.py仅将PHASE_BINARY字面值改为`output/ai/project-variable-lifecycle/candidate-phase-02/project-phase-stop.test`，复位该唯一字面值后与原entry逐字相同；selector/cases/controls/原sup/driver均不变，不另重跑矩阵。

source `065598c2` 的candidate02现已race-c/list实际通过（83514→d45aac，outer586303、Go586304/list586410原Wait0），compile6.139s、fresh5,634,961,408 B，list前重新fresh5,589,934,080 B，runtime两次空。唯一列举`TestProjectLifecycleLocalStopRound`；新binary37,381,608 B，SHA256 `4dd8bdb36756011224eae6553adb652b78bdd4257ae70d9429e34d2d1672f6a6`。没有运行测试正文、PG或重跑pure/vet。以下固定入口仍待root新PG窗口，不会覆盖candidate-phase-01、旧日志或原失败结论。

```sh
python3 -B .agent-state/project-variable-lifecycle/run.py \
  --driver "$PWD/output/ai/project-variable-lifecycle/candidate-01/pg-only-driver" \
  --binary "$PWD/output/ai/project-variable-lifecycle/candidate-phase-02/project-phase-stop.test" \
  --run '^TestProjectLifecycleLocalStopRound$' \
  --output "$PWD/output/ai/project-variable-lifecycle/pg-phase-02"
```
