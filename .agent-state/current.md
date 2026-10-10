# Secret Owner UI 当前恢复点

- 树 `/workspace/agenteam-secret-owner-ui`，root 创建的 main7a 基线，Git 写入仅 root。
- 本域卡：`docs/development/work-items/d27-project-secrets-owner-ui.md`。
- 已写首片段 14 个 web 源/测试及本卡/current，尚无测试结果。shared 原接口兼容，未覆盖 donor60dbcee7 的 shared 文件。
- 依赖：与 `/workspace/agenteam/web/package-lock.json` cmp0；本树 node_modules 指向现有锁定依赖，只读使用，不 npm ci、不改锁。Node v24.19.0。测试缓存将定向本域 output，不修改共享缓存。
- 当前无 Go/browser/PG/socket/网络进程。后继只定向三新 spec 与全 vue-tsc；真实浏览器/PG 需 root 独占窗口。无已通过声明，SPA concurrent-publication STOP 保留。
