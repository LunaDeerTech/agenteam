# Registry 配置前置当前恢复点

- 树：`/workspace/agenteam-tool-registry`，分支 `ai/tool-registry`，基线 `728cd45a`。本树唯一实现者 cleanup，所有 Git 操作归 root。
- 首四路径已保存 `1d60e34f`，目录/Agent 引用及九项必要纯控已保存 `51d4b299`；两个编码问题修复已保存 `27a42f5e` 并获非作者有限静审接受。canonical bytea/完整回读与严格 Unicode 转义保留；原静态问题事实不升级或回填。
- 范围与工程契约见 [D18 卡 §10](../docs/development/work-items/d18-tool-name-projection.md#10-registry-配置前置实施中)。Registry 仅 Builtin metadata/current/配置目录及同 Tx Agent refs；无安装 Backend 时 production install-skill 未绑定。NameTable、执行器、既有 tools STOP 不变。
- Agent owner content 的 consumer-owned Tool contract 四文件已由 root 按 `34755aa5` 同步；Registry 未修改借用源。00032 归 Agent，00033 唯一归本树；真实迁移必须连续组合。
- `pure-01` 已原整轮通过：九项新 race（Go 698760 Wait0）及两包 vet（Go 699049 Wait0），session9986/outer698759 最终0；70输入不变、两组双absent/runtime双empty/adopted[]。原日志/结果在 `output/ai/tool-registry/pure-01/`，固定私有 telemetry/offline/readonly modules，各阶段 fresh≥5GiB，lifecycle 热cache 已完整归还。
- 未 PG/socket/实际资源。纯测试替身不证明真实 Backend、授权、迁移或 F1 完成。后继按新窗口组织连续00032/00033及真实PG/Agent组合；当前无在途自有进程或资源，技术源停写。
