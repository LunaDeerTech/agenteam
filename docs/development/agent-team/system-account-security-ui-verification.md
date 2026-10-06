# System 账号安全 UI 验证与接受报告

2026-10-06，[账号安全卡 rev2](../work-items/d27-system-account-security-ui.md)的 27 路径完整结果已获独立最终组合 PASS、主线程采纳，提交推送 `40c904c0dd88420fc621f40c0a737d243d514ec1`，主线程核远端一致。本报告绑定该固定 Git，保留各轮原退出、失败和证据限制；不是完整 D27 或 SMTP 交付报告。

## 1. 接受行为与输入

管理员可在“平台配置／账号安全”读取当前设置，内联编辑 Session idle/absolute、密码重置期限及 challenge 阈值。六叶子／三组菜单和十个精确 return 目标接入既有权限规则。当前 GET、四项草稿及 PUT 历史 Settings 确认分别保存；没有 lookup API。未确认写只能由用户明确使用原 key/body/expected version 与合法 CSRF 重放，Session/GET 不作为写回执，已确认后的 GET 失败也不抹除确认。409 保留草稿，明确采用最新值才更换基线；期限变化影响后续签发，不追改既有 Session。

同身份 checking/恢复期间保留 App 期状态及待决确认，确认宿主与业务页面共存；身份或当前管理员权限变化按本域清理。第七域实际 I/O 尾部完成后才释放 Cookie owner。共享 Dialog 最新页面 fallback 和 Selection 取消读终态分别沿已接受结果接入，真实两页组合补齐业务焦点与导航验证。

最终 `input06/manifest.json` SHA256 为 `baeaeb37b348167d8407831314e83adb60615dfb7882f248506c7a314c0e4954`：26 源、33 dist 指纹、`debbb28deb7c883fd0b6b77a75354b9b5d7ece0b` 的 1051 固定基线文件。24 源与 input04 相同；两份 Selection 测试仅在已接受修复上重放原五处菜单／分组适配，保留取消尾部、错误终态、显式重读及零自动写断言。基线更新为已接受 controller/state，另纳入固定 styles 文档；driver 仅变更基线／该文档门禁／两个精确 Navigation 选择器。最后 README 单独核验后成为第 27 路径，SHA256 `1e5ceb67c81dfcf41edf3b4118c8f5005a6c5d43402c54b2da8ab3a62f85c1ba`，见[最终 27 路径清单](system-account-security-ui-verification-evidence/objects/c0a497bbf9d9b1c86d3f0f64f2355c00060948b0d22191a3dbbbda9e8afb24c4)。

正式上游直接复用 [Dialog fallback](dialog-fallback-focus-verification.md)（产品 `fd32120`，归档 `77087c01`）及 [Selection 取消读取恢复](model-selection-cancelled-read-recovery-verification.md)（产品 `debbb28`，归档 `0705020`）。来源以固定 Git 和归档对象重建；不复制依赖、dist 实体、缓存或二进制，也不读取后继 SMTP 活动源码。

## 2. 分阶段结果与原失败

[作者原报告](system-account-security-ui-verification-evidence/objects/350ce9ce7a3a04803ddd81df8e7751ca6ed3bca87f6c9fe9d4dc5ef9abb401fd)、[作者 input06 组合增补](system-account-security-ui-verification-evidence/objects/0941ae0e13fd556d220e719b0fb6da3159630aef8a57eeefb613d529b0d1b7b0)、[独立 input04 本域报告](system-account-security-ui-verification-evidence/objects/6120ebb1032d2f2a6ebbeb6696aa2285f2cbf87fbc537f9faf9d3b8f1e141680)及[独立最终组合增补](system-account-security-ui-verification-evidence/objects/3edbe3a75dfb85defce11446988b653e0f04813f401fd0768cb104193915882a)保持各自当时状态；后两份独立报告分别记录本域接受和最终组合接受，没有覆写旧“整卡待验”。

| 输入／阶段 | 实际结果与复用边界 |
| --- | --- |
| API／owner／页面固定阶段 | 独立 API 14、owner 9、controller 1＋App 4 通过；这些阶段没有产品 RED。只读 FileNotFound 准备错误按原说明保留，不补造 raw。 |
| input01／new01 | Lifecycle、Concurrency、OutcomeRecovery、AuthorityAndIdentity 四组通过；Navigation 关闭确认后的焦点断言失败。原轮未记录 active BODY 或 captured trigger，不能后来追填。 |
| input02／input03 | input02 是未执行的诊断草稿；input03 的 new02 实际记录 BODY、overlay=0、checking=false，仍未直接采样原 captured trigger。共享 fallback 另卡接受后才返修业务页面。 |
| fallback 页面红绿／input04 | 原 fallback-page-red01 为 7 通过、2 项页面焦点产品红；green01 为 9/9。完整 check 为 32 文件／1104 测试加 format/type/build 通过；new03 Navigation 通过。独立 app01 的 Vue VM Object.is 代理身份前提红不属产品失败，改用真实 uid 后 app02 两例通过。 |
| input04 旧回归 | core 7、Provider 3、Model 3 通过；Selection Outcome／Authority 通过，Navigation 保存按钮禁用超时。原现场没有 refs/busy/seq 分项或截图／trace，不能给出唯一已证原因。 |
| input05 诊断与撤回 | oldselection02 仅 Navigation 在引用条件齐全时通过；它不能抹除 oldselection01。input05 撤回至 input04 的原门禁保留，之后另案 Selection 缺陷诊断、修复及接受独立记录。 |
| input06 最终组合 | 32 文件／1115 测试及 format/type/build 通过，类型及两项浏览器发现通过。Go discovery 首次因私有 TMPDIR 不存在在编译前 exit1，仅创建目录后同 argv/cwd/env exit0；不是产品红。两页 Navigation 真实通过，原已通过域按明确差量复用。 |

最终新五组由 new01 四组与 input06 Account Navigation 组合；所需旧十六组由 core 7、Provider 3、Model 3、oldselection01 两组及 input06 Selection Navigation 组合。不是单次最终全矩阵重跑。Selection 独立屏障曾复现的是其自身取消读终态缺口，诊断绿不等于修复 PASS，也不能反推旧 Account timeout 的唯一原因。

## 3. 十轮真实执行与终局

以下均有原 `command/raw/result/cleanup`、实际 wait 及固定输入，详见[逻辑原件索引](system-account-security-ui-verification-evidence/index.json)的 `runs`。每轮分别核七个 exact-ID 双 absent、所属 PID/starttime 双扫空、实际 adopted wait、monitor0、自有 runtime 清空及原资源基线不变；历史 PPID1 Z 未触及、不计已回收。

| 实际轮次 | exit／秒 | 顶层结果 | 所属 PID／adopted wait |
| --- | --- | --- | --- |
| author new01 | 1／133.704 | 新四组 PASS；Navigation FAIL | 178／20 |
| author new02 | 1／55.758 | Navigation 诊断 FAIL | 82／4 |
| author new03 | 0／63.020 | Account Navigation PASS | 78／4 |
| author oldcore01 | 0／124.770 | 旧七组 PASS | 159／28 |
| author oldproviders01 | 0／89.416 | 旧三组 PASS | 113／12 |
| author oldmodels01 | 0／135.005 | 旧三组 PASS | 107／12 |
| author oldselection01 | 1／150.434 | Outcome／Authority PASS；Navigation FAIL | 109／12 |
| author oldselection02 | 0／71.509 | 仅 Navigation 诊断 PASS | 80／4 |
| independent independent01 | 0／160.685 | Replay／Conflict 首轮两组 PASS | 207／8 |
| author combination-navigation06 | 0／81.887 | Account／Selection Navigation 两组 PASS | 95／8 |

最终组合顶层分别 17.12s／16.49s，浏览器分别 13.9s／12.0s；45s 浏览器、2 分钟顶层、6 分钟每包、race/count1、worker1/retries0 保持。26 源／33 dist／1051 基线及 driver 前后相同，窗口已释放。完整 check 的 build 正常重建 dist，不把该检查写成 dist 前后不变。

最终组合 raw SHA256 `4eee875e4e6d105f8a17d8cb2a6c1e36ad7b540ba75f1f7912768d1f868573a6`，command `fc3d91807290410d1044718a6981590efc75a6d7ec367e4483a5c895e4e3a0b2`，result `f06348bfa7c3b9f2cc3ecc4eb8ebfc1cc958279db16b0ef1b15f7a5cd081a5a0`，cleanup `25462fd943232c7ecd31b5eb2550aa28a790b328a0b7129efb68716ff3f112bd`。原执行脚本仅保存，不在归档过程中执行。

## 4. 独立代表与最终复用

Replay 首轮顶层 8.07s／浏览器 5.1s：已接受 PUT 的响应截断，另一正式 Session 推进当前设置；显式原 key/body/非空合法 CSRF 重放返回历史 Settings，当前不回退。确认后 GET503 只触发只读恢复。浏览器及最终 Go 核 canonical identity、recordID→Audit、单 command／Audit、完整 metadata 和两尝试／一次已接受重放。

Conflict 首轮顶层 9.52s／浏览器 6.7s：正式 409 保留四草稿及旧版本，GET 不自动采用；单确认跨 Session503→新序号 200 EOF 同身份恢复，继续编辑后标题焦点与原生 Tab 通过。明确采用才换草稿；旧 Session 政策不变，正式新 Login 采用新 idle/absolute。期限证据来自正式新旧记录，不是睡眠至过期。

这两组固定 input04。最终独立增补逐核 input06 的 24 不变源、两测试差量、既有 Selection 独立三纯／一真实结果及本次两页真实原件，接受组合；没有把旧 dist 宣称为新 dist 下重跑 A/B。安全布尔／计数 native 观察器不替代纯 API/owner 的实际 I/O 尾部屏障。

## 5. 图片、缺件与适用限制

归档 32 张图：new03 Account 八图、oldselection02 诊断八图，以及最终两页各 light/dark × 1440/1024/834/390 共十六图。[本轮视觉检查记录](system-account-security-ui-verification-evidence/objects/48954d87e9adb21a1e83045d30cb16eb3995dfd1c084c5a5c897fbc305545828)记载作者实际逐张检查，主线程读取结果报告；独立增补及本归档不新增主线程或独立图审声明。窄屏下方字段／用途需主区域滚动，视口截图不证明全部内容同时可见。独立 A/B 未保存 screenshot/trace/video。

唯一已声明的历史浏览器前态缺件：`author/runs/harness-go01/input.json` 中 `tests/account-captcha-web/e2e/system-account-security.spec.ts` 的 `72e3a507ef49871c3b0170942ac43788df52414cf884b4b351965e7bc1840d37` 没有精确副本。该轮是未消费浏览器源的 Go 编译，原 fingerprint/command/raw/actual exit0 仍在；不重复查找、反推重建或声称全部历史版本可重建。最终真实输入及失败源完整，不把这项历史非消费前态当成最终验证缺口。早期只记录的 PATH／env overrides 不扩写为完整环境快照。

本卡不实现 SMTP 配置／发送、Session 撤销、新后端或迁移，也不证明 BFCache、原生缩放、其他浏览器引擎、生产 SPA／Vite 代理浏览器、Runtime 或外域接入。完整 D09/D26/D27 及 D08–D28/E01 未完成，E01 未开始；Summary 待决，Object/tools 原停止、Artifact/Project 与生产未绑定、ready503 等边界保持。

## 6. 最小档案与离线复核

[证据说明](system-account-security-ui-verification-evidence/README.md)与索引保存 559 个逻辑原件、388 个唯一 SHA 对象，其中 353 个新对象（8,434,718 字节）及 35 个永久档案复用对象；64 项原检查包含上述十轮真实执行。原 raw/diff 按字节保存，逻辑路径及 origin 明确对应；原报告内的私有相对路径不改写，按索引取件。

离线脚本核全部对象／复用索引、固定 `40c904c` 的 27 交付路径、各历史基线和最终 1051 文件、六版输入、原退出／实际 wait／双清及卡技术 SHA256 `35c2f7f00917213642d9897ae37bc9509728f9921f9f71f674b59505bf06a146`。它不访问私有 scratch、不启动服务／浏览器、不执行产品或原件脚本。

```sh
env PYTHONDONTWRITEBYTECODE=1 GIT_NO_LAZY_FETCH=1 python3 docs/development/agent-team/system-account-security-ui-verification-evidence/verify_archive.py
```

本次实际离线结果：PASS，559 逻辑原件／353 新对象＋35 复用对象／27 Git 路径／1051 最终基线／64 原检查含十轮真实终局；一个已声明历史前态缺件保留。归档接受依据为已执行证据，不新增动态验证。
