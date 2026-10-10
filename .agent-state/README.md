# 多 worktree 保存与跨设备接续

这里保存继续开发所需的最小状态，执行原则见[团队流程](../docs/development/agent-team/README.md)。主线程（root）统一处理本会话获分配分支的 Git；指定子代理编写状态与必要文档。独立模块默认使用不同任务分支和 worktree，先约定模块关系、上下游契约、调用方式与集成责任，再并行实现。

## 状态归属

- **全局协调分支的 `current.md`**：保存最小活动任务索引，每项只记分支名、任务、负责人或活跃会话归属、上游提交或分支与版本、集成状态、下一步；待清理或保留项在此注明真实用途、owner 及原因。使用实际指定的协调分支，不另建永久台账；只更新变化，不搬运各分支流水。
- **各功能分支的 [current.md](current.md)**：是该任务恢复细节的唯一来源，记录目标、进行中/阻塞/完成状态、分支与基线、上下游契约版本、必要文件、已完成/未完成、实际检查及失败限制、依赖与复现命令、下一步。分支有唯一活动写会话和状态文件作者。
- [任务台账](../docs/development/agent-team/tasks.md)继续保存正式总体产品状态，不复制各 worktree 进度。源码、测试 harness、probe 和不可再生的脱敏输入进入正式路径或受跟踪的 `.agent-state/<task>/`；原始日志和可重建产物放忽略的 `output/ai/<task>/`。

Git 保存分支中的文件和提交，**不保存本机 worktree 目录、代理 ID、进程或终端会话**。本地路径仅作提示，新云端环境可选自己的目录，从远端任务分支创建 worktree 后继续已保存工作，不能因目录不存在或默认 `main` 没有未完代码而重新开始。

## 每个分支分别 checkpoint

形成有意义成果或检查结论、子任务交付、长检查或等待前、暂停或回复用户前，负责人主动交可恢复片段；持续有改动时约每 5 分钟在下一个安全写入边界保存。只暂停本次保存路径的作者和后台写入，无关 worktree 继续；尚在运行的检查如实登记，不把 checkpoint 当作命令退出或资源清理。

状态作者更新该分支 `current.md` 后停写，root 核对分支、明确文件和已有暂存内容，再进入**该 worktree**调用现有[保存工具](../scripts/ai-checkpoint.py)：

```sh
task_tree=/chosen/path/task-name
(
  cd "$task_tree" || exit
  git status --short --branch
  python3 scripts/ai-checkpoint.py save --message 'wip: task checkpoint' -- path/to/source path/to/test .agent-state/task-name/probe.py
)
```

示例路径由 root 替换为本次实际文件。工具按调用时的工作目录定位 worktree，文件参数也相对该目录；仅使用脚本的绝对路径不会改变保存目标。一个调用只保存当前分支，不遍历其他 worktree。每个有待保存内容或未推送提交的功能分支均单独调用；协调索引有变化时，也在其分支单独保存。

工具依赖 Git 和 Python 3.9+，由 root 先确认所选树的状态内容与实际分支一致；工具不代写状态，也不检查会话归属或自动停写。实际行为如下：

- 仅接受有提交的 `ai/` 分支，拒绝分离 HEAD、进行中的合并/rebase/cherry-pick/revert 及未解决冲突。它不创建分支或切换 worktree。
- 自动纳入当前树的 `.agent-state/current.md`，包括首次新增；额外参数逐个列出新增、修改或删除的**具体文件**。仅状态变化或重试推送时可省略额外文件。
- 拒绝目录、忽略路径、符号链接、子模块和无关已暂存改动；不清理其他任务的 index。不要用 `git add .` 或强制纳入整个日志目录，必要材料先保存为仓库内普通文件。
- 有选定文件变化时提交 WIP 并普通 push 到 `origin` 同名分支；没有文件变化时不空提交，仍尝试推送已有提交。未完成、未编译或验证失败的源码可以保存，状态须照实写，不能称为 PASS 或正式完成。

必要源码、harness、probe 必须与所需版本、依赖和复现命令一并可恢复；只有文字摘要、失效临时路径或本机外部符号链接不够。凭据、私人配置及可重新下载的依赖不入 Git。

保存中途失败可能保留已暂存内容或本地提交，先核对状态再重试。push 失败时保留本地提交和其他改动，明确该分支“尚未远端保存”，在后续安全边界和离开前重试。远端出现新提交时，root 先 fetch，确认会话归属并保留双方工作，再协调整合；禁止 force push。没有新改动也要重试尚未推送的提交。

脚本检查 push 返回结果，不额外核对远端最终提交。root 对每个待保存分支分别检查结果，必要时用下面的查询核对预期提交仍可从远端获得；不能凭一次成功宣称所有树都已同步：

```sh
git -C "$task_tree" rev-parse HEAD
git -C "$task_tree" ls-remote --heads origin refs/heads/ai/task-name
```

正常单写情况下两者提交应一致；不一致先查明远端是否前进，不能覆盖。脚本显式推送同名分支，但不保证设置本地 upstream 或更新受限 fetch refspec 之外的 remote-tracking ref；恢复和比对始终显式指定分支。需要为新分支建立同名 upstream 时，root 可在确认归属后于该树执行 `git push --set-upstream origin ai/task-name` 并核对，不假设 `git branch --set-upstream-to` 在仅抓取 main 的配置下必然可用。只有必要文件已提交且对应 push 成功，才能说明该 checkpoint 可跨设备获取。这是运行中的 agent 在安全边界调用的保存机制，不是后台实时同步，突然断电前尚未保存的编辑不能保证恢复。

## 新环境先发现，再恢复

以下 Git 操作均由 root 执行。新 clone 使用已有仓库地址；已有 clone 先检查本地状态，不能直接切换或清场：

```sh
git status --short --branch
git branch -vv
git worktree list --porcelain
git log --oneline --branches --not --remotes
```

对列出的每个已有 worktree，分别检查 `git -C <该目录> status --short --branch`，识别 dirty 文件、当前分支、活动写会话和未推送提交。最后一条只反映本地已知远端信息，fetch 后再核对；远端缺失的本地任务分支也必须保留。保留现有改动，不用 reset、clean、强制切换或覆盖目录。需要保存时，按文件与分支归属协调其 root/作者先 checkpoint。

**显式 fetch 最新 `main` 和全部 `ai/*`，并 prune 已删除的远端跟踪引用**，避免 `--single-branch` clone 或受限 refspec 只取默认主分支：

```sh
git fetch --prune origin 'refs/heads/main:refs/remotes/origin/main' 'refs/heads/ai/*:refs/remotes/origin/ai/*'
git show origin/main:AGENTS.md
git show origin/main:docs/development/agent-team/README.md
git show origin/main:.agent-state/README.md
git for-each-ref --sort=-committerdate --format='%(refname:short)' refs/remotes/origin/ai/
```

这里的 prune 仅刷新上述范围的远端跟踪引用，不删除本地分支或任何 worktree。远端任务已删除而本地仍存在时，先按[清理操作与跨环境接续](#清理操作与跨环境接续)核对交付、归属和独有成果；不要直接运行 checkpoint 或重新 push 旧分支。

先采用最新 `origin/main` 的工作规范，并按任务补读其相关技能；旧功能分支的规范不能覆盖新规范。代码仍以任务记录的基线和已保存进度恢复，读取最新规范不等于把 `main` 自动合入所有任务。

既有会话在下一次接续或派工时也核对最新治理版本。执行 owner 评估本次需要同步的工作流文档、相关 `.agents/skills/` 技能与 `.codex/agents/` 角色配置及兼容范围，root 只按明确范围执行 Git 同步，冲突交文件 owner 处理；不盲目合入整个 `main` 或覆盖在制工作。尚未同步时，派工明确携带最新治理提交及适用规则、角色和技能的必要内容或可读取的版本引用，避免新实例沿用旧树指令。读取 `main` 不会自动刷新本地配置、当前会话 prompt 或已运行实例；按运行时实际支持重载或新建实例，核对实际加载结果，不能确认时如实说明限制。发现旧生效指令与新规则冲突时，暂停受影响派工并刷新，确认一致后恢复；派工文字不能证明旧指令已被覆盖。

读取已知协调分支的最小索引，再读取匹配目标的任务状态；首次发现时按候选分支查看，不按最新提交时间直接选任务：

```sh
task_branch=ai/task-name
git show "origin/$task_branch:.agent-state/current.md"
git log --oneline "$task_branch" --not "origin/$task_branch"
git log --oneline "origin/$task_branch" --not "$task_branch"
```

最后两条仅在同名本地分支存在时执行，识别双方未包含的提交。根据用户目标、状态、契约版本和会话归属选择进行中/阻塞任务，跳过已正式集成且完成的任务；目标确实无法判断时再合并澄清。

**原 Codex 会话仍在推进时，不接管同一分支。** 为不冲突的新目标从已确认基线另建唯一 `ai/<task>-<session>` 分支和 worktree，记录各自归属，不抢写原分支 `current.md` 或 Git。只有明确交接并确认原写者及相关后台写入停止后，才能由接任 root 接管同一分支；同一时刻仍保持单写。状态看起来陈旧不能替代会话交接。

确定接续归属后，新环境从远端任务分支创建本地 worktree：

```sh
task_tree=/chosen/path/task-name
git worktree add --no-track -b "$task_branch" "$task_tree" "origin/$task_branch"
```

`--no-track` 不依赖默认 upstream 推断；创建新任务时从确认过的起点分支/提交建树也使用此选项，避免从 `origin/main` 起步却误跟踪主分支。显式 fetch 不修改仓库的持久 fetch refspec，后续仍须使用上述全量 `ai/*` 获取方式。

若本地任务分支已存在，先保留并比较本地与远端提交；未被其他 worktree 使用时，以 `git worktree add "$task_tree" "$task_branch"` 接续它。已经存在的 worktree 直接复用，不强制重复挂载同一分支。同步需要时由 root 做正常快进或明确整合，文件冲突交文件 owner 修复。

进入所选目录，读取任务 `current.md`、核对实际源码与必要材料，只恢复缺少的依赖，从明确下一步继续。旧代理 ID、进程和终端不会自动复活；重新创建合适执行者并恢复必要命令，先确认现存进程与自有资源归属，不能凭历史状态重跑或清理另一会话资源。环境说明见[恢复指南](../docs/development/agent-team/recovery-2026-10-08-environment.md)，其中旧机器路径须按本环境调整。

## 上游同步、联调与正式交付

功能完成优先：模块具备首条真实功能路径并通过轻量基础自测后，尽早在隔离的 `ai/integration-<task>` 候选分支做小范围真实联调；未完成的上游能力和后续绑定仍明确记录。集成前由子代理评估共享 API、迁移编号与顺序、仓库根配置、依赖锁文件及调用方影响，列出应采纳的上游版本和受影响验证；root 执行 Git 同步，冲突由文件 owner 处理。工作树隔离不能替代数据库、端口、缓存和 fixture 隔离。

候选分支的 `current.md` 记录实际组合版本、已通过/失败/未验证范围和下一条联调路径；协调索引只更新相应集成状态。保留权限、数据、事务、协议、竞争及恢复等高风险关键验证，不用接口声明、stub 或基础自测替代真实集成事实。

`main` 只接正式验收结果，不承接多个未验模块的组合试验。验收后由 root 整理 WIP 为完整结果的 Conventional Commit，将实现、测试、必要文档和状态一并交付；不另造 `docs archive` 或补哈希提交。状态作者按该分支的当前完整任务范围记为完成并指出下一步，协调索引仅同步变化；`main` 推送确认后转入下述清理，避免下次恢复重复已完成任务。

## 清理操作与跨环境接续

触发时机、授权与成立条件统一见[已完成分支与 worktree 清理](../docs/development/agent-team/README.md#已完成分支与-worktree-清理)。以下由 root 针对**一个已明确归属的候选任务**执行，不提供按日期、名称或 merged 列表批量删除的命令。

1. **发现与核对归属。** 先按上文检查所有本地 worktree、分支及未推送提交，再显式 fetch 最新 `main` 与 `ai/*`。读取最新协调索引和候选分支 `current.md`，确认没有复用为新任务、其他会话或上下游仍在使用，也没有待接续的阻塞工作；离线或无法联系的 owner 不能推定为已停写。内容与材料的核对交执行者，root 核对其结论及 Git 状态。
2. **确认完整交付与材料。** 对照本次实际范围、主线交付、必要材料的位置和仍保留远端引用的用途。`--merged`、左右独有提交数、`close` 标题或单一文件 diff 均不能独立证明可删；对 squash 后的独有 WIP 提交，结合交付记录与限定内容比较，区分已交付内容、仍需材料与无须保留的流水；不要求永久保存重复 WIP、纯关闭状态或可再生日志。dirty、untracked、ignored 的关键材料或未推送提交先保留并处理，不以 worktree 看似干净代替材料检查。
3. **停止依赖并逐对象清理。** 确认相关作者与后台写入停止，自有命令、进程及资源已结束或明确迁离；停止 Git 写入并不代表进程已退出。root 从另一个保留的 worktree 逐个移除已核清的 worktree，再删除不再需要的本地分支和远端分支；每一步失败或状态变化就停在当前结果，不强制绕过。
4. **核实并记录结果。** 用 `git worktree list --porcelain`、本地引用查询和 `git ls-remote --heads` 分别核对实际结果；远端缺失只有在查询成功且对应引用为空时才成立。部分失败、离线目录或必要保留项仍记在已有协调索引并注明原因，全部完成后移出活动索引；交付简报记录已清理与保留对象，不新增永久清理台账。

root 可在明确设置实际目录与单一分支后，用以下只读查询辅助核对；命令输出不能替代 owner 的停写、完成及材料结论：

```sh
repo_tree=/chosen/path/retained-worktree
task_tree=/chosen/path/completed-task
task_branch=ai/completed-task
git -C "$repo_tree" worktree list --porcelain
git -C "$task_tree" status --short --branch --untracked-files=all
git -C "$task_tree" ls-files --others --ignored --exclude-standard
git -C "$repo_tree" log --oneline "$task_branch" --not "origin/$task_branch"
git -C "$repo_tree" ls-remote --heads origin "refs/heads/$task_branch"
```

比较命令仅在相应本地分支和远端跟踪引用均存在且已刷新后使用；远端缺失时直接保护本地成果并查明原因，不把查询失败或不存在的引用当作无独有提交。ignored 列表只用于定位可能丢失的材料，不读取凭据或私人配置；不可再生的必要材料由 owner 精简、脱敏后保存，再重新核对。

所有条件成立后，逐个使用 `git -C "$repo_tree" worktree remove "$task_tree"` 和 `git -C "$repo_tree" branch -d "$task_branch"`。`-d` 是否通过不是主线交付证据；如仅因 squash、cherry-pick 或原子整合未形成祖先关系而被拒绝，执行负责人已核实全部实际成果和必要材料可从 `main` 或明确保留的远端引用恢复，且无独有未保存成果、无任何 worktree 挂载或活跃依赖时，root 可对**这个已核定的冗余本地引用**使用 `branch -D`。该例外只删除冗余引用，不授权丢弃改动、未交付内容、未保存证据或强制移除 worktree；任何条件不确定就保留。

删除远端前，root 再次 fetch 候选引用并用 `ls-remote` 核对实际 tip，确认与清理结论的输入一致且没有活跃 writer，随后才逐个执行 `git -C "$repo_tree" push origin --delete "$task_branch"`。普通删除命令不会锁定上一次查询的 tip；预先查询也不能替代明确的单写归属与停写。发现远端前进、归属变化或无法排除并发写入时停止清理并重新核对，不强推覆盖，也不把远端缺失的旧任务自动重新发布。删除后再次查询远端并显式 fetch/prune 刷新本地远端跟踪引用。

远端删除不影响其他机器或离线环境的本地分支、worktree 和文件。该环境下次恢复时先保留本地改动，读取最新工作规范、协调索引和主线交付，按上文 fetch/prune 清除过期远端跟踪引用。已完成且已删除的旧任务不再执行自动 checkpoint/push；有独有未保存成果时先核归属并另行保存，需要新增工作时从确认基线建立新任务分支。只有本机对应对象重新满足清理条件后，root 才移除其本地 worktree 与分支；不把 remote prune 或 `git worktree prune` 当作删除其他环境目录、结束进程或保存成果的手段。
