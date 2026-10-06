# System SMTP 配置与私有凭据 UI 验证记录

状态：2026-10-06，**作者检查、独立有界验收通过，主线程采纳并提交推送 `628612cdfc730cc1d89cad4e24a5d20f36cc6812`，主线程核远端一致。** 本报告接受[配置首卡 rev2](../work-items/d27-system-smtp-settings-ui.md)29路径；技术§1–7 SHA256 `910b51cf93f8a97e04641f31d9bc1e76f9b9a7c95850b2a72edad7da3e269d86`保持原字节。这里只归位已冻结证据，没有重新运行产品或真实资源。

管理员通过 GET/PUT/unconfigure 读取、保存和停用 SMTP，私有密码的保持／替换／移除、重试策略、冲突与未确认原请求恢复形成完整结果。当前 GET、历史 applied 与同响应 current settings 分离；后读失败仍保留未确认请求的原 password/key/body/version，只能明确使用当前合法 CSRF 重放。没有 lookup、测试发送或连接检查入口。第八 Cookie 域保留实际 I/O 尾部，当前权限／身份失效清理、同 View 确认宿主及最新本地焦点后备已按分版本证据验收。

## 1. 固定输入与最终组合

| 输入 | 精确依据 | 使用边界 |
| --- | --- | --- |
| 原 input01–03 | [input01](system-smtp-settings-ui-verification-evidence/objects/b6cc573b0b243ebe2f2a50704f20d0e649b6710e9c9fdc775c991049f4f7fb06)、[input02](system-smtp-settings-ui-verification-evidence/objects/c92d749cf3fd86b828501f44cf47c6a06a6e4b37cfca1d244c15bb28b63c1b44)、[input03](system-smtp-settings-ui-verification-evidence/objects/c7b8dbb16e67c3466c15d40fba8fa0c7fe0ca14d125944adcaa61a5b8f45d541) | `40c904c`基线1040件，28候选／36dist指纹；保留各原红及当轮源码。 |
| 最终 input04 | [manifest](system-smtp-settings-ui-verification-evidence/objects/3aebb2e2116c890989128d529b8d14afbb4c61126753b40998897344580e9e37)，SHA `3aebb2e2116c890989128d529b8d14afbb4c61126753b40998897344580e9e37` | `f670cb1`基线1050件，28候选／36dist；继承已接受邮箱闭包。 |
| 独立执行 input02 | [原执行输入](system-smtp-settings-ui-verification-evidence/objects/f3cc5c68d97c5ce4e296bad2a8f7c3ae1fc8f677987b7b1a426affdfcc573a90)，SHA `f3cc5c68d97c5ce4e296bad2a8f7c3ae1fc8f677987b7b1a426affdfcc573a90` | 固定 input04，10私有源、521 Playwright／14工具指纹，真实 A/B 使用此版本。 |
| 28源接受 | [独立清单](system-smtp-settings-ui-verification-evidence/objects/bd2421fef1d9000df76c91f08e36f6b083ee044be9c9275d13e18769bea613c9)及[独立最终原报告](system-smtp-settings-ui-verification-evidence/objects/5cceac784525f56bc9fc73ab2214053173c53c5a33d35d7f991aa2e51de3b005) | 逐SHA等于最终29路径去掉README；不把当时待末件／待主线封口改写为已完成。 |
| 第29路径 | [原 delivery29](system-smtp-settings-ui-verification-evidence/objects/33374f5773848ea2b5c4b6d2d1a9e5aaac92309aff96ae98091c7346329dc2bf)、[最终 delivery29-v2](system-smtp-settings-ui-verification-evidence/objects/0c076538cc01f232b610ca685662401526716de23b90d864442577ab715793d4) | README v2 仅将重放措辞收紧为“当前合法 CSRF”；28源／36dist未变。最终清单SHA `0c076538cc01f232b610ca685662401526716de23b90d864442577ab715793d4`。 |
| 后继主线组合 | [原报告](system-smtp-settings-ui-verification-evidence/objects/72a15e092ac8235f597d3698e85d60d1b32aa125e5c6276eabd47f193c359b01)、[604／3504输入](system-smtp-settings-ui-verification-evidence/objects/c5990ffe7d1e5fbb2c254f7b76dad8d803a3dd530842f93812546405a17e0a60) | 受测HEAD为`63de0ac`（含`a942779`）加SMTP候选；随后才提交`628612c`。编译／构建和两个纯路由检查，不是主线浏览器重跑。 |

原 source_resolution 显式记录 input02／03差量与 input04复用来源，不假定每版目录都有完整树。最终29路径以固定Git提交读取；1040／1050基线、604个主线本地Go/Embed/锁输入也从固定Git复核。依赖／工具／36dist仅保留原指纹和构建记录，不复制整树、node_modules、Go缓存或二进制。

## 2. 纯检查、独立阶段与原失败

- 作者 input04 完整前端检查实际 exit0／33.895s，**1214测试、35文件通过**，类型、build、格式与Go编译／vet／发现记录保留。作者API新闭包35项、Go25向量oracle及阶段owner/page/focus检查按原输入复用，未称独立重跑全矩阵。
- 原API01按旧后端判据实际11PASS／1RED，原probe/raw不改。已接受[邮箱闭包](email-canonical-roundtrip-verification.md)规定的 `S ∪ lower(S)`由API02另验：raw资格不通过预先lower偷渡，DTO canonical严格，原body/key/password不改。[API02原报告](system-smtp-settings-ui-verification-evidence/objects/7297baf13b8a1da86fcb7e18ecd1a792c35ce8258d0d7bf3369357fdd635ae77)实际 exit0／0.628s，**新契约组1PASS、旧11组明确skip并按原字节复用**；不写成该轮12组全跑。作者api-revision-red02两红、green02的35PASS及oracle原件均保留。
- [owner原报告](system-smtp-settings-ui-verification-evidence/objects/15cf35b33f2372bff2349a579783811e2aaa711d2fe0b7c4024c2e1e873e077f)实际 exit1／1.125s：**6个契约代表PASS＋1个额外flush:sync栈内重入FAIL**。额外构造会在abandon尚未返回时启动新写，原progress被旧同步清理置空；主线程按正式消费链适用性排除，不授权owner返修。保留原期望、源码和整轮exit1，不称7PASS。受控原生cancel屏障验证实际尾部；fake timer的30秒不冒充墙钟30秒。
- [页面原报告](system-smtp-settings-ui-verification-evidence/objects/19b0e913d6e5b4ce867692084c26cca69cca0ddc01c7a872b332e4e2c58f5b7e)为真实App＋受控fetch，2项实际 exit0／2.301s；jsdom不能代替原生焦点。页面首红是旧停用反馈泄入新策略；后续diagnostic03的非响应式baseline与diagnostic05的最大版本门禁问题分别由ref和editor.version修复，原红／绿及精确源保留。
- focus-app-red01保留clean／dirty两种disabled trigger原红；focus-page-green01 state49＋App15实际64PASS。显示禁用拆为disableReady，动作canDisable仍保留权限、版本、owner和双模态门禁；原生场景另由navigation04实测。
- new01的SQL `i.email`不存在，使六组在fixture准备处失败、未进入浏览器；原静审漏判及勘误不删除。new02四项失败原现场只支持所记录的观察／超时，不能把后来dirty／focus诊断追填成每项旧现场的唯一原因。navigation03的焦点失败与后续修复分列。
- 作者早期api-type01 union装配、harness编译／格式／类型等准备失败和更正保留。独立input01两处错误确认标题在启动前被**静态挡下**，input02仅纠正精确标题；没有该版本的动态RED。独立type01 TS2688前提红的三份原cases/spec/tsconfig均已找到并按当轮SHA入档，不列缺件。
- README原验证器正则漏匹配无后缀`.spec.ts`，实际exit1后只修私有匹配器；原[失败记录](system-smtp-settings-ui-verification-evidence/objects/5e782d0c6036b9430fbf2d750cc4a8229f705e4e62858361ce643f33f18296b5)及最终检查保留，未因此修改业务源。

## 3. 十三轮真实原结果与实际终局

表中的秒数为原driver实际退出，包含fixture、编译和结束尾部，不是单一浏览器用例时间。全部保留原command/env/raw、PID/starttime、资源exact ID、实际wait及两次清理。每轮7个exact ID双absent，所属PID两扫空、runtime／短浏览器目录为空，monitor0、原2容器4网络不变。原历史PPID1 Z不触碰、不计本轮已回收。

| 原轮 | actual exit／秒 | 实际顶层结果 | 所属PID／adopted wait |
| --- | --- | --- | --- |
| [new01](system-smtp-settings-ui-verification-evidence/objects/cf98c8cf6f767ab0e4159dac6b99d3b97c70fe3fe61f5c9a3b110301c7a3c269) | 1／135.186s | 0 PASS／6 FAIL；fixture SQL准备 | 109／0 |
| [new02](system-smtp-settings-ui-verification-evidence/objects/48dfd9c8b0fed3c08c32140377c15936448e219bd223e9d647629953dec1ef45) | 1／180.320s | 2 PASS／4 FAIL；原整轮FAIL | 147／24 |
| [navigation03](system-smtp-settings-ui-verification-evidence/objects/7d32f48ae9a1f4d7bc9596b95bfffc1fae9ca6baa3aca799744560aa3f1b5b47) | 1／66.417s | 0 PASS／1 FAIL；原生焦点 | 83／4 |
| [navigation04](system-smtp-settings-ui-verification-evidence/objects/e6fa82734e7aaecb71240233e656f33bfac1a99ed44e0e4d2f8171ed1e0b7809) | 0／95.489s | 1 PASS | 93／4 |
| [lifecycle04](system-smtp-settings-ui-verification-evidence/objects/afc900dad12f6d6bb6f76df36e398bfab11f50bfed332ed298375090e5ac9dc5) | 0／62.024s | 1 PASS | 82／4 |
| [credential04](system-smtp-settings-ui-verification-evidence/objects/9214ebe28e122a4a787d4cae83d7646d1299b09810378de9dbe305911c247b32) | 0／61.320s | 1 PASS | 83／4 |
| [outcome04](system-smtp-settings-ui-verification-evidence/objects/69e07701acfa0e556b0fdea5b046a5761507ccc365f6e08b1459f5e173c6a8bd) | 0／63.663s | 1 PASS | 81／4 |
| [oldcore04](system-smtp-settings-ui-verification-evidence/objects/e5d50db9535b949942f45edd0db20c76d54a23260b09576224e00a20a1c1d8d6) | 0／138.229s | 7 PASS | 164／28 |
| [oldmodels04](system-smtp-settings-ui-verification-evidence/objects/d7e22bff9983c826825ad8aae1ad3582f1239dc73f0b1c4d95cf135da7f03a8d) | 0／111.891s | 3 PASS | 107／12 |
| [oldproviders04](system-smtp-settings-ui-verification-evidence/objects/69a622a7a103309e77982ff16cbad01fbfd3e6cbe3d7cf3e5c34b672a0e85bca) | 0／87.817s | 3 PASS | 107／12 |
| [oldsecurity04](system-smtp-settings-ui-verification-evidence/objects/0250db1f59e7753ce7d1c6dc9629fe2d038709e592289a32bde2aee4bbfb5b1b) | 0／85.674s | 3 PASS | 108／12 |
| [oldselection04](system-smtp-settings-ui-verification-evidence/objects/12a44e40aef304146d5b440715c42494e40339f89c5da2fc65c9d8d1c68bd1d1) | 0／92.332s | 3 PASS | 109／12 |
| [independent01](system-smtp-settings-ui-verification-evidence/objects/0acae7ba984684e356c1d48df10531c44d1da11d51bf64712f9f016aec862140) | 0／158.636s | 2 PASS | 181／8 |

作者新六组由 input04 的Navigation／Lifecycle／CredentialLifecycle／OutcomeRecovery四次PASS，加new02未变部分的Concurrency **8.95s**、Authority **18.66s**组合；new02原整轮exit1、另四FAIL不抹去。[独立组合审阅](system-smtp-settings-ui-verification-evidence/objects/16286341bfc1b7b7a66d46ae916ad05b7a770da372f857d558d581d5b7ffb022)逐项核client/useSession/App/router/Go fixture与前五browser动作原字节、普通小写邮箱分支、低版本／末次保存以及未走停用确认等适用性；新增domain-literal API纯测不扩称真实浏览器覆盖。旧19组分7＋3＋3＋3＋3五批通过，原轮未合并成一轮。

navigation04保留浅／深色各1440、1024、834、390八张PNG及[作者图审记录](system-smtp-settings-ui-verification-evidence/objects/ce66f6a4c24eee7abd0afb203c23a458f0a20e7311ec329941196c1380bd48ba)。**八图仅作者逐张查看，主线程未图审，本归档未重新图审。** 旧回归48张图只保留原索引／指纹，不增加逐图验收声明；独立敏感真实轮关闭screenshot／trace／video。

## 4. 独立 A/B 的实际到达

[独立最终报告](system-smtp-settings-ui-verification-evidence/objects/5cceac784525f56bc9fc73ab2214053173c53c5a33d35d7f991aa2e51de3b005)SHA `5cceac784525f56bc9fc73ab2214053173c53c5a33d35d7f991aa2e51de3b005`。independent01首次真实轮两顶层PASS：PUTReplay Go12.31s／浏览器6.7s，UnconfigureReplay Go12.77s／浏览器7.3s；whole actual0／158.636s，7IDs、181PID、8adopted实际wait双清，窗口释放。

A：带私有密码的正式PUT接受后响应截断，另一正式管理员停用。Session／GET仅观察currentfalse且零自动写；明确原body/key/合法CSRF重放确认原历史applied，保持更新后的currentfalse。原command／Audit各1，原MAC／预备引用／actor不变，当前引用0、旧清理登记已存在。严格确认后单独GET503只显示读取失败，不伪造reader EOF。第二次未知PUT在精确自有管理员资格降级后原重放真实403，DB／引用／Audit无新写；同Session恢复权限后旧恢复动作和私有材料已清空。

B：先明确放弃4/120草稿及私有输入，再捕获已保存3/60的停用确认。生产pageshow、真实Session503和同完整身份恢复经过两类确认，物理双层只顶层可访问、下层inert／aria-hidden，原生Tab及关闭后最新本地标题fallback通过。停用POST接受后截断，另一管理员重新配置；明确原三字段body/key/CSRF重放得到原历史applied＋当前true，版本不倒退，原command／Audit各1，当前引用1且不替换。

材料只在自有进程内比较，未输出原body/key/CSRF/密码或其材料hash。native observer保持同一fetch/read Promise和Response/reader/cancel，无clone、tee或额外读取；observer完成不替代owner实际join。身份准备相关受限日志已结束，配置阶段投递任务／intent维持该准备基线，禁止test/invite/reset/jobs请求为0；这不是整个身份准备过程从未产生投递事实。

## 5. 主线 Go 封口与 README

[主线原报告](system-smtp-settings-ui-verification-evidence/objects/72a15e092ac8235f597d3698e85d60d1b32aa125e5c6276eabd47f193c359b01)SHA `72a15e092ac8235f597d3698e85d60d1b32aa125e5c6276eabd47f193c359b01`。精确输入为`63de0ac`（含已接受Outbound `a942779`）＋SMTP29，604本地／3504外部Go输入逐命令前后一致；没有新fixture、浏览器、Docker、SMTP或业务端口资源。

| 原命令 | 实际结果 | 有效范围 |
| --- | --- | --- |
| account-race-compile01 | exit0／34.369s | integration＋race `go test -c`，零业务测试执行。 |
| app-pure01 | exit0／3.548s | 仅Outbound与Model两个root路由保留／单middleware纯测试。 |
| central-runner-build01 | exit0／17.675s | Central／Runner只构建，不运行二进制。 |

三命令actual wait、PID/starttime两扫空和输入一致均保留。[29路径末核](system-smtp-settings-ui-verification-evidence/objects/7f6dcc9d96a5dd35bb2b9e2088c4b8f8b40086405cde9f3a86af171eff09ac34)等于原28＋README v2；README仅交付指纹，不属Go输入。SMTP input04真实HTTP／浏览器仍是`f670cb1`，原更早失败／复用轮仍各自绑定`40c904c`；主线接缝由固定Git静审＋本节检查闭合，不改写成`a942779`或提交后`628612c`真实重跑。

## 6. 最小档案、记录限制与离线入口

[索引](system-smtp-settings-ui-verification-evidence/index.json)保存**834个逻辑原件、499个SHA去重对象（9,622,430字节）和100个逻辑Git复用引用**；78个作者／私有精确源版本保留消费者映射。固定Git29路径、1040／1050阶段依赖和主线604本地源可复核；36dist及外部工具指纹只证明原记录绑定，不等于当前dist或当前工具重验。[档案说明](system-smtp-settings-ui-verification-evidence/README.md)说明提取和复用方式。

历史两处有界限制照实保留：harness-format00有原source/raw与已完成tool exit1，但无原完整argv/env或前后绑定；harness-type00有原invocation/raw/tool exit2，但无单独child result／actualwait或精确当轮源指纹。不得事后拼装；最终输入／实际真实轮完整。独立type01三原版本存在，不与这两处混淆。页面／startup的只读FileNotFound等准备更正不补造不存在的child raw／wait。旧现场缺观察不从新诊断倒填。

在仓库根执行：

```bash
env PYTHONDONTWRITEBYTECODE=1 GIT_NO_LAZY_FETCH=1 python3 docs/development/agent-team/system-smtp-settings-ui-verification-evidence/verify_archive.py
```

离线检查实际PASS：原件SHA／字节、29提交路径集合、2722个固定Git blobs、卡技术原字节、76条原运行记录含13真实轮actual退出／双清，以及API02显式skip、owner原exit1、三条主线命令与八图格式。它只读保存字节和Git，**不运行归档脚本或产品测试**。本次检查器最初误把阶段文件记录都当字符串、再误把内嵌指纹和空env当统一文件形状，修正为原schema后通过；这些仅是归档脚本准备错误，未改原件，不是产品RED。

## 7. 接受边界

仅配置首卡接受，不包含测试收件人／发送、投递任务页面／人工重试、SMTP连接／认证／外部邮箱送达、Secret立即物理擦除、生产SPA/Vite代理或Runtime验收。GET不作写回执，无lookup或自动重放；真实响应截断不扩大为DB CommitUnknown证明。后续SMTP投递页和出站页面另验，完整D07/D27及D08–D28/E01未完成，E01未开始；Summary待决、Object/tools原停止、Artifact/Project与生产未绑定／ready503边界保持。旧文档、旧§1–21及其它工作卡均保留。
