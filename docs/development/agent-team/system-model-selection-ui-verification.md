# System 平台模型用途 UI 验收记录

2026-10-06，[Selection rev2](../work-items/d27-system-model-selection-ui.md)完整卡已由 `directory_backend`（frontend_worker）完成、`recovery_verification` 独立最终PASS，主线程采纳并提交推送 `870ebbb986f56bb62be34ccdfa2c77819ce995a6`，主线程核远端一致。本报告归档已执行证据；文档负责人只做离线原件/Git核验，没有重跑产品或资源。

接受四用途当前引用读取、有限补读、候选显式分页、整组保存、版本/当前权限裁决、原请求恢复与App期草稿及确认宿主行为。依赖[Model UI](system-models-ui-verification.md)的固定 `bc17167c42ee5d5fc1427adaac099ff888ca959b`、已接受后端 `9b3201547f9b7b61fd9716a6ba6540084961496c`与迁移1–19。原27候选按卡页首剔除无需修改的 `web/src/api/system-models.ts`，实际交付25业务/测试源加README共26路径；卡技术§1–7 SHA256 `0bce82bfeeba06a9b2aa894ad61b841de41306300f220437c4beb137fb325c07`不变。

## 1. 固定输入与原件入口

| 原件 | 固定标识与用途 |
| --- | --- |
| [作者最终输入 input02](system-model-selection-ui-verification-evidence/objects/af2f9f4a50f4c435e849bf8faf9d8676a11ccd7c3c6d61416dcbfda57f8a72c5.json) | SHA256 `af2f9f4a50f4c435e849bf8faf9d8676a11ccd7c3c6d61416dcbfda57f8a72c5`；25源/31dist指纹，1041条固定Git依赖、7132 runtime文件指纹和28链接前后门禁。依赖和dist实体不归档。 |
| [作者报告](system-model-selection-ui-verification-evidence/objects/5efb44803e6e435466990786859584a2b2099c8fea835f31bf72cdbfa3f6bfc8.txt) / [作者索引](system-model-selection-ui-verification-evidence/objects/952d7e2997f896fa565b6987a86d9c4670cb4c6665d95c22d238d05165d2191c.json) | 报告SHA `5efb4480…bfc8`，索引SHA `952d7e29…2191c`；原pending/README未写按当时状态保留。52条原检查与五轮真实命令均可定位。 |
| [独立最终报告](system-model-selection-ui-verification-evidence/objects/2b719675af5d1faed975c7d983bded6557b628bc873028ede19bb162036bdcda.txt) / [独立索引](system-model-selection-ui-verification-evidence/objects/02be986ffd1c69cf5fb19b2798f149a064ecd6e06b85b6c47e1c85a455efa2cc.json) | MD SHA256 `2b719675af5d1faed975c7d983bded6557b628bc873028ede19bb162036bdcda`；JSON SHA `02be986f…efa2cc`，三轮真实原件和阶段复用界限。 |
| [README最终26路径清单](system-model-selection-ui-verification-evidence/objects/34e87bcc072200a123a4b860c727c889fd309a05282683b428d0b5a9d5cd00b3.json) | SHA256 `34e87bcc072200a123a4b860c727c889fd309a05282683b428d0b5a9d5cd00b3`；README SHA256 `5ddfc8ae082230f788d38bb20acc3eaaad0ad2e84a65e20870ca4e78aee68e11`。其余25源、31dist指纹与input02相同，主线程检查后绑定接受提交。 |

[证据入口](system-model-selection-ui-verification-evidence/README.md)与[原件映射](system-model-selection-ui-verification-evidence/archive-index.json)保存1235个逻辑原件、445个按SHA去重对象，共10441518字节。原命令、环境、raw、输入、版本差量、失败与资源终局保持原字节；scratch路径只作历史出处，离线读取不依赖scratch或后继活动产品树。

## 2. 作者与阶段独立结果

作者完整隔离 `npm run check` 实际通过29文件/993测试、格式、类型和生产构建；race integration account编译、vet及修后browser格式/类型通过。六种浏览器模式用绝对CLI各发现1例；原生观察器28例306断言与12个私有门禁代表通过。`--list`、编译和纯观察器不计真实业务通过，实际argv/cwd/env和raw见作者原索引。

独立API15/15、owner8/8按未变源复用；页面原probe7/7与补充5/5通过。原产品红 **P-SELECTION-PAGE-01**：精确Provider被读为disabled后，已选Model草稿的`canSave`仍为真；原观察writes=0，未发PUT，不推断正式后端接受。page-stage02→03生产只改controller `useSystemModelSelection.ts`，state回归测试另变，其余16路径保持；原失败probe与返修original01字节完全相同。

其余原失败分列：

- 作者page-app01的16/17首红为成功后按钮处于既定2400ms可访问名称，测试立即按旧名称定位；page-app02只改观察。repair-pure01为100/101，原独立7与App17通过、state76/77；唯一新增测试错误假设Anthropic/json_schema可合法解析。pure02仅五个受影响用例通过，72未选，不称全77重跑或正式Provider协议可变。
- **H-SELECTION-01** 是先于真实运行的静态阻断：text/plain503不会产生成功reader EOF。input01仅修两处失败观察，200严格DTO/EOF和原生观察器字节保持，不标为实际产品红。
- harness-type01缺Node类型/DOM.Iterable命令选项、harness-format02格式检查失败，以及独立type01/list-a01/list-b01的Node PATH启动错误均保留。后三次子进程未启动、raw为空、没有child result/后输入，不补造结果或算作类型/发现已执行。
- 作者new01 Navigation在浏览器前因正式Provider seed名称130/129 rune超过128返回400，原轮没有图片；固定调用点是共享helper的 `/system/model-providers`，通用“Model setup”错误文字不是Model来源证据。input02仅把该导航seed重复35→34；正式`ProviderInput.Validate`/`ModelInput.Validate`四组原检查保留，新名称合法且模型原生ID256B/257B边界未削弱。new02只复跑Navigation。

## 3. 八轮真实执行与组合接受

下表耗时是原driver实际wait后的耗时；PID列是各轮跟踪的PID/starttime数，wait列是实际adopted waits。每轮7个exact资源均双absent，原2容器/4网络基线不变，输入前后匹配、monitor0、自有进程/runtime清零。其他包的`[no tests to run]`不计业务通过。

| 原轮 | exit / driver实际耗时 | 到达结果 | PID / adopted wait | 原命令与原终局 |
| --- | --- | --- | --- | --- |
| author/new01 | 1 / 204.111s | 新5 PASS；Navigation seed前提FAIL | 178 / 20 | [raw](system-model-selection-ui-verification-evidence/objects/f3020b646dcf39a46736fa86cdaded3f2c57f6bd350da24372bfedb0e567af68.txt) / [command](system-model-selection-ui-verification-evidence/objects/7b183c22ae3e17a24212f20ed9e1ec4abfa29f6d00a9fcfd8155ba06bf0de7e8.json) / [cleanup](system-model-selection-ui-verification-evidence/objects/07ca3808d9053000b448184a6707b5c7ab11d0f301b2074e1f2d3b6f0d87f740.json) |
| author/new02 | 0 / 69.029s | 仅Navigation 1 PASS | 89 / 4 | [raw](system-model-selection-ui-verification-evidence/objects/07a0e35f2b4660bf8583f8f8365f057b34e30beb7b1fb89785e68e1f046a14b2.txt) / [command](system-model-selection-ui-verification-evidence/objects/cdef7c39707cb62f0b500ad92811a914637b02debb919d3b45ca176132e775df.json) / [cleanup](system-model-selection-ui-verification-evidence/objects/7b07cd8b1138ce1da520dc8499b9b747a675f4ecd276b0e5c37b7e9933f3aa94.json) |
| author/old-core01 | 0 / 126.425s | 旧core 7 PASS | 168 / 28 | [raw](system-model-selection-ui-verification-evidence/objects/b037a32ef603ba54804719c7a20ba23cc94241be8ddb46b7bf2255e3196aef17.txt) / [command](system-model-selection-ui-verification-evidence/objects/9db36255979275b3e7cf7c2da9efec967720dc2e2b7969bb00721aaa0766f842.json) / [cleanup](system-model-selection-ui-verification-evidence/objects/cd69be609c883ada4650ee08b43839da2952bc0a6b418420e108221c80912f6e.json) |
| author/old-provider01 | 0 / 89.833s | 旧Provider 3 PASS | 111 / 12 | [raw](system-model-selection-ui-verification-evidence/objects/da0f796b59bb9b930a24ef30a33c7e88a4b3dc954f5a9dc6d82c25162970d20f.txt) / [command](system-model-selection-ui-verification-evidence/objects/77906152b4e2d0a72cd5c6fbee971a4574de49be6acb3c5404cb7636cea480ef.json) / [cleanup](system-model-selection-ui-verification-evidence/objects/a6e266d66153d6bb80f413f40faaa0f75699430f8687674eb31220b8182bb1b6.json) |
| author/old-model01 | 0 / 107.473s | 旧Model 3 PASS | 108 / 12 | [raw](system-model-selection-ui-verification-evidence/objects/8ec0d2902e7c0a7d1ad2af234721588524649068e313902aba0d941e1ea47e32.txt) / [command](system-model-selection-ui-verification-evidence/objects/bd3ad67c421d586809acd5f30bb56c5dfae2496a8d78942062579747eceddb4a.json) / [cleanup](system-model-selection-ui-verification-evidence/objects/4791dbbeab5114af916591bdb629e38c7f4ff043ca97e05d830919c65923d5b5.json) |
| real/independent01 | 1 / 199.715s | A审计摘要、B按钮名前提FAIL | 203 / 8 | [raw](system-model-selection-ui-verification-evidence/objects/ca4c09e3ebbf25727a2d413381fbe89c8b983eb6a78f2a5287130183b9bbbad2.txt) / [command](system-model-selection-ui-verification-evidence/objects/2ec9423e9e668259d19fe9f4ed98e3e5519f46d3054dee57d352c3e1fbb6d081.json) / [cleanup](system-model-selection-ui-verification-evidence/objects/66f4ef6165ac30a7fd343045f73fbae6965e23b2ee66c78d026401af8e6016a1.json) |
| real/independent02 | 1 / 77.600s | B完整PASS；A查证后文案前提FAIL | 95 / 8 | [raw](system-model-selection-ui-verification-evidence/objects/3f3cf0e6556c831e71990c1e4d8916d6b30b7358fb58f997938c47b2c6cb5dcd.txt) / [command](system-model-selection-ui-verification-evidence/objects/10d88722ef4dffbce17caeaaf0acc7e6811ec4d6c50d555908f266baee024659.json) / [cleanup](system-model-selection-ui-verification-evidence/objects/c6df4aae4013325c38e1e9a48dfa9d182eb2e9a473ba2cdadbdaf8aaac713f61.json) |
| real/independent03 | 0 / 61.415s | 仅A完整PASS | 84 / 4 | [raw](system-model-selection-ui-verification-evidence/objects/a05b325196ac89b3ae546f5172ef1102dde386bee07bb098fd885227e086ef1f.txt) / [command](system-model-selection-ui-verification-evidence/objects/020759ae573de3599746cba3054dd04b5f4d2cfdfbeb932c4aeb3936bd42838b.json) / [cleanup](system-model-selection-ui-verification-evidence/objects/3262ed6d3085328a7fbed964d426369fc34667f2063bfddfb76c39f9b0de7870.json) |

作者新六组由new01的Lifecycle、ReadAndPagination、ConcurrencyAndReferences、OutcomeRecovery、AuthorityAndIdentity五组，与new02 Navigation一组组成；旧core7/Provider3/Model3在input02通过。不得称new01或某个最终新六组整轮全绿。

独立01的A已到达正式接受PUT截断后的原command oracle；私有摘要未使用正式`cursor.Digest`的canonicalization，使审计匹配数错误为0。该轮未到Model正式删除替代、lookup与显式原重放。B已到达候选失效400/not_started、零部分写、草稿保持与打开核对确认，但错误寻找“放弃修改”，实际条件按钮为“读取并核对”，未到后续冲突/Session恢复。

独立02修正上述私有前提后，**B完整PASS 11.95s**；A已核原command/Audit/event、正式删除替代使当前E+2且旧目标404、同Session当前GET和原lookup历史回执，但在lookup后状态文案观察停止，尚未执行原重放及最终持久化唯一性。原01/02都保留exit1，前提错误不算产品红，已到达的部分事实不替代整组通过。

独立03只复验A，**完整PASS 9.38s，driver exit0/61.415s**；B在bound04/05中的完整段落字节相同，复用02结果。最终A真实验证：接受PUT响应截断保留未确认，正式Model删除/跨Provider替代推进当前E+2且旧目标404；当前GET和lookup不作确认；显式原PUT重放严格返回历史E+1、影响引用数0，原key/body/同Session合法CSRF一致，精确原command/Audit/计划配置事件各1且delivery为0，当前配置与引用不回退。

B验证正式候选失效400/零部分写、真实版本冲突409、显式核对、待决确认中Session503与同身份恢复、物理2/可访问1层、原生Tab和关闭后下层焦点。纯API/owner屏障承担实际read/cancel尾部证明，原生观察器不代替该证明。作者受限SQL增量旁证与独立精确原command/原body/CSRF断言分开。

最终独立03实际wait后7 exact资源双absent、84所属PID/starttime清零、4次adopted wait、monitor0、runtime/短TMP空，资源窗口已释放。历史PPID1 Z未触碰，不纳入本轮自有资源回收或已wait声明。原源以execution指纹绑定保存的bound03/04/05，未补造当轮source快照。

## 4. 视觉与证据限制

[八图原审阅记录](system-model-selection-ui-verification-evidence/objects/0ecfa2a098236e722b1d39c3db8af967788203fcb0d60d3f45b87b44c9300db5.json)覆盖light/dark×1440/1024/834/390，作者逐张实际查看；主线程实际查看并接受[light1440](system-model-selection-ui-verification-evidence/objects/f9f0b2735ad54d5cf00723a17b48bfd79fe7289de564d6cfea43b34e5a83b2f9.png)与[dark390](system-model-selection-ui-verification-evidence/objects/5d82c20a204535e04f2c5e717c6022c156bfa32cb08c7b1b8d4a8e755a9b07e2.png)。可见区域长字段折行、窄屏单列、主题与焦点指示正常。900px视口仅显示长卡上部，不据此声称下方全部字段人工视检；文档归档没有新增视觉测试或独立截图。

四份历史通过源码已由作者确认不存在：api-format01的三份格式化前API/测试源，以及owner-pure01的旧state spec（owner-type01也引用该同一版本）。此外，完整原索引中的十二份通过格式化命令的输入暂态版本在选定快照中未定位。两类均保留原SHA/command/raw/actual exit，并在archive-index分别标注“作者确认缺失”与“未定位”；不事后反推格式、补造或重跑。**失败测试源与最终受测版本齐全，不声称每个历史版本都可重建，也不把格式化暂态缺件当作最终生产验证缺口。**

本卡未验证外部Provider调用、Runtime、索引重建或serving生效、生产SPA托管、真实Vite代理、其他浏览器引擎、native browser zoom或真实BFCache。只消费正式System能力，不扩为Project override或其他消费面；后继账号安全UI另卡推进。完整D09/D26/D27及D08–D28/E01未完成，E01未开始；Summary待决、Object/tools原停止、Artifact/Project与生产未绑定及ready503边界保持。

## 5. 离线复核与交付范围

在仓库根实际执行并通过：

```sh
env PYTHONDONTWRITEBYTECODE=1 GIT_NO_LAZY_FETCH=1 python3 docs/development/agent-team/system-model-selection-ui-verification-evidence/verify_archive.py
```

[脚本](system-model-selection-ui-verification-evidence/verify_archive.py)核1235原件/445对象、固定870ebbb的26路径、bc17167的1041依赖、后端/迁移1–19未变、31dist原指纹、版本差量与八轮原wait/双清记录；只读本地Git和归档，不执行原命令、产品或资源，也不使用活动工作树证明已验输入。本轮另检查新增文档/脚本及限定入口的链接、UTF-8/LF/尾换行/空白；原日志与diff原字节不做格式重写。

本次唯一写入为本报告、同名evidence目录、[任务台账](tasks.md)页首、[恢复接续§15](recovery-2026-10-06-continuation.md#15-system-平台模型用途-ui-完整卡接受)及Selection卡接受页首。旧档与接续§1–14保持，未改产品、frontend README、账号安全卡、AGENTS或指南。档案交主线程核对后单独提交。
