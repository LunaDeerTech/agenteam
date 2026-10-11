# 普通 Project Variables 恢复与清理

状态：2026-10-11，完整清理资格审计、已提交历史及运行材料保全和原树/引用清理均已完成。这里不授权重跑旧失败或恢复既有 STOP。

## 已交付范围

原树 `/workspace/agenteam-project-variables-resume`，分支 `ai/project-variables-resume-20261011`，tip `352be73e28762b22ee15f84fa3a7d542c6e7b4e0`。普通变量目录、详情、创建、编辑、明确空值及成功后当前回读已随 main `3e28f10a90927e7b1baeaf2f2312f9d22565f5e4` 交付；旧 current 的“待 Git 集成”不是尚未完成的产品修复。

从共同基线 `29666627a5685c59ad10205571ebf7f1ced447ef` 至原 tip，共 21 个提交、52 个修改路径，其中全部 49 个技术路径逐字等原 main 交付。核对时 main 为 `a2bae64651e94f413addc2f699eb3b05192476f5`，原交付是其祖先；49 个技术路径仍全部存在，32 个逐字不变，17 个沿后继正式交付修改，不能用旧树覆盖它们。

## 历史源码

材料位置为 `output/ai/retired-20261011/project-variables-resume/retention/`。`source-history-plan.json` 保留合并前清单，列出 21 个提交及全部 119 个历史 path/blob 版本；当时仅 58 个版本可从 main 历史恢复。合并后的实际核对见 `result.json`，现在全部 119 个版本均可从 main 恢复。

root 已完成并推送仅保留已交付开发历史的 `ours` merge `66d45ce1b56b2599cf0e6a7b7ddee6ec67d0a557`。两个 parents 为原 main `a2bae646` 和原分支 `352be73e`；结果 tree 与原 main 同为 `a8f4ebcbb4faad5b5664025dd8f2fe3fc2a6882b`，文件零变化。21 个提交均为单父线性链：前 11 个属于已交 Skills/Milestone 与恢复方法，后 10 个属于 Variables 和两次已验 Skills 窄同步；唯一外部祖先为 `29666627a5685c59ad10205571ebf7f1ced447ef`，没有额外未审侧枝。已逐项核全部提交和 119 个 blob main 可达，不依赖另一保护分支，也不需要 bundle、永久归档分支或标签。

## 原件与恢复限制

完整原 `output` 已通过同文件系统 `renameat2(RENAME_NOREPLACE)` 无覆盖迁入 `output/ai/retired-20261011/project-variables-resume/output/`，旧树 output 已不存在。原工具 50353 实际退出 0；`retention/result.json` 记录迁移前后全部路径、SHA、inode/device 与权限一致。迁移前清单 `retention/output-before.json` 包含 493 个普通文件、237826255 字节：原 Go binary、两个构建并存的 114 个 dist 文件、全部前端及方法 FAIL/PASS 日志、唯一 native 的调用和结果、13 份响应证明、两张 PNG、npm cache 与独立 MinIO 副本均保留。不能把当前混合 dist 目录重新解释为两份各自冻结的完整构建。

原 Go binary 为 `output/ai/project-variables-web/compile-01/project-variables-race.test`，79402223 字节；保全时观测 SHA256 为 `23c95e9452eeaac17c331643a934c9099e9a4670c95aaba230f239b91bfd012d`。这是本次观测，不能补称当时已经保存逐项编译输入清单。

`/tmp/pvr-v1/ui-cafa3797ce3f44cb.log`、其同名目录内 `owned.json` 与 `request.json` 的逐字副本保存在 `retention/external/pvr-v1/`；复制记录见 `external-copies.json`，原 `/tmp` 保持。原 native 只有正常 create/update/clear 一条：Go 17.43s、13 请求全部 finished、浏览器零 Lookup；原 Node/Go/driver/四 Chrome 的实际 Wait、七资源双轮及 descendants/private/runtime/TCP 双尾齐，原 log `inputs_unchanged=True terminal=0`，107.642s；外层 result 为 0、109.217s。旧作者亲见工具 3954 的退出事实不改成此次审计亲跑。

原 `unit-01` / `type-01` 编译失败、`unit-03` 同 Session 恢复丢 Unknown、`unit-04` 冲突丢输入、`native-prep/controls-01` 环境遗漏失败都保留。后续分段通过不回填成首轮通过。早期纯测和编译没有逐次输入 manifest 或独立工具终态文件；尤其 unit-03/04 的新失败控制到 `bb468f12` 才与产品修复一起提交，未保存失败现场完整文件字节，不能从最终测试或日志片段补造。完整历史合并只保证已提交版本可恢复，不保证上述未提交现场可逐字复原。

## 依赖与退出边界

当前四个活跃 owner 明确确认 Operation 不借此树源码、output、binary、dist、node_modules、cache、MinIO 或旧引用；使用本树/main、SkillHTTP 热 Go cache 和 SkillsMilestoneResume 的只读 modules/MinIO。静态检查中 28 个 symlink 均解析在原树内且无断链，两个 node_modules 是实体目录。其它树中发现的是旧清理盘点和后继说明，没有当前可执行配方硬依赖。

被动 `/proc` 可读部分最后未见目标路径持有，首个并行盘点快照有一个短暂未归属的只读 FD，后续消失；大量进程/FD 权限不可读，因此这不能单独证明宿主无活动进程。清理依据仍须结合原命令实际退出与 owner 停写确认，不以扫描代替原 Wait。两个 node_modules 可从已保留锁文件恢复；外部共享 Go cache/modules 不属于本树，不搬移或删除它们。

历史合并、无覆盖迁移及结果核对完成后，root 已普通移除原 worktree，本地 branch -d 实际成功；远端原 tip `352be73e28762b22ee15f84fa3a7d542c6e7b4e0` 精确 lease 删除工具 4661 实际退出 0，fresh 查询为空、prune 0。清理累计为 28 个 WorkTree / 29 个本地分支 / 59 个远端引用，旧待核 148 个、显式保留 1 个不变，见[主线 current](../current.md)。本片不含删除、UI 原命令重放、Agent/Runner 环境注入、生产 Project initializer 或完整 Variables 生命周期；原 STOP 不随清理解除。
