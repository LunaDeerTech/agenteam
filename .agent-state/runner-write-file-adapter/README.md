# Runner write-file 恢复

范围与实际分段结果见[工作项](../../docs/development/work-items/d16-runner-write-file.md)。本片是可信 root fd 下的本地原子整文件写入，不是 Tool/WSS/生产执行或恶意宿主隔离能力；原 STOP 不变。

## 原件与源码

本树原件根为 `output/ai/runner-write-file-adapter`，目前未迁移。主线交付后拟无覆盖、同文件系统迁往 main 的 `output/ai/retired-20261011/runner-write-file-adapter`；实际移动与删除须另由 root 协调，不能从本说明推断已执行。

| 原记录 | 精确源码 | 实际结果 |
| --- | --- | --- |
| `checks/basic01` | `91d281c4425925f06d962a93348bca37e196c59d` | 原工具 49447 退出 1；33/41 top PASS（原有 31 全过）、8 FAIL；vet 未运行 |
| `checks/basic02` | `7ff8174a295f9736c60e21165e98ac6da1fd1cda` | 原工具 37839 退出 0；新 10 top / 13 sub race PASS；两包 vet PASS |

每轮 `invocation.json`、`*-invocation/process/result.json`、原 `.log`、`selection.json` 和三份 `inputs-*.json` 保留实际命令、进程返回与输入。两轮各 1,100 项输入在各自运行前后相同；每轮 33 个仓库内输入已通过 Git 对象内容逐项核对，与该轮历史提交和原 SHA 全部一致。其余是原 Go 工具链、标准库与 module 源，精确位置/SHA 在原输入表中，不能称其由 main 源码恢复。

在完整历史合入 main 后，使用表中提交恢复对应源码与 recorder；无须另存同内容 patch。删除分支前须确认这两提交仍由 main 历史可达。不要以修后代码替换 `basic01` 原输入：它实际只报告 `Unavailable`，未记录系统 errno；O_PATH/NONBLOCK 是后续静态定位，不能补写原动态 EINVAL。

## 保全准备

`preservation-plan.json` 逐项列出两个检查目录的 58 个普通文件的相对路径、大小和 SHA-256，原件合计 3,654,282 B；清单自身 11,321 B，SHA-256 `2a8b0520d70a9ecb4f3937fef3d3c00a84d61d2e394b867eea4aac5c2eeb58ce`。清单加原件共 59 文件 / 3,665,603 B，无链接或特殊文件；两轮私有 tmp 目录均已空。它只是本次输出的保全准备，不是新全树输入清单或已移动证明。

没有本树私有 Go/module cache、独立产物 binary、浏览器依赖或外部 `/tmp` 证据。原两次 recorder、Go 和 list/vet 均已实际退出，没有本任务后台持有者。`GOCACHE` 指向保留的 SkillHTTP 热缓存，`GOMODCACHE` 指向保留的 SkillsMilestoneResume 只读模块；保全本片时绝不能迁移或删除这些外部依赖。后继 RoundInput 不需本片运行输入；以后使用本地库应从正式 main 取得源码，不借用本树绝对路径。

需要另一次明确验证时，使用受跟踪 [check.py](check.py) 和新的输出目录；`--write-only` 精确选择原十个 Write top，成功后仍对两包 vet。这里的可复现命令不授权自动重跑，也不覆盖原失败。
