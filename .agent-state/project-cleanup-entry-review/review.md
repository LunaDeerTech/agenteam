# Project Skills Cleanup 独立 PG 入口窄审

结论：有限接受，无本轮 must-fix。审者 `/root/knowledge` 未参与此测试/入口实现；只读 `/workspace/agenteam-project-skills-cleanup-independent` 的冻结增量，原基线 `feff9e0c`，测试已保存 `58f5ea16`。不形成业务源码或真实 PG 接受。

仅 supervisor 新增精确 `^TestProjectSkillsCleanupIndependentRevalidation$` 的 mapping 与 1 父 3 子闭集，共 12 行；AST 定位逆去增量后全文逐字等于原基线，原 driver 逐字未改。直接核实际 Go 文件三个子名称、无 `t.Parallel`；复用已有作者编译/精确发现，不执行 Go 或候选。

独立 `a4d23f` 实际 exit 0，命令 `python3 -B .agent-state/project-cleanup-entry-review/probe.py`（cwd 本 HTTP 树）。24 控制覆盖所有 8 个子集（仅完整三子接受）、重复结果、parent SKIP、额外 top、错子、缺 parent RUN、非精确 selector；原三入口的正例保持。实际 `supervisor.main` 五格为正例、缺子、child exit 4、残留后代和 input 变化，均继续原 Wait 123/reap/双 descendant/TCP/input/terminal。进程、OS、信号、时间和 TCP 为显式替身；没有启动真实资源或向真实 PID 发信号。

另 `38d0e1` 原作者 19 控制原样复跑 exit 0。driver 的 Go 6m、原 105s 工作 + 15s 清理、supervisor 123s + 共用 3s 收尾、TCP 75s、实际 `cmd.Wait` 与原两 PG 资源/私有目录清理代码全部未改变。默认 generic nonroot 工具仍保持原通用行为；本有限接受只针对获授的 exact 入口，不把其它 selector 当作本业务通过。

源码未修改；未证明真实 SQL/权限/同 Tx 重验、实际资源双尾或此候选的动态预算。后继仍须 root fresh grant，并由拥有者拿到真实完整终态。
