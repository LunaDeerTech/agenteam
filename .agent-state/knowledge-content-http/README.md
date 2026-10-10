# 正文 HTTP 有限入口

本工具片段复用本树既有 supervisor、root adapter 与 native driver，仅新增下列两个闭集 literal。旧默认与所有旧 selector 可逆为 `e3145974` 原字节，不造新的资源监督者。2026-10-10 已完成 PG/native race candidate 与原 native driver 的离线构建及 exact top 列举，原 root adapter `--check` 实际0；native01 三 top/六 sub 与原完整收尾实际通过，PG01 已实际整组 FAIL，原失败与完整收尾见下文。

| 模式 | 唯一新增 selector | 必须实际出现的节点 |
| --- | --- | --- |
| root chain，Account/B02/D05 真实 PG | `^TestKnowledgeOwnerContentHTTP(CurrentBytes\|CurrentAuthority\|ReaderOwnership\|ReadTransactions)$` | 4 top、14 sub，RUN/PASS各恰一次 |
| 非 root，原生 HTTP | `^TestContentHTTPNative(Deadlines\|KeepAliveAndClose\|BackpressureAndDisconnect)$` | 3 top、6 sub，RUN/PASS各恰一次 |

表格内反斜杠仅 Markdown 转义；下面 shell 参数中的 selector 无反斜杠。前者仍由原 `test-objects.sh` 链拥有 7 个资源与 3 个 private 目录。后者只有自身 listener/connection/handler，测试端口使用明确的 Account/领域替身，不是生产 root、SQL 权限或 D05 证明。

原预算不变：PG Go 6m/root 540s+TERM 60s+KILL 3s，native Go 90s/driver 105s/supervisor 123s+3s；原后代、资源双次 absent/private/runtime、actual Wait/reap、host TCP 75s 双次空与输入尾不变。新严格节点/Wait检查失败也继续原完整尾；native 要求原 manifest child 与 Start/actual Wait0 同 PID、原 runtime/private 成功且 tmp 实际不存在。PG 原 test Wait 必须恰一条 code0。

新分支输入包含真实 `tests/knowledge/*.go` fixture/业务来源、contenthttp、现行全部非测试生产 Go/模块/SQL/embed、原完整帧 commitproxy、新 Schema/common 与 schema-controls；根模式继续包含原 Go/MinIO/scripts 输入。PG 还冻结实际执行的 Schema Python：初始要求显式绝对文件路径及可执行权限，纳解析后真实文件的字节 hash，并保原环境路径字符串。结束时重新枚举路径集合再逐一 hash、重验该环境路径与实际可执行文件；新增/删除文件、解释器原字节/路径/符号链接目标变化、读取错误不能通过。native 不要求 Schema Python 环境。此输入比较只是运行一致性，不替代候选编译、源码方法或业务验收；不扩成 site-packages/modcache 全依赖快照。

无需 Go/资源的控制：

```sh
PYTHONDONTWRITEBYTECODE=1 python3 -B .agent-state/knowledge-content-http/selector-controls.py
```

首实际 `b17961` 通过148项，但独审 `7d1d04` 确认其未冻结运行时 Schema 解释器，原材料不回填。窄修后 `ace5f7` 通过157项：精确节点删/重/缺/错/skip/非法UTF8、固定配置正负例、实际 supervisor main 的显式 Child/资源/TCP/proc 替身，覆盖错误 Wait/退出码、资源/runtime残留、输入新增/删除、解释器字节/环境/链接目标改变仍走完整尾；三工具逐字逆差异。缺失/相对路径/目录/不可执行解释器在启动前拒绝。不调用实际 child/proc/PG/socket，也未证明真正节点可发现。

候选准备后，真实命令分别如下。每次必须 root 单独授予真实窗口，首次同进程打印 UTC/statvfs≥5GiB、核新输出及完整固定环境，禁止自动重试或借 pure/compiled 推 PASS。

2026-10-10 新环境固定前置：`PATH` 前置 `/workspace/toolchains/go1.27.1/bin` 并保留继承项，`GOTOOLCHAIN=local GOENV=off GOWORK=off GOPROXY=off GOSUMDB=off GOTELEMETRY=off GOMAXPROCS=2`；`GOMODCACHE=/workspace/shared/agenteam-deps/go-mod` 只读复用，`GOCACHE=$PWD/output/ai/knowledge-content-http/gocache` 独占。`TMPDIR` 与 `GOTMPDIR` 均为本树 `output/ai/knowledge-content-http/tmp`，`XDG_CACHE_HOME` 为同层 `xdg`。上述私有目录先准备；旧 `AGENTEAM_{OBJECT,OUTBOUND,PG,PG_UNSUPPORTED}_FIXTURE` 与 native gate 从环境移除，由原 driver 配置本次自己的资源。Schema Python 显式设为 `/opt/codex/runtimes/codex-primary-runtime/dependencies/python/bin/python3`，该本地解释器及 jsonschema/referencing 已确认存在。

第一次离线 PG 构建因 `/workspace/go/pkg/mod` 缺当前锁定版本实际失败，未生成候选。共享依赖恢复后用新编号重新构建 `content-pg-race-02.test`，首次 native 候选与 driver 仍用 `01`。构建/列举结果只记准备，不算行为通过。MinIO 已从 root 提供的 `/workspace/shared/agenteam-deps/bin/minio` 按下述原固定 SHA 一次确认后普通复制到本树 `output/ai/deps-minio/bin/minio`（109289632 bytes），driver 保留原身份检查；无需再下载。

离线命令均 actualWait=0：`go test -mod=readonly -p=1 -race -tags=integration -c -o output/ai/knowledge-content-http/content-pg-race-02.test ./tests/knowledge`、`go test -mod=readonly -p=1 -race -c -o output/ai/knowledge-content-http/content-native-race-01.test ./internal/central/knowledge/contenthttp`、`go build -mod=readonly -p=1 -o output/ai/knowledge-content-http/content-native-driver-01 .agent-state/work-owner-http/native_driver.go`。本次两个 package 均无 `TestMain`；分别对候选执行下列 exact selector 的 `-test.list`，实际恰4/3 top、无额外节点、退出0。构建与 list 原工具已退出，无真实资源。

```sh
python3 .agent-state/task-planning-recovery/pg_only_supervisor.py \
  --root-chain \
  --driver "$PWD/.agent-state/work-owner-http/root_chain_driver.py" \
  --binary "$PWD/output/ai/knowledge-content-http/content-pg-race-02.test" \
  --run '^TestKnowledgeOwnerContentHTTP(CurrentBytes|CurrentAuthority|ReaderOwnership|ReadTransactions)$' \
  --output "$PWD/output/ai/knowledge-content-http/pg-01"

python3 .agent-state/task-planning-recovery/pg_only_supervisor.py \
  --driver "$PWD/output/ai/knowledge-content-http/content-native-driver-01" \
  --binary "$PWD/output/ai/knowledge-content-http/content-native-race-01.test" \
  --run '^TestContentHTTPNative(Deadlines|KeepAliveAndClose|BackpressureAndDisconnect)$' \
  --output "$PWD/output/ai/knowledge-content-http/native-01"
```

PG 另须显式 `AGENTEAM_KNOWLEDGE_CONTENT_SCHEMA_PYTHON` 指向已存在本地 jsonschema/referencing 解释器；未配置即业务失败，不能把 Schema skip 当通过。root adapter 仍要求 MinIO SHA `dc5298474f0bc87a068f0b1135c583bb1278c17c11c512212ed7644a238c89c8`。Go 离线构建与真实资源窗口分别协调，不因准备完成自动启动 PG/native。

native01 已在冻结 `937fdff2` 的 root 独占窗口实际完成：上列 exact selector 三 top/六 sub 恰 RUN/PASS；Go 原 child Wait0、driver 原 Wait0、outer/tool53466 terminal0，runtime/private 真实空且移除、desc 双空、host TCP delta 双空、输入初尾一致，supervisor 68.033s/0。业务覆盖自然2s/较早父期限、同连接GET→HEAD越过旧期限、实际Body.Close错误零发布、原Write背压Timeout、断连后原调用实际退出。首执行剩余17883721728 bytes通过5GiB门。日志为 `output/ai/knowledge-content-http/native-01/pg-25e936e1c9304901b2013ae26ff2866c.log`，属可再生普通输出；该输出目录现已有结果，不自动复跑或覆盖。当前无自有socket/进程/临时目录残留，资源窗口已释放；PG、生产root和Object Runtime全局join未由此证明。

PG01 在冻结 `6afc1ae2` 使用上列原候选/selector 实际执行：4top/14sub 全部 RUN，CurrentAuthority/CurrentBytes/ReadTransactions 三 top 与 ReaderOwnership 的 EOF+D05 Close 子通过；两 GET 的 live lease 前置在 owner/delete 刺激前失败。原 SQL err=nil，active 数未打印，不能重建为0。Go111926、driver106927 和 outer106924/tool22417 实际 code1；七资源、三private、runtime/desc/TCP双尾与输入一致均齐全，supervisor181.839s/1，原窗口已释放。最小安全原材料为 [pg-first-failure.json](pg-first-failure.json)，原日志留在忽略的 output；整组不接受。

该轮首次 Go 前使用新 `pg-01-environment/config/go/telemetry/mode` 文件内容 `off\n`、`XDG_CONFIG_HOME` 指向该私有 config，并移除 `TEST_TELEMETRY_DIR`；仅 `GOTELEMETRY=off` 不足以控制实际 Go telemetry。新 `DOCKER_CONFIG` 仅含 `{}` 的 config.json，移除继承的 Docker context/host/TLS 变量，未读取外部凭据。首 UTC `2026-10-10T03:28:48.575893+00:00`，available15877173248≥5368709120。后继继续这些前置且使用新编号，禁止覆盖原输出或自动重试。

Skills 独静审确认 `newIntegrityReader` 对短对象在返回 reader 前同步预读、核 EOF 并释放原 lease；测试仅持上层 reader 未 Read/Close 不足以推出 active==1。root 已授权只把两 GET 真实正文改为>StreamBufferSize并核真实 Meta.ByteSize，保 active==1、原 owner/delete 刺激、零上层 Read、一次实际 Close 与最终无 reader lease；不改产品/HEAD/预算。修后候选与窄独审待完成，真实同组须 root 新授窗口。
