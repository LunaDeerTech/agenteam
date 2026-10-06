# System Audit 只读 UI 规格记录

## 1. 规格接受与历史输入

[正式卡rev1](../work-items/d27-system-audit-ui.md)已由主线程采纳，提交推送`a7a29c34acbe392d9a1375967309123b9a74ec16`并核远端一致。正式全文SHA `a22e10e8ba0531b043afdf5aae214a66dcda58a37088ae220c2654ee1077cac1`，技术§1–7 SHA `e7c96b588eed6218030ef6ce42154c7967667ff156bd8c343d0cdd14cf2bd8d5`；被审私稿全文`096b18e3e50d1612d3ce03e92073164eff60db35b3732f785297f52aa421b010`。正式页首只更新上游接受与实施门槛，原技术正文及36候选保持。这是规格接受，不是UI产品验收。

[独立原报告](system-audit-ui-spec-verification-evidence/objects/dc4afce5faf17d4dc458c516c41170b52dd8b1c7a039a3315ccf1184dea769ba)与[原JSON](system-audit-ui-spec-verification-evidence/objects/aeb3a5264f1e70b2cda3157720478fbbd693e1fd290ed13e3e54d01802105ba9)为有界STATIC PASS，无阻断。原审查固定前端1ff0442及Audit Q/input03，46有限输入＝32前端＋13后端＋1技术卡；原44项清单、补充到46项清单及静态检查结果均保留。后端后来在b124650接受，[冻结独立计划](system-audit-ui-spec-verification-evidence/objects/6020672b841df73ad0bb4cc8f954d6e35b4e20ccfa74f23beecb8c0fd6e97325)只将五关键HTTP文件重绑同一Git字节，没有新增业务执行。原输入、报告内当时pending文字保留，不替换为后继状态。

原两次只读`git show`因猜错文件名返回exit128，随后按精确路径读取；现存证据仅原review叙述，无独立command/raw/时长文件，不补造原命令或称产品RED。46输入与五文件重绑都是有限静态依据，不能当作未来源码／工具／运行闭包。

## 2. 被接受的接缝与验证边界

页面仅消费两个固定GET，14过滤字段及独立limit/cursor，53 action／25 resource是输入合法集，37 action／17 resource／13 Service与条件metadata是System输出集。页期draft与applied筛选分开，完整EOF与整页验证后发布；详情内联、显式取消／刷新、无关联补读或业务写。两成功口各1MiB，Problem600000B／Provider列表2MiB不变；975420B只是保守编码算术，不是极值producer页面实测。

第十一Cookie域沿既有owner实际尾部，与原十依赖双向排他；新GET不继承写CSRF/intent/lookup分类。九叶四组、十三精确return与36路径（35实施源＋最后README）限定保留。新三真实组和旧Outbound／SMTP Delivery两导航组是计划；另六旧browser仅数量变化的复用不得写成本轮执行。原八旧纯菜单／七分组／一return集合，八旧browser十四link与四group计数的精确位置由原static-checks保存。

独审特别分开两场景：生产pageshow listener在busy时直接返回，checking→Session503→同身份恢复／新页默认一次应从空闲owner证明；新controller等待旧owner实际tail后一次初始读、等待中销毁零续发，另用受控owner或合法导航证明。不绕过生产busy门禁，不借此扩App写权；原生焦点、实际浏览器、正式HTTP/PG均待未来授权与固定输入。独立计划的API／owner／App各两风险组合、最终最多A/B两代表均是计划，非通过记录。

## 3. 后继MIME修正与当前实施状态

规格及原计划的b124650旧OpenAPI输入未被覆盖。随后正式Go接受集与九个MIME schema分支不一致，经独立验证后两文件修复另提交`fa2d775fe1bbc1082f11f09c94eb5114a6ced6f5`；旧首红、原受测版本及修复结果见[Audit HTTP永久档案](system-audit-management-http-verification.md#5-后继mime-schema缺陷及修复边界)。本卡原技术已要求正式Go Parse/Format接受集，无技术修订；当前API材料需按主线程确认的fa2d775绑定，不能沿旧schema较窄集合实现。

以下仅记录主线程本次移交确认的行政事实，不引入产品证据：唯一frontend作者已在私有`agenteam-system-audit-frontend-m5wh30ot`启动35源分阶段实施；A五源最终180测试／type／format通过并冻结、MIME追加绑定fa2d775。独立A离线验证已实际启动，尚无终局；B尚未开始，没有真实资源或UI产品PASS。README第19路径仍须其余产品独立完整接受后最后授权。本档没有读取独立活动探针、作者后续候选或运行任何产品；阶段原件未来单立产品档案。

## 4. 最小原件与离线复核

[index.json](system-audit-ui-spec-verification-evidence/index.json)映射13逻辑规格／计划原件，按SHA为10对象、100047B；正式卡的两个副本用固定Git复用，受审私稿的两个副本共一对象。54个去重Git引用包含46静审源、五路径重绑的新增项、正式卡、四个旧HTTP原件及两项后继MIME输入；不复制产品树、依赖或缓存。旧HTTP报告／Q manifest直接引用94eda接受的永久对象。

离线checker只核原字节、固定Git、技术正文、候选计数与链接；不执行归档中的计划、脚本或产品：

```sh
env PYTHONDONTWRITEBYTECODE=1 GIT_NO_LAZY_FETCH=1 python3 docs/development/agent-team/system-audit-ui-spec-verification-evidence/verify_archive.py --repo .
```

本次行政基线94eda044，旧记录完整保留，通过新入口说明当前状态。Project Audit UI、完整D04/D27/D28/E01及生产SPA/Runtime均不由本规格交付。Object/tools及SPA publication停止不重试／改写／转派，SPA既有scope/native通过不等于产品接受；Summary待决、生产未绑定／ready503保持，E01未开始。
