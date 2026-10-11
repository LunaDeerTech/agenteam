# D16 Workspace 原子 write-file 本地库

状态：实现中；基线 main `7c874adc`，尚未编译或运行测试。variables_review 独占 filesystem/workspace 源码、相邻控制及本卡；root 负责 Git 与共享工具链窗口。

依据：[Execution Runtime §4/6](../../architecture/runner/execution-runtime.md#6-write-file)、[Workspace](d16-runner-workspace.md)。本片只完成可信 root fd → Manager Handle → 原子 Write → 原 Read 的本地调用；不新增 Tool、Control/WSS、数据库、shell/edit-file、生产默认 executor 或授权旁路，既有 STOP 不变。

## 本地合同

- `filesystem.Root.Write` 与 `workspace.Handle.Write` 接相对 path、原字节及显式 overwrite。空文件合法，单次最多 1 MiB；父目录必须已有，不自动创建。此上限是本地工程限制，不声明新 wire schema。
- Linux 从原 root fd 安全打开并固定父目录，拒绝 symlink、magic link 与子 mount；目标只允许缺省或普通文件。临时文件位于同一父目录，独占创建，完整写入并 fsync 后才原子 rename，再 fsync 父目录。新建/替换一律 0600，不继承旧 inode 的 mode/ACL，不就地 truncate。
- overwrite=false 使用内核 no-replace，竞争只能一个成功；true 是完整替换，非 compare-and-swap，并发覆盖按实际 rename 顺序。观察到非普通文件/链接即拒绝；最后瞬间目录项置换也不能导致跟随链接写其目标。父目录重开比较 dev/ino，已观察到的置换拒绝；可信宿主仍不是恶意宿主隔离/sandbox。
- 返回 `WriteResult{State,BytesWritten,Durable}`，出错时仍须检查。rename 前未发布为 not_committed；rename 实际成功为 committed，后续取消、目录 fsync 或 close 失败不抹掉事实；不能确定的 rename 错误保留 unknown。Durable 仅表示本次文件和父目录 fsync 均实际成功，不保证文件不会被后续写覆盖。
- Stop 禁新并取消在途；原调用仍持有临时文件/父目录直到所有原 syscall、清理和 close 返回，Drain 才能退出。取消不回滚已经发布的内容，不后台清理、不自动重试。临时文件清理失败须返回错误，不声称失败毫无副作用。非 Linux 沿原构造 unsupported。
- 材料仅本次调用私有副本，结束清除；默认 fmt/JSON/slog 不输出路径或内容。宿主权限错误使用固定脱敏错误。

## 必要验证

真实临时目录 Write→Read，空/二进制/上限、overwrite 与竞争、完整替换而无半文件、跨 Project/Agent 隔离；非法 path、symlink/非普通文件/父目录置换；受控真实 syscall 返回后的取消、fsync/close 失败与实际 Drain 等待；停止后禁新及请求/错误脱敏。仅两个受影响包 race/vet，待 root 授窗口后执行；不复跑 PG 或旧 Runner 全矩阵。
