# System 运行信息 HTTP 十四路径验收记录

十四路径已由主线程采纳、提交推送 `9b074f809df4603923faa68d963cb30a5fe4d7eb`，并核远端一致。本记录归位已接受的只读 HTTP 产品结果；[工作卡](../work-items/d28-system-runtime-information-http.md)技术§1–7 `aeb4222baf0e2d4547abf13277dc7789f4157e8c3d98843c73377e00d31d2bab`保持。旧[规格档案](system-runtime-information-http-spec-verification.md)保留当时service返修阶段，不回填后来的实现结果。

## 1. 接受范围与固定输入

唯一 `GET /api/v1/system/runtime-information` 在同 root 的 Account 当前授权和真实读事务终局之后发布安全快照。它只读取原缓存及现有内存 Status，不新增健康探测；Central版本明确未知，数据库版本属于历史成功样本，Object Storage是聚合观测。GET200不改变`ready=false`及`/readyz`503。没有写口、lookup、刷新、页面、部署版本提供者或新的Object生命周期。

[14路径原交付清单](system-runtime-information-http-verification-evidence/objects/4c9c56e7d76f6c9278c42f883ade2830e9c52443ffe3be62e44f36caa51593bb)绑定13技术源与最后 `docs/development/backend/README.md`。真实验收使用后端 `b124650aee095b26191bc181dc8cf5d6e0f977f5` 加冻结候选。A02/B/C/D的12/246/343/446依赖是各阶段有限输入；driver另绑定904固定Git依赖，不能混称同一闭包。作者3501运行文件完整沿用，独立增加两probe和overlay成为3504；其GOFLAGS及私有临时目录不同，不称整个runtime对象逐字相等。每个实际轮自己的输入前后相同。

[独立最终报告](system-runtime-information-http-verification-evidence/objects/669e35b0178555a5b3acb60d39d3139711bd4697e6cd045882f100eba8d07793)全文SHA `669e35b0178555a5b3acb60d39d3139711bd4697e6cd045882f100eba8d07793`。受测Source、probe、driver、原argv/cwd/选定env、raw和终局均按原件保存；正式产品14个Git blob与末件清单逐一核同。归档自身只运行离线字节/Git/原记录检查。

## 2. 分阶段接受与复用

| 阶段 | 实际结果和复用 | 证据边界 |
| --- | --- | --- |
| A service | 作者stage01 pure/race各7top58sub是历史结果；三行guard及SnapshotValidation窄修后，受影响1top35sub（18组合＋17字段）通过。独立原crossfield01两RED，完全同probe的02两GREEN；terminal01两个top五子例通过 | 原错误笛卡尔期望不算有效复用。独立自然3s与75ms较早parent，以及Source尾部、Tx尾部的实际release/join分开；受控坏Source不表示正式root已产生矛盾值 |
| B HTTP | 作者pure/race8top38sub、vet/format通过。独立Go两topactual0／19.467892s；Python标准Draft202012Validator对实际Go向量76断言actual0／0.116966s | 正式Boundary顺序与受控auth/Reader并存。真实自然3s／较早parent、Body.Close/callback/Flush尾部可复核；短Write/Flush失败已发部分或完整数据，不冒零wire。合法3690B与synthetic16384/16385分别记录 |
| C app | 作者原pure01九topPASS＋POST缺Origin前提红；仅测试补合法Origin后受影响路由23sub通过，最终race10top75sub全部通过。独立两topactual0／19.870720329s、37PID双清 | 原Source/initialize/run、一次T/20s/六项聚合及后置失败Close实际尾部。该阶段受控listener无socket，实例装配图不能单独代替后来真实鉴权root |
| D/最终真实 | D静审及离线编译/vet/list之后，作者native02、新Account、新root三轮通过；最终独立两个真实代表同轮PASS | 作者矩阵沿未变输入复用，不宣称最终独立重跑全部。原DiagnosticRoutes早期仅list，native02才实际执行 |

分层原报告：[service独立报告](system-runtime-information-http-verification-evidence/objects/6532d461369eb7f40d1dfabb697c05edeb1ebb4834b05a6c5fa5e38f86eb1395)、[HTTP独立报告](system-runtime-information-http-verification-evidence/objects/00df09efcd72374e5a628bfa1f65e8cd02dc6057e74f76b6f4232e8e3d465e23)、[app独立报告](system-runtime-information-http-verification-evidence/objects/2606689b04b2a390ea9c9362ae3ef6dc9c6913f5011e01f2cac5da0735e6a34c)、[作者三轮原汇总](system-runtime-information-http-verification-evidence/objects/010db96ab23fd0152f3a6d965f6cacd8dc763dc3523a054022510976c981d22b)。A/B/控制C原输入各自绑定，3513/3533/3536、3753/101和4208等指纹清单不能跨阶段冒充同一执行环境。service自然3s、HTTP预算、数据库1s和更早parent各以原层次实证，不相互替代。

## 3. 原失败、修复与缺件

- `P-RUNTIME-01` 为实际产品缺陷：DB/Object非available却允许DEPENDENCY_UNBOUND。独立 `crossfield01` actual1／0.945248s，两矛盾值被接受；stage02三行guard修复，原同字节probe在02 actual0／1.974563s。旧失败、输入和错误作者笛卡尔断言均保留，不改写成原轮全绿。
- 作者 `app-pure01` actual1 是测试POST缺合法Origin先被403拒绝的前提错误；仅测试调整，四份生产源不变。原九PASS＋受影响路由PASS与最终全race分列。
- Runtime闭包枚举脚本曾把带引号的非import前缀当package，原工具exit1发生复制前；只有原源码与工具返回，没有supervised raw，不补造。
- 原 `native01` 的bare `python3` 实际为3.12、冻结期望3.13，门禁在test Popen前exit1。原目录缺command/raw/result/cleanup/process baseline和input-before/after，后来的marker双扫是另外的观察；不能给原轮追填actual wait或双清。`native02`仅明确`/usr/bin/python3`与新轮名，driver/input/候选未变。
- service独立两次格式化前态只保留before SHA、argv/exit记录，缺精确preimage（`89d90c9a…46e8`、`1ffb4461…44d2`）；原实际RED/GREEN和最终测试源完整。不重构缺源，也不声称所有历史格式版本均能恢复。
- 第14说明两次私有检查前提错误：`no-index --check`有预期diff时实际1、无诊断被误要求0；之后猜错`diagnostics_test.go`而Git128，实际为`app_test.go`。原raw/result保存，但inline checker没有原独立脚本；不是产品红、无测试或资源执行。

历史非本任务PPID1/Z只保留原观测，不称本任务回收。后轮清理不替任何旧记录补wait；也不改变Object/tools/SPA publication停止边界。

## 4. 四个完整真实轮及实际终局

| 轮次 | 实际退出 / 秒 | 到达范围 | exact IDs / owned PID / adopted |
| --- | --- | --- | --- |
| 作者native02 | 0 / 7.502 | 三个Runtime native＋旧DiagnosticRoutes，4top | 0 / 5 / 0 |
| 作者new-account01 | 0 / 104.547 | 当前授权/事务1top6sub | 7 / 104 / 0 |
| 作者new-root01 | 0 / 57.201 | 原root/cache正式入口1top | 7 / 74 / 0 |
| 独立independent01 | 0 / 48.663 | A 2.52s、B 1.95s，两top同轮 | 7 / 74 / 0 |

这些完整轮都有原command实际wait、前后输入、两次所属PID/starttime空扫描、runtime空及0monitor errors。三PG轮的7个nonce归属ID分别双absent，原2container/4network基线保持；native轮不声称检查Docker基线。独立两次fresh磁盘门槛实过，driver预算仍每top2m/每pkg6m、race/count1。

独立A在真实PG事务内验证同Tx User SH/current Session/System Read、Source一次；正式Logout EX通过`pg_blocking_pids`实际阻塞，Committed后释放/join，后继同Session401。受控并发安全writer直接观察终局前status/body/flush为0；400ms较早parent下尾部仍未提前释放，放行后NotCommitted、handler/callback实际join且零发布。**这是受控writer证据，不是TCP提交前零字节。** 作者“调用尚未返回”的门闩也不能单独冒充该writer观察。

独立B使用原factory/Source/core及委托原Check，在同一实际TCP连接顺序读取Runtime→Session→Runtime→ready503，四响应完整EOF/Close、安全headers和单一canonical RequestID通过。每GET一次T/一个Committed、零新增Check，实际PG版本/原历史时间与闭合DTO/六项ready相符，client与root正常关闭实际join。测试内1h采样窗仅隔离tick，生产10s/2s/20s未改；没有重建被停止的Object生命周期、注入Object故障或验证生产SPA。

## 5. 第14文档与主线兼容

文档末件基于`aa9ea0b63de0c395525f3084ef514752ce2f1dfd`，仅新增Runtime局部说明与精确测试入口。主线实际受测为该HEAD加已复制13源；第14文档绑定私有已验候选，后由主线程提交成`9b074f8`，不能把后来提交号写成当时受测HEAD。

[主线编译兼容原报告](system-runtime-information-http-verification-evidence/objects/d1c28096a69d6988a10f0f0ac9290f734af6296369975c5ed40c4d7ff3c0dbed)记录五条实际离线命令：graph0／0.150s、Account race编译0／24.638s、精确list0／1.022s、App race编译0／3.645s、精确list0／1.041s。622本地路径、3499外部/来源指纹、396packages/31modules，合计27PID/starttime双清、0adopt；两个binary的SHA/字节保留、发现后已删除。只编译/list，没有业务测试、native、listener或fixture。非候选生产输入与b124650无差量，按授权复用旧Central/Runner构建，未额外重跑。

## 6. 持久原件与后续边界

[索引](system-runtime-information-http-verification-evidence/index.json)保存701逻辑原件、481个内容寻址对象（22756952B）、127个固定Git/既有永久原件引用。14源共26个生产/测试/说明历史版本可定位；实际受测源没有新缺件，两个未消费格式preimage例外如上。904依赖通过固定Git重建，3501/3504和主线3499只保存原指纹；不复制缓存、modcache、完整树、runtime凭据或二进制。

```sh
env PYTHONDONTWRITEBYTECODE=1 GIT_NO_LAZY_FETCH=1 python3 docs/development/agent-team/system-runtime-information-http-verification-evidence/verify_archive.py
```

校验器只检查原字节、固定Git、原记录关系、限定新增链接及行政差量，不执行产品。私有交付可使用`--repo /workspace/agenteam --documents /path/to/payload`核对，正式归位后默认仓库根。

本结果仅接受Runtime HTTP14。Audit UI仍处夹具修复、未接受产品，本档不消费其活动源或改变其档案。Runtime UI、完整D27/D28、生产SPA/发布与完整D08–D28/E01均未完成，E01未开始；Summary待决、生产未绑定／ready503保持。原Object join、OpenAI tools及SPA publication停止不重试、不改写、不转派；既有SPA scope/native证据不是SPA完整接受。
