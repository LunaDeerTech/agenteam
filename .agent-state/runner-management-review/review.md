# Runner Management independent harness review

限定接受，无 must-fix。输入为独立树 `1ce69576..e7742936` 的两工具与作者 harness-controls；不审自己实现的 Runner 产品，不改独立业务源。

Go 只接受新增完整三 top literal，原单 top 正则保持；Python 只在该非 root literal 检查原日志 top 集合及总数恰三。读取错误与非法 UTF-8 返回固定安全失败，仍进入原 descendants/TCP/input 尾。两工具逆去增量逐字基线，原 Go6m/105+15/123+3/TCP75 与两 PG 资源不变。

实际离线验证：`fb57ee` 执行作者 `python3 -B .agent-state/runner-management-independent/harness-controls.py`，25 控 exit0；含实际 main 外部 process/time/resource 替身。`81fe57` 执行本目录 `entry-controls.py`，14 控 exit0；增加六排列、六同 count 的缺失/重复组合及原 invalid-byte 日志安全拒绝。均没有启动 driver、child、PG 或 socket；没有丢失终态。

新 driver 尚未编译，实际 selector guard 待构建后检查。固定 management.test 的三业务 top 尚未真实执行，不能据本审查称业务通过。候选、原失败与全部资源门槛保持。
