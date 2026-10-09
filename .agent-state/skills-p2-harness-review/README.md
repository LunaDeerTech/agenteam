# Skills P2 独立验收工具映射窄审

输入：`/workspace/agenteam-skills-p2-independent` 的 `29b252c7` 后冻结两工具及 `.agent-state/skills-p2-independent/harness-controls.py`。结论：限定工具增量有限接受，无 must-fix。本实例没有审查自己的 Skills 产品或独立业务测试断言，只核新 exact 入口、完整编译 package 源闭包及原监督器门槛。

- driver 只新增 `^TestSkillIndependentP2ConfirmationAndPackage$ → tests/skills`，该 selector 追加实际 package 九个 Go 源。supervisor 的 expected top 同步，只对该 literal 给 `input_paths` 传 selector。旧默认/旧 selector 输入及所有其他代码逆去增量后逐字29b。
- 唯一 top 范围之外的前后缀、宽regexp、子selector、扩展top/组合均拒；新目录不能已存在或是symlink。完整 package 九源与实际目录一致，includes/candidate 不靠单个业务新文件代替旧fixture来源。
- 原 root-chain Go6m/540+60、PG123+3、7资源、原Wait/owned descendants/private/runtime/TCP75/input gate 不变，未增加failfast或额外真实资源。沿用原root-chain顶层集合与实际test Wait判据，未宣称新增四sub动态计数门。

本人实际命令（cwd 作者独立树）：

```sh
PYTHONDONTWRITEBYTECODE=1 python3 .agent-state/skills-p2-independent/harness-controls.py
```

`aea89f` actual exit0，42控。实际运行 configuration、supervisor main/record decoder/observer；child/Docker/process/TCP为明确替身，无真实PG/MinIO/socket/Go test。缺/多top、错Wait selector、child1、input变化、资源/runtime残留拒绝；全部相应 main 格仍走原 Wait540、14次资源观察、private/runtime/owned 双尾、TCP双空和input/terminal，legacy入口保留。

没有改作者源、新业务编译或重做作者2pure7sub；该工具接受不证明P2实际业务通过，不消除Skills Cleanup/Purger/root待接入边界。所有命令已终态，无自有资源。真实运行另需root fresh grant。
