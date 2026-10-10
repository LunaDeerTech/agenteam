# Skills Human Owner 目录与详情 HTTP

工作树 `/workspace/agenteam-skills-owner-http`，分支 `ai/skills-owner-http`，基线正式 main `3b7ed9da`。唯一作者 Runner；Git 保存与真实资源窗口由 root 负责。

当前可恢复片段：新 [短规格](../docs/development/work-items/d10-skills-owner-read-http.md) 已固定两 GET/HEAD、空 query/body、安全八字段、真实 P2 ListSkills/GetSkill 与当前身份/Project Read gate、原 2s/I/O 尾。Skills 对短合同只读核真实P2/Project/Account、字段上限与3链接（45bf1c）有限接受，无must-fix；不是产品实现验收。现独立产品3源 handler/wire/io、5纯测试源、Schema及其本包testdata/schema.py已形成，固定gofmt/JSON解析完成；本包初次pure/race实际40059→a88f0b exit0：11top/47sub/0skip，标准Schema21控及18HEAD无体状态通过，原日志 `output/ai/skills-owner-http/pure-race-01.jsonl`。没有 PG/native/HTTP 资源。作者在授权新包内实现，不改 P2/Project/Cleanup/root/迁移。四停项不解除。

允许新 `internal/central/skill/http/**`、专用 `api/openapi/skill-owner.json`、`tests/skills/owner_http*`、短卡/current；不含包流/安装/分配/UI。原完整 P2/00027 的适用实际结果可复用，不把它提升为本 HTTP 已通过。

root 已交接唯一可写热 GOCACHE：`/workspace/agenteam-project-variables-independent/output/ai/project-variables-independent/gocache`；Skills 确认无在途或排队 writer。只读 GOMODCACHE：`/workspace/agenteam-runner-control/output/ai/runner-control/go-mod`。后继离线 Go 固定 `/workspace/toolchains/go1.27.1/bin/go`，PATH 前置并保原 Node，GOTOOLCHAIN=local、GOPROXY=off、GOSUMDB=off、GOTELEMETRY=off、`-mod=readonly -p=1`；同进程先打印 UTC/available 并要求至少 5,368,709,120B，否则不启动。不得新建大 cache；PG/socket/native 仍须 fresh grant。

下一步：已过 pure/race/Schema/vet 复用；新 PG/native 片段只作源码准备，待磁盘门与独立静审后编译/发现，再由 root 安排各自真实窗口。原I/O实现仅从正式Knowledge HTTP复制改package，传输控制保实际 callback/领域调用尾；不把源码已成形当实际通过。

离线验证：首次同process门采 2026-10-10T01:21:41.992595Z / 5,409,042,432B，通过后固定Go运行 `go test -mod=readonly -p=1 -race -count=1 -timeout=60s -json ./internal/central/skill/http`，显式 `AGENTEAM_SKILL_HTTP_SCHEMA_PYTHON` 指向启动包装的实际 `sys.executable`，实际终态0。随后vet预飞 e0a7ad 于01:22:24.529801Z采5,287,940,096B，exit78，未启动Go/vet；不据前一次门冒后一次通过。当前无在途命令/cache writer。产品3源与短卡未因纯控制改变，PG/native验收仍待。

后继稳定片段：新增 `internal/central/skill/http/native_test.go`（Linux TCP 3top/6sub）及 `tests/skills/owner_http_fixture_test.go`（PG2 组合 fixture）已 gofmt、未实际运行；后者尚未编译。native 只消费私有局部控制，包含原 2s/更早期限、真实同连接、Close错误和原领域调用尾；合法最大单Metadata背压通过SYN前设置接收buffer建立，未伪造大响应。PG fixture复用真实P2数据库初始化及既有受控Object，正式Account Bootstrap/Invitation/Redeem/Login/HTTPBoundary、Project gate；Project/Creation/ready是明确上游seed，不冒Create/D05物理发布。root已认可该最小边界，metadata HTTP零Object消费，原P2真实D05结果单列复用。

Skills对固定890d632f产品实现独立静审 eb56ef 有限接受，无must-fix：10技术源与该点相等、真实实例/参数、严格输入、完整安全投影/Unknown和原I/O尾。仅只读检查并复用作者pure，不冒本人Go或PG/native验收。后继native/PG不在该结论。

vet后继单次获root恢复授权：35671→45b0c1 actual0，首同process采2026-10-10T01:25:49.002528Z / 5,388,660,736B，固定同cache/env `go vet -mod=readonly -p=1 ./internal/central/skill/http`，日志 `output/ai/skills-owner-http/vet-02.log`。涵盖包内native源码类型检查，不执行socket；原vet首次门78保留。当前无在途命令、无自有真实资源。current与上述两新源冻结给root；另两PG业务matrix仍在新文件实施，未纳本片段。

后继两业务矩阵现冻结：`tests/skills/owner_http_test.go`、`owner_http_transactions_test.go`，4top/12sub 源码，gofmt 051635、静查 0894dd；尚未编译/执行。Metadata 和 CurrentAuthority 消费原 fixture 的真实 Account/P2/HTTP Boundary，核 GET/HEAD/Schema、严格请求、当前 Session/Owner、合法未初始化与 missing-publication 差异、Archived/Deleting；八业务表快照排除 Account 合法活动。Transactions 以原真实 Tx/PID 与精确 User SH/EX 两序，以及 Skill relation AccessShare/AccessExclusive 原 SQL 等待观测取消与 Tx 实际退役。CommitUnknown 复用现有完整帧代理，按原 `skill-read` JobCause 选择同物理只读 Tx，not-forwarded 与 committed-ack-lost 两方向保实际结果/上游 join，不发布候选；后续 GET 不确认旧 Unknown。没有改变产品或原 P2 fixture/代理。2026-10-10 当前只读盘采 5,324,996,608B 低于 5GiB，未启动新 Go 编译，不把这次采样当已运行的 exit78。两 matrix + current/card 给 root 保存；原 native/PG fixture 已存6fa6c733。
