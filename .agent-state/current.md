# Knowledge Owner 只读 HTTP

当前结果为独立adapter候选，尚未通过native或PG实际门；production root/正文/下载URL/D13/UI及Object Runtime停项不在范围。基线正式main29dd4298。四生产源和pure/native/Schema输入固定b6635c27，真实Account fixture为1ee8b7e3，四PG top矩阵为bb98b6cd；本树无B02产品或SQL改动。

- 作者pure：6157/884f76首10精确top实际exit1，唯一失败是新测试误期待公开cause_id。仅修测试，67777/e3a2ed定向race exit0/1.017s，其余未变9项复用首轮；原整体FAIL不改。54742/cec8af标准本地Schema20正反例实际race0/1.132s。90700/34e070仅native三个top的race编译/发现actual0，无body执行。首生成ddc1bf是write前Python匹配错误，后已正常落源。
- 独立Runner：0b74首五路径/e48d7a只读接受；abcfdd实际指出40个HEAD错误Schema错误声明body，窄修后cc7bba原红控制转绿，GET/通用Problem/HEAD200/DTO未变。75434/0db8c1两个实际pure风险控race0/1.023s，覆盖真实handler AgentRun安全投影/坏末项零候选，以及原Body.Close后仍等待held AfterFunc实际return与无提前清deadline。限定纯接受，无剩余本轮mustfix；不是正式Owner/SQL/native验收。
- 三新tests形成四top十四子：Metadata4、CurrentAuthority5、Transactions3、CommitUnknown2。正式Account Bootstrap/Invitation/Redeem/Login/Logout，实际B02发布/树/删除、五GET/HEAD/Schema/无读事实，两个真实User SH/EX顺序与原SELECT取消/Tx退出，complete-frame两种原COMMIT Unknown零候选。Project初始化/转Owner/Deleting为明确上游测试SQL事实，Deleting保原门/manifest/participants/FK，不冒Project.Create/BeginDelete/Skills结果。
- 一次定向integration race-c：95050/ddfc46 actual0；cb0358精确list actual0恰四top、未执行body。候选`output/ai/knowledge-owner-read/knowledge-owner-read-http-race.test`为40,566,169B。首同process UTC2026-10-09T23:11:16.199609+00:00 available5829890048B，末5763674112B；没有命令/资源在途，不建新cache。旧首pure阶段全机波动约438MB不可归因单缓存，已按协调暂停并获本次定向编译授权。
- 原两root工具仅新增一个封闭四top入口/expected集；driver另纳入实际运行需要的本地Schema helper、两Schema JSON与当前Python解释器4项input，显式传该同一解释器。原所有input、7资源、Go6m/root540+60+3/TCP75/全Wait/双尾保持，无TCP诊断算法移植。559377 pure controls actual0：逆去精确增量全文=bb98、config1正6负、observer22格每格14资源替身。资源/时间为明确替身，未main/PG/socket；不能冒真实尾。Runner 1f7561复跑原控制actual0，d2262e独立输入闭包/解释器替换/遗漏负控actual0，入口有限接受。该入口与独立Project Cleanup入口审查分别已保存c4b13347/391f0cb7。root已本地复制固定MinIO并核109,289,632B及既定SHA；没有下载或真实运行。

## 可复用命令与剩余门

以下环境固定；PATH必须保继承值后仅前置Go。GoMod共享只读，原Knowledge独占cache：

```sh
export PATH=/workspace/toolchains/go1.27.1/bin:$PATH
export GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOTELEMETRY=off GOMAXPROCS=2
export GOMODCACHE=/workspace/agenteam/output/ai/model-ui-recovery/go-mod
export GOCACHE=/workspace/agenteam-knowledge/output/ai/knowledge/go-cache
```

已执行编译为`go test -tags=integration -mod=readonly -p=1 -race -c -o output/ai/knowledge-owner-read/knowledge-owner-read-http-race.test ./tests/knowledge`，外层timeout5m；list为候选`-test.run=^$ -test.list='^TestKnowledgeOwnerReadHTTP(Metadata|CurrentAuthority|Transactions|CommitUnknown)$'`。入口纯控：`python3 -B .agent-state/knowledge-owner-read/root-selector-controls.py`。

后继PG仅在root fresh grant后启动。当前输入与未用输出（219cb6只读确认）如下；不生成第二份manifest，原driver的input_paths仍负责完整闭包：

| 输入/输出 | 固定值与范围 |
| --- | --- |
| 生产/测试/候选 | HTTP源b6635c27、Account fixture 1ee8b7e3、四PG矩阵bb98b6cd；候选40,566,169B，95050/ddfc46编译，cb0358恰四top发现 |
| 实际入口 | 本树root_chain_driver.py与pg_only_supervisor.py；c4b13347已验PG增量，当前新增native分支只在nonroot指定selector触发 |
| Go/cache | Go1.27.1、原Knowledge独占GOCACHE、共享只读GOMODCACHE；继承PATH前置Go，driver原GOFLAGS=-mod=readonly -p=2 |
| 固定MinIO | output/ai/deps-minio/bin/minio；109,289,632B；SHA dc5298474f0bc87a068f0b1135c583bb1278c17c11c512212ed7644a238c89c8，root已核 |
| 运行时Schema | 实际Python /opt/codex/runtimes/codex-primary-runtime/dependencies/python/bin/python3.12；schema-controls.py、knowledge-owner.json、common.json；都进入原input gate |
| PG输出 | output/ai/knowledge-owner-read/pg，当前absent；由本轮监督器创建，禁止复用旧父目录自动重跑 |
| 其余原闭包 | binary、Go、MinIO、两个工具、go.mod/go.sum、三个shell、原生产Go及embed/迁移；全部原input项保留 |

可复制启动命令（未执行；cwd必须本树；首同process fresh stat/UTC，低于5GiB退出78）：

```sh
python3 -B - <<'PY_RUN'
from datetime import datetime, timezone
import os
from pathlib import Path
import subprocess
import sys
repo = Path('/workspace/agenteam-knowledge-http')
os.chdir(repo)
v = os.statvfs(repo)
available = v.f_bavail * v.f_frsize
print('UTC', datetime.now(timezone.utc).isoformat(), 'available', available, flush=True)
if available < 5368709120:
    raise SystemExit(78)
output = repo / 'output/ai/knowledge-owner-read/pg'
if output.exists() or output.is_symlink():
    raise SystemExit('STOP output parent must be absent')
go = '/workspace/toolchains/go1.27.1/bin/go'
env = os.environ.copy()
env.update(PATH='/workspace/toolchains/go1.27.1/bin:' + env.get('PATH', ''),
           AGENTEAM_GO=go, GOTOOLCHAIN='local', GOPROXY='off', GOSUMDB='off',
           GOTELEMETRY='off', GOMAXPROCS='2',
           GOMODCACHE='/workspace/agenteam/output/ai/model-ui-recovery/go-mod',
           GOCACHE='/workspace/agenteam-knowledge/output/ai/knowledge/go-cache',
           AGENTEAM_MINIO_BINARY=str(repo / 'output/ai/deps-minio/bin/minio'))
version = subprocess.check_output([go, 'version'], env=env, text=True).strip()
if version != 'go version go1.27.1 linux/amd64':
    raise SystemExit('STOP fixed Go mismatch')
print(version, 'output_absent=True', flush=True)
os.execve(sys.executable, [sys.executable, '-B',
    '.agent-state/task-planning-recovery/pg_only_supervisor.py', '--root-chain',
    '--driver', str(repo / '.agent-state/work-owner-http/root_chain_driver.py'),
    '--binary', str(repo / 'output/ai/knowledge-owner-read/knowledge-owner-read-http-race.test'),
    '--run', '^TestKnowledgeOwnerReadHTTP(Metadata|CurrentAuthority|Transactions|CommitUnknown)$',
    '--output', str(output)], env)
PY_RUN
```

原业务每top含fixture cleanup上限120s，整个Go6m；不自动rerun或扩预算。PG完整Go/driver/outer actualWait、7ID双absent、private/runtime/desc/TCP双delta与inputsame才算整轮通过。七次独立Account/PG fixture setup未实测总时长，若不适合原6m则依实际有意义拆组。

native仍独立短窗，必须`AGENTEAM_KNOWLEDGE_HTTP_NATIVE=1`且精确`^TestKnowledgeHTTPNative(Deadlines|KeepAliveAndClose|BackpressureAndDisconnect)$`才运行真实socket；三个top六子已源码准备，未执行。默认无该env跳过。它只证明transport，正式权限依PG矩阵。完整有限结果仍须native与PG实际、入口/业务风险独审及真实尾；不把当前编译或test-only fixture当生产root可用。

## Native入口与候选（离线ready，未真实运行）

复用原 `.agent-state/work-owner-http/native_driver.go`，只新增 Knowledge 三top唯一exact selector和对应环境gate。原Go90s/driver105s/nonroot supervisor123+共享3s退休/TCP75不变；无Docker。监督器仅对该nonroot selector要求恰三父六子RUN与PASS，无缺项/额外项/重复/SKIP。原PG分支、旧native selectors与所有尾部保留，native driver+binary沿原input gate。测试自身join实际Serve/conn/handler；native只证明传输与资源生命周期，不替代真实Account/Owner/SQL证明。

作者离线97611e实际0共76控：64子集、7非法日志、缺文件、实际main4格（明确process/OS/time替身）；逆去native增量全文逐字391f0cb7。首7cc5ca控制在driver逆还原断言失败，是控制字符串遗漏移除else-if的闭括号，实际本次修正后转绿；原失败保留，不是native业务FAIL。5a3165原PG控制actual0，逆投影仅先去这段native增量，原config/22observer与运行时输入未改。Runner按bca64eb5有限独审接受，无must-fix：addbbc原76控actual0、0cbac4原PG控制actual0，edb28c独立实际main两格actual0（非法UTF8安全失败仍完整Wait/desc/reap/TCP/input尾、legacy Work不触新gate）。无Go/native/PG/socket，不冒六子业务通过。未新增监督框架。

已获root单次离线编译授权，60658/ef37a0→04e2a4 actualexit0：原fixed env/-p1/原cache下 `go test -mod=readonly -p=1 -race -c -o output/ai/knowledge-owner-read/knowledge-owner-read-native-race.test ./internal/central/knowledge/http`，再 `go build -mod=readonly -p=1 -o output/ai/knowledge-owner-read/native-driver .agent-state/work-owner-http/native_driver.go`，共用timeout5m，随后候选 `-test.run=^$ -test.list='^TestKnowledgeHTTPNative(Deadlines|KeepAliveAndClose|BackpressureAndDisconnect)$'` 恰三top。源码组合为原b6635c27 native/production与bca64eb5 driver，候选18,847,132B、driver4,857,127B；原PG候选仍40,566,169B。首同process UTC2026-10-09T23:29:19.619180+00:00 available6,130,180,096B，末23:29:23.796354 available6,106,140,672B；全机净变化24,039,424B，仅记采样、不作缓存归因。实际body=0，没有命令/资源在途，不把无env跳过冒成功。

真实native另等fresh grant，输出父 `output/ai/knowledge-owner-read/native` 当前absent；同前述fixed env/stat/UTC/output gate，命令为 `python3 -B .agent-state/task-planning-recovery/pg_only_supervisor.py --driver /workspace/agenteam-knowledge-http/output/ai/knowledge-owner-read/native-driver --binary /workspace/agenteam-knowledge-http/output/ai/knowledge-owner-read/knowledge-owner-read-native-race.test --run '^TestKnowledgeHTTPNative(Deadlines|KeepAliveAndClose|BackpressureAndDisconnect)$' --output /workspace/agenteam-knowledge-http/output/ai/knowledge-owner-read/native`。必须原Go child/driver/outer实际Wait、三父六子全部PASS、runtime/private/desc/TCP双尾与inputsame齐；成功与失败均收完原尾。PG和native均未开始，不能宣称有限HTTP交付完成。
