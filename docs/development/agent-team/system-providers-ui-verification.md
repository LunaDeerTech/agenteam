# System Provider 管理 UI 验收与证据

2026-10-06，[Provider rev4 完整卡](../work-items/d27-system-provider-management-ui.md)由 `directory_backend`（frontend_worker）实现、`recovery_verification` 独立最终PASS，主线程采纳并提交推送 `f465f45b899e21c225e7d4107c099b256538239c`，主线程已核远端一致。接受范围为Provider分页/详情/创建/更新/启停/删除、Credential创建后独立绑定及安全恢复、Models只读子列表；本报告不接受后继Model管理。

最终[input07](system-providers-ui-verification-evidence/objects/e758cfd704f8677fe23cc57c8a41a23adff769fd85461a07092bedf1c6b26428.json) SHA-256 `e758cfd704f8677fe23cc57c8a41a23adff769fd85461a07092bedf1c6b26428`固定21个源码/测试路径、26项dist哈希、1016项只读依赖。第22路径README在独立通过后由[独立末件记录](system-providers-ui-verification-evidence/objects/545ce90981a24337ed65b9a80ba767704752613ed451072a395ccbd7b60f1d42.json)绑定，SHA-256 `16ec1f74dc723537a187161abc5b93f9c509a4a1b3c2bdaf07c859ddfa61a340`；22项全部与接受Git相符。基线为邀请UI `1c82d888adfef0d8b58ab51c920557ca4e2f084f`，后端 `9b3201547f9b7b61fd9716a6ba6540084961496c`与迁移1–19未变。卡技术§1–7 SHA-256 `827ddadeffd6ef196de56af6d076e10aaf6eb30d064969bb1d5d8f2ff7254d1b`保持原字节。

原[作者报告](system-providers-ui-verification-evidence/objects/8c2d5fe45ca845182edb795fd96afb31a793e2cbc0395466a5dfe13333c6b25a.txt)、[作者逐件索引](system-providers-ui-verification-evidence/objects/08580bcbe86216de3e39167a84026e9000554655bcfcb9ebf02457a2fdf9c7eb.json)和[独立最终报告](system-providers-ui-verification-evidence/objects/02b66ab11ba9c892387870af27a11ab07749a887e1a186e077c2498250995b6c.txt)、[独立逐件索引](system-providers-ui-verification-evidence/objects/072fe5c2bd3a1b580a05ef5896324eef7271f9618c4735457c3e3f2ead9d4e40.json)均按原字节保留；作者报告的README pending、独立报告的root adoption待定是当时状态，最终接受由上述提交及README末件补齐。[证据说明](system-providers-ui-verification-evidence/README.md)、[完整映射](system-providers-ui-verification-evidence/archive-index.json)给出历史路径、SHA、长度及物理对象或Git来源，不要求scratch仍存在。

## 1. 原产品红与独立阶段关闭

| 阶段 | 原事实与最终覆盖 |
| --- | --- |
| API01 → API02 | 原独立19项中17PASS/2FAIL：WHATWG `new URL`拒绝Go正式允许的IPv6 zone和数值外观host原文。返修保留原19项断言后19PASS，另14项JS/14项Go校验对照通过。原API三源与最终input07相同。严格DTO/命令receipt、2MiB Provider页及超界真实read/cancel尾部沿原屏障证据；不把metadata、lookup absent或列表当执行回执。 |
| owner01 → owner02 | 原5项中4PASS/1FAIL：Credential严格receipt后清材料通知的同步重入已abandon，旧延续仍发布submitting。原记录是无材料/无额外Provider POST但陈旧progress，不能写成泄密或越权绑定。两个公开通知后增加live门禁，原5项不改断言后全PASS，另1项确认重入通过；生产useSession与最终相同，后增3项作者测试的源字节差异单列。 |
| App纯组合 | 原app01第二组合PASS，第一组合末尾把永久aria-hidden字符的textContent误作可访问状态；app02仅复验受影响组合PASS，无推进2400ms来绕过。固定40文件闭包可由input01与Git重建，覆盖App真实controller/Session、部分成功/共同宿主/身份清理；不作原生浏览器焦点证明。 |

五阶段原报告及其原命令、probe、失败raw均由逐件索引关联。两步未确认、原key/body/CSRF Execute重放、严格版本和receipt、部分成功引用保留且材料销毁、分域CSRF与current身份、30秒可见截止后实际owner尾部沿真实传输屏障验证。浏览器安全观察器18 cases/239 assertions只证fetch/read Promise、Response/reader/cancel身份与受限响应事实，不是owner join证明。

## 2. 作者输入与真实组合

完整 `check01` 实际exit0/19.349s，23文件603测试及format/type/build通过；Go1.27.1 race integration compile0/44.605s、vet0/25.553s、config语法通过。原browser类型准备错误与早期pure03/04失败保留。后续产品只有caption局部CSS变化，受影响16组件、format/type/build及最终browser type实际通过；最终build0/5.864s、browser type0/.966s。未重称input07再次执行完整603，也不累加重复用例。

| 输入 | 相对前版真实差量 |
| --- | --- |
| input01 | 20候选源、26dist、1017依赖，含已关闭的API/owner生产字节；README待后补。 |
| input02 | Provider浏览器观察/harness修正；按rev4授权把旧邀请Drawer link count2→3加入第21源码，故只读依赖1017→1016。生产与dist未变。 |
| input03 | 仅Provider浏览器required label及稳定登录选择修正，生产与dist未变。 |
| input04 | 仅该测试补登录完成、物理两层/可访问一层与inert门禁；不回填new02未记录的物理DOM。 |
| input05 | 仅该测试补新Session请求序号和精确同身份200 EOF，再观察恢复层；生产与dist未变。 |
| input06 | View补局部sr-only样式；同一浏览器测试补两caption可访问性/裁剪断言和正式新增Model准备，dist重建。 |
| input07 | View为table添加定位上下文；同一测试补caption offsetParent、标准模式文档高度、main/doc归零及标题入视区，dist重建。 |

表中差量由七份原manifest、逐版源码及原diff复核，input02–05只改测试；input06–07不改API/owner或其他业务源。六个新真实组最终按Lifecycle/Credential/Outcome/Read取new02、Authority取new03、Navigation取new05组合；旧七组在input07运行。不是最终一轮重跑全部十三组。

| 作者真实轮 / 输入 | 实际exit / 秒 | 原顶层结果 | 资源终局（均7 exact IDs） |
| --- | --- | --- | --- |
| [new01](system-providers-ui-verification-evidence/objects/3cd33e1449179b398dabe92178664a4a1d22d5fdcd19dab193b4ced3207b9206.txt) / input02 | 255 / 192.639 | Lifecycle、Credential、Outcome因required label前提FAIL；Read准备有界中断，其余未跑 | 106PID/13wait；原cleanup=false，另次补清 |
| [new02](system-providers-ui-verification-evidence/objects/3a45f809f3c4c76acfe920991f312c3ac8a02f274622c8ed8f1c61936066e9bd.txt) / input03 | 1 / 158.810 | 上述三组及Read PASS；Authority登录完成门禁、Navigation层数前提FAIL | 176PID/24wait，双清 |
| [new03](system-providers-ui-verification-evidence/objects/fe522a570fcb7f17ce4dc67bbd9f9f1cdd5755008865d1780b4aae5c085165a0.txt) / input05 | 0 / 74.146 | Authority、Navigation PASS；图片未接受 | 95PID/8wait，双清 |
| [new04](system-providers-ui-verification-evidence/objects/012e7310531b5f4b6b483f529db61cf1b50d17a87431391db8d9d3296f6f2b17.txt) / input06 | 0 / 64.226 | Navigation PASS；图片仍未接受 | 85PID/4wait，双清 |
| [new05](system-providers-ui-verification-evidence/objects/f418132f63bb6cf7a42f2b97770425f02551ea5c0a0802b10da250b6b2023d57.txt) / input07 | 0 / 62.511 | Navigation PASS；最终八图接受 | 86PID/4wait，双清 |
| [old01](system-providers-ui-verification-evidence/objects/0a3367e66cbc97f76d2c27f3ffcac84c3cec4c1aeeaf8aee3a19cd1c007e32e3.txt) / input07 | 0 / 127.681 | 公开入口1、认证1、个人设置1、邀请2、目录2，七组PASS | 168PID/28wait，双清 |

Lifecycle真实正式Create→GET同时接受Go合法zone与数值外观host原文。Read正式26 Providers/26 Models分页及大于600000B、小于等于2MiB的合法Provider页通过；原raw未给精确字节数的地方不编造。预算始终browser45s、顶层2分钟、race/count1/每包6分钟、worker1/retry0；无匹配测试的包不计业务通过。

## 3. 两次图片拒收及最终审阅

new03功能通过后发现缺少sr-only定义，caption可见；new04裁剪后绝对定位无table上下文，外层出现空白，另有选中行导致main滚动的取景准备问题。原两版均未因测试绿而接受：保留new03 [light1440](system-providers-ui-verification-evidence/objects/bc26663b96567057bb09ff8963ff200291f1a8a2090c14bfe5aeffc29a4f6ecf.png) / [dark390](system-providers-ui-verification-evidence/objects/f33de3e50e10197f518543d79499a57a40283cebe7cfbb548be0452d44f37a4a.png)及new04 [light1440](system-providers-ui-verification-evidence/objects/3b71f06c3b779da84d6b0da275f7c8b0ed44d0e35e2be0215e90249f8b2c08a4.png) / [dark390](system-providers-ui-verification-evidence/objects/02ea330650f1355921b7a99b14e7fcfbcb153d49a994e1a8822a63301297e5dc.png)代表图，全部旧图哈希仍在作者原索引。

input07定位修复与取景门禁后，new05 light/dark ×1440/1024/834/390八张高度900的原图全部归档。作者逐张查看，主线程实际查看[最终light1440](system-providers-ui-verification-evidence/objects/e3219c42d6eb0dcd998d26b7baf488e9283d4903f450005bdbe882f0cd016df5.png)和[最终dark390](system-providers-ui-verification-evidence/objects/f68ba927f6cd385f6b0c71d0f9446b114c4302f50c85ba4acd37f45d02d7561d.png)并接受；没有新增独立截图。固定spec实断言两caption名称、clip/offsetParent、标准scrollingElement/body、main/doc归零和标题入viewport；图片只在textarea为0与物理overlay离场后采集。

caption-layout01/02是私有诊断按钮命名前提错误；03缺doctype导致quirks末文档高度断言失败；04以input06固定CSS和简化DOM在标准模式实际观察1440高度1789→900、390高度1680→900、BODY→table容器，main范围未变，exit0/1.077s、12PID/4wait双清。它只解释定位机制，不替代正式App、后端或完整页面验收；四轮原记录均保留。

## 4. 独立真实最终结果

| 独立轮 | 实际exit / 秒 | 原结果与归因 | 清理 |
| --- | --- | --- | --- |
| [independent01](system-providers-ui-verification-evidence/objects/b4841adbc4ef755a6d03a2f4ece3de8c30a32911d21f908484ee42d20965340c.txt) | 1 / 143.610 | 两个物理Playwright副本使test注册失败；两个顶层FAIL，已做fixture种子但浏览器业务未进入 | 7IDs/185PID/0adopted wait，双清 |
| [independent02](system-providers-ui-verification-evidence/objects/5bc2d0d7b8b1deae2cd8409893e3b8252fdafefc2f120ea9a8af843c2a6fc551.txt) | 1 / 74.579 | Replay PASS11.35s/browser7.5s；Partial FAIL12.56s为私有text/plain503硬要reader EOF的错误前提 | 7IDs/99PID/8wait，双清 |
| [independent03](system-providers-ui-verification-evidence/objects/1742d924c3076c798d17153eb52c4f2b0ed0fe7322dfb5524db53ed3cdbc8e7d.txt) | 0 / 56.399 | **仅Partial** PASS9.93s/browser5.6s；Replay复用02 | 7IDs/82PID/4wait，双清 |

最终由02 Replay和03 Partial组合，不是一轮两顶层全绿。第一次只修私有CLI的物理Playwright路径并核实际发现；第二次只删503的 `fact.ended`：客户端在Content-Type门禁await body.cancel，根本没有reader EOF。新seq503、错误页、物理0、恢复按钮、控制数1仍严格；成功恢复仍要求新seq200 EOF及精确user/session/role。[精确probe差量](system-providers-ui-verification-evidence/objects/8c8ab1ab39f5ae336e47be7e36d7951d6e46790320d2aa158f25b56f2f5f9eb2.txt)及各轮实际source/driver保留，Replay后缀、helper、fixture、tops、config、overlay和input07均未变。两次独立真实失败归私有装配/断言前提，不记产品RED。

Replay真实覆盖Credential和Provider两阶段已接受200回执截断、原key/body/CSRF精确重放与唯一正式DB事实，Session/list/lookup不能代替Execute receipt；确认写成功后一次GET503仍保持已确认，显式只读恢复零新增写。Partial真实覆盖Credential仅一次→Provider409、材料销毁/已备引用保留，待决确认经pageshow Session503共同卸载、精确同身份200 EOF后恢复cohost/top/焦点/Tab，仅重基Provider、旧共享引用保留、立即新表单干净且零POST。真实执行原test-security及两测试文件私有overlay；旧 `-c` 编译二进制从未执行。

## 5. 资源、归档与限制

九轮原命令均实际结束，argv/cwd/env/raw、actual wait、前后源/私有输入、observed IDs和PID/starttime、两次清理均保留。原2容器/4网络基线逐轮不变，monitor0；成功终局自有PID/runtime清零。new01原两次Docker/PID检查已空但私有browser runtime仍有子目录，故原cleanup=false保持；[后续精确目录清理与两次检查](system-providers-ui-verification-evidence/objects/d5ac892cd748432eef54638dde83660afb85ea5dc775b9ac5e5b7b09fbb96d27.json)另记true，不倒改原轮。最终独立03窗口已交还，无活动命令。历史非自有PPID1 Z（包括旧共享任务记录）未触碰、未由当前父wait，不纳入本次回收声明。

去重归档676项逻辑原件、409个物理文件（5,374,101字节）及16项固定Git引用。离线[verify_archive.py](system-providers-ui-verification-evidence/verify_archive.py)实际exit0，核22交付源、七历史输入、1026基线来源、九真实轮原终局、阶段复用及02→03差量；只读保存字节/固定Git，没有重跑产品、浏览器或归档命令。26dist只留hash/构建记录，不复制全树、依赖、缓存或二进制。准备时top-level旧probe后来修订，旧字节可由已冻01/02的source精确匹配；映射明列来源，不拿当前文件替代。

早期作者pure03/04没有完整旧源字节，原输入哈希、命令/失败raw保留，不补造。受控纯传输屏障证明actual read/cancel尾部，浏览器观察不越界为owner join；App纯组合不越界为原生焦点。只接受本卡，协议不可改/options={}、无连接测试、不原地轮换/自动补偿删除Credential的限制保持。未验证外部Provider调用、Runtime ready、生产SPA、真实Vite代理、其他浏览器/native zoom/真实BFCache，也未重跑所有作者旧组或每个端点的真实组合。完整D09/D26/D27及D08–D28/E01未完成，E01未开始；Summary待决、Object/tools原任务停止、Artifact/Project阻塞、生产未绑定与ready503边界保持。
