# System Runtime Information HTTP 规格记录

## 1. 正式采纳与修订来源

[正式卡rev1](../work-items/d28-system-runtime-information-http.md)技术沿被独审的私稿rev2，主线程已采纳、提交推送`1cf08c70db1b88e7e8dd5f635193b5752fef4452`并核远端一致。正式全文SHA `e4cbf79d922c1b1456024e7cbf64fefa73937c75b5916e29c4e18fe66868bdfb`；被审rev2全文`bed0c30c025003460d1237f30329ffbfa9b3d9bd95029a751b14c3a794acff7b`，技术§1–7 SHA `aeb4222baf0e2d4547abf13277dc7789f4157e8c3d98843c73377e00d31d2bab`。本记录只接受规格，不是产品实现、编译或动态验收。

[原私稿rev1](system-runtime-information-http-spec-verification-evidence/objects/a2956d5491f5cce1409cff0400571aa031df61bdb0d66ca858872e5193aa451d)将尾斜线与子路径一起写为404，主线程发现其与既有HTTPBoundary路径先验冲突；[rev1→rev2原diff](system-runtime-information-http-spec-verification-evidence/objects/4de07be04461c684530ecb630017c027f1bf816fd6bd5012dd5e009e8d9c18a2)仅修页首、§2路径判定及§7对应验收。合法浏览器安全边界之后，尾斜线／重复斜线／dot段／非空RawPath等先400，规范未知子路径404，精确根非GET405；不重写请求或绕过Boundary来凑状态码。该发现是规格冲突，不是运行中的产品RED。

[独立STATIC PASS原报告](system-runtime-information-http-spec-verification-evidence/objects/a6b00fdb0cc6060434631f02ad1cc0085b1506435f7098768a30f5a7f4ccb727)与[原JSON](system-runtime-information-http-spec-verification-evidence/objects/19a660f7afa352f38531f7ca8a1dc6ac2d75cef0c9df4a6fae9e74e75b22b063)无阻断，固定后端b124650的31份必要源码。原inputs01／02／03各为17／29／31源码加rev2与diff两件；最终33条并不是33源码或运行闭包。首次误猜`diagnostics_test.go`的只读Git128／外层读取1保存在[原准备说明](system-runtime-information-http-spec-verification-evidence/objects/c6e59b136077ce17e7ec056d314dc86e9571abd17d9dfe9d31ed3543fadadf3c)，随后找到固定`app_test.go`；没有单独command/raw/时长原件，不补造或称产品测试失败。

独审后formal-header01曾放临时scratch审查链接，[页首01→02差量](system-runtime-information-http-spec-verification-evidence/objects/f2d88c8d50a34ef697a91ad619e31c2abfdeb071b93cb6760f4908dd8c32e257)在正式提交前改成非链接文字；两轮页首全文与差量均保留，技术不变。本次再将正式页首指向永久原报告及汇总，不修改技术正文或旧版本。

## 2. 被接受的边界与尚待验证事项

唯一GET `/api/v1/system/runtime-information`读取同一root已存在的内存观测，不触发健康探测、刷新、Object/MinIO或其他网络操作。Central版本明确未知；数据库版本与检查时间是最近成功样本，Object字段仅为现有聚合观测，不能表示MinIO server版本或逐项故障。一次缓存复制与同一T、monotonic年龄、失败优先于陈旧、恰20s未过期沿原健康规则；Secret/Outbound各取一次内存状态，不能声称多个领域的原子快照。

同一真实Tx内User Shared、当前Session/admin与grant匹配先于Source一次观察，只有实际Committed且context/deadline有效才发布；Unknown／取消／不完整源零候选，无业务写、receipt、lookup或修复口。预认证前3s／更早parent贯穿实际body/service/write/callback尾部；pure自然期限、native可能Canceled及PG1s分别验证，ctx.Done不能代替join。GET200只表示观察成功，ready仍false，原readyz仍503。

工厂经dependencies→startupResult显式传回，在原唯一monitor填入初始样本后、Serve/health启动前只绑定一次；后置纯构造失败须走原资源cleanup出口，不修改健康写入或停止任务的生命周期。14候选包含新service/HTTP/OpenAPI、同root观测适配与三旧app接缝，后端说明第14最后另授；不扩Account公开口、前端、公共httpapi、迁移或发布脚本。

原静审独核618B固定壳＋两个256×6B转义版本＝3690B＜16384B。它是保守算术，不是已执行Go编码／HTTP最大包络。原受控、native与两真实顶层都只是验收要求：一组当前权限与受控Source/事务，另一组真实root缓存/零probe/旧诊断兼容，证据不能互换。`TestDiagnosticRoutes`实际会监听，必须留给另授native窗口；45s场景／2m真实／6m包及真实终局要求保持，无本档动态执行。

## 3. 当前行政状态

按主线程本次移交事实，唯一backend作者在私有`agenteam-runtime-information-author-vE8bAGLk`获授前13源、实际仅两源；service阶段A两源已冻结，pure／race各7顶层58子例通过，vet／format通过，尚非独立接受。独立已复现跨字段校验缺口：DB／Object为stale或unavailable时仍可接受Readiness=DEPENDENCY_UNBOUND，与卡§3任一基础项失效须为DEPENDENCY_UNAVAILABLE不一致。当前返修中，尚未独立接受；HTTP／root／真实资源未开始，产品未接受，README第14最后另授。本档不读取产品源、不纳动态原件，仅记录主线程确认的行政状态。

独审31源码在后继fa2d775仍逐字相同；MIME修正接受见[Audit HTTP档案](system-audit-management-http-verification.md)，不改旧静审输入。Audit UI、Runtime后续页面与生产版本提供者另行交付，不借本卡宣称完整D05/D09/D27/D28完成。Object/tools及SPA publication停止不重试／改写／转派，SPA既有scope/native通过不等于接受；Summary待决、生产未绑定／ready503、完整D08–D28/E01未完成及E01未开始保持。

## 4. 最小原件与离线核验

[index.json](system-runtime-information-http-spec-verification-evidence/index.json)保存16逻辑原件，按SHA去重为13对象／122015B；正式卡由固定Git复用，独审中的rev2/diff副本不重复存。32个Git引用＝31有限源码＋正式卡；不复制完整依赖树或缓存。原rev1、rev2、两轮formal页首及所有原diff保持字节，归档只读校验不执行其中产品：

```sh
env PYTHONDONTWRITEBYTECODE=1 GIT_NO_LAZY_FETCH=1 python3 docs/development/agent-team/system-runtime-information-http-spec-verification-evidence/verify_archive.py --repo .
```

行政基线ddb5044109，AGENTS/tasks仅新增当前入口，continuation只追加§32并保留§1–31，正式卡只更新页首且技术SHA保持。没有主树／Git／资源写操作。
