# D26 原生 DOM disabled 焦点观察

**PASS：固定 Playwright 1.56.1 能完成系统 Chromium 的本地 DOM 操作；原生禁用焦点控件会同步使焦点移到 body，UI02 的 U2 静态接缝所依赖的浏览器前提现已实证。** 只执行一次，无重试。这不是 Vue 页面、正式构建、Account API、Cookie 或完整认证浏览器验收；UI02 原 BLOCKED 报告不改。

授权计划 `/tmp/agenteam-d26-dom-focus-plan-u5cuud7j/plan.md` SHA `de5b09fbb1a821779897e69e94369ba0a4e2bf05851d088fcee34a123633150e`。原固定 harness lock SHA `cb6dfd30b1fa05013b617e2dfb1c5d063115b4a7d231e368a24ffd14df03f534`，三个 Playwright 包均 1.56.1 且 installed version/resolved/integrity 对应原锁；二进制 `/usr/lib/chromium/chromium` SHA `d387400aaf740ccb75e5e996a34aa0940e6e97683a4eabd6ae34c1eed6804723`。实际 CDP 返回 Chrome/151.0.7922.173。使用已有依赖，没有安装、下载或修改作者文件。

实际命令：`python3 /workspace/agenteam-d26-dom-focus-v-7k_tkbe3/run.py`，exit0；Node driver `probe.cjs` 单次 exit0，1.84731s，未触及 45s 硬界。先执行的 Node `--version`、`--check` 都 exit0。完整实际 argv/cwd/安全 env/exit、原 stdout/stderr、源码和输入 SHA 均在 `evidence/`，无产品运行或资源启动的第二次尝试。

| 原生动作 | 实际观察 |
| --- | --- |
| 聚焦 range，真实 Enter 事件中设 disabled=true | 禁用前 active=angle、题容器 owns=true；同步禁用后产生 blur/focusout，active=body、owns=false；微任务及下一帧仍相同。 |
| 真实点击 button，事件中设 disabled=true | 禁用前 active=verify、owns=true；同步禁用后产生 blur/focusout，active=body、owns=false；微任务及下一帧仍相同。 |
| 外部普通输入持焦时禁用未聚焦的 range | 禁用前后 active=external，未被观测装置移走。 |

前两例没有调用 blur()、body.focus() 或删除元素来制造结果。浏览器只使用自有空 profile、about:blank 与内嵌 HTML；context offline、页面 route 全拒绝，实际页面请求记录为零，没有应用/API/HTTP 服务、Docker 或真实身份材料。这是页面请求观察，不冒称系统级网络抓包。

资源终局：await context.close 已返回，Node 正常退出，无超时/清理 kill 信号。12 个实际观测到的自有进程按 PID+starttime 两次核对均不存在，浏览器进程基线前后均空；driver 只回收自己的被领养后代。自有 profile/cache/tmp 已清理，runtime 为空，清理前轻量文件指纹留存，不归档缓存或二进制。资源证据见 `process-final.json`、`process-second-check.json`、`runtime-cleanup.json`。浏览器窗口已交回主线程。

该结果证明 UI02 在“题删除时才判断 contains(activeElement)”之前确有原生失焦路径。UI03 的焦点记忆/用户移动保护仍需固定差量独立静审；正式浏览器还须观察真实成功、拒绝及 30s 超时等页面接续。后续步骤另按主线程授权执行。本报告、源码及证据冻结后不再改写。
