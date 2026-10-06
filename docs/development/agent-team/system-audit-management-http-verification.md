# System Audit 管理 HTTP 验证记录

## 1. 接受版本与范围

主线程接受14路径并提交推送 `b124650aee095b26191bc181dc8cf5d6e0f977f5`，核远端一致；第14[后端说明](../backend/audit.md)单独完成。MIME后继两文件修复已另接受并提交 `fa2d775fe1bbc1082f11f09c94eb5114a6ced6f5`；其原红、独立验证与旧运行的复用边界见§5。 [工作卡](../work-items/d04-system-audit-management-http.md)技术§1–7 SHA `687c85d4a41880828d038251bf6321accc48092aa043a10addb1606ab02c6f21` 保持，既有[规格静审档案](system-audit-management-http-spec-verification.md)不改写。

本结果提供两个GET-only System Audit读口：同一Tx内持User Shared并按当前Session/admin授权，完整检查每行、limit+1哨兵及Close/Err，在事务真实终局且context仍有效后才发布。Unknown／坏后项等不返回候选；没有新增写入、lookup、retry授权或SQL改role。旧generic/Project库接口保留。HTTP使用同root的原auditor和Account边界，3s预算涵盖预认证与实际读写／取消尾部，成功JSON上限1MiB。明确37动作、13Service、17资源、5关联及合法Session类型；975420B是保守组合算术，不是每种合法实物都已动态遍历。

动态固定input03 SHA `c6a4d5781b7028e4b4ea84b6210e2a40c390938ec3a0d533701df1f96ebc551b`；产品代码身份213cf5c、依赖Git f843506，原execution字段`product_base`本身记录f843506，两个标签不混改。13候选＋467审查依赖与实际13＋893执行依赖分开。旧14清单 SHA `d70c098f931f4fe210822c6ffa551ee46c15dba3e22e19d6e3194a3211fe8d43`、说明SHA `6c0447804f5bd92c6963d56984848130cddebeff5142e27f7f338c8f426274ca`；其前态来自固定Git，末件没有被冒称当时真实受测源。

## 2. 原阶段、失败与复用

| 阶段 | 实际结果 | 原失败与证明边界 |
| --- | --- | --- |
| 作者query | 最终pure03六top／20子例、包race与vet通过；两生产源全程相同 | pure01直接比较含closure的LockKey编译失败；pure02把零UUID.String当空串，原早失败的goroutine join未被记录，process wait不能补填；后续显式joined独立列示 |
| 独立Query Group1 | query01首top七子例PASS、第二top四PASS一FAIL；query02只valid-page一例PASS，分轮组合 | 私有DeepEqual对Scope/Resource/Metadata非nil func为false；原raw无字段差异，不称产品语义不同。改用完整公开语义/MetadataJSON/cursorVerify，不削屏障与内容断言 |
| 作者HTTP／独立Group2 | 原生产handler/wire/schema在六轮不变；作者选中修复、补充组及最终八Pure选择器race通过；独立http01两top六子组actual0／23.190265s | 作者非法reason和测试$ref基址前提红保留；race非verbose只给包PASS，八top数来自精确发现。独立枚举脚本漏单行const首exit1，schema02只修提取器；schema-check01.py是事后原文记录，非预先绑定快照 |
| root／harness静审 | input01成功DTO误传旧Problem assertTrace被STATIC挡下；input02只修新测试内RequestID及恰一条route日志 | architecture独审当时只有消息结论，没有独立报告文件/SHA，不能拿作者报告替代；其后编译／vet／list按原命令通过，不是业务执行 |
| 原生预算 | native01两topPASS复用＋native02受影响SlowBody top两子例PASS | 原native01整个命令仍exit1；实际Canceled与3.002585778s／120.974343ms已记录，但取消路径先后未采样。修后保留deadline夹界、Done、终态到deadline、Err闭集及原零Service／Close／abort／join要求 |

独立[Query原报告](system-audit-management-http-verification-evidence/objects/84e5adf7ed621f3cbf90bc829ec7f11d2760a438a21cac6e9689d4fdef1788e7)的自然3s／较早parent75ms是真正Service授权屏障，尚不等于HTTP或DB期限；独立[HTTP原报告](system-audit-management-http-verification-evidence/objects/53cd3d02b63f4c49d8809d18cb62f421b47649507db671c1869ffe163afc788e)在CheckRequest自然等待3002.179ms、parent仍有效，及90ms受控callback/body尾部门闩另列。作者真实DB1s锁超时和较早250ms取消另有typed Fault／SQLSTATE／实际终局，不能把DB1s当3s证明。早期作者checks仅input与actualwait，没有虚构input-after或所属进程双扫；query-vet原249输入中187额外项为固定Git Account import闭包，不称全部实际被Audit-only vet编译，也不把阶段62说成vet完整目录。

## 3. 真实轮及终局

| 轮次 | 实际顶层 | 原退出／秒 | exact ID／owned PID-starttime／adopted wait |
| --- | --- | --- | --- |
| 作者native01 | EOFAndKeepAlive PASS、SlowBody FAIL、WriteAndFlushAbort PASS | 1／4.405 | 0／3／0 |
| 作者native02 | 仅SlowBody，两子例PASS | 0／5.186 | 0／3／0 |
| 作者new-account01 | QueryAndProjection 3.61s、TransactionAndBudget 6.59s PASS | 0／103.558 | 7／111／0 |
| 作者new-root01 | RootProducerBinding 2.23s PASS | 0／55.982 | 7／73／0 |
| 作者old01 | 原AuditPaginationBindingsFiltersAndRevocation 1.01s PASS | 0／55.256 | 7／70／0 |
| 独立independent01 | CurrentAuthorityAndTerminal 3.10s、RootProducerProjection 2.32s首轮PASS | 0／105.717 | 7／122／0 |

每轮原argv/env、输入前后、driver、raw、actualwait及两扫原件保留；四轮PG原2容器／4网络基线不变，7exact IDs双absent、owned PID/starttime及runtime空、monitor0，均无browser/Node。native分支没有fixture，空Docker baseline不代替全局库存核验。历史PPID1 Z不归本轮所有权，不称已reap或全局清零。

[最终独立原报告](system-audit-management-http-verification-evidence/objects/0075fadaef56a16c5ff23e873909bbc533fffa61e70ce8cee2ce2d6eabbd77f0)接受固定13源，复用未变Query3及handler/wire/schema；handler_test的native差量并未被误称同字节。A通过正式Logout、UserSH/EX与真实PG提交/取消门禁，使用受控ResponseWriter观察提交前零status/body；这不是TCP提交前零字节。B通过正式Profile×2、Outbound×1生产事件，同根同一原生连接顺序list/detail、cursor/filter、typed golden、唯一RequestID对应日志、只读计数及实际root join，不能替A证明事务门禁。

真实Unknown未注入，沿独立受控IQ/IH；B client.Timeout不是服务端parent证据。作者runtime为3501指纹，独立为3504：Python3.12→3.13替换，加两probe与overlay，准确一删四增，共有3500项不变；每轮自身前后相同。两次独立format只绑定runner+gofmt，不据此虚构probe前态；后续编译与真实两probe版本完整绑定。原报告当时pending/running文字原样保存，由后续终局补充，不回写。

## 4. 主线离线兼容与末件

[主线兼容原报告](system-audit-management-http-verification-evidence/objects/a815506b698470b63b18bcb9269c15dacb9c5fe3ce23d6fc26b933e22b12baf9)受测的是HEAD2742cf7＋已复制14路径，后来才提交b124650。graph0.120s、account race-compile31.624s／list1.052s、app race-compile3.238s／list1.021s均actual0；外层也actual0。两包精确发现2＋1个目标，44个PID/starttime双扫空、0adopt，二进制SHA保存后实际删除，无listener、Docker、浏览器或业务测试。614本地＋3496runtime／394package／31module是选中Go图的保守闭包，非全仓；非候选匹配2742cf7，两项Outbound harness另匹配1ff0442。它只证明当前组合编译／发现，不将旧真实结果倒填为最新main动态重跑，也没有新增vet声明。

后端说明的三链接／fragment、格式自查与唯一diff保存；首个no-index wrapper把预期差异exit1误当要求exit0的准备错误保留，零空白诊断不是产品失败。验收后root只删除Q的可再生GOCACHE 938986372B，来源／证据／modcache及活动缓存保留；这是后续维护，不是原轮cleanup或input-after的一部分。

## 5. 后继MIME schema缺陷及修复边界

旧b124650的九个`media_type`分支要求slash并漏brace字符，正式Go构造→Decode→NewEntry纯oracle却接受单token和含brace的规范值；这是后来发现的schema过窄，不抹旧14路径验收，也不把新修复回填旧运行。没有启动Object服务或生产者，原停止的Object任务不被重试。

[修复作者原报告](system-audit-management-http-verification-evidence/objects/51504e2db46fb1ab37024f060c5fcce69174410f01ea8d576e365aa2869b3bdc)仅改OpenAPI与handler_test；生产Go constructor、handler、query、producer不变。42向量×九分支经实际typed构造／投影和完整字段schema：旧Go actual1／29.041s，99 schema失败／279分支通过；旧Node actual1／0.053s，198差异／756检查。新三受影响Pure top0／10.049s、race0／19.849s、vet0／4.680s；同Node probe0／0.053s，756/756。原runner只GOCACHE行变化且旧版本／diff保留。

Node v24.19.0无m时原裸`$`本已拒绝终止符，不称观测到anchor泄漏；新`not`是显式CR/LF/U+2028/U+2029约束。257B值原完整字段已有maxLength拒绝，不把pattern单测当完整schema缺陷。Node探针只实现所用string关键字，不声称完整OpenAPI验证器。

[独立修复原报告](system-audit-management-http-verification-evidence/objects/b4f46218ea3ebd5f740e1cd84828b63350f624b6420661d587aef3842a411c1e)对固定两源及252依赖静核：只九字段pattern/not变化，去掉唯一新增测试后旧测试原字节相同。独立Go首轮actual0／5.169429s，298正式构造判定向量，42边界向量×九分支共378完整记录／指定分支校验；Node首轮actual0／0.070543s，default/u无m两模式5364次实际ECMAScript对照。各原45s预算、argv/overrides、输入前后、actualwait及两扫保存，分别5／1个owned PID/starttime终局空、0adopt，TMP移除，无HTTP/PG/Docker/browser/Object服务。主线程据此另接受两路径并提交推送`fa2d775fe1bbc1082f11f09c94eb5114a6ced6f5`、核远端一致；不把这次受影响离线验收扩为旧HTTP矩阵重跑。归档同时核原b124650十四源与fa2d775两修复源的Git字节。

## 6. 可复核归档与范围外

[index.json](system-audit-management-http-verification-evidence/index.json)当前收723逻辑原件、487内容对象／30333761B、93个固定Git引用；14逻辑源路径25必要版本可定位。固定Git复原依赖，不复制全源码树、module/cache、二进制、runtime内容；原raw/失败/diff保留字节。独立原编译产物与已删除缓存只保留记录，不补造。命令env只是原选定overrides，不虚构完整继承环境。归档checker只读原字节／Git／原退出和清理，不执行任何原脚本或业务：

```sh
env PYTHONDONTWRITEBYTECODE=1 GIT_NO_LAZY_FETCH=1 python3 docs/development/agent-team/system-audit-management-http-verification-evidence/verify_archive.py --repo .
```

Audit UI规格后来获独立STATIC接受，作者已私有API实施、独立计划已准备，但无实现验收；本档不消费活动UI候选，后继规格档案另立。Project Audit HTTP/UI、生产SPA、外部依赖绑定和全平台未在本结果中。Object/tools与SPA publication原停止不重试／改写／转派，SPA既有scope/native通过不等于接受；Summary待决、ready503保持。完整D08–D28/E01未完成，E01未开始。
