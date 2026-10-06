# 遮罩关闭焦点修复验收

结论：共享 `UiDialog` 遮罩关闭焦点修复及 `UiDrawer` 继承行为已接受。`directory_backend` 实现四路径，`recovery_verification` 独立 PASS；主线程采纳提交推送 `b53895f7eb1d020276e8f54a99a7c0821b286481`，并核对远端一致。本报告只接受这一共享组件结果，系统用户目录 UI 正重新组合验证，尚未接受。

## 1. 固定输入与变化

[工作卡 rev1](../work-items/d27-dialog-outside-focus-repair.md)被审稿 SHA-256 为 `787a893476b2f925ea1d855a9ee5616517252f3a0a8f1393ba8baf9d055fdb5e`。业务基线为 `0f68d445df66bb05228fca3604618d4f2a7e6ffa`；[作者四路径输入](dialog-outside-focus-repair-verification-evidence/inputs/author-input01.json) SHA-256 `5a2300ba7a852feea5ad39ea48aa20c1e10f04e3ca4d76a5d335b0e3ea01a140` 与接受提交逐字节匹配：

- `web/src/components/ui/UiDialog.vue`：唯一生产变化，保留模板 `.self`，仅顶层且允许 outside 时先同步 `event.preventDefault()`，再调用原 `close('outside')`。
- `web/src/tests/dialog-outside-focus.spec.ts`：真实组件的关闭门禁、事件原因与兼容单测。
- `tests/account-captcha-web/dialog-outside-focus.config.js`、`e2e/dialog-outside-focus.spec.ts`：独立真实组件浏览器壳及回归。

没有修改 `useLayer`、`UiDrawer`、样式、关闭策略或锁文件，没有增加延迟补焦点。独立[十文件组件输入](dialog-outside-focus-repair-verification-evidence/inputs/independent-input01.json) SHA-256 `c37736517c75b406841fc62b6e92ec2fdab01b72ac47b31c33014d7e4aa1856a` 的九项依赖直接取已提交基线，只有 UiDialog 替换为候选。作者[隔离检查输入](dialog-outside-focus-repair-verification-evidence/inputs/author-frozen-input.json) SHA-256 `f2e2008a6d7161110c11535a003d428e03fa0ffc1ff62f07307c8f285a5e2506` 记录完整必要指纹；没有消费活动系统用户页源码。

## 2. 原失败与实际检查

[作者原报告](dialog-outside-focus-repair-verification-evidence/author/report.md.txt)和[独立原报告](dialog-outside-focus-repair-verification-evidence/verification/final-report.md.txt)保留实际命令、输入、结果与限制；逐文件原路径和 SHA 见[去重索引](dialog-outside-focus-repair-verification-evidence/archive-index.json)。下表退出码均来自原命令记录，未由文档任务重跑。

| 检查 | 实际结果与证据 |
| --- | --- |
| 原业务 UI `new02` | driver exit1 / 72.622s：Read PASS 6.74s，Authority FAIL 6.76s，Navigation FAIL 12.49s。Navigation 的原第544行在真实遮罩点击后要求 trigger focused，持续5s未满足；先前 Escape 已通过。[原 raw](dialog-outside-focus-repair-verification-evidence/history/new02/raw.log)及命令/输入/资源记录保留，不算本组件或完整 UI 通过。 |
| 原生 DOM 机制 | `focus-mechanism01` exit1 / 0.529s，长 TMPDIR 导致 Chromium Unix socket 在页面启动前失败；原命令、raw 和定点清理保留。短目录的 `focus-mechanism02` exit0 / 1.189s，[事件原件](dialog-outside-focus-repair-verification-evidence/history/focus-mechanism02/events.json)显示不取消默认行为时 trigger→body，先 preventDefault 后保持 trigger；这里只证明机制。 |
| 作者旧组件 `old01` | exit1 / 6.643s；390px、normal motion 的同一真实 pointer/click 断言复现失焦。[原 raw](dialog-outside-focus-repair-verification-evidence/author/runs/old01/raw.log)、旧 UiDialog、原测试/config、事件、截图和 trace 保留。 |
| 作者候选 `new01` | exit0 / 21.443s，11/11、0 skip、0 retry。[原 raw](dialog-outside-focus-repair-verification-evidence/author/runs/new01/raw.log)及 Playwright 原结果保留；旧红与新绿的浏览器断言/config 原字节相同，十文件闭包仅 UiDialog 不同。 |
| 隔离 `pure01` → `pure02` | 首轮 exit1 / 9.534s，160 PASS、1 FAIL：私有副本遗漏旧 styles 测试读取的已提交颜色文档。只补该基线文档后，`npm run check` exit0 / 12.065s，13文件/161测试、格式、类型和生产构建通过。[首红](dialog-outside-focus-repair-verification-evidence/author/runs/pure01/raw.log)、[最终 raw](dialog-outside-focus-repair-verification-evidence/author/runs/pure02/raw.log)均保留。 |
| harness 纯检查 | 首次 standalone tsc 缺 web Node type root，工具记录 exit2，源码未改；补显式 `--types node --typeRoots web/node_modules/@types` 后，config语法、类型和四文件格式均 exit0。见[首次工具记录](dialog-outside-focus-repair-verification-evidence/author/runs/harness-types01-tool-result.json)及[最终命令](dialog-outside-focus-repair-verification-evidence/author/runs/harness-pure02/terminal.json)。独立首版 probe 的单独严格类型检查也 exit0；不将它外推为所有后续文件重新类型检查。 |
| 独立真实组件 | 同一个独立顶层执行两轮：`independent01` exit0 / 4.609s，后为验证壳补 nonce 等来源限定及转换缓存清理，组件与行为断言不变；`independent02` exit0 / 6.361s。[首轮 raw](dialog-outside-focus-repair-verification-evidence/verification/runs/independent01/raw.log)、[最终 raw](dialog-outside-focus-repair-verification-evidence/verification/runs/independent02/raw.log)及两版 probe 均保留，不写成两个不同独立组。 |

作者11例覆盖 UiDialog/UiDrawer × 390/1440 × normal/reduced-motion，以及禁用 outside/Escape、内部编辑、action 和嵌套恢复。独立探针另核原生完整 pointer 序列、默认行为取消门禁、嵌套顶层返回下层触发节点和后续用户焦点不被抢回；记录的 defaultPrevented 为 true、true、false、false、true。被上层遮挡的下层 overlay 使用合成事件，只计非顶层门禁。jsdom 单测和原生 DOM 对照没有替代真实 Vue 组件浏览器结果。

[实际版本](dialog-outside-focus-repair-verification-evidence/author/versions.json)：Node24.19.0、npm11.9.0、Vue3.5.43、Vite8.3.1、Vue插件6.0.9、Vitest4.1.11、VTU2.5.1、TypeScript5.9.3、Playwright1.56.1、Chromium151.0.7922.173。Chromium 二进制 SHA-256 为 `d387400aaf740ccb75e5e996a34aa0940e6e97683a4eabd6ae34c1eed6804723`，未复制二进制。组件壳只服务自有 nonce/loopback，单worker、零重试、每例45s；不调用账号 API，不启动后端、PG、MinIO、SMTP 或 Docker。可用注册工具中没有 Playwright 技能，本轮按主线程授权使用仓库锁定 harness。

## 3. 资源终局与保留限制

**作者旧红不是全清零轮次。** `old01` 的首 runner 未设 subreaper，主命令退出、server 关闭后，PID `180182`、`180185`（starttime 均 `408854`）仍为 PPID1、Z、cmdline 0B。它们已经退出；当前验证者不是父进程，无法 `waitpid`。[独立只读核验](dialog-outside-focus-repair-verification-evidence/verification/author-old01-orphans.json)与原终局双次观察均保留，未触碰 PID1 或其他进程。旧轮仅自有转换缓存的后续删除另记；后轮成功不证明这两个旧 PID 已由当前父进程回收。作者目录中的 `run-browser.py` 是后轮修正版本，不能冒充首次缺 subreaper 的脚本原件。

作者 `new01` 启用 subreaper，实际等待主命令并记录4个 adopted wait；41个观察 PID/starttime 双次为空，server 关闭、私有 TMPDIR 移除，原监听端口不变。两轮独立命令均 actual wait、server.close 完成，各13个所属 PID/starttime 消失、4个 adopted wait，监听基线保持。`independent01` 原 `result.json` 的 **clean=false** 保持原字节：当时只剩4个任务自有转换缓存，随后[post-run-cleanup](dialog-outside-focus-repair-verification-evidence/verification/runs/independent01/post-run-cleanup.json)记载实际删除和两次最终检查。`independent02` [双次终局](dialog-outside-focus-repair-verification-evidence/verification/runs/independent02/cleanup.json)直接满足 clean=true；不回写首轮结果。

历史 UI `new02` 的七项 exact-ID 资源、115个所属进程及双清记录随原失败保存；原生机制第二轮的15个所属 PID、4个 wait 和空 runtime 也有原件。本报告没有操作这些资源，更未读取或归档后续活动 UI `new03`。

## 4. 持久证据与后继门槛

[证据说明及离线核验](dialog-outside-focus-repair-verification-evidence/README.md)保存110份逻辑原件、96份不同字节原件，按 SHA 共享重复副本；源码重建使用 Git 基线加四条精确候选，不复制完整 web、node_modules、缓存、dist 或可执行文件。离线脚本只读归档和本地 Git，核四路径接受提交、组件/纯检查必要输入、原日志与终局事实；不启动任何原测试或 driver。

基础修复先独立交付，后续[系统用户目录 UI](../work-items/d27-system-user-directory-ui.md)必须固定消费 `b53895f`，重跑原 Navigation 遮罩焦点、Escape、内部导航和布局组合；原焦点期望保持，不能手工 focus 后再断言。本结果不涵盖原 UI Authority 观察失败、业务页表格换行或完整 UI 接受，也不包含邀请读口/00019、其他浏览器、生产 SPA 托管或 Vite 业务代理验证。

Summary 待决、Object/tools 原任务停止、Artifact/Project 阻塞、生产未绑定与 ready503 边界不变；完整 D08–D28/E01 未完成，E01 未开始。当前文档交付只归档已冻结事实、核对字节与链接，没有重跑产品测试或执行 Git 写操作。

六份 Markdown 的188个本地链接、11个 fragment、UTF-8/LF与格式检查通过；台账旧正文、continuation既有段落及两卡技术正文逐字节保留。100个新增文件均做 whitespace 检查，其中三份原始失败日志保留自身尾空格/末空行，具体路径见证据说明；其余无诊断，不把原件格式提示写成全文件零告警。
