# 非空 Mount DirectText 材料恢复

本片正式内容在 main `24865fa7`，技术等于已验 `d3f64149`，任务文档等于 `23148d11`。范围和限制见[正式卡](../../docs/development/work-items/d22-execution-mount-directtext.md)。本说明仅用于旧工作树退役后的必要恢复，不授权重新运行或解除 STOP。主线及本材料的远端保存、工作树和引用删除均由 root 另行完成。

## 原件位置与结论

旧 `/workspace/agenteam-execution-mount-directtext/output/ai/execution-mount-directtext/` 已整体同文件系统移动至[保留目录](../../output/ai/retired-20261011/execution-mount-directtext/)。移动时的 111 文件（约 239 MiB，含本次保全检查记录）保持 inode、大小和 mtime；未重写 invocation、manifest、原日志或终态。原 `/tmp/emd01`、`/tmp/emd02`、`/tmp/emd03` 保留，每轮必要监督日志及 `owned.json`、`request.json` 另有逐字副本在对应 `native*/retained-namespace/`。路径变更不把历史 invocation 中的旧绝对地址变成可直接执行的新配方。

| 保留目录内路径 | 实际意义 |
| --- | --- |
| `checks/unit01-*`、`app01-*` | 16 top / 45 sub 定向 race 通过；App 仅 compile-only。 |
| `compile01/`、`prepared-before-stopfix/` | 早期 fixture 在历史重入前 Stop；真实编译/list 已退出 0，但该 binary 未运行 native。 |
| `compile02/`、`native01/` | 中途错误检查整个 driver 的 Joined 导致 wholeFAIL；另一子例通过，资源退出尾完整。 |
| `compile03/`、`native02/` | 修后两业务通过，原 TCP 差额四行未空，wholeFAIL；被动 FD 归属材料只证明当时持有，不升级失败。 |
| `native03/` | 同一 compile03 binary，一次另授的有界原方案复验；两业务和原全部退出门 wholePASS，独立有限接受。 |
| `main-integration01.json` | 主线 20 路径采纳、旧 profile/方法保全、00062 与链接的限定静态核对；原记录中的旧 output 前缀按本说明映射。 |
| `retention-checks.json` | 作者停写/后继无依赖、可见进程检查边界、私有 patch 复原及材料移动结果。 |

三个原 binary 与各自记录全部保留。native02/03 使用 compile03 的 `5d90570e5ac19d04186fd1322746e187c4ecae782af1d1d9b60e8f28ea5cb7b2`。01/02 的失败不回写，不把分段证据称为一次全矩阵通过。terminal Unknown 是实际 COMMIT 后受控丢 ack；非空 start-Unknown、默认 App 行为、Runner 操作与物理目录不在本片动态验收内。

## 最小源码恢复

正常结果直接使用 main `24865fa7`，无需恢复完整旧树。下列两个 patch **各自以该 main 为基底，不能叠加**，仅改变 `tests/projectvariable/execution_mount_first_round_test.go`：

- [retired-inputs-compile01.patch](retired-inputs-compile01.patch)：恢复原 `01204ff9` 的提前 Stop，一行差额，对应未执行的 compile01/prepared。
- [retired-inputs-native01.patch](retired-inputs-native01.patch)：恢复原 `54715a5e` 的中途 Joined 断言，三行差额，对应 native01 原失败。

两个 patch 已在分别创建的私有临时目录用 `patch -p1` 实际应用，恢复文件逐字等于对应原提交；未在工作树应用，也未重跑测试。早期静审发现的 `Tx=nil` 类型错误在首次 Go 检查前已修复，不存在对应运行失败原件；未保存编辑中的任意瞬间不承诺可复现。

[retained-methods.patch](retained-methods.patch) 只恢复四个必要原方法脚本到原 `output/ai/execution-mount-directtext/` 相对位置：pure 记录器、compile03 记录器、native03 薄记录器、有限 profile 控制。已在空私有目录实际应用并逐字核对。原 profile 的两个 before 文件仍在保留 output 中，也可从 main 历史 `562d3538` 的两 harness 取得；正式监督器和 driver 本体已在 main，不另复制。方法脚本含历史本机路径，恢复时应在新隔离树准备独立配方，不能直接覆盖或修改上述原件。

原主要命令为 Go 1.27.1 的 `go test -race -tags=integration -c -p=1 -o <private-binary> ./tests/projectvariable`，随后该 binary 的 `-test.list '^TestExecutionMountFirstRound$'`；实际运行仍使用原两 harness 的精确 top/两 sub、540+60/6m、七资源实际 Wait 与双尾、75s 自动 baseline TCP 差额双空。详细 argv/env 留在原 invocation。旧热 cache `/workspace/agenteam-skill-package-http/output/ai/skill-package-http/go-build`、只读模块 `/workspace/agenteam-skills-milestone-resume/output/ai/skills-milestone-resume/go-mod` 不在退役范围，未移动或删除；新环境须按锁恢复，不假定这些路径仍存在。

## 停写与保存边界

原 native01/02/03 均已实际退出。实施 owner、read-file 后继 owner、System Runner UI 与 Secret owner 确认无旧树运行依赖；可见 `/proc` 的 cwd/exe/FD 无匹配，权限不可读不被记为进程不存在。此次仅移动材料并写上述必要恢复内容，无服务、Go、网络、Git 写入、原树或引用删除。

忽略的 output 原件属于本机保全，不因 README/patch 推送便声称已跨设备保存全部日志或 binary。Git 中的 main 技术、两个必要源变体和方法 patch 足以恢复限定源码；跨设备需要原终态证据时须另行保全该原件目录，不能重新运行后冒充原失败。root 仅在主线与本材料推送成功后处理旧树和引用。
