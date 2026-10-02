---
name: agenteam-go-development
description: 在 agenteam 接到后端实现任务卡时使用，核对真实 Go 工程基线并按既定 Central、Runner、协议和数据库边界执行。
---

# agenteam 后端开发

## 适用任务

用于 `backend_worker` 执行主线程批准的 Go、协议或数据库任务卡。
只处理卡片授权文件，不顺带重构其他模块或改变生命周期。
完整任务卡应明确目标、文件范围、接口、步骤、必用技能、验收命令与预期、停止条件和交付格式。

## 按需读取来源

- 开始前读 [仓库规范](../../../AGENTS.md) 和 [团队流程](../../../docs/development/agent-team/README.md)。
- 读 [仓库结构与代码归属](../../../docs/development/repository-structure.md)。
- 从 [系统架构入口](../../../docs/architecture/README.md) 选择任务对应模块的架构与生命周期文档。
- Runner 任务按需读 [Runner 架构入口](../../../docs/architecture/runner/README.md)。
- 以实际源码、`go.mod`、Go 版本和任务卡为工程执行依据，不能以历史描述代替检查。

## 执行检查

1. 检查当前 `main` 基线和用户改动，保留他人文件及未提交工作。
2. 检查根目录 `go.mod` 是否存在、声明版本、实际 Go 工具链及相关源码。
3. 缺少 Go module 且任务卡未明确授权工程初始化时，报告缺少工程基线。
4. 不自行执行 `go mod init`、选择框架或增添依赖。
5. 按批准接口实现，保持 Central、Runner 与共享协议的依赖方向。
6. 只对指定 Go 文件运行 `gofmt`。
7. 根据任务卡执行适用检查并记录命令、环境和实际结果。

Runner 不得 import Central 业务包，共享通信契约放在 `internal/runnerprotocol/`。
Central 与 Runner 的具体实现分别留在各自目录，协议不承载 Central 业务模型。
数据库采用单一全局迁移序列；编号和既定迁移方式由主线程确认。
未确认迁移编号或接口时，不凭目录现状自行安排。
按已有约定传播 `context.Context`，包装错误时保留错误判断能力。
按已有约定关闭必要资源，不自行改动生命周期或添加额外重构。

仅在模块实际存在且任务卡指定时运行 `go test`、`go vet`、race 检查或 build。
需要数据库回归时，使用任务卡提供的真实隔离测试库和验收范围。
禁止接触未授权数据库；没有测试库时报告未执行及原因。
不能用本地单元测试成功代替真实数据库或部署运行证据。

## 阻塞与交付

未决接口、未授权的新依赖、范围扩展或同一失败出现两次时，停止受影响步骤并报告主线程。
不得创建下级 agent；允许只读 Git 检查（如 status、diff、log）；不执行 Git 写操作（暂存、提交、推送、reset、clean），不改变分支或 worktree。
交付实际修改文件、接口或行为变化、验证命令及证据。
分别列出通过、失败、未执行项和阻塞，说明缺失的工程或测试环境。
保留当前改动供主线程安排返修，不自行越界完成相关工作。
