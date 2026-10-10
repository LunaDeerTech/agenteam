# Registry 配置前置当前恢复点

- 当前有效 regexp2 已统一为 v1.12.0；复用 AgentSystem `f8962895` 的同 core 5＋adapter 3 项 race 与两包 vet 实际 PASS，旧 v1.11 记录保留为原版本结果。

- 树：`/workspace/agenteam-tool-registry`，分支 `ai/tool-registry`，基线 `728cd45a`。Registry 实现归 cleanup；新增 `tool/builtin/skill_install*.go` 六文件归 work_ui，当前均停写。所有 Git 操作归 root。
- 首四路径已保存 `1d60e34f`，目录/Agent 引用及九项必要纯控已保存 `51d4b299`；两个编码问题修复已保存 `27a42f5e` 并获非作者有限静审接受。canonical bytea/完整回读与严格 Unicode 转义保留；原静态问题事实不升级或回填。
- 范围与工程契约见 [D18 卡 §10](../docs/development/work-items/d18-tool-name-projection.md#10-registry-配置前置实施中)。Registry 仅 Builtin metadata/current/配置目录及同 Tx Agent refs；无安装 Backend 时 production install-skill 未绑定。NameTable、执行器、既有 tools STOP 不变。
- Agent owner content 的 consumer-owned Tool contract 四文件已由 root 按 `34755aa5` 同步；Registry 未修改借用源。00032 归 Agent，00033 唯一归本树；真实迁移必须连续组合。
- `pure-01` 已原整轮通过：九项新 race（Go 698760 Wait0）及两包 vet（Go 699049 Wait0），session9986/outer698759 最终0；70输入不变、两组双absent/runtime双empty/adopted[]。原日志/结果在 `output/ai/tool-registry/pure-01/`，固定私有 telemetry/offline/readonly modules，各阶段 fresh≥5GiB，lifecycle 热cache 已完整归还。
- 未 PG/socket/实际资源。纯测试替身不证明真实 Backend、授权、迁移或 F1 完成。后继按新窗口组织连续00032/00033及真实PG/Agent组合；当前无在途自有进程或资源，技术源停写。

## install-skill v1 适配层

- [D21 固定契约](../docs/development/work-items/d21-skill-install-builtin.md)已保存 `94d96653`；六个 Builtin 源/测试及只读借入的 Skill `install_types.go`、`install_repository.go` 已保存 `e66ec018`。实现严格参数到原 Skill Package、不可变调用/scope/key、同步 typed adapter 和 receipt 投影；未生成 Operation/SkillID、签发授权或注册 Source，缺真实 Runtime/Service Agent 授权仍不能成为可执行 Tool。
- `skill-install-pure-01` wholePASS：2026-10-10 12:58:28–12:58:38 UTC，session90304→ca4805/outer743893 Wait0；恰六个新 top race（743896，6.620s）及单 builtin 包 vet（744057，3.297s）均 Wait0。两阶段 group/desc/runtime 双尾空、adopted[]，范围与方法输入前后不变；同启动 fresh5,497,159,680B、私有 telemetry off、离线只读模块，热cache已归还。
- 固定命令与结果保存在 `output/ai/tool-registry/skill-install-pure-01-launcher.py`、`output/ai/tool-registry/skill-install-pure-01/`。只验本次六项，不重旧 Registry/Skill/P2；控制中的 Operation/Service 为显式纯替身，未证明真实发布、Agent witness、Runtime 或 Backend 绑定。
- 非作者审发现完整 InstallReceipt 未交 Runtime 的缺口，已在 `7a3de50b` 两个 adapter 源/测试修复并获增量静审接受：成功返回私有 `SkillInstallResult`，完整 receipt 值与模型三字段输出分开显式投影；原 Service 成功实际返回后，仅纯校验/投影不受迟到取消影响，不抹去已提交事实。失败仍零结果，不造 Runtime 持久 writer。
- 修后 `skill-install-pure-02` wholePASS：2026-10-10 13:12:23–13:12:28 UTC，session81734→e54c68/outer758616 Wait0；仅两个原 adapter top race（758619，3.963s）及 builtin 包 vet（758713，0.544s）Wait0。group/desc/runtime 两尾空、adopted[]，11个限定输入及方法不变，同启动 fresh6,189,920,256B；原 launcher/result 在 `output/ai/tool-registry/skill-install-pure-02-launcher.py` 与 `skill-install-pure-02/`。热cache已交回，未运行其它四个不变 top 或任何真实资源。当前技术/记录停写，后继真实 Runtime、Service Agent 授权及 Source 注册仍未绑定。

## 标准 schema 核心

- `internal/central/tool/schema` 两源与依赖锁已保存 `48c7c054`，有限独审接受；固定 jsonschema/v6 v6.0.3（Apache-2.0）及 regexp2 v1.11.0（MIT），保原 x/text。Draft 2020-12、仅内存文档、实际引用/dynamic-anchor 闭包、精确数字及同步有界 regex；未绑定生产 Registry/Runtime。
- 冻结源码 `core-01` wholePASS：`go test -mod=readonly -p=2 -race -count=1 -timeout=90s -json -run '^TestToolSchema(ActualInstallDefinitions|Draft202012AndExactNumbers|StrictJSONAndOfflineResources|ECMAScriptBudgetAndCancellation|SafeProjectionAndConcurrentReuse)$' ./internal/central/tool/schema` 恰5top PASS，及 `go vet -mod=readonly -p=2 ./internal/central/tool/schema` PASS。2026-10-10 14:31:52–14:32:03 UTC，session68090→2e799d；race/vet/outer实际Wait0、group/runtime双空、adopted[]，热cache已归还。固定入口/原结果为 `output/ai/tool-schema/run-core-01.py` 与 `output/ai/tool-schema/core-01/result.json`。Runtime适配器后继仅在AgentSystem树新增，不把此结果外推为真实持久/执行授权链。
