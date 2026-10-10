# Skills Human Owner 目录与详情 HTTP

工作树 `/workspace/agenteam-skills-owner-http`，分支 `ai/skills-owner-http`，基线正式 main `3b7ed9da`。唯一作者 Runner；Git 保存与真实资源窗口由 root 负责。

当前可恢复片段：新 [短规格](../docs/development/work-items/d10-skills-owner-read-http.md) 已固定两 GET/HEAD、空 query/body、安全八字段、真实 P2 ListSkills/GetSkill 与当前身份/Project Read gate、原 2s/I/O 尾。Skills 对短合同只读核真实P2/Project/Account、字段上限与3链接（45bf1c）有限接受，无must-fix；不是产品实现验收。现独立产品3源 handler/wire/io、5纯测试源、Schema及其本包testdata/schema.py已形成，固定gofmt/JSON解析完成；尚无本任务 Go 编译/测试或 PG/native/HTTP 资源。作者在授权新包内实现，不改 P2/Project/Cleanup/root/迁移。四停项不解除。

允许新 `internal/central/skill/http/**`、专用 `api/openapi/skill-owner.json`、`tests/skills/owner_http*`、短卡/current；不含包流/安装/分配/UI。原完整 P2/00027 的适用实际结果可复用，不把它提升为本 HTTP 已通过。

root 已交接唯一可写热 GOCACHE：`/workspace/agenteam-project-variables-independent/output/ai/project-variables-independent/gocache`；Skills 确认无在途或排队 writer。只读 GOMODCACHE：`/workspace/agenteam-runner-control/output/ai/runner-control/go-mod`。后继离线 Go 固定 `/workspace/toolchains/go1.27.1/bin/go`，PATH 前置并保原 Node，GOTOOLCHAIN=local、GOPROXY=off、GOSUMDB=off、GOTELEMETRY=off、`-mod=readonly -p=1`；同进程先打印 UTC/available 并要求至少 5,368,709,120B，否则不启动。不得新建大 cache；PG/socket/native 仍须 fresh grant。

下一步：本片段冻结给root checkpoint；鲜磁盘门通过后运行新包限定pure/race/vet，修正仅本域问题并保留失败。原I/O实现仅从正式Knowledge HTTP复制改package；传输控制复用其真实callback/领域调用尾方法，独立PG/native仍未实现执行。
