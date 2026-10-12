# Runner Command owner 当前状态

- 树：`/tmp/agenteam-runner-command-owner`；分支由 root 管理，初始 main 基线 `b6f158c3`。
- 任务：Linux 单次可信 cwd / argv owner；本地真实 Start、Wait、输出管道、取消、Stop/Drain 与 filesystem/workspace 桥。见[专属卡](../docs/development/work-items/d16-runner-command-owner.md)。
- `recovery_lead` 独占 command 包与本卡/current；`skills_milestone_review` 独占 filesystem/workspace 新 command 桥和必要控制；root 唯一 Git / 资源排期；principal 独立只读审查。
- 当前仅源码与 gofmt，尚无 Go 编译、测试或真实进程验证。下一步冻结必要源码/控制，再按 root 窗口运行本片范围。
- 无 WSS / PG / 迁移 / Central 授予 / 默认绑定 / shell / Secret 环境。完整初始化、ready、生产 nil executor、原 tools HTTP A/B、Object join / SPA / 来源获取等既有 STOP 不变；不把 trusted-host cwd 校验当进程 sandbox，不声称可证明逃逸整树退出。
- 主线全局任务、清理累计和原 FAIL 恢复记录沿正式 main 当前索引，本任务 current 不替换主线索引。
