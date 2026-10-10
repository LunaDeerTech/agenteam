# Knowledge tree-command independent entry review

有限接受，无 must-fix。只审 exact `^TestKnowledgeTreeCommandHTTPIndependentReceiptOwner$` 入口与完整尾，不参与 Skills 的业务断言设计，不将本结果视为其独立业务测试或真实资源通过。

输入为 `/workspace/agenteam-knowledge-tree-http` 相对 `fa7fcc6fab8b7ed16ed9f4edeac9f0d3462c0d94` 冻结三路径；最终只读核与已保存 `663c5ca384c3837f507445df8cff1bdc566f31f0` 相同（68e1e5 读取提交及 SHA）。

| 路径 | SHA-256 |
| --- | --- |
| `.agent-state/work-owner-http/root_chain_driver.py` | `8374558beda40d7d1e1e638ff0260700d3353ba4a72298b7618a238f9c8f3d50` |
| `.agent-state/task-planning-recovery/pg_only_supervisor.py` | `646dde73ff84a950f62780e7bbfe6c92e268cf45b24319ff07d08bbe9bd20ca5` |
| `.agent-state/knowledge-tree-http/independent-selector-controls.py` | `d8ef6f27e905c38b152ac7a3801c36dfeea6014e693fbc4c4d019055a8ce6c3b` |

Driver 仅追加唯一 literal；supervisor 只追加常量、group、expected top 与 main 严格观察分支。新增 top 恰一个、无 sub；缺失、重复、额外节点、FAIL/SKIP、错误 Wait 及读日志错误均不能补绿。逆移除明确新增后，两工具全文等于旧基线。原 Go 6m、root 540+60、TERM 后 3s、TCP 75s、七资源、实际 Wait/reap、private/runtime/desc/TCP/input 尾保持。

原 `tree_command_inputs()` 未改，实际包含新业务文件、全部 `tests/knowledge/*.go`、原 commitproxy、command HTTP 源与两个 schema；原 adapter 继续提供生产 Go、module/embed/migration 和固定工具输入闭包。这里只核来源进入闭包，不评价业务 oracle。

## 实际离线控制

- 063e7b actual0：复取作者 26 控，其中 5 个 actual-main 场景明确用进程、资源、TCP 替身。只将临时目录重定向到本树 ignored output；未使用 `--artifacts`。原 whole inverse、exact 配置和错 selector、源闭包与预算通过。
- e4d398 actual0：本目录 `controls.py` 24 独立检查。实际 supervisor main 的 7 个场景：对照、资源未退役、悬空 private symlink、runtime 残留、错误原 test Wait selector、Wait 中输入变化、driver exit2。每格核原 Wait `[540]`、14 次资源检查、两轮 private/runtime/desc/TCP 与最终 input gate 均到达；失败不得被 top PASS 覆盖。

复验命令（cwd 本 Variables UI 树）：

```sh
PYTHONDONTWRITEBYTECODE=1 python3 -B .agent-state/knowledge-tree-independent-entry-review/controls.py
```

本人没有执行 Go 构建、业务候选、PG、native/browser/socket 或真实清理动作。作者随后报告 94048→8d8fdd race-c/唯一 top list actual0，候选 40,703,589B、SHA-256 `4819208c96130ecb5222b828877ba2a418c625280f6c5c061a5f06da77d67802`；49451→3b0b83 `--artifacts` 28 控 actual0。这些是作者的编译/发现结果，不是本人重跑或业务通过。先前编译与磁盘预飞失败保留；唯一 top 真实执行及原完整尾仍需另行授权并实际取得。
