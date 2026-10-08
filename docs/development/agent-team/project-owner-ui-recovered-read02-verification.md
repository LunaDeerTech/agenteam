# Project Owner UI 恢复后 read02 局部验收

2026-10-08，作者实际执行的 `read02` 与任务所有权范围内退役证据，经独立复核 **PASS**。范围只有 `TestAccountProjectOwnerWebReadAndNavigation` 和本次移交的四个 harness 源；独立审查者未另跑浏览器或资源。本轮不是完整 D27、独立真实 A/B、其它4新/16旧top或视觉验收。[read01 原失败](project-owner-ui-recovered-read01-verification.md)继续保留，不被本轮成功覆盖。

四个 harness 路径已接受并提交推送 `be34ff9916d3e2354b4edd33df08c99021b02c2c`，root核远端一致；UI19源保持产品 `088f4d3490db4d86781090f0602299901c5f3247` 的原字节。绑定[作者 handoff](project-owner-ui-recovered-read02-verification-evidence/originals/author/read02-handoff.json) SHA-256 `7c1613351fcbff44fe2fb835413b738f366e55bf41d966f292afe43b525f288c` 的111个运行原件，数量、长度、SHA与文件集合核同。独立 runtime 的[原报告](project-owner-ui-recovered-read02-verification-evidence/originals/independent/review.md) SHA `9274ffee79b67ca6a962f7caf27c5d2623ad2ce06c2e109b99ed6b91d2ac2a00`、[原证据](project-owner-ui-recovered-read02-verification-evidence/originals/independent/evidence.json) SHA `c075b5c1fac0ac83d2609e997ae3d09f646260ae3a13ff32bc363d642f0d4001`均已STOP。

## 实际命令与冻结输入

唯一获准的 new-read 仍是原完整固定 fixture chain：

```text
sh scripts/test-objects.sh -run '^(TestAccountProjectOwnerWebReadAndNavigation)$'
```

[原命令及wait](project-owner-ui-recovered-read02-verification-evidence/originals/run/command.json)实际 exit0、59.722秒；唯一选中 Go top PASS 15.36秒，浏览器1 case/1 worker PASS 8.0秒。其余包的 `[no tests to run]` 不计作已验证Owner场景。[原raw](project-owner-ui-recovered-read02-verification-evidence/originals/run/raw.log) SHA `bd8ed5c8b376800bd52d44091397bdce3ec46dcc0332bc0c94e7a9e012c3de8d`，[原result](project-owner-ui-recovered-read02-verification-evidence/originals/run/result.json) SHA `f3dccd94e94af3ea82d25235811a423245b32cdd687dc63f3aff162d135e79ef`。

[本轮授权输入](project-owner-ui-recovered-read02-verification-evidence/originals/run/frozen-input.json) SHA `b3f7cc6153c15a84995bc864d21549e8926de54b3508e9d8c76d6efb06968eed`，绑定Go04、browser-v3 manifest `55f8ebe064a27558bbf3c822c825d4b35cc4fa5433dd3a71258343da5f97c3d8`、原driver `bb666f55bf39f6454a370f62459d3e72416d90767f14a75d2e99775b8c11b38e`、原工具/锁/镜像/53-file dist。root truecopy只改变本轮授权字段、授权记录和状态，范围为new-read/read02。

[source-map](project-owner-ui-recovered-read02-verification-evidence/source-map.json)给出38个必要Git源定位；19UI指向088f，四个harness及其余稳定源指向be34。只读递归展开固定final02 delta→final01 delta→原closure后，955个仓库输入逐hash均匹配be34。没有复制Go/JS产品源码、整个源树、base graph大索引、依赖/工具或资产实体；必要小manifest与原driver通过原字节保存或复用read01已提交原件。

## 同一响应 body 的实际检查

46份闭集sidecar绑定39份去重原body，46个原生request ID唯一，body SHA、source_run及本轮input hash均一致。作者固定Python schema checker实际exit0，stdout为`46`，见[原schema结果](project-owner-ui-recovered-read02-verification-evidence/originals/run/safe-http-evidence/TestAccountProjectOwnerWebReadAndNavigation/schema-validation.json)。原[同body检查结果](project-owner-ui-recovered-read02-verification-evidence/originals/run/safe-http-evidence/TestAccountProjectOwnerWebReadAndNavigation/same-body-validation.json)记录：

| 实际检查 | 数量 | 范围 |
| --- | --- | --- |
| schema | 46 | 全部sidecar对应原body，包括准备与IPC响应 |
| 浏览器公开client List | 6 | 真实分页、limit及生命周期query |
| 浏览器公开client Resolve | 3 | 实际username/project_name与当前owner上下文 |
| 浏览器公开client Get | 3 | 对应稳定project_id与owner上下文 |
| 浏览器公开client Problem | 4 | deleting409及无权/旧名等404 |

16次浏览器client读取与46次schema校验分开计算。001–028为浏览器前fixture正式准备GET；042 GET及043 PATCH为外部改名IPC helper，均不计入浏览器数量，也不把IPC PATCH称为页面保存已验。

冻结browser-v3观察器按浏览器原生`X-Request-ID`匹配sidecar，核实际endpoint/status/hash，再将原bytes、status、Content-Type及request ID构造Response供公开client消费。List使用实际URL中的limit/cursor/lifecycle，Resolve使用实际username/project_name及当时owner，Get使用匹配的project_id/owner。参数绑定证据来自已独审的固定实现与实际完成断言；readFacts中的完整浏览器URL/owner仅驻内存，没有额外持久化跟踪可供冒称。

本次归档仅检查这些原结果、原字节与JSON语法，没有重新执行schema或client；此处schema/client PASS来自作者真实轮和独立原件复核。

## 本轮完成的读取行为

冻结浏览器断言在实际通过的case中完成：普通Owner登录回跳`/projects`、25+4分页且无重复及50项选项、archived/deleting筛选、deleting行无内容链接/描述、deleting真实Resolve409 `PROJECT_NOT_ACTIVE`的read-error终态及零后续Project Get/写请求、无form/ProjectNav/内容/事实、可显式重读与返回列表；普通非Owner与admin均被拒绝；外部改名后旧地址失效，旧名复用后进入另一个稳定ID。

点名直链使用真实浏览器的036→037（home）与038→039（settings）Resolve→Get，[036](project-owner-ui-recovered-read02-verification-evidence/originals/run/safe-http-evidence/TestAccountProjectOwnerWebReadAndNavigation/response-036.json)、[037](project-owner-ui-recovered-read02-verification-evidence/originals/run/safe-http-evidence/TestAccountProjectOwnerWebReadAndNavigation/response-037.json)、[038](project-owner-ui-recovered-read02-verification-evidence/originals/run/safe-http-evidence/TestAccountProjectOwnerWebReadAndNavigation/response-038.json)、[039](project-owner-ui-recovered-read02-verification-evidence/originals/run/safe-http-evidence/TestAccountProjectOwnerWebReadAndNavigation/response-039.json)均指向`01a119f0-21f5-71d3-bee4-e32d16adc961`，原body SHA `00455333c2808d3584094021af93386d3e01578d69679c2118ccf83062d3f2ce`。013是准备GET，不用它替代浏览器证据。

大写点名直链HTML200、小写canonical path及`/settings`→`/settings/general`的证据是本轮通过的冻结浏览器断言；没有另存HTML/DOM快照或截图。直链托管属于本任务私有同源静态harness，正式API来自真实default root；不是Central生产SPA/fallback或发布验收。

## 实际退役与后置资产恢复

[原cleanup两帧](project-owner-ui-recovered-read02-verification-evidence/originals/run/cleanup.json)、[资源记录](project-owner-ui-recovered-read02-verification-evidence/originals/run/observed-resources.json)、[adopted waits](project-owner-ui-recovered-read02-verification-evidence/originals/run/adopted-waits.json)及独立证据核实：

| 范围 | 实际结果 |
| --- | --- |
| direct与adopted | direct PID108861/start637583实际wait、exit0；113264/113266（start640790）、113269/113270（start640791）四个adopted实际wait、exit0，身份均匹配观察记录；watchdog线程join |
| 正式组件尾部 | raw记录Node wait、proxy Serve/body handlers join、准备Project service在ProcessGuard release前join、default root join，然后fixture清理 |
| 精确资源与目录 | 4容器+3network共7个精确ID均两次absent；owned process、general/browser private runtime、新资源两扫空；原无关Docker baseline不变 |
| 控制与输入 | monitor/cancellation/forced action均0；1165个source/tool/dist等输入before/after相同，包括本轮53文件dist |
| TCP补充 | host TCP/tcp6全状态差量含TIME_WAIT，额外40.377秒后两扫空；只是主机polling，不证明所有短连接或其所有权，没有据TCP推断signal进程 |

四个新增PID1 `containerd-shim` zombie独立登记：109089/start637779、109443/start637995、109713/start638181、110342/start638388。它们非本driver所有，未停止、未声称已wait/join；owned退役通过不代表全机进程清零。

root最后TCP扫描完成后恢复原3文件assets，[原恢复记录](project-owner-ui-recovered-read02-verification-evidence/originals/root/restore-after-read02.json) SHA `d95b39988b77761a300a3d552cc6f7de1276832d85844e61cccf4626bb6d785e`，保留53文件测试资产于`/workspace/scratch/owner-ui-assets/test-dist-ui-v1`。后置恢复不是本轮输入漂移；后续edit窗口的资产与资源不属于本记录。

## 限制与归档检查

独立真实A/B、其它4新top、16旧回归top、布局/截图/视觉、真实response-loss或Unknown恢复三态、D10 Skills/runtime、生命周期参与者、任意Python异常/取消注入及生产SPA发布链均未由本轮接受。fixture的Skills准备和辅助终态事实也不等于这些运行能力通过。原停止项保持，[静态/受控29项](project-owner-workspace-ui-controlled-verification.md)与[API258项](project-owner-ui-client-verification.md)继续保持自己的边界。

本次只新增本报告与证据目录。[归档检查](project-owner-ui-recovered-read02-verification-evidence/archive-checks.json)核124个逻辑原件：新增114个物理原件581189 bytes，另复用read01已提交的8个物理原件、9个逻辑引用。原run111项、作者/独立结论、必要manifest及恢复记录均按source-map定位；共同原件按SHA复用，不改read01字节或结论。没有复制私有material、password/key/cookie文件、依赖、产品源码或资产实体。

[格式例外](project-owner-ui-recovered-read02-verification-evidence/format-exceptions.json)精确登记88个原件：46 sidecar、39 body及两份验证结果共87份JSON原无末换行；raw第32、34、37行有尾空格；CRLF/独立CR均0。新增文件逐件实际作`git diff --no-index --check /dev/null <file>`，原字节保持。JSON语法、链接、Git和hash自查没有重跑业务、资源、schema或client，也没有网络或Git写操作。
