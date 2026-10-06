# 系统待注册邀请 UI 验收与交付

状态：2026-10-06，主线程已采纳作者与独立最终结果，22路径提交推送 `1c82d888adfef0d8b58ab51c920557ca4e2f084f`，主线程确认远端一致。本报告接受[邀请 UI rev4 完整卡](../work-items/d27-system-invitation-ui.md)的分页管理、创建/重发/投递重试/撤销与未确认请求恢复；不代表完整 D27 或平台完成。技术§1–7 SHA256仍为 `774ffbda7e3d9dbd79e224febcd80981cd915d08a7076ea83ce170dc6aa11c65`。

## 1. 固定输入与可复核来源

最终测试输入是 `input07.json`，SHA256 `5a0de8fd67b9fd46f9249686d469258c06a8ecb7674e8ae8b02338b9c922dfa6`：21源码/测试路径与24 dist哈希。第22路径 `docs/development/frontend/README.md`在独立通过后单独检查，SHA256 `553fbe7c8db6bfb0ac99d83a1dd3207c2a9128deadb36de1d45587c014920247`；21源及README逐条与正式Git提交吻合。README原/终字节、精确diff、21链接和7 shell块检查保留；历史作者报告“README deferred”按当时事实保留。

直接前置为[邀请最近投递读口](system-invitation-delivery-read-verification.md) `9b3201547f9b7b61fd9716a6ba6540084961496c`及迁移1–19，以及[共享模态焦点修复](modal-focus-restoration-verification.md) `79f922ec259d2838052a903612e2a27005618c11`。此前[目录 UI](system-user-directory-ui-verification.md)、认证/个人/公开入口及同一Cookie owner沿原接受结果消费；无锁或迁移变化。重建源使用固定Git79f922及精确21源；独立早期pure闭包另按固定Git6be5321和各版指纹定位，不消费后继活动树。

[作者原报告](system-invitations-ui-verification-evidence/objects/ea601e4e591653fe479582333aae0526bebc856c379f473d921cce4cb5a243fe.json)；[独立最终原报告](system-invitations-ui-verification-evidence/objects/635106b9d51ec91261bfde82b3d3a00fce5c37f902602072b1b08c2c771ee91e.txt)；[README补交原记录](system-invitations-ui-verification-evidence/objects/4d8e1ff2e4732d2a54a158a90d89e8db63ede9748da9224f44775fdd76e08ef1.json)；[全部原件与精确路径索引](system-invitations-ui-verification-evidence/archive-index.json)。作者七版源码共147个逻辑副本按SHA去重；24个dist只保存各版哈希、构建日志和来源，不复制实体，也不声称本次重建了字节相同的dist。

| 输入 | 相对上一版的实际差异及适用证据 |
| --- | --- |
| input01 | 原21源；386测试check与首轮new01。首红及原输入保留。 |
| input02 | 六路径：App、View、邀请组件测试、Go顶层/fixture、浏览器spec；修SubmitEvent触发按钮、当前表单反馈与初次确认宿主。独立pure05通过，pure06发现checking重挂仍使确认成为非顶层。 |
| input03 | 五路径：App、View、邀请组件测试、Go fixture、浏览器spec；rev4同页四层共同卸载/重挂，状态与Promise留在App，防旧实例focus。消费共享79f922。453测试check及独立pure07/08；其17个Web源和24dist至input07完全相同。 |
| input04 | 仅Go fixture与浏览器spec；收紧真实请求/响应观察、失败控制与checking观察。new03仍保留原红。 |
| input05 | 仅浏览器spec；封闭GET原reader观察与精确可访问标题。new04的Lifecycle通过；Outcome原重发409假设被真实204否定。 |
| input06 | 仅Go fixture与浏览器spec；正式RetryMailJob制造真实job/version竞争，并核终态与join。new05的Outcome/Navigation及old01五组通过。 |
| input07 | 仅浏览器spec；截图前在原expect预算内等待物理 `.ui-overlay` 为0，不加sleep、不改产品或原交互断言。new06只重跑已有Navigation顶层补图。 |

原manifest中“本版尚未运行”为执行前冻结事实，后续原command/raw/result才构成动态结果。未将这些原件改写为最终状态。

## 2. 作者实际检查与分版组合

最终 `npm run check` 原日志为453/453测试、20测试文件、格式/类型/build通过。最终fixture的Go race编译与vet06、浏览器type11/format07通过；原reader观察器十项局部契约通过。具体命令、cwd、记录的环境、退出值和raw哈希沿原manifest/报告保存；未把编译当真实组，也未从观察器推导Cookie owner实际join。

以下每行是一次真实脚本运行。资源列依次为exact资源数/所属PID-starttime数/实际adopted wait数；失败行保持失败，表中通过子组仅按后述未变语义复用。

| 运行/输入 | driver exit/耗时 | 实际PASS顶层 | 实际FAIL顶层 | 资源/PID/wait |
| --- | --- | --- | --- | --- |
| new01/input01 | 1/202.265s | DeliveryRetry | Lifecycle、OutcomeRecovery、ReadAndPagination、AuthorityAndIdentity、NavigationAndLayouts | 9/185/20 |
| new02/input03 | 1/133.806s | DeliveryRetry、ReadAndPagination、AuthorityAndIdentity | Lifecycle、OutcomeRecovery、NavigationAndLayouts | 9/152/24 |
| new03/input04 | 1/85.656s | 无 | Lifecycle、OutcomeRecovery、NavigationAndLayouts | 7/111/12 |
| new04/input05 | 1/88.408s | Lifecycle | OutcomeRecovery、NavigationAndLayouts | 7/112/12 |
| new05/input06 | 0/72.000s | OutcomeRecovery、NavigationAndLayouts | 无 | 7/96/8 |
| old01/input06 | 0/99.577s | PublicEntryWebIdentityNavigation、AuthenticationWebSessionLifecycle、PersonalSettingsWebThemeAndNavigation、SystemUserDirectoryWebAuthorityAndIdentity、SystemUserDirectoryWebNavigationAndLayouts | 无 | 7/142/20 |
| new06/input07 | 0/62.085s | NavigationAndLayouts | 无 | 7/87/4 |

最终六新组的证据分别为Lifecycle=new04/input05，DeliveryRetry、ReadAndPagination、AuthorityAndIdentity=new02/input03，OutcomeRecovery=new05/input06，NavigationAndLayouts=new06/input07。旧五组全部在old01/input06通过，input07只改邀请截图等待；没有“最终input07一轮重跑全部十一组”的声明。每版差量、17Web/24dist不变、真实断言及原输入支持复用。

## 3. 原红、归因与修复

- **真实产品缺陷**：new01字段拒绝后恢复焦点未捕获实际submitter；旧成功反馈泄入新空表单（独立pure04）；初次确认宿主的Teleport视觉顺序与逻辑栈不一致；input02合成pageshow/checking重挂下层表单后将待决确认变为非顶层/inert（独立pure06）。input02修初始问题，input03按rev4共同宿主并保留App期草稿/Promise、隔离旧实例；关闭后落在剩余modal内依赖已单独接受的共享79f922，未放宽焦点断言。
- **观察或fixture失败**：原连接失败、before-header丢失且自动重试计数不足、new02 POST/new03 GET的CDP body不可用、checking渲染前快照、UiState标题的隐藏感叹号造成精确DOM文本不符，不能直接称为产品失败。最终使用成功headers后受限原body截断、原生reader观察、真实checking DOM观察和原精确可访问标题；不clone/tee/额外fetch，不改变原read Promise或cancel身份。
- **竞争前提错误**：new04期望重发后409，实际正式Resend保留invitation version而正确返回204。input06通过正式RetryMailJob202作用于sent/backend_log且terminal/io_joined的真实目标，核旧job version+1、新job同invitation及实际join，再由原UI旧job/version恰一次POST获得409 VERSION_CONFLICT。显式重新读取/预览不再发邮件任务POST；没有SQL改version，也不把邮件任务计数解释为全局无POST。
- **视觉原件缺口**：new05 light390是离场帧，该轮测试PASS不使这张图成为页面证据。保留原图，input07/new06以物理overlay0补图；其他预算与断言未放宽。
- 作者本地pure04–06、rev4 pure07/08与type06等原红日志，以及更早保留的本地检查日志均按原字节归档；没有为缺少完整历史命令/环境/逐轮源快照的孤立日志事后拼装原件或增加通过声明。

## 4. 独立验收、原首红与实际终局

独立owner-tail在pure01的单独子组已PASS；该轮整体因App探针失败而exit1，不能称整轮通过。pure01–06的原probe/command/raw/input保留，区分探针时序/标签前提与pure04、pure06真实产品红。最终input03的pure07三组、pure08四组实际PASS，覆盖未确认写/读503与原请求恢复、新表单无陈旧成功反馈、checking时四层全卸载后确认仍top、同Session恢复及待决route/logout的continue/discard与Promise终局。17个Web源未变，至input07复用；早期30秒可见超时与fetch/body/cancel尾部owner纪律沿未变子组复用，不用读DTO或额外身份oracle代替实际尾部。

独立真实唯一组 `TestAccountInvitationsIndependentComposite` 在independent02实际PASS：driver exit0/56.812s，Go顶层9.48s，Chromium一例4.5s；raw SHA256 `959143ec84aebf24cfd937af20a869c545f5615469d059cb4fba698d8662817d`。真实201 headers后原body截断保留未确认请求；待决确认经历一次真实Session503、App恢复与同Cookie身份，继续后真实焦点及Tab/Shift+Tab留在底层表单。Session200和列表200不充当写receipt；显式重试保持同method/path/raw body/key/CSRF，严格201 receipt后GET503只显示列表读取失败，显式GET恢复。浏览器恰两次System POST，最后Go SQL实际核commands1/intents1/invitation1及cut/replay各一次。

**independent01保留工具首红**：driver exit1/58.654s，私有testDir误发现归档source副本，共执行两例；第一例安全事实PASS，第二例同邮箱触发真实429，Go末尾DB断言尚未执行。只给私有config加 `testIgnore: ['**/runs/**']`，`--list`精确一例后同预算重跑independent02；产品、行为断言和其他配置字节未改。类型编译1.131s、Go race编译5.257s、list0.961s只算静态/编译记录。

私有overlay仅增加独立Go顶层并绑定冻结两Go fixture；runner只在本outcome实例开放既有一次Session503控制。生产dist用固定快照软链，未复制全树；受限日志投递与真实SMTP组合分别由独立本轮和作者DeliveryRetry支持。45s浏览器/2m Go顶层/6m包、race/count1、workers1/retries0未放宽。

作者七轮和独立两真实轮各自actual wait、exact-ID双absent、原2容器/4网络基线不变、所属PID/runtime为空、短浏览器目录删除、输入/验证源不变。独立最终7资源、86 PID/starttime、4次actual adopted wait、monitor0；窗口已释放。pure07/08 driver/child实际wait且两次所属扫描为空，私有编译缓存按原记录保留，不宣称已删。共享旧任务两个PPID1 Z已退出但未由当前父wait的限制沿[旧报告](dialog-outside-focus-repair-verification.md)保持，不属本轮清理成果。

## 5. 截图、离线归档与限制

最终new06八张light/dark ×390/834/1024/1440图保存原字节。作者实际查看new05全部图并识别light390缺口；主线程实际查看new05 light1440、dark390及new06替换light390，后者页面内容可读、长邮箱换行且无可见横向溢出。没有宣称八张new06均由所有角色逐张审阅。保留原离场图与三张已审定位：[new05 light390缺口](system-invitations-ui-verification-evidence/objects/8bbee98d91923153ed5fdac3ac322a636f11c435410b2d180f33a0b3d730088c.png)；[new05 light1440](system-invitations-ui-verification-evidence/objects/74edc04ac385f4b4734e8de78b1a2be3fac276cc28144cdc3b83ef5bfc98d0a9.png)；[new05 dark390](system-invitations-ui-verification-evidence/objects/0ce84976c647742b56096fe27548ed0160b3953bf9f10c0ca98b9cdfb73dee88.png)；[new06 light390](system-invitations-ui-verification-evidence/objects/8be2881688e38ca24a0a147d24982c6d64157434137dad9f7af2d56a4c7b6eed.png)。窄屏下方操作依正常纵向滚动。

[证据说明与离线入口](system-invitations-ui-verification-evidence/README.md)记录503项逻辑原件、261份去重实体及22项固定Git来源，共2,756,912字节实体。索引保存来源/长度/SHA与复用映射，原md和脚本作为文本保留，绝不执行原件。文档负责人仅执行：

```sh
env PYTHONDONTWRITEBYTECODE=1 GIT_NO_LAZY_FETCH=1 python3 docs/development/agent-team/system-invitations-ui-verification-evidence/verify_archive.py
```

该检查核原字节、七版21源/24dist元数据、固定22交付路径/Git闭包/锁/迁移、七作者真实/八独立pure/两独立真实及资源终局原记录，PASS不表示重跑产品或重新观测现存资源。未复制dist、依赖、缓存、二进制和完整候选树。

Chromium沿主线程授权的仓库harness执行，当前Playwright技能不可用。合成pageshow调用生产listener不等同真实BFCache；生产SPA托管、真实Vite代理、其他浏览器和native zoom未验。此卡没有Provider管理、完整D26/D27或平台接受结论；Summary待决，Object/tools原停止任务不重试/改派/重建，Artifact/Project及未绑定生产端口、ready503保持。完整D08–D28/E01未完成，E01未开始。
