# ToolCall 持久事实恢复

本片已随 main `95c092bc` 正式交付，完整开发提交历史由 main 保留。范围与原失败见[正式卡](../../docs/development/work-items/d22-execution-tool-call-facts.md)；不要把有限 PG 通过扩为真实 Model tools HTTP、ToolOperation / Runner 派发或生产绑定。

完整运行材料已从 `/workspace/agenteam-execution-tool-call-facts/output/ai/execution-tool-call-facts` 同文件系统无覆盖迁至：

`/workspace/agenteam/output/ai/retired-20261011/execution-tool-call-facts`

包含全部 checks、三套 compile 二进制 / inputs / 原 Wait、native01–03 的配方与结果，以及 integration01 并集检查。`retention/manifest.json` 保存原绝对路径、文件 SHA / size / inode 和新位置映射；`retention/result.json` 保存实际迁移核对结果。原文件内容与 inode 均保持，不回写旧配方路径或把后续重建当原运行。

`retention/external/etf01`、`etf02`、`etf03` 是原 `/tmp/etf01`、`/tmp/etf02`、`/tmp/etf03` 的逐字必要副本，含原 PG log、owned / request JSON 和 telemetry mode；原 `/tmp` 未删除。三个 native 分别为 FAIL / FAIL / PASS，原错误与完整退出尾保持。

| 原输入 | main 可达源码 | 实际边界 |
| --- | --- | --- |
| unit01 | `acff3e4d` | 四包 21 top 通过，Execution 私有接口编译 FAIL |
| unit02 / vet01 | `22be0912` | 只补 Execution 13 top / 10 sub，五包 vet 0 |
| native01 / compile01 | `c97af95e` | 默认 Model no-tools 捕获门 FAIL |
| model01 | `18a5c13b` | 25 PASS / 1 测试 scaffold FAIL |
| model02 / native02 / compile02 | `6e0b925c` | 单控制修后 PASS；native 的夹具 SQL / Runtime 依赖 FAIL |
| native03 / compile03 | `b395810d` | 真实 Runtime registry 和 SQL 类型修补后，两场景 whole PASS |

`retention/source-history.json` 已逐字核对：两轮基础清单各 100 项、两轮 Model 清单各 129 项，以及三轮编译的 1804 / 1806 / 1806 项已跟踪输入，均等对应 main 可达提交；每套编译另两项外部工具输入保持原 manifest 与产物身份。以上原源码均在 main 历史中，无需重造恢复 patch。恢复时以对应输入 manifest、实际 argv 与当时依赖为准；旧绝对路径表示原运行位置，迁移后的材料本身不是可直接重跑的现行配方。只读 modules、活跃热 cache 和 MinIO 仍由各自明确保留树管理，均未随本树保全搬移。重新执行需要新隔离工作树、当前依赖校验和 root 资源窗口，不重复使用旧 namespace 或覆盖原件。

本树所有原 Go / native 工具已实际退出，后继 Operation 只消费已提交 main 源码，无本树源码、产物或依赖绝对路径引用。root 已完成原树普通 `worktree remove`、本地 `branch -d`（完整历史 main 可达）及远端 tip `b395810d0641dbdad2324a4c5a62d4c5b9c4191f` 的精确 lease 删除；原工具 7230 实际 0，fresh `ls-remote` 为空、explicit prune 0，旧树已不在列表。清理累计为 27 个 WorkTree / 28 个本地分支 / 58 个远端引用；旧待核 148 个与显式保留 1 个不变，见[主线当前索引](../current.md)。
