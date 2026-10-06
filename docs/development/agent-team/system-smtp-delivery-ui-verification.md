# System SMTP 测试与投递任务 UI 验收

## 1. 接受范围与固定输入

主线程已采纳17路径完整结果，提交推送 `213cf5c3f552e6b05b541ce02afc1dd65ce9db93`，并核远端一致。冻结[工作卡rev1](../work-items/d27-system-smtp-delivery-ui.md)技术§1–7仍为 `bee4f7ddb2b480b8c57b7d1b6b81be36bfce9f726222ebc10c8b468b90a54cda`。[最终17路径清单](system-smtp-delivery-ui-verification-evidence/objects/f4cf3f32634243dfe2dde9d3bb88f6dba3c6dfbe5c56d4d1a659ecb60b61cd44)绑定16个受测源及README末件；README SHA为 `112a6fe33110a44e49a9eb6cc6a79bf0505fdbb73c7d6ccc31f1cea96998cc43`。

真实受测产品基线始终为 `628612cdfc730cc1d89cad4e24a5d20f36cc6812` 加作者input04；manifest SHA `620e88880ce309ee524d7670e1892e718b5b091e1dd2c1a15789fcab2464439e`。16源、36dist指纹、1083固定Git依赖与独立私有四源闭合。主线程核后续至3981888只改变文档，再整合17路径；接受提交绑定实际受测字节，不改写当轮受测HEAD或声称重跑主树。前置[SMTP配置](system-smtp-settings-ui-verification.md)、[管理读](system-mail-job-management-reads-verification.md)、[邮箱闭包](email-canonical-roundtrip-verification.md)与[本卡规格静审](system-smtp-delivery-ui-spec-verification.md)复用各自原边界。

## 2. 本次确认的行为

同一SMTP叶子显式切换配置和测试/任务区，使用四个固定API发起测试、分页/精确读取安全任务投影及申请人工重试周期。严格202确认请求接受，任务phase/实际attempt结果与客户端请求未确认分别表达；当前GET、Session和版本推进均不是原写receipt，没有新增lookup。确认后的详情读取失败只重读GET。派发后404/5xx、截断或取消保留原请求，unknown粘性不被后续拒绝抹除；恢复仅用同完整identity、仍合法的原CSRF及原method/path/key/body/version。当前配置停用不预先阻断合法历史确认，CSRF变化不移植新token至旧intent。

投递为第九独立域，与配置域共用唯一Cookie owner，实际fetch/body/read/cancel结束才释放；放弃不撤回已发邮件。确认DOM随View共同卸载，同Session恢复保留Promise。input02修复已清理anonymous的新导航；input04修复稳定普通用户或当前403拒绝身份的新clean离页。入口捕获及等待后的身份/代次门禁保持，旧待决导航不会因随后身份失效或降权追获许可。

独立A实际完成：test202接受后截断、真实SMTP sent/terminal/io_joined；另一个正式管理员停用配置，原请求重放仍返回原job，命令/intent/事件/Audit各一、发送一次；确认后GET503不产生第二写。独立B实际完成：区分任务unknown与retry请求unknown，pageshow→Session503→同身份200/EOF恢复确认和原生focus/Tab；retry接受后截断、child sent/joined，原请求重放返回同child，原命令/Audit/事件唯一、源版本只推进一次、两代各一个attempt。详见[独立最终报告](system-smtp-delivery-ui-verification-evidence/objects/703600fccf13d2ce6d752f620eb8a40126b53a93b277e9ee9498f4c5c34dec05)和[独立原机器报告](system-smtp-delivery-ui-verification-evidence/objects/7535ed3d3bdc193d4954800f7ac0b07378d4ca0d9737ffb96ac5e7a5c2aa93e0)。

## 3. 分版本验证与真实终局

作者页面阶段完整check为38文件/1303项，含格式、类型、构建；后续只重跑受影响检查。独立API两组合各4 PASS、owner为3+4 PASS、web两App/router/factory组合PASS。30秒纯证据使用虚拟时钟及受控原生Promise屏障，不是墙钟30秒；jsdom/getClientRects不证明原生focus。独立最终六项离线检查仅race编译/发现、vet、TS、两次browser列表和输入门禁，累计12 PID实际结束、两扫空；不算业务执行。

作者新五组按input01 Read/Test/Outcome、input02 Authority、input03 Navigation；旧五组为input03的Settings Outcome/Navigation和Invitations两组，加input04的Settings Authority。API/owner及旧浏览器断言未变，input02/04仅coordinator资格和对应App测试，input03仅五行严格详情→列表动作。两个定点独立修复回归加最终input04 A/B支持复用；不称input04一轮重跑全部十组或1303全套。[作者原报告](system-smtp-delivery-ui-verification-evidence/objects/74cea2b599c9a55b76dcdd84d170dd236ce52f921e079029873a2bb4e239667b)当时写独立待验/README待交，保留原时态；后续由最终报告和17清单补齐。

下表每行有原command、raw、输入、result和两扫cleanup。ID/PID/wait为exact资源、owned PID/starttime和实际adopted wait数；三条作者非零整轮保持失败。

| 真实轮 | actual exit / 秒 | ID / PID / wait | 原顶层结果 |
| --- | --- | --- | --- |
| authority01 | 1 / 69.374 | 9 / 85 / 4 | FAIL:SystemSMTPDeliveryWebAuthorityAndIdentity |
| authority02 | 0 / 63.128 | 9 / 84 / 4 | PASS:SystemSMTPDeliveryWebAuthorityAndIdentity |
| navigation01 | 1 / 102.297 | 9 / 87 / 4 | FAIL:SystemSMTPDeliveryWebNavigationAndLayouts |
| navigation02 | 0 / 72.160 | 9 / 83 / 4 | PASS:SystemSMTPDeliveryWebNavigationAndLayouts |
| oldauthority02 | 0 / 59.566 | 7 / 80 / 4 | PASS:SystemSMTPSettingsWebAuthorityAndIdentity |
| oldinvitations01 | 0 / 78.905 | 9 / 95 / 8 | PASS:SystemInvitationsWebDeliveryRetry, PASS:SystemInvitationsWebOutcomeRecovery |
| oldsettings01 | 1 / 136.911 | 7 / 103 / 12 | PASS:SystemSMTPSettingsWebOutcomeRecovery, FAIL:SystemSMTPSettingsWebAuthorityAndIdentity, PASS:SystemSMTPSettingsWebNavigationAndLayouts |
| outcome01 | 0 / 65.431 | 9 / 81 / 4 | PASS:SystemSMTPDeliveryWebOutcomeRecovery |
| read01 | 0 / 113.527 | 9 / 124 / 4 | PASS:SystemSMTPDeliveryWebReadAndPagination |
| test01 | 0 / 72.895 | 9 / 85 / 4 | PASS:SystemSMTPDeliveryWebTestAndRetry |
| independent-real02 | 0 / 73.874 | 11 / 111 / 8 | PASS:SMTPDeliveryTestReplay, PASS:SMTPDeliveryRetryRecovery |

这11条完整监督轮逐一记录实际退出、精确资源双absent、owned进程清零、输入和本轮基线不变、monitor0、runtime/browser runtime清理。历史非本轮PPID1 Z不在已wait声明内。其它包的`[no tests to run]`不计业务通过。独立最终[real02 原 raw](system-smtp-delivery-ui-verification-evidence/objects/856850a075faf1fac70adea6c1894cb88f2dde5c0f1e304259b0bc9699233316)为两顶层A12.11s/B13.72s、浏览器各6.4s，actual0/73.874s、11 IDs/111 PID/8wait；原45秒browser、2分钟top、6分钟package、race/count1、worker1/retry0/noTrace未扩大。

## 4. 原失败、恢复与缺件

- `page-probe01`两产品红：确认child的版本下限未覆盖列表、离页先重置active区导致未detach。`page-probe02`同选中callback AST通过，但整spec有格式/profile占位差异，不称全文同字节。
- `authority01`为fresh anonymous新导航产品红；原raw有Session401/SESSION_REVOKED/EOF后URL仍SMTP，没有现场DOM/bootstrap快照。input02作者同spec红绿及独立App修复回归保留。独立authority `app01`只在Session次数前提失败，B通过复用，`app02`仅A通过，不写同轮两PASS。
- `oldsettings01`普通用户退出产品红：旧login helper等`#login-email`至45秒；原raw无现场DOM，不补造即时观察。固定源码及独立诊断证明新coordinator资格阻断。input04在同一完整最终spec上保留`rejected-probe05`3产品红/2旧待决通过→`probe06`5通过；之前probe01混有未到403的前提失败，probe03/04总GET计数误包含routerguard恢复读取，原件不删。独立rejected-repair04两组实际通过及原Authority真实复验闭合。
- `navigation01`为测试操作前提：详情模式没有列表刷新按钮；原locator超时，无DOM/图片，不能反推overlay/busy现场。input03只补显式返回列表及严格状态断言，预算不改，navigation02通过。harness原typeRoots、unused pgx、缺cancelledID编译错误及格式失败均保留；[作者原矩阵与14条非零记录](system-smtp-delivery-ui-verification-evidence/objects/795ecfd43128e2b30f274373d89afe40b718d4b0fad530dad273881593a0e8dd)列全部14条作者非零原记录，未把前提或编译失败当产品缺陷。
- 四份`page-type01`历史前态App/coordinator/Panel/View精确字节缺失。其原command/input/raw/actualexit仍在，原检查实际0/7.306s，不当失败或最终12源验证；没有补造旧源。最终受测版本及失败输入中现存精确源均保存，三个曾仅指纹的runner和三份harness前态已由原现存副本补定位。详见[index](system-smtp-delivery-ui-verification-evidence/index.json)中的精确缺件SHA。

[real01 原 raw](system-smtp-delivery-ui-verification-evidence/objects/16380a3f95339d95aa415e4815007d83fc193730289a804a0d3f7be0ff85ca34)记录两业务顶层和最后DB断言通过，但环境中断使监督actual wait、result/cleanup/input-after、owned PID/资源账及adopted wait文件缺失。原command仅启动资料，整轮**未完成**；原8文件保持，不以重启后进程消失或real02通过补造退出。

[恢复报告](system-smtp-delivery-ui-verification-evidence/objects/473663ed242632fb7a8a078f92e5d5b997a92ce939bf1bbf026a54375a8373f4)另记授权恢复：4个exited/PID0容器、3空网络、2关联匿名volume和runtime；原SMTP两对资源已经不存在但缺原exact-ID账，不能称原11 IDs完成双清。默认bridge被环境重建，旧/新ID分列；原其余5基线资源保持。7条删除命令实际exit0；初次清理结果exit1是缺失oracle未匹配小写`no such volume`，没有重复删除。后续仅只读verification02 actual0/0.921s，7IDs+2vol两遍缺失、进程空/runtime移除、恢复时非本轮6资源保持。runtime删除169,506,747B，原证据未改；恢复清理不是原轮验收。real02为新授权、同冻结输入的完整新轮。

## 5. 可复核归档与适用边界

本档924逻辑原件按SHA收敛为504对象/7771934字节，184个逻辑原件直接读取固定Git；另沿原manifest核1083依赖，36dist只保留指纹。没有复制完整依赖树、缓存、dist或二进制。对象保持原字节，原路径只作来源定位；重建源使用固定Git加已保存精确变化源/diff。四项历史缺件限制不被“可复核”措辞掩盖。

```sh
env PYTHONDONTWRITEBYTECODE=1 GIT_NO_LAZY_FETCH=1 python3 docs/development/agent-team/system-smtp-delivery-ui-verification-evidence/verify_archive.py
```

检查器只核原字节/Git来源/提交17集合、各输入与原实际退出/清理，不启动产品或执行归档脚本。八PNG沿作者navigation02逐图检查，主线程、独立者及文档作者未作当前八图图审。真实焦点/Tab与合成pageshow分别记录，合成事件不称BFCache。

取消目标的pass>0只证明due扫描事务处理，不证明队列进入或逐次ClaimBusy；正式Revoke后目标cancelled/0/null与终局零新增成立，两hold自身真实发送不计目标零attempt，ClaimBusy复用旧已验服务语义。SMTP仅自有私网none模式，本卡未重验全部TLS/崩溃矩阵、外部邮箱或生产托管，不改变Runtime/ready503、Summary待决、完整D08–D28/E01未完成及E01未开始。

## 6. 同期外域停止状态

以下仅为主线程调度交接，不属于本SMTP档案独立结论或新增对象：SPA原scope的controlled01/native01已actual PASS并双清；后续concurrent-publication探针被平台内容安全机制终止，主线程只读确认run目录不存在、无执行证据。stage02竞态静态缺口未关闭，发布脚本返修/发布暂停，不重试、改写或转交该停止任务。Object/tools原停止限制不变。

SPA作者冻结harness阶段定位为 `/workspace/scratch/agenteam-central-spa-author-gfjizhr4/stages/harness-stage01`，manifest `4101179abbf6bdfe1e8129846fd93759948f61f208ca3ee1d4350856d68f0342`，execution-plan `e2ce5f998805c9a19cec3e4bd0fcf7c19e193af924fc8ca2f429a0451f157998`。三个harness文件仅格式/type/list通过，Go compile、真实build和两top未运行；无活动命令/资源，SPA产品仍未接受。本档不读取这些外域源码，也不把此前scope/native通过外推发布竞态或完整SPA。
