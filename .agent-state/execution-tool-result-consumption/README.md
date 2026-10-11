# Execution Tool 结果消费恢复与清理

2026-10-11，本片完整开发历史已随 main `c84a1257` 合入。正式范围见[工作卡](../../docs/development/work-items/d22-execution-tool-result-consumption.md)：成功 read-file 原持久结果一次性写原 Execution 的 seq3、消费记录和版本；没有第二 Model、终态退休或默认生产绑定。9 top 分段 race、5 包 vet、原 native01 wholePASS 已独立接受。主目标 D01–D28/E01 与原 STOP 不因本片完成而解除。

## 原件映射

- 原树：`/workspace/agenteam-execution-tool-result-consumption`，原分支 `ai/execution-tool-result-consumption-20261011`；最终任务文档提交 `f20f52b1`，全部历史已在 main 可达。
- 原 `output/ai/execution-tool-result-consumption/` 已通过同文件系统 `renameat2(RENAME_NOREPLACE)` 整体迁至 main `output/ai/retired-20261011/execution-tool-result-consumption/`。原 87 个目录项中有 75 个文件，共 92,995,060 字节；逐项 SHA-256、size、inode、device、mode 与根目录 inode/device 均保持。清单见该目录的 `retention/manifest.json`，新增保全说明和副本另列，不冒充原运行产物。
- 完整保留 `checks/tool-loop01`、`basic02`、`basic03`、`method01`、`compile01` 和 `native01`，包括原调用、实际 Wait、输入映射、唯一二进制、原失败日志及薄记录脚本。compile01 二进制 86,062,174 字节，SHA-256 为 `85cba434c9e2dad5ca6190630014767b9867e097257357a1632e07b13232309f`。
- `/tmp/etr01/pg-c3a6148f50c846438a01103bd33187f7.log` 及同 nonce 目录的 `request.json`、`owned.json` 三份必要原件已无覆盖复制至保全目录 `retention/external/etr01/`，逐字核对，原 `/tmp` 文件保持原位。配置/实际环境中的旧绝对路径是原调用身份，不改写为新运行通过。

## 历史输入与失败边界

首次 `tool-loop01` 只有真实 deps 查询成功，保守脚本闭包门退出 1，race/vet 未执行。原脚本版本 `6e6ac31a`、统一范围版本 `923cbad5` 与补验版本 `9f8bbfd7` 均 main 可达。

`basic02` 原 52198 整体退出 1：Tool/Loop 5 top 已通过，Execution 测试误用两个变量接收三个 Lookup 返回值而编译失败，vet 未启动。原输入提交 `7b005c95` 与修后 `basic03` 提交 `9f8bbfd7` 的该测试文件均已按原 manifest SHA 核合；映射见 `retention/source-history.json`。basic03 只补 Execution 4 top 及 5 包 vet，实际 0；不把首轮改写为全绿。完整 Git 历史保留该失败一行及后续修复，无需恢复补丁、额外 tag 或依赖旧分支永久存在。

原 native01 工具 54636 由执行者亲收退出 0，supervisor 734423 实际 0 / 138.224s；精确 1 top/2 sub PASS 46.62s。真实 COMMIT 后的受控回执丢失不冒充数据库物理未知。原 Go/driver Wait、7 个资源双次 absence、desc/private/runtime/TCP 双尾及输入不变全部齐，细节与限定结论保留在工作卡和原日志。上游 Operation 的旧失败仍沿其[独立恢复说明](../runner-read-file-operation/README.md)保留，不被本片通过覆盖。

## 依赖与当前清理状态

本线所有原工具及资源已退出，原树无源码 writer、正常 untracked 或剩余 output 文件；没有本树 node_modules、Go cache/module、dist 或私有共享二进制依赖。原 Go 执行只使用独立 SkillHTTP 热 cache、SkillsMilestoneResume 只读 modules/MinIO 与固定工具链，这些外部保护目录不随本片清理。后继源码应从 main 获取，后继证据只使用上述保全路径。

本片清理已实际完成：main `5905df69` 推送工具 83333 实际 0，保留完整 `c84a1257` / `f20f52b1` 开发历史与保全说明；原工作树普通移除成功，本地分支 `-d`（原 tip `f20f52b1`）成功。远端 fresh tip `0bf996ab156b16ab16ca10fe34a5cc321775ab77` 经精确 lease 删除，工具 37215 实际 0，再 fresh 查询为空、prune 0。上述旧目录和引用只作为原件身份保留，不再是活动依赖。Operation 另片此前也已完整清理；全局累计现为 30 个 WorkTree、31 个本地分支、62 个远端引用，另 147 个旧引用待核、1 个显式保留不变。
