# NextRoundProjection 的 PG 方法

仅新增 `^TestNextRoundProjection$` metadata profile，目标包 `tests/projectvariable`，精确一个 top、一个 sub：

- `TestNextRoundProjection`
- `TestNextRoundProjection/committed-input-projection`

PG 作者独占 `tests/projectvariable/next_round_projection_test.go`；本方法不将正在实施的 fixture 称为可编译或已通过。真实链需读取上游 68 已提交的后继输入，不发下一 Model 或 Runner，也不绕过上游失败、STOP 或默认未绑定边界。

两份原 harness 只增加 `METADATA_INPUTS` 和 `METADATA_GROUPS` 字典项，`TARGETS` 继续由原映射推导。原函数、PG 540+60 秒、Go 6 分钟、七资源精确身份、实际 Wait、private/runtime/desc 双尾、原 75 秒内自动 HOST_TCP baseline delta 两次 empty 及输入不变门全部沿用，不新增监督器或第二个虚构 sub。

`run-pg-checks.py` 复用 68 的编译、精确 list 和准备流程，只替换本片 selector、单 sub 和范围说明。它调用原 `metadata_inputs` 收集同包所有 fixture、完整 internal 输入和全部迁移（含 00068），并记录实际编译/list 进程 Wait、前后输入及 binary 身份。profile 中的六个显式路径不是完整编译闭包。`prepare` 只校验已成功编译原件并生成 fresh namespace 请求，不运行 Go、读取 TCP 或启动 native。`record-native.py` 与原薄记录器逐字相同；实际 native 仍须 root 单独排窗。

后继命令仅供获相应窗口后使用，本轮未执行：

```sh
python3 -B .agent-state/next-round-projection/run-pg-checks.py compile --output output/ai/next-round-projection/compile01
python3 -B .agent-state/next-round-projection/run-pg-checks.py prepare --compile output/ai/next-round-projection/compile01 --output output/ai/next-round-projection/native01 --namespace /tmp/nrp01
```

离线控制入口为 `python3 -B .agent-state/next-round-projection/profile-controls.py`。基线 `1050fdc7` 的两个原 harness 在剥除本片唯一新增键后完整 AST 相同，旧有效 maps 逐项保持；原 `metadata_results` 的一正例接受，缺 sub RUN、FAIL、非零 Wait、额外 case、重复 Wait、错误 selector 和整体 FAIL 七反例全部拒绝。

本轮原控制 PID 790759 实际 Wait 0 / 0.439s，外层工具 chunk `206d93` 实际退出 0；五项方法输入前后相同，五个 Python 源均完成内存语法解析，原 recorder 字节保持。原 invocation、process、result、日志和前后限定输入位于 `output/ai/next-round-projection/checks/profile01/`。这只证明方法增量与离线门控制，不代替 Go 编译、PG 业务或完整资源退出验收；本轮未运行 Go、服务或 native。
