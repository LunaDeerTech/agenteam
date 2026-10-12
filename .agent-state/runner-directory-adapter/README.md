# runner-directory-adapter：本次新增归档恢复指引

本 README 是 2026-10-12 新增的恢复说明，不是旧验收记录。原退役裸件 `output/ai/retired-20261011/runner-directory-adapter/compile01/projectvariable.test` 已按本次精确清单移除；其 83,263,502 B 原字节保存在 [archive02/run02/history-binaries.tar.gz](../../output/ai/retired-compile-binaries/archive02/run02/history-binaries.tar.gz) 的同名 member `retired-20261011/runner-directory-adapter/compile01/projectvariable.test`，binary SHA-256 为 `c31a5bb94059ab320f73e97e36d982017cf228ec10584c1bbff818954955776e`。

归档共 21 个成员，SHA-256 `9f3da6012aacd41f3cc49a7e1b7d657ccf601e74368aee6ad52b75918e71a20b`；本任务仅涉及上述一个 member。完整恢复元数据见 [before.json](../../output/ai/retired-compile-binaries/archive02/run02/before.json)，完整解流及移除结果见 [归档结果](../../output/ai/retired-compile-binaries/archive02/run02/result.json) 和 [removal02](../../output/ai/retired-compile-binaries/removal02/result.json)。原 compile/native 日志、input maps、FAIL 和源码恢复材料未改写。

先核归档 SHA，解到新空目录并按 [恢复说明](../../output/ai/retired-compile-binaries/archive02/run02/README.md) 核原字节、SHA/size、权限和 nsmtime。解包后的 inode 是新分配的，历史 dev/inode/ctime 仅记录原保全事实。恢复和重新运行须另行建立独立输入，不覆盖旧材料或复用旧 namespace。
