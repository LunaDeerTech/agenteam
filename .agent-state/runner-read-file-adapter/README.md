# read-file 首次编译失败的最小恢复材料

`unit01-recovery.patch` 将已交付的修复版两个产品 Go 文件恢复为首次 unit01 的实际输入。它是 863a162e → 311d7777 的有限反向差额，旧字节已逐项核对原 `unit01-inputs-before.json`。只在独立恢复快照中应用；它会重新引入已解决的 `mount/contract → agent/contract → mount/contract` 循环引用，不用于当前产品。

原 unit01 执行六包定向 race：工具 10167 actual1，Go 568623 /24.308s，62 个相关输入前后不变；Central 三包 setup FAIL，Runner 三包 12 top PASS。原命令、环境、前后输入、日志、进程与实际退出保存在任务证据的 `checks/unit01-*`、`checks/unit01.log` 和 `checks/basic01-tool-terminal.json`；以后整体保全到 retired 目录时保持这些相对路径。

单独 PG fixture 的对应 helper 也在后续修复中改动，但不属于 unit01 编译/运行输入，恢复这个实际失败无需携带其旧版本。原失败尚未运行 vet 或 PG；该补丁不补造任何未发生的动态结果。

同目录 `profile-controls.py` 是原冻结 selector 的历史方法控制，基于 7a0e817d 的旧 harness；主线后来新增其他合法 profile 时，不用其旧完整 AST 比较冒充当前组合检查。当前组合以本次主线 integration 原件与实际保留的字典并集为准。
