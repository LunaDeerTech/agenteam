# D26 core02 独立纯行为验证

**PASS，限此处两个独立纯测试；正式页面/真实浏览器仍未验。** 单文件 2 tests、0 FAIL/SKIP，Vitest 4.1.11，598ms；TypeScript 独立 typecheck exit0。两条命令均首轮通过，没有修断言、重跑或扩大预算。没有重跑作者 29 tests。

固定 `457b1979c9d6563740543b2011eedc06cce34c71` + core02 三生产/两 package（原 manifest `0bd7611e421549ef4d13a148b50e04359b3b53845fabfb6a6b597fdddf74eece`）；只补原基线 useTheme、tsconfig 与自有 Vitest 配置/probe。共 9 输入，`input-manifest.json` SHA `33e27059549cd19c6c2e3f2c90f51c0463ece48d4191a6c49bc91d08db3cc715`，末检全部不变。没有读活动 UI，也没有改核心代码。

两个增量信号：

1. 真实 `createAccountAPI + controller` 收到错误 Content-Type 的 Response，底层 cancel 被 deferred 持有。30000ms 后页面等待已返回 Unknown，actual cancel 尚未返回、busy 仍 true；此时 leave/restart/restore/新 login/logout 均不增加 Cookie 请求。实际 cancel 返回后才释放旧 owner；新的 Session 查询等待中，旧 Promise 完成不能清新 busy 或发布身份，只有新的受控 Session 响应返回后才发布其 user/session。
2. login200 后实际 typed GET 分别只更换 user ID、只更换 session ID，两分支均拒绝发布、禁止注销请求；保留期望身份。新的查询仍挂起时旧 login Promise 不会清新 busy，只有后继匹配的 Session 响应可发布身份。第二个 `it` 内逐一执行这两个 ID 反例，不虚报额外顶层测试数。

这些是受控 Fetch/Response/ReadableStream 纯测试，使用真实 typed client 与协调者，不是真实 HTTP Cookie jar、服务端 COMMIT Unknown 或浏览器导航。每个场景在 finally 放行自有 deferred 并等待调用，不通过提前 release 使关键断言转绿。敏感输入均为合成测试材料，日志只包含安全测试名/断言，不输出 body、pass 或 CSRF。既有 F1/F2 原红修后与 F3/F4 作者有效纯证据复用；UI 字段释放、路由与正式同源 dist/真实挑战消费留给后续完整冻结验收。

实际命令由 `run.py` 执行，完整 argv/cwd/selected env/exit/原始输出 SHA 在 `logs/typecheck-01.json` 和 `logs/independent-01.json`，raw 为同名 `.log`。Node 为 v24.19.0；没有 npm install、下载或升级。只读复用作者已恢复的锁定 node_modules，独立核 146 个安装条目的 version/resolved/integrity 与本次 package-lock 一致，复用目录锁末检不变。Vite/Vitest cacheDir、TMPDIR 都在本目录；configLoader=native 避免向共享依赖目录写配置 bundle。

实际启动命令：`python3 /workspace/agenteam-d26-pure-v-1z5m6v_m/run.py`，exit0。早期只读准备曾查询不存在的可选 `web/tsconfig.app.json`（固定 Git exit128），随后使用实际 `web/tsconfig.json`；该定位错误记录于 setup，未执行产品，不冒称测试首红。

runner 已 wait 两命令，末核本目录 cwd 的 Node 进程 0；首次 runtime 检查只留下一个 Node compile cache 文件，原记录保留在 process-final.json，记录其 SHA/字节数并精确删除后 runtime 为空（runtime-cleanup.json）。本轮未创建 browser/Go/Docker/网络资源。所有写入仅本私有目录，未写仓库/作者原件/业务/既有归档，无 Git 写操作。此结果可用于独立核心验收，不能扩大为完整 D26 或 D28 部署完成。冻结后 all-stop，等待正式续派 UI 静审。
