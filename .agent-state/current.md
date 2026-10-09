# 当前检查点

- A 已正式交付：main `8cb0a95338dc417ff34be06c00086fbbc7159efa`，范围仅 D10 Secret Variable 纯合同、独立 OpenAPI Schema及独立复验资产；不是整 Secret Owner 后端或生产能力。原分支 `ai/secret-variables-owner`，原基线main `3cea6076`；rev2 SPEC获Model有限接受，A另获Variables独立有限接受。
- 五个 `projectvariable/contract/secret_*` 源、三个对应测试、`api/openapi/secret-variables.json` 与独立复验资产均保持冻结。root已完成正式整合；本轮只关闭本current并纠正卡顶的过时状态。普通DTO/CommandName、Purpose.Valid、D04/Project/root共享源、SQL/迁移未由A修改；00028不占号。
- 已闭合 A：材料请求独立克隆/UseValue/Destroy、安全metadata/receipt、独立Secret命令与两态identity-only Lookup、专用Catalog事件、F1目录/引用 typed contracts与opaque issuer/锁/完整binding、闭合OpenAPI。没有Owner服务/HTTP实现、D04 provider或生产装配；F1实际authority与Store/Tx/当前权限须后继真实实现，不把plan形状当授权。
- 实际离线检查：新7top race `90452/c20c4b` actual0/1.092s；旧普通合同race `28070/cfbedb` actual0/1.084s；vet `bc2800` actual0。OpenAPI172本地ref解析 `6d3918` actual0，Draft202012本地registry38正负控制 `3ae145` actual0；schema无网络获取。首次仅编译 `56776/324761` enum重复FAIL已修；首schema `fc1edf` 错用无6小数Instant的夹具FAIL，改canonical夹具后通过。没有Secret PG/socket/browser/真实服务检查。
- Variables 独审原 `28456/6bc29f` actual1 确认六解码路径未知成员名泄入公开 Fault；`3738f6` schema 反例确认名称末尾换行被 `$` 接受，原失败保留。本次仅新 Secret 的 `secretFields/decodeSecret` 将未声明成员的错误路径投影为固定空路径，已知 schema path/错误码保留；包括 deleted DTO 与事件在内八 decoder 均使用它。Name 改为真实 EOF negative lookahead。第一轮新8top race `97569/3743f9` actual0/1.105s；补事件后最终8top race `20400/7e3ef9` actual0/1.120s（新回归8子，各正向+3未知成员，另已知path控制）。事件夹具初用不存在的 i.Operation，7143bd 编译FAIL，改本包 Operation 后通过；最终vet `f5a918` actual0（补充前fe3c96亦0）；Name schema14边界 `f25bfd` actual0。独立续验结果见下方；不复用旧通过掩盖红例。
- 可复制Go命令（cwd本树，独占Knowledge cache）：

```sh
env PATH=/workspace/toolchains/go1.27.1/bin:$PATH GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOTELEMETRY=off GOMAXPROCS=2 GOMODCACHE=/workspace/agenteam/output/ai/model-ui-recovery/go-mod GOCACHE=/workspace/agenteam-knowledge/output/ai/knowledge/go-cache /workspace/toolchains/go1.27.1/bin/go test -mod=readonly -p=1 -race -count=1 -run '^TestSecret' ./internal/central/projectvariable/contract
env PATH=/workspace/toolchains/go1.27.1/bin:$PATH GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOTELEMETRY=off GOMAXPROCS=2 GOMODCACHE=/workspace/agenteam/output/ai/model-ui-recovery/go-mod GOCACHE=/workspace/agenteam-knowledge/output/ai/knowledge/go-cache /workspace/toolchains/go1.27.1/bin/go test -mod=readonly -p=1 -race -count=1 -run '^TestVariable' ./internal/central/projectvariable/contract
env PATH=/workspace/toolchains/go1.27.1/bin:$PATH GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOTELEMETRY=off GOMAXPROCS=2 GOMODCACHE=/workspace/agenteam/output/ai/model-ui-recovery/go-mod GOCACHE=/workspace/agenteam-knowledge/output/ai/knowledge/go-cache /workspace/toolchains/go1.27.1/bin/go vet -mod=readonly -p=1 ./internal/central/projectvariable/contract
```

- 独立最终接受：Variables原四组race35659/9cf442 actual0；原六公开error-path红例修后24897/dbdbc0 actual0；新增deleted/event、原对象不变、known path与nested receipt反例79379/cb8b88 actual0；正式Schema83控ce6a22 actual0。独立者未参与实现，无剩余must-fix，结论仅纯合同/端口形状/schema。原FAIL未回填。
- 后继：Model在独立D04树基于main8cb实施producer SPEC/ports；D04 dedicated purpose/typed authority、kind3回执全生命周期/rotation/canary/CleanupProject/native Audit尚未交付。D10单final Tx事实/私有Outbox、Project/Audit/root与迁移由root分派，当前不修改共享契约或重做A。Owner后端真实矩阵仍待后继验收。
- 本实例其他树：B02 Process50756已完整PASS，其源冻结供Skills独验；D13 lexical-v2四数据独立语义复核977906有限接受，工具/后端不在该结论。当前无自有活动命令/真实资源。
