# D27 系统设置壳与用户目录 UI 验证记录

状态：**PASS，主线程已采纳并交付**。前端作者实现[完整卡 rev1](../work-items/d27-system-user-directory-ui.md)，`recovery_verification` 独立验收；主线程提交推送 `7ef3e30cf06b5df6d19516f24c34855a8308ef05`，并核 HEAD 与远端一致。接受范围为管理员系统设置壳、只读用户目录及其同一 Cookie owner、权限/身份、分页、导航和布局组合，不等同完整 D26/D27 或生产托管接受。

## 1. 固定输入与可重建交付

最终作者 input04 SHA-256 为 `c8f823330cd8ab9a226f96d5b2bdc6cbe9ec3d5d4a81080080ca8bdcc4b35966`，含 **19 个实际测试源码与22个 dist 哈希**。独立组合 `frozen-input04.json` SHA-256 为 `3e9eb47554d3ca87471a5e8c21adf6e0f28b12e37b61f7a8a88907e65ca12333`，固定82个源/依赖及同22个 dist：后端六源为已接受 `3affc0194214101cfa1e6fdc583afa5d60005db8`，共享焦点四源为已接受 `b53895f7eb1d020276e8f54a99a7c0821b286481`，迁移仍为00001–00018。邀请投递读口及00019没有混入此验收。

第20路径 `docs/development/frontend/README.md` 在动态通过后另行补充，作者只做文档检查；不是浏览器输入的一部分。[delivery20 原清单](system-user-directory-ui-verification-evidence/author/readme-final01/delivery20.json) SHA-256 `503c60ed42fc4f521bf83c16379d429c155a8e77a4a8d0e28d9532e388053ce6` 的20路径全部与交付 Git 字节匹配。原独立报告保留“产品提交待主线程”的冻结时态，本报告补记实际交付，不改写原件。

| 作者输入 | 原清单 SHA-256 | 相对前版变化与用途 |
| --- | --- | --- |
| input01 | `490e7af16f406b61512256ee4f80ba01628041213f32569d32a9065455ca4184` | 首次19源/22 dist；new01三组红，独立pure两测试在此输入通过。 |
| input02 | `0a2a8e7628a4afade78700ee31c14aa84469d36fc3979bf8ecbecef6794d340b` | 仅两个测试源变化：保留真实尾空格比较、补权限响应窄诊断；所有生产/Go/dist原字节不变。new02保留两红。 |
| input03 | `e45a32cf24db3920b3468395a983b8a4768528a86f04c01025bbf56fdce7f917` | 三源变化：局部表格换行/列宽、长字段与cell几何断言、权限响应观察修正；重建dist，共享焦点依赖当时仍待接受。 |
| input04 | `c8f823330cd8ab9a226f96d5b2bdc6cbe9ec3d5d4a81080080ca8bdcc4b35966` | 19源与input03完全相同；消费已接受焦点四源后完整check244测试并重建dist，用于最终新3/旧3/独立1。 |

input01–03 可由固定 `3affc01` Git 基线加各自19源重建，input04 由固定 `b53895f` 加19源重建。各版精确源码按 SHA 去重，原路径、SHA与物理文件映射见[证据索引](system-user-directory-ui-verification-evidence/archive-index.json)。不复制完整web、依赖缓存、dist实体或测试二进制。

## 2. 作者检查与独立静审/纯测试

最终 `npm run check --prefix web` 实际 exit0：16文件 **244/244测试**、格式、vue-tsc、production Vite build全部通过，见[input04检查记录](system-user-directory-ui-verification-evidence/author/input04/author-checks.json)与[原始log](system-user-directory-ui-verification-evidence/author/logs/revision04-check-01.log)。Go integration race compile/vet复用input01未变Go源码；浏览器独立tsc与native-reader纯探针复用input03未变harness，未虚称全部命令在input04重跑。仅编译及无匹配测试包不计动态通过。

作者原失败一并保留：锁定依赖镜像403导致首次npm准备失败；首次单测79/80通过，Teleport确认框查询位置错误，修测试后81/81通过；首次Go编译选到缺当前锁定版本的离线缓存，依赖加载即失败、没有TestMain；input03首次harness类型检查TS2683缺显式this类型，补注解后通过。这些与真实页面缺陷分列，不删除原日志。

独立静审核对固定same-origin GET/limit25/不透明cursor；九字段目录DTO向旧八字段User解析器投影、有效Gregorian日期与完整微秒；冻结页、严格顺序/重复ID拒绝；完整UserID/SessionID/epoch及页代次门控；401失效、403清理并待成功Session确认；dispose不取消restore/个人/公开操作；个人草稿离页确认与壳默认行为保持。历史review01/02保留当时未完成结论，最终以[原独立报告](system-user-directory-ui-verification-evidence/verification/verification-report.md.txt)为准。

独立 `runs/pure01` 实际 **2测试PASS、exit0/1.088s**，使用真实client/controller加受控transport：fetch忽略abort、可见30秒到期及body cancel未结束时，logout均不能越过唯一Cookie owner；实际尾部完成才继续。还核严格日历、六位微秒排序、重复ID及旧User拒绝新增created_at。这两测试在input01执行，其涉及的client/account/system-account/useSession/controller至input04字节未变，按此边界复用，不称最终版本重新跑过pure。

## 3. 真实浏览器原命令、失败与最终通过

每轮 `command.json` 保留实际argv/env/cwd/退出及wait，`driver.py.txt`为执行时驱动，独立轮另保留精确Go overlay/TS/config。实际命令如下（此文档归档未重跑）：

```sh
sh scripts/test-objects.sh -run '^(TestAccountSystemUserDirectoryWebReadAndPagination|TestAccountSystemUserDirectoryWebAuthorityAndIdentity|TestAccountSystemUserDirectoryWebNavigationAndLayouts)$'
sh scripts/test-objects.sh -run '^(TestAccountAuthenticationWebSessionLifecycle|TestAccountPersonalSettingsWebThemeAndNavigation|TestAccountPublicEntryWebIdentityNavigation)$'
sh scripts/test-objects.sh -run '^(TestAccountSystemUserDirectoryWebIndependent)$'
```

| 轮次 | 输入 | 实际exit / 总秒数 | 指定顶层原结果（秒） |
| --- | --- | --- | --- |
| new01 | input01 | 1 / 230.626 | Read FAIL8.41；Authority FAIL7.58；Navigation FAIL6.15。 |
| new02 | input02 | 1 / 72.622 | Read PASS6.74；Authority FAIL6.76；Navigation FAIL12.49。 |
| new03 | input04 | 0 / 71.438 | Read PASS7.27；Authority PASS9.80；Navigation PASS9.16。 |
| old01 | input04 | 0 / 84.826 | PublicEntry PASS14.58；Authentication PASS4.74；PersonalSettings PASS14.49。 |
| independent01 | input04 | 1 / 60.810 | 同一独立顶层FAIL8.74，普通登录实际200后的CDP body缓存观察失败。 |
| independent02 | input04 | 1 / 53.812 | 同一独立顶层FAIL6.77，首次管理员登录实际200后的同类CDP观察失败。 |
| independent03 | input04 | 0 / 64.675 | 同一独立顶层PASS8.83（browser3.8）。 |

new01 的observer对textContent做trim，破坏fixture真实尾空格；当时Authority聚合断言未记录克隆读取细节。input02仅修比较和窄诊断，真实组件单测证明产品保留原文。new02确认Authority实际401，但clone读取因abort失败；原严格SESSION_REVOKED/FORBIDDEN断言未放宽。观察器改为真实响应优先与原native reader有界旁路，返回原Promise，不clone/tee/额外读取，不延迟abort；观察body done/DTO仍不能替代owner/cancel实际尾部证据。

new02另有两项真实UI问题：遮罩关闭后触发按钮失焦，以及截图显示继承nowrap造成桌面仅邮箱列可见。局部表格修复及全部cell几何断言进入input03；共享UiDialog焦点另卡独立接受 `b53895f` 后才进入input04组合，原焦点断言始终保持。原new02日志及两张已审截图保留；原生DOM机制、基础组件旧红/新绿及准备失败沿[已接受焦点归档](dialog-outside-focus-repair-verification.md)引用，不用机制实验代替页面验收。

独立两轮CDP首红属于测试观察方法失败，不写成产品失败。最终两次登录统一为实际200、稳定目标页之后发起同浏览器原生Session GET，精确核身份/角色、原八字段User与Session UUID；Playwright Response仅读status，不再读取CDP body缓存。原probe快照、revision02/03差量均保留，未改变产品、断言预算或角色/撤销期望。最终TS SHA-256 `980ab1e41ad2241c352d5d193274c9a5dcb0f1ec9d6d51bbcf6806a9c21bd0a8`；该额外Session oracle证明当下身份，不冒充登录body形状或owner尾部证明。

最终独立真实链经正式bootstrap/邀请形成3账户，逐行核PG canonical时间与五列；同Session恢复；仅对自有库精确admin行降权/恢复，经历真实403、Session重验再恢复；精确撤销捕获的自有Session，经历真实401并清空旧行。新普通身份在同浏览器精确确认，直达目录拒绝且users GET计数为零。三次DB定向操作各影响1行，Go终局复查角色恢复与同Session撤销。新3组另覆盖cursor分页、上一页重读、刷新、身份清理、个人草稿/注销以及遮罩/Escape焦点；旧3组验证既有认证、个人与公开入口组合。

## 4. 截图、环境与资源终局

验收者与主线程实际查看new03的[light1440](system-user-directory-ui-verification-evidence/verification/runs/new03/images/system-directory-light-1440.png)和[dark390](system-user-directory-ui-verification-evidence/verification/runs/new03/images/system-directory-dark-390.png)，文档负责人亦实际查看这两张：桌面五列同屏且长文本换行，390px为五项明确标签卡片。验收者还实际查看independent03同两尺寸截图。new03自动几何覆盖light/dark×1440/1024/834/390，逐项核全部25行五个cell及窄屏标签顺序；归档八张new03、两张独立最终、两张new02原布局，共12张。截图证明布局，权限和焦点依赖实际交互断言。

环境固定Go1.27.1/local、锁定只读离线模块，Node24.19、Playwright1.56.1、Chromium151.0.7922.173；MinIO实际SHA-256 `dc5298474f0bc87a068f0b1135c583bb1278c17c11c512212ed7644a238c89c8`，PG17.8/vector0.8.1与固定PG16 fixture的实际版本/镜像digest在raw中。其他包无匹配测试，不据启动fixture宣称PG16拒绝测试再次执行。全程browser45秒、Go顶层2分钟、每包6分钟、workers1/retries0、race/count1；没有延长预算换取通过。

| 轮次 | 观察所属PID/starttime数 | actual adopted waits | 资源与输入终局 |
| --- | --- | --- | --- |
| new01 | 393 | 12 | 各自7个exact-ID双次absent；实际wait；所属进程/runtime为空；原2容器/4网络不变。 |
| new02 | 115 | 12 | 同上，该轮独立证据保留。 |
| new03 | 112 | 12 | 同上；源/dist/验证输入不变。 |
| old01 | 114 | 12 | 同上；源/dist/验证输入不变。 |
| independent01 | 86 | 4 | 同上，CDP原红并未遗留自有活动资源。 |
| independent02 | 84 | 4 | 同上，该轮独立证据保留。 |
| independent03 | 86 | 4 | 同上；driver exit0、monitor errors0，短浏览器目录删除，窗口已交还。 |

每轮原baseline、观察资源/PID、双清理与实际wait均独立保留，离线脚本核七轮原baseline逐项相同、七个exact-ID均双次absent，不以后轮终局推定旧轮清理。共享焦点作者old01的两个PPID1、Z历史记录已退出但当前父进程未wait，此限制沿旧归档保留；本UI轮没有声称回收它们，也不把原基础轮改称all-clean。

## 5. 持久证据、离线复核与边界

[证据说明](system-user-directory-ui-verification-evidence/README.md)与[索引](system-user-directory-ui-verification-evidence/archive-index.json)记录264个逻辑原件，SHA去重为147个新物理原件及16个复用旧档原件，新增原件1,993,538字节。历史源码/报告/log/资源JSON不改字节，绝对scratch路径只作为来源标识；scratch删除后可由仓库档案与固定Git离线核对。22个dist仅保存各版哈希与构建原log；归档核验不重新构建或检查当前dist，不宣称依赖缓存可永续使用。

文档负责人实际运行以下只读脚本，exit0：264原件、四作者输入、20交付路径、82组合输入、143个本地Git blob、七轮原日志/退出/资源终局关联全部PASS。只读原字节及固定Git，不访问网络、不执行所记录的历史命令、Go/npm/浏览器/Docker或产品测试。

```sh
env PYTHONDONTWRITEBYTECODE=1 GIT_NO_LAZY_FETCH=1 python3 docs/development/agent-team/system-user-directory-ui-verification-evidence/verify_archive.py
```

使用任务自有same-origin dist服务器，不覆盖生产SPA托管或真实Vite代理浏览器；未验原生浏览器缩放、其他浏览器、邀请投递或00019。Playwright技能/浏览器connector不可用，实际使用锁定仓库harness，不虚称调用技能。原Object修复及tools独立停止任务未恢复、重试、改派或重建；Summary初值待决、Artifact/Project阻塞、生产未绑定与ready503保持。完整D08–D28/E01未完成，E01未开始。
