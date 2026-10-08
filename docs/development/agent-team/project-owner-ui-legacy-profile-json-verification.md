# Owner 工作区 legacy profile 正文取证窄修验收

限定接受额外单路径 `tests/account-captcha-web/e2e/personal-settings.spec.ts` 的 STATIC 修正，以及作者固定 v4 显式 strict 类型检查和 owned 退出记录的独立原件复核。源码已在 `80ec2ab378c7420d65240bf9c1b8a48e871e5acd` 交付；这不是新的真实 profile/browser PASS，也不接受 final05 准备、old14、独立实际 A/B、README #22 或完整 D27。

[独立正式报告](project-owner-ui-legacy-profile-json-verification-evidence/originals/independent/review.md)、[固定 evidence](project-owner-ui-legacy-profile-json-verification-evidence/originals/independent/evidence.json)与 [STOP manifest](project-owner-ui-legacy-profile-json-verification-evidence/originals/independent/manifest.json)按原 bytes 保存。[原 old16 失败档](project-owner-ui-recovered-old16batch01-verification.md)中的 `oldprofile01` CDP `duplicate.json()` FAIL 保持；该轮虽已收到 status400，却缺少原 body、当时 DOM 和 trace，field_errors、UI、当前读取、头像及最终 facts 不能由本次 STATIC 或类型检查补成实际通过。

[来源映射](project-owner-ui-legacy-profile-json-verification-evidence/source-map.json)将 baseline 绑定 Git `367156d89773660c4a671a4b73d5ea7a16e24f50`／SHA `155bba1a7bf85ac5020c97b68dd45ade920b0e5016e6dd1f97d7a0202b1699ba`，final 绑定 Git80ec／SHA `a0fede03ce80118de78a9ba033540bca25587288927791f2daa761c48ea30603`。另固定 Git367 的 `web/tsconfig.json`／SHA `bead3f9d04e58ab0862ca71237283c24cb2723e7afc849e9d925572159b54a18` 作为原类型 lib 的依据；共三项 Git/blob/SHA 引用，不复制或重扫 955 源图、全部 legacy JS、工具或依赖。

Git baseline→final 的变化恰好两个 hunk、4 行插入和 5 行删除：`SettingsJSONTarget` 闭集加入 `{ path: "/api/v1/me", method: "PATCH", status: 400 }`；重复用户名提交改用既有 `settingsJSONFor` 并从观察结果读取 `duplicate.field_errors`。[作者原 delta](project-owner-ui-legacy-profile-json-verification-evidence/originals/author/v1/delta.patch)原样保留。400、`/username`／`ALREADY_EXISTS`、aria-invalid、当前 GET、取消/清空显示名、全部头像后段和最终 facts、预算与重试均未削减。

独审以精确两次替换重建候选，其余 baseline 字节相同。`function installSettingsJSONObservation` 到 `let observationSequence = 0` 之前的既有 helper 共 **4,694 字节**，两版本相等，SHA 为 `08ef788bb6814e5a52e5103d2ae1080406a0f1cf3108ec31e2804a34c8d4b4b4`。保留原 native fetch promise/Response，不新增请求或重试、不等待 observer 后才交产品。观察器匹配同源精确 path/method、要求恰好一次，实际响应与 clone 的 status 都须为400，仍验证 JSON 对象和原 field_errors，失败观察仍使断言失败。

helper 只 clone 有界错误 JSON，保留 fatal UTF-8、600000-byte 上限，以及 finally 恢复 fetch并 join clone read/cancel/release。clone 会 tee 正文，此有限取证机制不能证明未受观察的 stream/owner tail，也不证明后继页面最终状态；这些语义来自固定代码独审，本次没有重新运行 helper 或浏览器。

| 冻结版本 | 固定实际动作与原结果 | 原耗时秒 | direct PID／starttime | 输入记录与停止边界 |
| --- | --- | --- | --- | --- |
| [v1](project-owner-ui-legacy-profile-json-verification-evidence/originals/author/v1/freeze.json) | 单文件 Prettier `--check`，exit1，FAIL | 0.611240405 | 277612／1466027 | 24 输入前后同；类型检查未运行 |
| [v2](project-owner-ui-legacy-profile-json-verification-evidence/originals/author/v2/freeze.json) | 单文件 Prettier `--write`，exit0；保存越界旧格式改动并恢复 | 0.618533796 | 279358／1482710 | inputs_same=false，唯一变化为目标文件；后续 format-check02/type 未运行 |
| [v3](project-owner-ui-legacy-profile-json-verification-evidence/originals/author/v3/freeze.json) | 显式 personal spec strict 类型命令，exit2，TS2488 | 1.218863914 | 281656／1501182 | 24 输入前后同，候选源码不变；未再格式化 |
| [v4](project-owner-ui-legacy-profile-json-verification-evidence/originals/author/v4/freeze.json) | 仅补 DOM.Iterable 的同一类型命令，exit0，PASS | 1.473605731 | 283343／1517411 | 24 输入前后同，无诊断，源码不变 |

四 freeze 的 artifacts 合计 **41 份／213,288 字节**：v1 为 13／95,587，v2 为 12／90,156，v3 为 8／14,260，v4 为 8／13,285；均逐 SHA/bytes 核合。四份 freeze 自身与独审三原件另计，原状态和旧失败没有改写。v4 freeze SHA `76058b575eb4d8efb3233b450b7b2b49530f1f9b9f7e9ce115e134ae0648fc20`，其原 raw 是实际 **0 字节**、不是缺失文件。

v1 的[原格式错误](project-owner-ui-legacy-profile-json-verification-evidence/originals/author/v1/format-check01/raw.log)没有被替换为 PASS。v2 formatter 只将旧 helper 的单行 if/throw 拆为两行；[formatter 差量](project-owner-ui-legacy-profile-json-verification-evidence/originals/author/v2/formatter.delta.patch)与[原 formatter 输出 snapshot](project-owner-ui-legacy-profile-json-verification-evidence/originals/author/v2/personal-settings.formatter-output.ts)均保留。精确位置为 **baseline171／final172**；早期“169”是 unified diff 的 hunk 起始行，不是该语句行号。

formatter 输出 SHA 为 `dab4013cb76c4514f231df0d7b1334e4670a36893b334ccf3f65e34f708a8081`。作者随后恢复为原候选 a0fede，v2 restored 与[最终 snapshot](project-owner-ui-legacy-profile-json-verification-evidence/originals/author/v1/personal-settings.after.ts)逐字节相同；来源映射保存两者的原独立路径并指向同一实体。root 已明确接受继承的旧格式例外，v3/v4 没有再运行 Prettier，也没有新的全文件 Prettier PASS。formatter 的 exit0 只表示当次写格式命令成功，不是恢复后候选的格式接受。

v3 的[原 TS 诊断](project-owner-ui-legacy-profile-json-verification-evidence/originals/author/v3/types01/raw.log)为当前候选 385、1038 行旧 NodeList spread 的 TS2488，相关源行已存在于 baseline。v4 相对 v3 [原命令](project-owner-ui-legacy-profile-json-verification-evidence/originals/author/v3/types01/command.json)的唯一 argv 差量是 `--lib ES2022,DOM` 改为 `--lib ES2022,DOM,DOM.Iterable`，与固定 web tsconfig 一致；显式目标 spec、`--strict`、`--noEmit`、Bundler、原 `--allowJs --checkJs --skipLibCheck` 等参数均保留，没有降低类型要求或改源码来绕过诊断。

v4 的[实际命令](project-owner-ui-legacy-profile-json-verification-evidence/originals/author/v4/types01/command.json)与[原结果](project-owner-ui-legacy-profile-json-verification-evidence/originals/author/v4/types01/result.json)记录 exit0／1.473605731 秒。四次原离线命令均完成实际 direct wait；每次 adopted=[]、记录的两次 owned 扫描均空、没有 timeout。[固定监督器](project-owner-ui-legacy-profile-json-verification-evidence/originals/author/v1/run-offline.py)中的 process.wait／watcher.join 与记录由独立负责人核对。本归档只保存并核对这些历史原件，没有运行监督器、类型命令或主机扫描，不扩大为全机清零结论。

八份 before/after manifest 各列 24 个离线输入。v1/v3/v4 的原 bytes 前后相同；v2 after 只把目标 spec 的 SHA 改为 formatter 输出，`inputs_same=false` 如实保留，恢复是其后的单独版本事实。现有 UI19／Go04／工具引用沿已接受永久档复用；不拿这24个离线输入代替完整构建图或 final05 的变更输入审查。原固定 browser-v5 工具 metadata 和 oldprofile01 独审只链接既有档，不复制宽工具闭包或重新读取安装目录。

本档保存明确授权的四个源码 snapshot 逻辑原件（base、after、formatter-output、restored），按 SHA 合并为三个实体；baseline/final 另与固定 Git blob 逐字节核合，中间 formatter 输出保留为原未提交状态。除此之外没有复制产品源树；后续 final05/old14 准备或当前资产窗口没有作为本档输入读取。

归档合计 **48 个逻辑原件／241,454 字节**，去重为 **41 个新实体／182,808 字节**，7 个重复逻辑引用通过来源映射保留；另外两项旧永久证据引用共 38,160 字节，按 `f8e548a701217280d89c825b67b46656fc74365d` 锁定并原位复用，不混入上述48原件计数。没有复制 cache、依赖、资产实体或私有材料。

[格式登记](project-owner-ui-legacy-profile-json-verification-evidence/format-exceptions.json)实核这些原件没有缺 EOF、trailing whitespace、CRLF 或单独 CR；空 raw 按0字节原件保留。登记另设继承的源码 Prettier 例外，区分它与归档 Git 空白诊断；没有将旧 formatter FAIL 隐去。[归档自查](project-owner-ui-legacy-profile-json-verification-evidence/archive-checks.json)记录原件/存档 SHA与bytes、三项 Git、命令差量、原 wait/input、JSON和链接的有界核验，本次未运行 type/format/schema-client/list/business/build 或任何资源。

本次关闭的是已识别的单测试路径取证修正。原 oldprofile01 真实失败与缺失原正文/UI/avatar final facts 保留，首次两旧 auth 成功的后继复用仍需最终单源差量及 config/fixtures/tools 不变的正式依据。final05、后续 old14、独立实际 A/B、README22、完整 D27 和生产/既有停止边界均未因本档解除。所有档案写入完成后 STOP。
