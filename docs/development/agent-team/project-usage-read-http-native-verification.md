# Project Owner Usage HTTP：native 限定验证

主线程已采纳独立 **native01 限定 PASS**：一次真实测试 binary actual exit0，12.036552s，3顶层/9子例通过；原 wrapper **exit1** 保留。结论由该次行为证据与后续只读端口映射、清理恢复共同组成，没有重跑测试。原空端口双清无效，不能把恢复后的结果倒填到原 wrapper 结束时。本结果不接受 C/default root、正式 Session/Owner、真实 PG 或[工作卡](../work-items/d09-project-usage-read-http.md)整卡产品。

## 固定输入与一次执行

[独立报告](project-usage-read-http-native-verification-evidence/review.md) SHA `1e274ef8681a32dbe374b5c86ed7ba4ad608fd8b7dbd1778610cc97b923313ac`；固定已接受 [B](project-usage-read-http-stage-b-verification.md) `e7304512e73fdddb25bdbf1c0882400ad5b5f791` 与 Summary 纯契约 `e6cb70bdfc6ef7569d740f767f7dca3211a2ef7a`。[前置核对](project-usage-read-http-native-verification-evidence/preflight.json)记录291个产品路径匹配固定 Git、无活动 C/app/fixture import、无自定义 TestMain；实际 race 图为379包/2349文件。[执行前](project-usage-read-http-native-verification-evidence/native/input-before.json)、[执行后](project-usage-read-http-native-verification-evidence/native/input-after.json)及恢复后的闭包摘要一致，SHA `211c9e568bd1ce2aa9499b82a5ba0676841feb40a044b98036e19177773ad3b1`。本次归档复用这些已接受绑定，不重新读取产品树。

复用 B race binary，SHA `fc86e607bc6b9f5abebdca787634b06fb79ad8a012451ccb11235dbe9c7acf50`，21119045 bytes。原 [driver](project-usage-read-http-native-verification-evidence/run.py) SHA `f8edfd51dec7a9244bf4ba5e0283b5163350a117c4cdd3ff83ecf36276b91f3e`，在主线程确认唯一 loopback 窗口后只运行一次；入口与参数见 [READY](project-usage-read-http-native-verification-evidence/READY.md)和[原 command](project-usage-read-http-native-verification-evidence/native/command.json)。既有 `-race -p=1 -c` 单包 binary 使用 `-test.count=1 -test.parallel=1 -test.timeout=45s` 与三顶层精确 selector，外层业务执行预算仍45s；15s尾部宽限仅用于实际 wait/cleanup。本轮没有超时或信号。

预备的无 socket `/bin/sleep 0.25` [probe command](project-usage-read-http-native-verification-evidence/probe/command.json)及 trace/双清原件单独保存，只用于确认 tracer/subreaper 的实际退出接缝，不计入三项产品测试。

## 实际通过与边界

全部输出见[原 raw](project-usage-read-http-native-verification-evidence/native/raw.log)，追踪见[原 trace](project-usage-read-http-native-verification-evidence/native/trace.raw)，SHA分别为 `5821ebb462a057f60ba7af2128f4279b73ea043cc48fa0366e5689c416bce39f`、`67a505e56ed6d9e9577e79af1817242b4cad5fec3b8559d47cf831698c94514a`。

| 顶层 | 真实证据及限制 |
| --- | --- |
| `TestProjectUsageHTTPNativeKeepAlive` | 同一 TCP 连接完成 list/summary/resolve 的六次 GET/HEAD，首个120ms parent期限过后继续成功；核真实 EOF、Content-Length、RequestID与 Body EOF/Close次数。 |
| `TestProjectUsageHTTPNativeSlowBody` | declared/hidden两种实际阻塞输入，各覆盖自然2s和较早120ms parent，共4子例；认证前期限、Done、实际耗时、service零调用、一次Close及handler终局通过。 |
| `TestProjectUsageHTTPNativeWriteAndClose` | 5子例覆盖short write、write error、flush error、close error及native write deadline。前四类错误由测试writer/body注入；最后一例使用真实小socket buffer与暂不读取的peer，核timeout、部分输出、期限清除、handler退出及连接EOF。 |

最后 write-deadline 子例的6.52s包含peer排空已缓冲数据；独立报告按trace定位服务端accepted socket在连接建立约2.003s后成功close，不能把整例耗时当作handler预算。auth/service仍为受控依赖，真实网络行为通过不等于正式账户/Owner/PG事务通过，也不宣称取消能够撤回已经写出的部分字节。

## wrapper 首红与只读恢复

本环境 `strace -yy` 输出 `TCP:[inode]`，原 driver 却期待 `127.0.0.1:PORT`，漏提取端口并触发10-listener门槛失败。[原 ports](project-usage-read-http-native-verification-evidence/native/ports.json)的端口集合为空，尽管保存了10条listen记录；原 command的 `pass:false`、[错误双清](project-usage-read-http-native-verification-evidence/native/cleanup-double-zero.json)与观察原件全部保留。其“空集合两次为空”不能证明本轮端口消失；binary exit0和wrapper exit1是两个实际结果。

获准后的[恢复 parser](project-usage-read-http-native-verification-evidence/recover.py) SHA `298b60a46cb556cebeee0d93d331c0bfb981c7a4a873f97d0d89e6e16ab2c267`只读原trace，按TID重组unfinished/resumed syscall，将listen inode关联成功getsockname sockaddr。[精确映射](project-usage-read-http-native-verification-evidence/recovery/recovered-ports.json)恢复10个loopback listener、10对连接、20个端口；全部33个TCP socket inode有成功close，含Go探测用的3个未listen socket。423条重组调用原件同时保存；所有connect均为127.0.0.1，无外网、Docker、PG或MinIO操作。

| 原始观察时点（UTC epoch） | 可作出的结论 |
| --- | --- |
| 原运行 direct/adopted wait | direct `60727/starttime308203` 与 tracer `60731/starttime308203`，同pgrp60727，均实际wait0；该进程终局证据不依赖错误的空端口集合。 |
| `1791377200.141313`，恢复首次扫描 | 所属进程及活跃socket均空，但仍有1个TIME_WAIT。其后两次中间观察仍保留该状态。 |
| `1791377203.158210`、`1791377203.362451` | 连续两次确认所属进程与20端口全部TCP状态（含TIME_WAIT）均空，只证明这两个实际时点。 |

完整[恢复观察](project-usage-read-http-native-verification-evidence/recovery/cleanup-observations.json)与五轮 `/proc/net/tcp`、`tcp6` 原件保留，包括最初TIME_WAIT及最后两轮全空；[恢复结果](project-usage-read-http-native-verification-evidence/recovery/result.json)明确原wrapper exit1、原空端口检查无效、test_rerun=false及原件未变。[最终状态](project-usage-read-http-native-verification-evidence/final-state.json)再核pgrp与driver消失，native窗口结束。没有倒称原wrapper结束时TIME_WAIT已清零，历史A未join僵尸及其他资源也不纳入本次清理。

## 保存范围与后继

[来源映射](project-usage-read-http-native-verification-evidence/source-map.json)保存39份运行/恢复原件、205043 bytes，另留工作卡修改前页首与本次自查。原trace、首红command/ports/错误双清、恢复parser/精确映射、重组调用、当前各次清理原件和实际wait均保留；11份重复预备/probe基线或输入只列指纹。B的大race图、binary、工具、源码和cache复用既有指纹，不重复复制，B历史归档不修改。

本次文档工作仅核原件SHA、JSON字段、原raw计数、链接、格式及页首差量，不执行产品、原driver/parser、资源或Git操作。[归档自查](project-usage-read-http-native-verification-evidence/archive-checks.json)确认工作卡技术§1–7 SHA仍为 `36a7fa5c64cbb8d6716f87851b2b5eea895d344319944f6d558c37c55f53a11d`。C/PG完整14源冻结门槛与真实Session/Owner、名称/稳定ID/cursor、数据库锁/事务及默认root验收仍在后续；整卡、完整D08/D09、ready503和Object/tools/SPA publication三停止边界不变。
