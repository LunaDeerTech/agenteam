# Runner Command owner 当前状态

- 树：`/tmp/agenteam-runner-command-owner`；分支由 root 管理，初始 main 基线 `b6f158c3`。
- 任务：Linux 单次可信 cwd / argv owner；本地真实 Start、Wait、输出管道、取消、Stop/Drain 与 filesystem/workspace 桥。见[专属卡](../docs/development/work-items/d16-runner-command-owner.md)。
- `recovery_lead` 独占 command 包与本卡/current；`skills_milestone_review` 独占 filesystem/workspace 新 command 桥和必要控制；root 唯一 Git / 资源排期；principal 独立只读审查。
- 固定技术/方法 `2664c9c6` 的首次 basic01 已实际完成：race 14 个行为 top / 20 sub PASS，3 个 helper 仅 Skip，三包 vet 0；1,111 项实际输入 before / after-unit / after 相同。原工具20601亲收actual0（chunk2709b5），wrapper850742 0/19.852s；各原Popen Wait与本SID两次空观察齐，Go/cache窗口已释放。唯一主审已最终有限接受，可交main；尚不提前记正式合入或本树清理。
- 完整output已实际跨文件系统复制并核验至main `output/ai/retired-20261012/runner-command-owner`：52文件/1,996,308B/5目录，原dev34→副本dev27，内容与mode/uid/gid/nsmtime相等，副本inode不同，原output仍完整保留。旁 `runner-command-owner-preservation` 保存精确证明；44个仓库输入版本等tested2664且任务历史可达，main完整history可达待root合入后核。详见[方法与恢复说明](runner-command-owner/README.md)，本树/分支尚未清理。
- 范围仅Linux trusted-host本地argv/cwd桥；无 macOS 验收、WSS / PG / 迁移 / Central 授予 / 默认绑定 / shell / Secret 环境。完整初始化、ready、生产 nil executor、原 tools HTTP A/B、Object join / SPA / 来源获取等既有 STOP 不变；不把 trusted-host cwd 校验当进程 sandbox，不声称可证明逃逸整树退出。
- 主线全局任务及原 FAIL 恢复记录沿正式main索引；root已确认清理累计35 WorkTrees /36本地分支 /70远端引用，141待核与keep1不变，不含本Command树。本任务current不替换主线索引，其它在制事项和保护树保持。
