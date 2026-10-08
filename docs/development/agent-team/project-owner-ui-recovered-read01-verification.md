# Project Owner UI 恢复后 read01 失败与退役验收

2026-10-08，环境恢复后的新实现 `read01` 保留 **FAIL**；独立审查只接受该轮实际任务所有权范围内的失败退役证据。不是完整 Owner Read、D27 或浏览器验收通过，也不是重建此前遗失的历史 read01/read02。原件已停止写入，后继 browser-v3/resource-driver-v02 不属于本轮输入。

绑定[作者 handoff](project-owner-ui-recovered-read01-verification-evidence/originals/author/read01-handoff.json) SHA-256 `21085e942b887d083b3b9e0e88e3766e4d03245fad8c211acb116f50b27b2200`，其93个运行原件逐项字节、长度、SHA均相同。独立 runtime 审查已冻结：[原报告](project-owner-ui-recovered-read01-verification-evidence/originals/independent/review.md) SHA `91bd78d6763577fc1d6626bd737b634900ac9b2c822b7f9aacc74cd3232e995c`、[原证据](project-owner-ui-recovered-read01-verification-evidence/originals/independent/evidence.json) SHA `19be55ac6d251b9a1e5a6a2ff6de1497791d0984d82dae022482fa49879c27e4`。本次归档没有执行新业务、资源、网络、探针、进程停止或 Git 写操作。

## 原失败与契约归因

原[命令](project-owner-ui-recovered-read01-verification-evidence/originals/run/command.json)是唯一获准的 new-read：

```text
sh scripts/test-objects.sh -run '^(TestAccountProjectOwnerWebReadAndNavigation)$'
```

完整固定 builder/package 链实际114.299秒、exit1；唯一选中 Go top 实际17.21秒 FAIL。[原 raw](project-owner-ui-recovered-read01-verification-evidence/originals/run/raw.log) SHA `c5cf12b7a1cf93e4d060d03eff89c89966c4ae3e04ae98006884f544029f0e8d`，[原 result](project-owner-ui-recovered-read01-verification-evidence/originals/run/result.json) SHA `d1b1377b225ba356b11869f01e4641e8eb8fc38ab1578dc0feb60e6f6d99c05b`，`accepted=false`、`selected_tests_passed=false`。日志中的其它包 `[no tests to run]` 不计作 Owner 场景通过。

失败发生于冻结 browser-v2 的 `project-owner.spec.ts:465`：进入 deleting Project 的 `/settings/general` 后，等待固定 heading“项目不可用”5000ms，locator未找到。此前最后一个真实[sidecar](project-owner-ui-recovered-read01-verification-evidence/originals/run/safe-http-evidence/TestAccountProjectOwnerWebReadAndNavigation/response-035.json)是 `GET /api/v1/projects/resolve`、409、`application/problem+json`；对应[原 body](project-owner-ui-recovered-read01-verification-evidence/originals/run/safe-http-evidence/TestAccountProjectOwnerWebReadAndNavigation/body-5c481df4498f0f277a63120b04d4620e464a13cdea76f479af74adab6239dfe6.json)为 `PROJECT_NOT_ACTIVE`、`commit_state:not_committed`，body SHA `5c481df4498f0f277a63120b04d4620e464a13cdea76f479af74adab6239dfe6`。

独立 runtime 对照[D09](../work-items/d09-project-usage-read-http.md) §32、正式 Authority→Read gate和[D27](../work-items/d27-project-owner-workspace-ui.md) §3.2：deleting按原typed gate拒绝、HTTP不发布候选符合契约。D27禁止展示删除中内容，但未要求所有409都映射为unavailable或使用固定“项目不可用”标题。冻结 ui-v1 的 denied分支仅含401/403/404，本409归入read-error，固定view对应“项目信息读取失败”。这是源码与真实响应的关联推导；**本轮没有DOM快照，不能写成已独立观察到该实际标题**。

必修仅为#21测试的过强标题期待：按既有read-error语义核真实Resolve409/PROJECT_NOT_ACTIVE、无后续稳定ID Get/写请求、无form/ProjectNav及返回列表。产品19源、API/parser/Session、Go和协议不因本次失败修改。后继v3的窄修及任何重跑须以其自己的冻结输入、授权与原件另记，不覆盖本轮红结果。

## 输入与安全响应原件

本轮[原授权输入](project-owner-ui-recovered-read01-verification-evidence/originals/run/frozen-input.json) SHA `af1c94db9b1ab1a4155b1dbc150c9f592b573013c4fd1062682883137091fb72`，绑定 driver `bb666f55bf39f6454a370f62459d3e72416d90767f14a75d2e99775b8c11b38e`、Go candidate04、browser-v2 manifest `9ca0cefb52b4c67d1d5f84600c6bdecb6886fda0927898cff0ca24365cb3ccae`、ui-v1与当时53文件dist。原 Node/Chromium/Go/MinIO、两个固定PG镜像、依赖与实际输入摘要均保留，没有用恢复后的活动工作树代替本轮冻结图。

[source-map](project-owner-ui-recovered-read01-verification-evidence/source-map.json)提供34个必要稳定Git源定位，UI19源复用产品 `088f4d3490db4d86781090f0602299901c5f3247`。原closure加冻结delta的955个仓库输入经只读Git核对：951个与088f逐hash一致，其余4个未提交源与Go04/browser-v2副本一致。归档保留四个原字节文本副本，使用`.txt`后缀避免进入Go/测试发现：

- [Go fixture](project-owner-ui-recovered-read01-verification-evidence/frozen-sources/tests/account/project_owner_web_fixture_test.go.txt)与[Go tops](project-owner-ui-recovered-read01-verification-evidence/frozen-sources/tests/account/project_owner_web_test.go.txt)。
- [browser-v2 config](project-owner-ui-recovered-read01-verification-evidence/frozen-sources/tests/account-captcha-web/project-owner.config.js.txt)与[browser-v2 spec](project-owner-ui-recovered-read01-verification-evidence/frozen-sources/tests/account-captcha-web/e2e/project-owner.spec.ts.txt)。

未复制903项Go源树、完整base graph索引、依赖/工具实体或dist实体。原图SHA、只读Git比对摘要、必要delta/manifest保留；绝对路径和原结果不为迁移而改写。归档不是一次新的工具恢复或闭包重建。

35份sidecar指向34个去重原body，逐SHA和run/input绑定一致：28个200 Project Get来自浏览器启动前的正式fixture准备，6个200 List及末次Resolve409属于后续阶段。不能把这28个准备读取当浏览器的28个详情场景。原body按字节保存，没有重新序列化或替换为投影摘要。

**本轮 `verifyBodies` 未到达，same-body schema与公开client检查均未执行。** 归档时JSON语法解析与hash校验只说明原件完整，不构成端点schema/refinement或client PASS。

## 实际退役与非所有进程

[独立证据](project-owner-ui-recovered-read01-verification-evidence/originals/independent/evidence.json)与原[cleanup两帧](project-owner-ui-recovered-read01-verification-evidence/originals/run/cleanup.json)、[observed resources](project-owner-ui-recovered-read01-verification-evidence/originals/run/observed-resources.json)、[adopted waits](project-owner-ui-recovered-read01-verification-evidence/originals/run/adopted-waits.json)证明：

| 范围 | 实际终态 |
| --- | --- |
| Go/Node/Central | 原log记录Node直接子进程actual wait、proxy Serve及全部body handler join、Project准备服务join后释放ProcessGuard、default root join |
| 外层owner | direct PID82997/start516107实际wait，exit1；4个adopted PID89218/89219/89220/89215（start均521280）实际wait，exit0；watchdog线程join |
| 资源与私有目录 | 7个精确ID（4容器、3network）拓扑一致且两扫逐ID absent；owned PID、general/browser private runtime、新资源两扫空，原Docker baseline不变 |
| 输入与控制 | before/after及verification输入相同；forced tail action、monitor error、cancellation均0；top watchdog完整 |
| TCP补充观察 | 含TIME_WAIT的host TCP/tcp6 delta在额外7.235秒后两扫空；此polling不是连接归属或所有短连接的完整轨迹，不据此signal进程 |

4个新PID1 `containerd-shim` zombie单列为daemon所有：83411/start516592、84098/start517308、84466/start517522、85086/start517791。它们不在本driver owned表，未signal、未冒称wait/join；历史PID1 zombie也未动。这里的退役PASS不等于全机清零。

本轮实际覆盖正常注册cleanup处理浏览器断言失败；没有注入任意Python post-Popen/记录异常，不能将本轮退役结果扩大为所有异常路径均动态验证。

root在真实reader已退役后恢复原3文件 `web/dist`，[原恢复记录](project-owner-ui-recovered-read01-verification-evidence/originals/root/restore-after-read01.json) SHA `d77915d321b78e1804369e8007ef2a714743dba28f16fcf9b2e292a3dfaf5faa`，另保留53文件测试dist。此后置恢复没有改写本轮before/after证据；后续资产窗口也不混入此次结果。

## 未到达项与归档检查

第465行失败后，第466行“无form”断言也未执行；第467行dotted direct navigation、随后nonowner/admin不豁免与旧名复用场景均未到达。没有same-body schema/client、点名直链或视觉PASS，截图为0。完整read-top、后续新场景、旧16个真实top、独立A/B与完整D27仍未通过；[此前静态与受控29项](project-owner-workspace-ui-controlled-verification.md)及[API258项](project-owner-ui-client-verification.md)保持各自有限接受范围。生产SPA/发布及其它停止项没有解除或重试。

本次只新增本报告与对应证据目录。[归档检查](project-owner-ui-recovered-read01-verification-evidence/archive-checks.json)核111个逻辑原件、109个物理原件，共403653 bytes，包含93个原run文件、作者/独立结论、必要manifest、恢复记录与四个小源。无私有material、password/key/cookie文件、截图或资产实体。

[格式例外](project-owner-ui-recovered-read01-verification-evidence/format-exceptions.json)登记70个原件：35 sidecar与34 body共69份JSON原无末换行，另原raw第48、50、53、55、60、64、65、74、75、78行有尾空格；CRLF/独立CR均0。每个新增文件实际作`git diff --no-index --check /dev/null <file>`检查，原字节不改。JSON、链接、Git定位与hash检查是文档归档验证，没有重新执行本轮或后继业务。
