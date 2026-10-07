# 独立 native01：限定 PASS，原 wrapper 首红保留

固定已接受 B `e7304512e73fdddb25bdbf1c0882400ad5b5f791` 与 Summary `e6cb70bdfc6ef7569d740f767f7dca3211a2ef7a`；291 产品路径逐字匹配固定 Git，实际 race 379 包/2349 源文件闭包前后及恢复后 hash 一致。复用 B binary SHA256 `fc86e607bc6b9f5abebdca787634b06fb79ad8a012451ccb11235dbe9c7acf50`，无自定义 TestMain，无活动 C/app/fixture 依赖。driver `run.py` SHA256 `f8edfd51dec7a9244bf4ba5e0283b5163350a117c4cdd3ff83ecf36276b91f3e` 在 root 确认唯一窗口后只运行一次。

精确执行入口及内部参数见 `READY.md`、`native/command.json`。原 `-race -p=1 -c` 单包 binary 用 `-test.count=1 -test.parallel=1 -test.timeout=45s` 与三顶层精确 selector，独立外层也在45s结束业务执行，仅实际wait/cleanup可用尾部宽限。本轮无超时/信号：binary actual exit0，12.036552s，3顶层/9子例全部 PASS。全部日志见 `native/raw.log`；无产品修补或测试重跑。

- KeepAlive：同一 TCP 连接完成 list/summary/resolve 的六次 GET/HEAD，首个120ms parent 期限过后继续成功；真实 EOF、Content-Length、RequestID、body EOF/Close次数通过。
- SlowBody：declared/hidden 两种实际输入阻塞，各经自然2s与较早120ms parent 到期；验证认证前期限、Done、实际elapsed、service零调用、一次Close与handler终局，4子例通过。
- WriteAndClose：short write、write error、flush error、close error及真实socket write deadline，5子例通过。前四种错误由测试writer/body注入；真实 write deadline 使用小socket buffer和暂不读取的peer，确认 timeout、部分输出、期限清除、handler退出及真实连接EOF。最后子例6.52s含peer排空已缓冲数据；trace中服务端accepted socket在连接建立约2.003s后已成功close，不能把整个子例耗时当handler预算。

原 wrapper exit1 必须保留：本环境 strace `-yy` 仅输出 `TCP:[inode]`，driver期待 `127.0.0.1:PORT`，因此漏提取端口并触发10-listener门槛失败。`native/ports.json` 的空集合及原 `cleanup-double-zero.json` 不构成端口清理证据；没有将它们覆写为通过。原始 `native/command.json` 的 `pass:false` 保持原样。

获准后仅追加只读恢复：`recover.py` SHA256 `298b60a46cb556cebeee0d93d331c0bfb981c7a4a873f97d0d89e6e16ab2c267` 按TID重组原trace的unfinished/resumed syscall，以listen inode关联成功getsockname sockaddr，复得10个loopback listener、10对连接及20个端口；所有33个TCP socket inode均见成功close（含Go探测用的3个未listen socket）。完整映射见 `recovery/recovered-ports.json`。所有connect均为127.0.0.1，无外网、Docker、PG或MinIO操作。

实际wait已在原运行完成：direct60727/starttime308203 与 adopted tracer60731/starttime308203 均actual wait0，同pgrp60727。补核开始于UTC epoch1791377200.141313，进程及活跃socket均空，但仍有1个TIME_WAIT；只读观察后在1791377203.158210与1791377203.362451两次确认所属进程、20端口的所有TCP状态（含TIME_WAIT）均空。每次/proc原件及时间保存在 `recovery/cleanup-observations.json`，不倒称原wrapper结束时TIME_WAIT已经清零。最终pgrp及driver不存在，见 `final-state.json`；历史僵尸及其他资源不属于本次清理。

结论由本轮真实测试通过与后续只读清理恢复组成：三native测试的产品行为限定 PASS；wrapper原端口解析失败保留。该结果证明 handler 的真实网络I/O行为；auth/service仍为受控依赖，不证明 C默认root绑定、正式Session/Owner/真实PG权限、资源fixture或整卡验收。没有扩大45s预算、运行C或改动生产/测试源码。native窗口已结束，本目录停止写入，等待root归档。
