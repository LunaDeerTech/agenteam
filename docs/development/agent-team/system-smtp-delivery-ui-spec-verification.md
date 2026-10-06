# System SMTP 测试与投递任务 UI 规格审查记录

状态：2026-10-06，**冻结rev0.2获独立STATIC PASS，主线程采纳为[正式卡rev1](../work-items/d27-system-smtp-delivery-ui.md)，已提交推送 `8bdfb006b32fbbc8889a190d7829f05e93e29787`，主线程核远端一致。** 这是规格与行政状态记录，不是产品、浏览器、worker或真实投递验收。

固定产品基线为`628612cdfc730cc1d89cad4e24a5d20f36cc6812`，依赖[已接受SMTP配置首卡](system-smtp-settings-ui-verification.md)、[管理读](system-mail-job-management-reads-verification.md)和[邮箱闭包](email-canonical-roundtrip-verification.md)。不把较早f670真实证据扩大为整棵628612c动态重跑。原停止的Object/tools任务未触及。

## 1. 固定规格与独立审查

| 来源 | 保存与结论 |
| --- | --- |
| 原rev0.1与rev0.2 | [原稿](system-smtp-delivery-ui-spec-verification-evidence/objects/765373802de023dadceb5d825b19a5049a0a1729da34ea620af2b70837568d02)、[被审rev0.2](system-smtp-delivery-ui-spec-verification-evidence/objects/f34d86d5720824c18b9fb162fc980e93d7454e753d98238c1ef6c3f5bf6746ce)、[精确差量](system-smtp-delivery-ui-spec-verification-evidence/objects/ab4a25e4c04314d9cec3d70a0b6b2cf2514cc8e2bd4c3e9945ae9c237e767256)；原范围18收敛为17，未覆盖旧稿。 |
| 冻结rev0.2 | 全文SHA `f34d86d5720824c18b9fb162fc980e93d7454e753d98238c1ef6c3f5bf6746ce`；技术§1–7 SHA `bee4f7ddb2b480b8c57b7d1b6b81be36bfce9f726222ebc10c8b468b90a54cda`。 |
| 17范围／正式接缝 | [精确17路径](system-smtp-delivery-ui-spec-verification-evidence/objects/01618cbc641bc76b85fdb8de954dcaf94bb9bbcfaab5a41262c03e033dc55e6f)、[接缝表](system-smtp-delivery-ui-spec-verification-evidence/objects/2026256b6b5a96fc7cd1886852a3c050d95439a9376c4d2347c95128cd5dd6da)、[固定Git输入](system-smtp-delivery-ui-spec-verification-evidence/objects/63e65068823c6ab446816cd5884d411fd961121935d8d084c0a4dc8fcc4d9fc4)；10新增／7既有，无新增后端、锁、菜单或路由。 |
| 独立静审 | [STATIC PASS原报告](system-smtp-delivery-ui-spec-verification-evidence/objects/1f0a5faca177f46a769615011a6758d2f3f3b74a7a8e6a5adb7ea8cf1c653b65) SHA `1f0a5faca177f46a769615011a6758d2f3f3b74a7a8e6a5adb7ea8cf1c653b65`；[原JSON](system-smtp-delivery-ui-spec-verification-evidence/objects/0bc66903f7adec96143518822c498fa9a640c709af89761563b98cb987d3fba1) SHA `0bc66903f7adec96143518822c498fa9a640c709af89761563b98cb987d3fba1`。无规格阻塞。 |
| 正式页首两版 | [原rev1私有末件](system-smtp-delivery-ui-spec-verification-evidence/objects/e1c138b6f10f22867acf39af618fa0fdd7031a1f7f0c7a53e6e3edfb531be05a)及[最终页首单句删除](system-smtp-delivery-ui-spec-verification-evidence/objects/40a04a17e022d9078fb42bf3a0ca501047c0e52c7cdfb85dccb075efb8fd785f)保留。提交版全文SHA `c7cb2de94d800e5578f5a0eb288d41853c8c7beeb0d0528ae50aceed3fb4be3f`，技术正文与被审rev0.2逐字相同。 |

作者43项固定来源加独立六码／后读核对6项，共**49个逻辑Git引用，去重47个文件**；authority.go和service.go重复引用，不能称49个不同文件。早期独立baseline另有4个准备期指纹，仅保留原清单定位，不把它们或47份静态来源说成运行依赖闭包。

## 2. 独立静态结论的有效范围

- 四API闭集可由现有正式服务实现：test与retry均202 JSON，test只返回job_id，retry返回新child的job_id/version，version可大于1且不强求源版本加1。管理页／详情严格投影与独立opaque cursor保持，完整页解析后发布；没有第五接口或公共lookup。
- 两写的新写／历史路径都有提交后GetMailJob。NOT_FOUND、所有5xx、不完整成功等不能证明原写未提交；五码确定业务拒绝须同时满足卡规定的status、commit_state与此前从未unknown。先unknown后拒绝保持粘性；401／当前403销毁恢复资格不表示回滚。GET／Session只能观察，原Execute重放不被“新写资格”门禁误拦。
- **同完整identity下，原CSRF须仍等于当前合法token。** Session token变化按既有publish规则更换epoch并销毁旧intent；不能将新token移植给旧intent继续恢复。CSRF不是后端命令幂等身份材料。此说明已在正式页首明确，不改变技术正文或产品规则。
- 第九域保留统一Cookie owner实际尾部、独立revision和双向abandon隔离。App协调器消费既有配置controller的confirmLeave／attach／detach，默认配置不挂新Panel、不读mail-jobs；切区、checking、同身份恢复与真正身份失效分别结算，不依赖旧finally写新代。
- 双确认在当前View共宿主，checking卸载而Promise留App，同身份恢复保留决定，失败有明确终点。取消读取loading同步退休为可显式重读状态；actual owner未join仍busy。正常close使用最新本地fallback，不延迟抢焦点。
- 五新组／五旧顶层和45秒浏览器／2分钟顶层／6分钟包预算有明确落点。正式worker、受控socket、accepted-cut、同command/intent/event/Audit关联及原材料内存比较具备可实现路径；不得SQL改phase/result/fence/join/next_at。**这些是可执行性静审，不证明用例已运行、预算实际满足或清理已通过。**

## 3. 原静态命令、准备更正与持久证据

[原命令记录](system-smtp-delivery-ui-spec-verification-evidence/objects/634d420edf56cf12af8ad334a7be1e38b10d84f3fafb135e5dab0fcfda9f2236)：`/usr/bin/python3 verify-static-input01.py`，原cwd `/workspace/agenteam`，实际exit0／0.284s／actual wait；[原输出](system-smtp-delivery-ui-spec-verification-evidence/objects/06a057a0e17733c64d02a98fcd903eb549c82641ad8d00499d39b1374a460d6b)与[原绑定结果](system-smtp-delivery-ui-spec-verification-evidence/objects/fe1830e48a324288a6b92ec0f03b63ffe56c0403e046e9f36015e4345314a888)保留。只核Git对象、文件指纹、候选存在性、五旧selector、五正式链接和文本格式，没有npm／Go测试、浏览器、数据库、worker、Docker或端口资源。

原准备消息一处误写retry204/header，已按固定handler纠正202 JSON，[原准备说明](system-smtp-delivery-ui-spec-verification-evidence/objects/1fddea67621b3e8b4fa871f5b1186378c36d34858b4a656c5a0c7995fd23e9c5)保留。正式页首precheck曾因`raw.index`误匹配页首内联节标记而exit1；[原失败](system-smtp-delivery-ui-spec-verification-evidence/objects/b9d4a90ea3595c3c62045eb7666ba1ac8d44980aad4d84775fe1a7dfe064b7a9)与原source、后续检查保留，属于文档提取前提，不是技术正文改动或产品RED。最终v2只删“尚未写入主树”旧时态，原rev1不覆盖。

[最小档案](system-smtp-delivery-ui-spec-verification-evidence/README.md)含30逻辑原件、29个SHA对象（219,962字节）及1个正式卡Git原件复用；47份正式源码直接按固定Git读取，不复制source树、依赖、缓存、dist或二进制。

```bash
env PYTHONDONTWRITEBYTECODE=1 GIT_NO_LAZY_FETCH=1 python3 docs/development/agent-team/system-smtp-delivery-ui-spec-verification-evidence/verify_archive.py
```

本次离线核验实际PASS，只核保存字节、47固定来源＋正式卡、17范围、原静态退出／raw、两版页首差量与不变技术；不执行归档脚本或任何产品测试。原记录缺失的命令／环境不补造，未来实现结果不并入本规格档。

## 4. 当前实施授权与未完成项

主线程已另授唯一frontend作者**17路径私有实现、适用离线检查，README末件最后**；已收到实际启动ACK，作者根为`/workspace/scratch/agenteam-smtp-delivery-frontend-fpf9njbd`，从固定628612c开始四API阶段。独立verification的[两代表计划](system-smtp-delivery-ui-spec-verification-evidence/objects/016cf031d76f5420bfa10bae7a52010acfea25cb6c0d405f6a97fb3c19dddb48)和输入指纹已冻结；A/B仍待实现冻结与真实资源授权，不是执行结果。这里只登记主线程确认的启动事实，不读取活动候选形成结论。

**尚无真实资源授权，也没有本UI实现接受、浏览器／worker／SMTP动态PASS或发送／收件成功声明。** 后续按冻结输入另验新五组、旧五组、真实状态与资源终局；旧TLS／底层及unknown/null例外只能在明示范围复用。外部邮箱、所有TLS、完整SMTP页面／D07／D27、生产SPA及Runtime均未由本次规格接受。D08–D28/E01未完成，E01未开始；Summary待决、Object/tools原停止、Artifact/Project及生产未绑定／ready503保持。本次只归位报告与行政入口、替换新卡页首临时审查链接；旧§1–22、产品与其它工作卡原字节保留。
