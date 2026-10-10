# Runner 三回归入口：独立窄审

输入为 delivery 树 `545d2b33` 后作者冻结的 `native_driver.go`、`pg_only_supervisor.py` 与新 `cli-controls.py`。仅三现有回归的 exact selector 和方法接线；不改测试/产品，不重复 Default 独审，不执行真实资源或 Go 构建。该现有信号测试使用 held TLS challenge，原进程实际 Wait 与 handlerReturned 后才成功，不能按无 socket 场景判断。

本轮发现两项新 CLI 输入门缺口，作者在 Default 原窗口完整结束后作了限定返修。修后有限方法接受，无剩余 must-fix；下列原红保留，不影响原 Default 输入或给其运行归因。

1. `9c45fa` 实际只读检查原 `runner_cli_inputs`，缺全部 18 个 `tests/process/*.go`，包含三目标及 TestMain；继承 root adapter 的非 test 生产源码不能补齐此实际编译包。
2. 持久 `controls.py --only addition`，`d246c0` actual1：执行原 supervisor main，使用明确 Child/proc/TCP/artifact 替身，仅让输入函数在原 Wait 后多一个动态源码。对照与新增组均返回 0、输入函数仅调用一次；末尾没有重取集合，因此可漏 TestMain 实际编入的新源码。两组原 Wait/desc/TCP 尾均达到，缺陷是输入门假绿，不是实际进程证据。

`controls.py --only observer` 的 `30e736` actual0/18 控，核同数量错 RUN/重复 PASS、错误 selector、重复启动/Wait/terminal、原 child PID 与 manifest、未知字段安全输出以及 dangling tmp/私有目录拒绝。本人 `8d5238` 重取作者 24 observer/main 控实际0，仅将临时目录放本人 ignored output；两工具逆增量全文回到基线、原预算和尾路径检查通过。作者原控制对闭包的 expected 使用同一 adapter 列表，因此没有覆盖第一缺口。

必要修订限定 exact CLI：实际 process 包输入及末尾文件集合一致性/读取失败保持失败；旧 Default/generic 与 90s/105s、nonroot123+3、TCP75、实际 Wait/desc/private 尾不变。只涉及 supervisor 清单/观察时，无需重编 driver 或固定业务候选。真实运行、所有 socket/子进程完整退役与新 main 三项回归结果仍待后续 fresh grant。

返修输入为 `119e666f` 后冻结 supervisor/作者 controls 两源；driver 和业务候选未变。本人原持久 `controls.py` 全跑 `e1da5f` actual0/25 控：18 包源已包含，新增动态源的原假绿转为 exit1，原对照仍0，两组都重取输入两次且原 Wait/desc/TCP 尾达到。本人 `f4ab9f` 实际重取作者27控，新增/删除/不可读输入均拒绝、原两源逆增量全文仍545；仅临时输出重定位自有树。未执行 Go/driver/candidate 或实际 socket。
