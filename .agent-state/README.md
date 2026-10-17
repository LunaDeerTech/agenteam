# 多 worktree 保存与跨设备接续

这里保存继续开发所需的最小状态，执行原则见[团队流程](../docs/development/agent-team/README.md)。主线程（root）统一处理本会话获分配分支的 Git；指定子代理编写状态与必要文档。默认直接在 `main` 继续开发，先约定模块关系、上下游契约、调用方式与集成责任，再按唯一写者并行实现。需要隔离并行写入、保护其他会话或保存未验组合时使用短期 `ai/` 分支和 worktree，正式交付后清理，不保留长期前端重建分支。

## 状态归属

- **当前 `main` 或明确指定协调分支的 `current.md`**：保存最小活动任务索引，每项只记分支名、任务、负责人或活跃会话归属、上游提交或分支与版本、集成状态、下一步；待清理或保留项在此注明真实用途、owner 及原因。使用实际指定的协调分支，不另建永久台账；只更新变化，不搬运各分支流水。
- **各功能分支的 [current.md](current.md)**：是该任务恢复细节的唯一来源，记录目标、进行中/阻塞/完成状态、分支与基线、上下游契约版本、必要文件、已完成/未完成、实际检查及失败限制、依赖与复现命令、下一步。分支有唯一活动写会话和状态文件作者。
- [任务台账](../docs/development/agent-team/tasks.md)继续保存正式总体产品状态，不复制各 worktree 进度。源码、测试 harness、probe 和不可再生的脱敏输入进入正式路径或受跟踪的 `.agent-state/<task>/`；原始日志和可重建产物放忽略的 `output/ai/<task>/`。

Git 保存分支中的文件和提交，**不保存本机 worktree 目录、代理 ID、进程或终端会话**。本地路径仅作提示，新云端环境默认从远端 `main` 恢复；仍有用途的隔离任务从对应远端任务分支恢复，不能因旧目录不存在而重新开始。

## 每个分支分别 checkpoint

形成有意义成果或检查结论、子任务交付、长检查或等待前、暂停或回复用户前，负责人主动交可恢复片段；持续有改动时约每 5 分钟在下一个安全写入边界保存。只暂停本次保存路径的作者和后台写入，无关 worktree 继续；尚在运行的检查如实登记，不把 checkpoint 当作命令退出或资源清理。

状态作者更新当前 `current.md` 后停写，root 核对分支、明确文件、已有暂存内容与适用检查。当前[保存工具](../scripts/ai-checkpoint.py)只接受 `ai/` 分支，不为使用脚本而将 `main` 工作转到新分支。`main` 上由 root 使用明确路径提交并普通推送：

```sh
git status --short --branch
git add -- path/to/source path/to/test .agent-state/current.md
git commit -m 'feat: describe the completed change'
git push origin main
git ls-remote --heads origin refs/heads/main
```

示例文件和提交说明须替换为本次实际内容；没有文件变化时不空提交，已有未推送提交仍按原分支重试。提交与推送不代验收，未验组合需要保存时使用短期隔离分支。对实际使用的 `ai/` 任务分支，进入**该 worktree**调用现有工具：

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

默认读取 `origin/main:.agent-state/current.md` 并在确认唯一写会话后接续 `main`；有仍需恢复的隔离任务时，再读取对应分支的状态。首次发现时按候选任务查看，不按最新提交时间直接选任务：

```sh
task_branch=ai/task-name
git show "origin/$task_branch:.agent-state/current.md"
git log --oneline "$task_branch" --not "origin/$task_branch"
git log --oneline "origin/$task_branch" --not "$task_branch"
```

最后两条仅在同名本地分支存在时执行，识别双方未包含的提交。根据用户目标、状态、契约版本和会话归属选择进行中/阻塞任务，跳过已正式集成且完成的任务；目标确实无法判断时再合并澄清。

**原 Codex 会话仍在推进时，不接管同一分支。** 为不冲突的新目标从已确认基线另建唯一 `ai/<task>-<session>` 分支和 worktree，记录各自归属，不抢写原分支 `current.md` 或 Git。只有明确交接并确认原写者及相关后台写入停止后，才能由接任 root 接管同一分支；同一时刻仍保持单写。状态看起来陈旧不能替代会话交接。

确定接续归属后，`main` 工作直接使用主工作树。只有恢复隔离任务时，才从其远端任务分支创建本地 worktree，沿下节 helper 默认 sparse：

```sh
task_tree=/chosen/path/task-name
python3 scripts/ai-worktree.py create --branch "$task_branch" --base "origin/$task_branch" "$task_tree"
# 核对计划与唯一写会话后，由 Git 负责人实际创建。
python3 scripts/ai-worktree.py create --apply --branch "$task_branch" --base "origin/$task_branch" "$task_tree"
```

`--no-track` 不依赖默认 upstream 推断；创建新任务时从确认过的起点分支/提交建树也使用此选项，避免从 `origin/main` 起步却误跟踪主分支。显式 fetch 不修改仓库的持久 fetch refspec，后续仍须使用上述全量 `ai/*` 获取方式。

若本地任务分支已存在，helper 拒绝创建，不覆盖它。先保留并比较本地与远端提交；未被其他 worktree 使用时，以 `git worktree add --no-checkout "$task_tree" "$task_branch"` 接续，再按下节设置 sparse/首次初始化。已经存在的 worktree 直接复用，不强制重复挂载同一分支。同步需要时由 root 做正常快进或明确整合，文件冲突交文件 owner 修复。

进入所选目录，读取任务 `current.md`、核对实际源码与必要材料，只恢复缺少的依赖，从明确下一步继续。旧代理 ID、进程和终端不会自动复活；重新创建合适执行者并恢复必要命令，先确认现存进程与自有资源归属，不能凭历史状态重跑或清理另一会话资源。环境说明见[恢复指南](../docs/development/agent-team/recovery-2026-10-08-environment.md)，其中旧机器路径须按本环境调整。

## 新建稀疏工作树与共享依赖

[ai-worktree.py](../scripts/ai-worktree.py) 的 `create` 默认仅计划，`--apply` 才写 Git。它将显式 base 解析为 commit，创建新 `ai/` 分支时不推断 upstream；拒绝已有分支、已有目录及嵌套在其他 worktree 内的目标。随后只在新树执行以下首次初始化，不 reset 既有工作区。任何步骤失败都保留现场供 Git 负责人检查，不自动删除分支或目录：

```sh
git -C "$task_tree" sparse-checkout set --no-cone --stdin <<'PATTERNS'
/*
!/docs/development/agent-team/*verification-evidence*/objects/
!/docs/development/agent-team/*verification-evidence*/**/objects/
PATTERNS
git -C "$task_tree" read-tree -mu HEAD
git -C "$task_tree" status --short --branch
```

`read-tree` 此处只初始化刚 `--no-checkout` 创建、无 WIP 的树；不得照抄到活跃树。sparse 只省工作区物化，原历史文件仍可 `git show <revision>:<path>` 读取；特定旧测试确需某对象时由 owner 定位后恢复那份输入，不关闭 sparse 全量回填。主仓保留的合同和源码以真实 Git 版本为准。

隐藏目录也可能包含正式 Go 测试 import 的源码，定制 sparse 规则不可仅凭目录名将其视为历史证据；`.agent-state/project-variables-independent/commitproxy/` 必须保留。

共享路径选在 worktree 外，例如 `/chosen/shared/agenteam`，具体版本沿工具链与锁文件核对；它不是生产配置，也不由 helper 自动下载、复制或迁移。新任务先查现存匹配内容，只恢复缺项：

```sh
agenteam_shared_root=/chosen/shared/agenteam
export GOCACHE="$agenteam_shared_root/go-build/go1.27.1"
export GOMODCACHE="$agenteam_shared_root/go-mod"
export npm_config_cache="$agenteam_shared_root/npm-cache"
# 本轮目录另行创建并交给原 fixture；不可指向共享缓存或他人的输出。
agenteam_run_dir=/chosen/task-output/run-unique
```

Node 安装需匹配 package.json/lock、Node 与平台，复用前核对；不要整树复制另一任务的 node_modules。使用共享安装的工具入口或链接须符合原工具解析方式，并把 Vite/Vitest 等可写缓存与构建产物配置到本轮目录，不在正在共享的安装目录运行 npm ci。Go cache 正常读写遵守 Go 自身并发协议；清缓存、更新共享依赖与迁移路径不能与实际消费者并行。固定 MinIO/浏览器、不可变 fixture 文件和 Docker 镜像也沿原版本复用；现有活跃资源待原 Wait 和 owner 退出后再迁移，不能为清树先挪路径。详见[资源规则](../docs/development/agent-team/README.md#worktree-与测试资源复用)。

新的必要 Go 编译可显式选用 `-trimpath`，减少 worktree 绝对源码路径对共享缓存命中的影响；不设置全局 `GOFLAGS`，也不为优化重建已验证候选。`runtime.Caller` 的文件路径不能用于定位仓库：需要从源码构建子程序的 SMTP fixture 从实际 cwd 逐级查找固定 `go.mod` module，错误模块或找不到时明确拒绝，保持固定 `AGENTEAM_GO` 与 Go 1.27.1 检查。缓存回收先评估可用空间和待跑夹具峰值，在实际消费者退出后有界淘汰可再生条目、保留近期内容；mtime 只是近似 LRU，不证明无人使用。不例行全清 `GOCACHE`，本机单轮采用的容量目标不作为所有环境固定上限。

预编译的定向 PG 套件若已核实不需要 unsupported-PG 反控，可以显式设置 `AGENTEAM_FIXTURE_PG_CURRENT_ONLY=1`，同时保留原 `AGENTEAM_FIXTURE_TEST_BINARY/CWD` 和真实选择器检查；默认不设，仍运行完整版本。不得对全套测试批量默认开启，缺反控环境必须失败。原自有 DB/nonce、实际 Wait、完整退出和资源归属门不变；资源列表采用本轮实际生成 ID，不伪造未启动的 PG16。

## 上游同步、联调与正式交付

功能完成优先：模块具备首条真实功能路径并通过轻量基础自测后，尽早在自有隔离环境做小范围真实联调；多个未验模块确需组合隔离时使用短期 `ai/integration-<task>` 候选分支；未完成的上游能力和后续绑定仍明确记录。集成前由子代理评估共享 API、迁移编号与顺序、仓库根配置、依赖锁文件及调用方影响，列出应采纳的上游版本和受影响验证；root 执行 Git 同步，冲突由文件 owner 处理。工作树隔离不能替代数据库、端口、缓存和 fixture 隔离。

候选分支的 `current.md` 记录实际组合版本、已通过/失败/未验证范围和下一条联调路径；协调索引只更新相应集成状态。保留权限、数据、事务、协议、竞争及恢复等高风险关键验证，不用接口声明、stub 或基础自测替代真实集成事实。

默认直接在 `main` 开发，适用验收通过后由 root 用明确文件路径提交和普通推送。多个未验模块的组合试验或未验 WIP 保存使用短期隔离分支；验收后由 root 整理为完整结果的 Conventional Commit，将实现、测试、必要文档和状态一并交付；不另造 `docs archive` 或补哈希提交。状态作者按该分支的当前完整任务范围记为完成并指出下一步，协调索引仅同步变化；`main` 推送确认后转入下述清理，避免下次恢复重复已完成任务。

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

远端备份仍是任务分支：正式 main 已交付且任务完整结束后，本地/远端一起收尾，不能只删一侧。删除远端前，root 再次 fetch 候选引用并用 `ls-remote` 核对实际 tip，确认与结论一致且没有活跃 writer，再做带精确 lease 的空 ref 删除。下例的 `observed_tip` 必须是刚核实的完整提交 ID，不能用旧聊天值或省略：

```sh
git -C "$repo_tree" push --no-follow-tags \
  --force-with-lease="refs/heads/$task_branch:$observed_tip" \
  origin ":refs/heads/$task_branch"
git -C "$repo_tree" ls-remote --heads origin "refs/heads/$task_branch"
```

这是防并发的删除，不是覆盖历史的 force push。tip 前进、归属变化或任何查询失败立即停，不能刷新 expected tip 后盲删。第二条成功且输出为空才证明远端缺失；此前保留本地可恢复来源。远端缺失确认后，查看 `git config --get-all remote.origin.fetch`，只移除为这个任务添加的精确条目（兼容带 `+` 和不带 `+`），保留 main、通配和其他任务配置：

```sh
for task_refspec in "refs/heads/$task_branch:refs/remotes/origin/$task_branch" \
                    "+refs/heads/$task_branch:refs/remotes/origin/$task_branch"; do
  if git -C "$repo_tree" config --fixed-value --get-all remote.origin.fetch "$task_refspec"; then
    git -C "$repo_tree" config --fixed-value --unset-all remote.origin.fetch "$task_refspec" || exit
  fi
done
git -C "$repo_tree" fetch --prune origin
git -C "$repo_tree" for-each-ref --format='%(refname) %(objectname)' "refs/remotes/origin/$task_branch"
```

受限 refspec 的 prune 可能不再覆盖刚移除配置的 tracking ref；若仍存在，只在重新核对该 ref 的当前 tip 后用 `git update-ref -d <精确tracking-ref> <该tip>` 删除，再核本地分支、远端分支、tracking 和精确配置均已收口。不得 `unset-all remote.origin.fetch` 不带匹配值，也不改其他环境的工作区。

用户明确清理非活跃旧分支而其中仍有独有历史时，先在工作树之外创建包含所有候选精确 refs 的完整 bundle（不使用排除历史的增量参数），`git bundle verify`、`list-heads` 对齐候选 tips，并在小型空仓库实际恢复需要保留的引用。bundle 是源码历史恢复材料，ignored 文件/进程不会包含在内，需另按既有规则保留。确认可恢复且无活跃依赖后才删这些旧引用，不要求为了备份永久保留远端分支，也不宣称它们已合入 main；用户未授权的活跃、阻塞或归属不明任务仍不删除。

远端删除不影响其他机器或离线环境的本地分支、worktree 和文件。该环境下次恢复时先保留本地改动，读取最新工作规范、协调索引和主线交付，按上文 fetch/prune 清除过期远端跟踪引用。已完成且已删除的旧任务不再执行自动 checkpoint/push；有独有未保存成果时先核归属并另行保存，需要新增工作时从确认基线建立新任务分支。只有本机对应对象重新满足清理条件后，root 才移除其本地 worktree 与分支；不把 remote prune 或 `git worktree prune` 当作删除其他环境目录、结束进程或保存成果的手段。
