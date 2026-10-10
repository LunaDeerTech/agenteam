# Skills Human Owner 目录与详情 HTTP

工作树 `/workspace/agenteam-skills-owner-http`，分支 `ai/skills-owner-http`，基线正式 main `3b7ed9da`。唯一作者 Runner；Git 保存与真实资源窗口由 root 负责。

当前可恢复片段：新 [短规格](../docs/development/work-items/d10-skills-owner-read-http.md) 已固定两 GET/HEAD、空 query/body、安全八字段、真实 P2 ListSkills/GetSkill 与当前身份/Project Read gate、原 2s/I/O 尾。Skills 对短合同只读核真实P2/Project/Account、字段上限与3链接（45bf1c）有限接受，无must-fix；不是产品实现验收。现独立产品3源 handler/wire/io、5纯测试源、Schema及其本包testdata/schema.py已形成，固定gofmt/JSON解析完成；本包初次pure/race实际40059→a88f0b exit0：11top/47sub/0skip，标准Schema21控及18HEAD无体状态通过，原日志 `output/ai/skills-owner-http/pure-race-01.jsonl`。没有 PG/native/HTTP 资源。作者在授权新包内实现，不改 P2/Project/Cleanup/root/迁移。四停项不解除。

允许新 `internal/central/skill/http/**`、专用 `api/openapi/skill-owner.json`、`tests/skills/owner_http*`、短卡/current；不含包流/安装/分配/UI。原完整 P2/00027 的适用实际结果可复用，不把它提升为本 HTTP 已通过。

root 已交接唯一可写热 GOCACHE：`/workspace/agenteam-project-variables-independent/output/ai/project-variables-independent/gocache`；Skills 确认无在途或排队 writer。只读 GOMODCACHE：`/workspace/agenteam-runner-control/output/ai/runner-control/go-mod`。后继离线 Go 固定 `/workspace/toolchains/go1.27.1/bin/go`，PATH 前置并保原 Node，GOTOOLCHAIN=local、GOPROXY=off、GOSUMDB=off、GOTELEMETRY=off、`-mod=readonly -p=1`；同进程先打印 UTC/available 并要求至少 5,368,709,120B，否则不启动。不得新建大 cache；PG/socket/native 仍须 fresh grant。

下一步：本片段冻结给root checkpoint；鲜磁盘门通过后运行新包限定pure/race/vet，修正仅本域问题并保留失败。原I/O实现仅从正式Knowledge HTTP复制改package；传输控制复用其真实callback/领域调用尾方法，独立PG/native仍未实现执行。

离线验证：首次同process门采 2026-10-10T01:21:41.992595Z / 5,409,042,432B，通过后固定Go运行 `go test -mod=readonly -p=1 -race -count=1 -timeout=60s -json ./internal/central/skill/http`，显式 `AGENTEAM_SKILL_HTTP_SCHEMA_PYTHON` 指向启动包装的实际 `sys.executable`，实际终态0。随后vet预飞 e0a7ad 于01:22:24.529801Z采5,287,940,096B，exit78，未启动Go/vet；不据前一次门冒后一次通过。当前无在途命令/cache writer。产品3源与短卡未因纯控制改变，PG/native验收仍待。

后继稳定片段：新增 `internal/central/skill/http/native_test.go`（Linux TCP 3top/6sub）及 `tests/skills/owner_http_fixture_test.go`（PG2 组合 fixture）已 gofmt、未实际运行；后者尚未编译。native 只消费私有局部控制，包含原 2s/更早期限、真实同连接、Close错误和原领域调用尾；合法最大单Metadata背压通过SYN前设置接收buffer建立，未伪造大响应。PG fixture复用真实P2数据库初始化及既有受控Object，正式Account Bootstrap/Invitation/Redeem/Login/HTTPBoundary、Project gate；Project/Creation/ready是明确上游seed，不冒Create/D05物理发布。root已认可该最小边界，metadata HTTP零Object消费，原P2真实D05结果单列复用。

Skills对固定890d632f产品实现独立静审 eb56ef 有限接受，无must-fix：10技术源与该点相等、真实实例/参数、严格输入、完整安全投影/Unknown和原I/O尾。仅只读检查并复用作者pure，不冒本人Go或PG/native验收。后继native/PG不在该结论。

vet后继单次获root恢复授权：35671→45b0c1 actual0，首同process采2026-10-10T01:25:49.002528Z / 5,388,660,736B，固定同cache/env `go vet -mod=readonly -p=1 ./internal/central/skill/http`，日志 `output/ai/skills-owner-http/vet-02.log`。涵盖包内native源码类型检查，不执行socket；原vet首次门78保留。当前无在途命令、无自有真实资源。current与上述两新源冻结给root；另两PG业务matrix仍在新文件实施，未纳本片段。
