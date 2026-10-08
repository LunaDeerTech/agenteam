# Project Owner 工作区 UI 静态与受控验收

2026-10-08，固定 ui-v1 的 19 个前端源完成限定独立 **PASS**，无未关闭必修，root 已采纳。产品提交 `088f4d3490db4d86781090f0602299901c5f3247` 已推送并由 root 核远端一致。本次接受 Session、workspace、App/router 与项目页面的静态审查及受控补集；不是完整 D27、真实浏览器、PG/API 集成或生产 SPA 托管接受。[D27 规格](../work-items/d27-project-owner-workspace-ui.md)其余门槛保持。

审查者未参与产品实现，产品零写入。[19 源冻结表](project-owner-workspace-ui-controlled-verification-evidence/originals/author/ui-v1/freeze.json) SHA-256 为 `b7a9ef34a10d0230b541d0daffb19d765d023aa27469a9ca144952f5d1bd9f64`；原副本、实际运行输入、最终产品提交逐项相同。[source-map](project-owner-workspace-ui-controlled-verification-evidence/source-map.json)以 Git blob 和 SHA 定位源码：19 源复用产品 088f，其余独验必要源码复用接受基线 `7dbd42a3fb70f9cea94c21a5c3f77197d65b04b2`，不复制产品树。

此前[封闭客户端与纯安全返回路径的 258 项独验](project-owner-ui-client-verification.md)继续复用，没有整块重跑。本次 `auth.ts` 只增加 Project 导航注册与守卫，纯 route/return 解析字节未变。独立静审覆盖新增 Session Project 分支及旧权限谓词、workspace controller、App 生命周期与确认链、正式 router、ProjectNav、五个 Project view、对应测试差量；`system-user-directory.spec.ts` 仅更新合法新增导航期待。

## 本次独立实际检查

[原结论](project-owner-workspace-ui-controlled-verification-evidence/originals/independent/final/result.json)与[原报告](project-owner-workspace-ui-controlled-verification-evidence/originals/independent/final/review.md)保持交付时字节。实际命令为：

```text
python /workspace/scratch/owner-ui-verification/ui-v1/run_checks.py run02
```

[driver 原件](project-owner-workspace-ui-controlled-verification-evidence/probes/run_checks.py)以绝对路径启动 Node 24.19.0、Vitest 4.1.11，使用 jsdom、单 worker，预算45秒。原 [argv/cwd](project-owner-workspace-ui-controlled-verification-evidence/originals/independent/run02/command.json)、[stdout](project-owner-workspace-ui-controlled-verification-evidence/originals/independent/run02/stdout.raw)、[Vitest JSON](project-owner-workspace-ui-controlled-verification-evidence/originals/independent/run02/vitest.json)及[结果与指纹](project-owner-workspace-ui-controlled-verification-evidence/originals/independent/run02/result.json)保留。

最终实际 2.006 秒、exit0、29/29 通过：11 个 owner/身份场景、18 个 workspace 场景。参数内的多个断言没有重复计数。37 个固定源码加工具、锁与探针共52个输入前后相同；direct actual wait 完成，adopted wait 为0，两次已观察 owned PID 扫描均空，不声称全机进程清零。

| 补集 | 证实的受控行为 |
| --- | --- |
| 唯一 Cookie owner | 原生 Response/ReadableStream 的 cancel 成功与拒绝两种 actual 尾部，均保持 busy；14 个旧 mutator 逐一被 busy 拒绝，Logout/restore 零新增派发。反向 personal owner 同样阻止 Project update，且不遗留新 intent |
| 权限与身份 | Project 403/404 不污染 System denied；System 拒绝不撤销普通 Owner 请求。当前401/CSRF失效清共同身份。旧 success/401/403 尾部不发布旧内容，owner 阻止重叠恢复，随后新 Session 正常读取 |
| 地址与两次授权 | 非法 raw path 零 Project 请求；Resolve 候选不发布，随后稳定 ID Get 的403/404清内容。旧路由 Resolve actual 尾部结束后，才进入新对象 Resolve→Get |
| 草稿与冲突 | 版本冲突保输入和原 expected_version；先 fresh Get，再显式采用，取消保草稿。同 identity checking 隐藏后可恢复草稿；Session/CSRF变化清草稿。重复离开确认不产生写入 |
| 原意图恢复 | Unknown 粘性；受控 in_progress 禁原重放、not_observed 不降格；后续草稿变化不能改变原 body/key。lookup 历史回执与当前 Get 分开，当前 Get 决定标准地址；合法同版本 no-op 被确认 |
| 旧双槽与聚合 | Project 本地确认不清四用途/会议 Summary 两个独立未决 intent；原聚合取消保两槽，明确确认才丢两槽。既有两个实际 UI draft 的公共交互另复用同输入作者全量组件回归，不将此两槽探针称为真实页面两份 draft |

补集原件为[共享 owner/身份探针](project-owner-workspace-ui-controlled-verification-evidence/probes/ownership.independent.spec.ts)、[workspace 探针](project-owner-workspace-ui-controlled-verification-evidence/probes/workspace.independent.spec.ts)与[受控 Fetch 装配](project-owner-workspace-ui-controlled-verification-evidence/probes/fixture.ts)。使用正式客户端解码、真实 Session/controller，Response 流是真实 Web API 对象；HTTP/lookup/版本内容均受控，无网络、PG 提交、服务 Unknown 或真实三态事实。

## 作者最终证据复用

[输入对应核验](project-owner-workspace-ui-controlled-verification-evidence/originals/independent/final/author-binding.json)验证以下三轮原命令、结果、raw SHA及实际 wait/两扫空。每轮180个前后输入相同，19 源对应 ui-v1，剩余161个对应 Git 7db；不是只采纳作者口头声明。

| 原轮 | 实际结果 | 本次用法 |
| --- | --- | --- |
| [integrated-unit-all01](project-owner-workspace-ui-controlled-verification-evidence/originals/author/integrated-unit-all01/result.json) | 52文件、2077/2077，通过；26.377秒 | 复用新页面公共交互及全部旧 personal/System/Selection/Summary兼容回归 |
| [integrated-build01](project-owner-workspace-ui-controlled-verification-evidence/originals/author/integrated-build01/result.json) | 完整 `vue-tsc --noEmit` 后 Vite production build，通过；12.117秒 | 复用固定完整类型图和构建，不称独立重跑 |
| [integrated-format-check01](project-owner-workspace-ui-controlled-verification-evidence/originals/author/integrated-format-check01/result.json) | 19源 Prettier通过；1.449秒 | 复用固定源格式检查 |

另独立核对任务自有 `dist-ui01` 的53项文件与[资产 SHA 表](project-owner-workspace-ui-controlled-verification-evidence/originals/author/ui-v1/assets.json)完全相同，manifest SHA `00bd636e8446c8e75690bcf07745975bd7a4f9b906172de4d3ae55ab10393752`，没有 Debug chunk/源码导入命中。此项是产物静态检查，没有启动生产发布或浏览器。定点 `git diff --check 7db -- <19 paths>` 及19源含未跟踪文件的行尾/尾空格检查通过。

## 保留的失败原件

独验 [run01 原结果](project-owner-workspace-ui-controlled-verification-evidence/originals/independent/run01/result.json)为28/29、exit1、2.021秒，输入同、两扫空。唯一失败是探针在未激活旧 System 页时调用 aggregate，却期待返回 true；既有 `activeRoute` 条件正确拒绝。只补探针的正式 `afterNavigation('/system/model-selection', '')` 上下文，未改产品。[原失败探针](project-owner-workspace-ui-controlled-verification-evidence/originals/independent/run01/ownership.independent.spec.ts)与当轮 SHA 一致，[原失败日志](project-owner-workspace-ui-controlled-verification-evidence/originals/independent/run01/stderr.raw)保持。这不是产品红。

作者历史失败与其原 command/raw/result、before/after SHA均保留，详见[失败分类](project-owner-workspace-ui-controlled-verification-evidence/author-failure-summary.json)。失败源没有单独旧副本，不能由 hash 重建旧字节或声称已重现历史实现。

| 原轮 | 原始结果及修正归因 |
| --- | --- |
| [state-unit01](project-owner-workspace-ui-controlled-verification-evidence/originals/author/state-unit01/raw.log) | 37通过/1失败：rename成功后 dirty仍true。作者说明原 baseline为普通对象，后改 reactive，使采用当前值/改名后的 computed重新计算；这是冻结前修复的实现缺陷。最终源及同输入全量回归包含此行为，历史源码差量未重建 |
| [state-unit04](project-owner-workspace-ui-controlled-verification-evidence/originals/author/state-unit04/raw.log) | 44通过/1失败：旧mutator矩阵一项先返回invalid-input。作者说明 invitation测试改为合法email，才到达busy检查；没有放宽生产校验。最终合法输入与独立14项busy结果另有证据 |
| [integrated-unit01](project-owner-workspace-ui-controlled-verification-evidence/originals/author/integrated-unit01/raw.log) | 124通过/3失败：按钮helper把aria-hidden内容计入textContent，找不到公共按钮。最终helper按aria-label或排除aria-hidden后的文字定位；最终2077包含三个场景 |

作者给出的历史修正原因与独立观察的原失败、最终源和最终验证分别登记，不把缺失的历史源码说成已独立复原。此前 API 阶段的失败保留在其[原永久报告](project-owner-ui-client-verification.md)，不重复复制。

## 边界与归档检查

本次没有运行浏览器、Go、PG、MinIO、listener、真实资源或停止探针。未接受八组合布局、焦点/键盘、真实登录与撤权、26项目数据库分页、真实响应丢失、实际HTTP同body schema/client、新5个或旧16个真实top；后续由冻结的 runtime/browser 输入另验。完整D27、production SPA/fallback/发布及生命周期停止链没有因本小块而解除。

仅新增本报告与对应证据目录。55个逻辑原件按SHA复用为45个文件，共194439 bytes；37个源码引用复用Git。没有复制源码树、cache、node_modules、二进制、dist或大body。原脚本中的绝对路径与结果不为归档重写；迁移后通过source-map找到原件和固定Git输入，不能直接把活动工作树当旧输入运行。

[归档检查](project-owner-workspace-ui-controlled-verification-evidence/archive-checks.json)核JSON、链接、原字节/SHA、Git定位及格式，没有重跑已通过的产品检查。[格式例外](project-owner-workspace-ui-controlled-verification-evidence/format-exceptions.json)精确登记7个原件：两份Vitest JSON缺末换行，5份raw末尾有空行；本组原件无CRLF或独立CR。每份新增原件实际使用 `git diff --no-index --check /dev/null <file>` 检查，原字节保持，不为消除诊断而重排日志。
