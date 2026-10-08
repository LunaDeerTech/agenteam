# Project Owner UI 重建 edit01 失败与退役证据

2026-10-08。作者获单独授权执行的 `new-edit/edit01` 实际 **FAIL**：browser-v3 在 `project-owner.spec.ts:627` 等待精确标签定位耗尽既定 45 秒。Go 顶层 `TestAccountProjectOwnerWebEditAndRename` 为 FAIL 52.78 秒，直接命令 exit 1 / 96.558 秒；该轮编辑流程未完整通过。本档保留原始失败，不将此前已完成的操作写成完整编辑验收。

独立审查已正式 STOP：原件完整性及本轮 owned 退役复核 **PASS**，名称定位器存在 harness 必修项。本文档归档操作未启动业务、Go、browser、PG、网络或 schema/client 验证命令。历史 [read01 失败](project-owner-ui-recovered-read01-verification.md)、[read02 限定成功](project-owner-ui-recovered-read02-verification.md)、[UI 受控验收](project-owner-workspace-ui-controlled-verification.md)及 [API/return 验收](project-owner-ui-client-verification.md)继续保持各自范围。

## 固定输入与实际失败

Go04/browser-v3 四个 harness 源由 Git `be34ff9916d3e2354b4edd33df08c99021b02c2c` 定位，19 个前端源复用 `088f4d3490db4d86781090f0602299901c5f3247`；本档不读取后继活动 selector 修订。共同闭包沿用 read02 已核的 955 项 Git 输入，不重新生成大图或复制源码、依赖与资产。edit01 的 frozen-input 相对 read02 仅 `permitted_groups`、`root_authorization`、`status` 三个顶层字段变化，分组仅 `new-edit`。

| 输入 / 原件 | 固定 SHA-256 或结果 |
| --- | --- |
| [作者 handoff](project-owner-ui-recovered-edit01-verification-evidence/originals/author/edit01-handoff.json) | `b82defa1f9b8b051d5d4b4e646da7089754d422e545d838abdc96fbbdacf7238` |
| [作者说明](project-owner-ui-recovered-edit01-verification-evidence/originals/author/edit01-handoff.md) | `e0e445bbdf09597a0a808fd4281fafcffa5af9f179eac0668fe77f11c654860e` |
| [本轮授权与冻结输入](project-owner-ui-recovered-edit01-verification-evidence/originals/run/frozen-input.json) | `d7c694849a176880ac90e19cfb0f19a0392416d644402f8fa8dab8c7adbe4fb9` |
| browser-v3 freeze | `55f8ebe064a27558bbf3c822c825d4b35cc4fa5433dd3a71258343da5f97c3d8` |
| 原 driver-v02，复用已提交原件 | `bb666f55bf39f6454a370f62459d3e72416d90767f14a75d2e99775b8c11b38e` |
| [原始 raw.log](project-owner-ui-recovered-edit01-verification-evidence/originals/run/raw.log) | `ff96885c679f00369e6b31e18161717ed3d65330bff24e90cd4803a9772860fc` |
| [原始 result.json](project-owner-ui-recovered-edit01-verification-evidence/originals/run/result.json) | `2ba6607faccec1e9facbe5b8baee29613604b6791e81247a5b18d23cf2c44a27`；`accepted=false` |

[command.json](project-owner-ui-recovered-edit01-verification-evidence/originals/run/command.json)记录实际命令 `sh scripts/test-objects.sh -run '^(TestAccountProjectOwnerWebEditAndRename)$'`、开始/结束时刻与 direct wait。外层 exec session `83644` 实际 exit 1，完成 chunk `85b2ea`；[launch 原件](project-owner-ui-recovered-edit01-verification-evidence/originals/author/edit01-launch.raw)和作者 handoff 保留其终局。browser case 45 秒、top watchdog 120 秒、fixture package 6 分钟和 TCP 尾部观察 75 秒预算均未延长。其他 package 的 `[no tests to run]` 不计为新增业务测试通过。

raw 保留的失败是 `getByLabel('项目名称', { exact: true }).fill('owner-duplicate')` 等待超时。此前“取消修改”点击及第 626 行“项目描述为空”的断言已完成；第 628 行保存及其后的名称冲突、版本冲突、显式重读/采用、rename canonical 导航、confirmed current-read failure 和最终 same-body 校验均未到达。原轮未保存 DOM 快照，因此不能用源码或后续最小实验冒充当时 DOM。

## 独立归因与两次最小实验

[正式独审报告](project-owner-ui-recovered-edit01-verification-evidence/originals/independent/review.md) SHA `b6a32620298a78476ab464b920642cb018a91da625424e29703e46ca13e2d111`、[evidence.json](project-owner-ui-recovered-edit01-verification-evidence/originals/independent/evidence.json) SHA `8cf9387f1566fc32127e1facb9b7aef5f50ac21d05c64d5419c38f49bc29650b` 及 [STOP manifest](project-owner-ui-recovered-edit01-verification-evidence/originals/independent/manifest.json)绑定该轮原件审查和两次独立最小实验。归档仅复制这些已停止的小原件，没有再次运行实验。

固定 `ProjectGeneralSettings` 的名称字段为 required，`UiField` 的 label 因而含 `aria-hidden="true"` 星号，标签文本为“项目名称 *”。Playwright 1.56.1 的精确 label 引擎匹配完整标签文字；role 引擎按无障碍名称排除该星号。名称与描述处于同一 `editor.ready` form，固定取消路径会复制当前字段并设置 `editor.ready=true`，未见仅隐藏名称字段的分支。原失败日志也没有已解析元素后的 readonly/actionability 阻塞记录；现有证据不能把它诊断为已证明的产品取消状态缺陷。

`label-semantics01` 因独审者选择的 TMPDIR 过长，Chromium `SingletonSocket` 启动失败，未执行 DOM/locator。其 [result](project-owner-ui-recovered-edit01-verification-evidence/originals/independent/label-semantics01/result.json) 和 [retirement](project-owner-ui-recovered-edit01-verification-evidence/originals/independent/label-semantics01/retirement.json)保留 FAIL：direct exit 1、4 个 adopted wait 0、owned 两扫为空在 0.627 秒完成，但仍有一个空临时目录。[另次精确空目录清理](project-owner-ui-recovered-edit01-verification-evidence/originals/independent/label-semantics01/late-empty-directory-cleanup.json)发生于后续时点，明确 `late_cleanup_not_within_original_15s=true`；不将其回填为首次 15 秒内完整清理成功。

root 单独授权的 `label-semantics02` 仅将 TMPDIR 缩短，[共同 experiment.cjs 原件](project-owner-ui-recovered-edit01-verification-evidence/originals/independent/label-semantics01/experiment.cjs) SHA `43063778e5b5bdbd0c5f8b68d90954033fdc18bc23f285bc1356c79e4bf481d4` 不变。实验只对 `about:blank/setContent` 合成标签执行控制，0 页面请求，未加载 Vue、产品资产或业务 fixture；[实际结果](project-owner-ui-recovered-edit01-verification-evidence/originals/independent/label-semantics02/result.json)为 PASS：

| 合成标签结构 | exact label“项目名称” | exact textbox role/name“项目名称” | 描述 exact label |
| --- | --- | --- | --- |
| 原 required + aria-hidden 星号 | 0；短 fill 预期超时 | 1；fill 成功 | 1 |
| 星号可见于无障碍树 | 0 | 0；带星号名称匹配 1 | 1 |
| 移除星号 | 1 | 1 | 1 |

[第二次 retirement](project-owner-ui-recovered-edit01-verification-evidence/originals/independent/label-semantics02/retirement.json)记录 exit 0 / 1.483 秒（含清理）、browser.close、direct 与 4 个 adopted actual wait、owned 双空及 runtime 空。环境缺少 Playwright 技能，root 已明确允许独审使用固定工具；报告没有声称读过该技能。

该实验确认定位器语义问题，不补回 edit01 当时 DOM，也不排除当时全部状态因素。最小必修范围为 browser-v3 第 627/632/658/1117/1119/1131/1153 行共七处名称定位，统一按 textbox 精确无障碍名称定位；描述定位及共享产品 `UiField` 无需因此修改。后继修订按文末独立输入记录，真实复跑不属于本轮结果。

## 原始 HTTP 证据边界

40 份作者运行原件共 338584 字节，逐项与 handoff 的 bytes/SHA 一致。其中 10 个 safe response sidecar 绑定 6 份去重后的原始 body，均保留原 bytes；8 个 GET 200、2 个 PATCH 200。001–004 为 fixture 准备 GET，后续记录继续按原 endpoint/method/request ID 保留；不能将全部 10 个响应计为独立 browser 读取矩阵。

005–006 为 browser 初读；007 PATCH/008 GET 为 version 2，保留描述的空格和换行；009 PATCH/010 GET 为 version 3 的空描述。每对 PATCH/GET 复用相同 body SHA。最后 010 为项目 `01a11a03-4428-7bd2-97cb-ad4982f9be4b` 的 GET 200，request ID `01a11a03-5506-7c96-8085-bf8efb4d92ea`，body SHA `d46945e16ad0c01090ad4b70ede99593fc8c47e59efb44a420683fe2a34c4216`。所有 sidecar 的 input hash 均为本轮授权 SHA，source run 均为 `TestAccountProjectOwnerWebEditAndRename`。失败前的顺序断言及原 body 支持描述保存、version +1、保存后 disabled/事实不变、清空描述、dirty 导航继续编辑与取消描述复原这些已到步骤，仍不构成整轮编辑 PASS。

本轮没有 `schema-validation.json` 或 `same-body-validation.json`，收尾 schema/public client 矩阵未执行；截图为 0。保留的响应、JSON 语法与 SHA 核对不构成 schema/client PASS、完整编辑验收或视觉验收。

## 实际退役与资产恢复

直接子进程 PID `130520` / starttime `762993` 已 actual wait；[4 个 adopted wait 原件](project-owner-ui-recovered-edit01-verification-evidence/originals/run/adopted-waits.json)为 PID `134836`、`134838`、`134841`、`134842`，starttime 均 `766094`，实际 wait/exit 0。watchdog complete 且线程 joined；raw 保留 Node direct wait、proxy Serve/body handlers、preparation service 与 root join 的记录。

[observed-resources](project-owner-ui-recovered-edit01-verification-evidence/originals/run/observed-resources.json)保留 D03/D04/D05 nonce、7 个完整资源 ID 及 mount 事实；[cleanup 两次观察](project-owner-ui-recovered-edit01-verification-evidence/originals/run/cleanup.json)逐 ID absent，owned process、两处 runtime entries 和新增剩余资源均为空，既有 Docker baseline 不变。monitor error、cancellation、forced tail action 均为 0，输入前后相同。

补充 host TCP/tcp6 delta 在原 75 秒观察预算内，经 40.36585689399999 秒取得两次空结果；[原始 TCP 尾部记录](project-owner-ui-recovered-edit01-verification-evidence/originals/run/tcp-tail-observation.json)逐字节保留。该 host polling 仅为补充观察，不证明所有瞬时短连接或进程所有权。

四个新增 PID 1 `containerd-shim` zombie 独立列示：`130746/start763228`、`131099/start763459`、`131465/start763633`、`131933/start763807`。它们由 daemon 所有，非本 driver 可 join 子进程，未发送 signal；owned 退役不表示整机进程清零。

root 在原 reader 退役后恢复原 3 个全局 dist 文件；[restore-after-edit01 原件](project-owner-ui-recovered-edit01-verification-evidence/originals/root/restore-after-edit01.json) SHA `111c0cf88c0d06af9e9fa279a4df5fd7554ccee2be4eac719d6da565e8a5a9af` 记录逐文件 bytes/SHA 和 `original_restored_exactly=true`。53 个测试资产保留于 `/workspace/scratch/owner-ui-assets/test-dist-ui-v1`。本档只归档该时点记录，不混入后继资产窗口。

## 归档完整性与未验范围

[source-map](project-owner-ui-recovered-edit01-verification-evidence/source-map.json)记录 92 个逻辑原件引用，其中 67 个属于原失败与独审、25 个为文末后继窄修；对应新增 68 个物理原件（400778 字节）和复用 13 个已提交物理原件，共同脚本与 inputs 亦按 SHA 去重。43 项固定 Git 引用区分旧四 harness/19 前端源与后继单源修正，记录必要既有契约、组件及锁文件。闭包绑定复用 read02 已提交 source-map，未重新扫描 955 源图；无产品源码实体、全树索引、依赖、dist、私有 material、key、Cookie 或密码副本。

[format-exceptions](project-owner-ui-recovered-edit01-verification-evidence/format-exceptions.json)精确登记 18 个原件格式例外：10 个 sidecar 和 6 个 body 缺 EOF，edit01 raw.log 第 32/34/36/37/39/43/44/53/54/56/57 行原始 trailing whitespace，以及后继 v4-list01 raw.log 第 25 行原 EOF 空行；无 CRLF 或独立 CR。全部保持原字节。新增派生 Markdown/JSON 使用 UTF-8/LF/单个 EOF；[归档检查](project-owner-ui-recovered-edit01-verification-evidence/archive-checks.json)仅验证原件、链接、Git 引用、JSON 语法和格式，不改写原失败。

本轮 FAIL 继续保留。其余三个新分组、旧 16 组、独立真实 A/B、视觉、recovery 三态、完整 D27 均未因此接受；私有 Skills prepared 及辅助生命周期事实不能代表真实生命周期执行。生产未绑定、ready503、D08–D28/E01 和既有三停止边界保持。

## 已交付的定位器窄修：独立后继输入

root 随后已提交并核远端相同的 `f09f311256a88dd535a4315b65e378d3b3682465` 仅修正 #21 名称定位：新增 `projectName(page)`，使用 `getByRole("textbox", { name: "项目名称", exact: true })` 替换七处查询（edit 3、layouts 4）。原 fill、值、focus、焦点样式和 readonly 断言均保留，31 处描述查询保持；#20、19 个 UI 源、Go04、API 与锁文件不变。

此后继源码只由 Git `f09f3112` 定位，SHA `0c498b269c1b52d76ef18f2e249eba3714d643b1aa4dbd353f06e60953444cf0`；[v4 freeze](project-owner-ui-recovered-edit01-verification-evidence/originals/followup-v4/author/browser-v4/freeze.json) SHA `9688d909627f3e8253c68e989ebecd7cac8b6f16d84b9196b9297ba854f006d9`，[原 delta](project-owner-ui-recovered-edit01-verification-evidence/originals/followup-v4/author/browser-v4/delta.patch) SHA `a38a421b04f7b0100287ec805ecfe4e56536d0c580aa85b3e724a626eed40f23` 与 Git be34 → f09f 差量完全一致。它们未替换 edit01 的原 browser-v3/frozen-input。13 项显式输入中只有 #21 改变，共同 imports/tools 原件继续复用。

[独立 v4 验收报告](project-owner-ui-recovered-edit01-verification-evidence/originals/followup-v4/independent/review.md) SHA `294311ed90f015ba5e84a711d15fec344b9f7c47604cd8baa90e0368cfc8c897`，与其 [evidence](project-owner-ui-recovered-edit01-verification-evidence/originals/followup-v4/independent/evidence.json)、[STOP manifest](project-owner-ui-recovered-edit01-verification-evidence/originals/followup-v4/independent/manifest.json)均保留原 bytes。作者 [checks](project-owner-ui-recovered-edit01-verification-evidence/originals/followup-v4/author/browser-v4/checks.json)所列实际离线结果如下，各命令有 direct actual wait、相同的输入前后指纹及两次 owned 空扫描：

| 已完成检查 | 实际结果 | 时间 |
| --- | --- | --- |
| format-check | exit 0 | 0.602 秒 |
| strict TS / checkJs | exit 0 | 1.499 秒 |
| 五个 mode 各列举 1 项 | exit 0 | 4.284 秒 |

三组原 command/result/raw 及输入绑定均归档。`--list` 只列举测试；其结果不代表业务执行。独审复用已完成的 label-semantics02，没有再启动浏览器实验或业务资源。接受结论仅关闭七处名称定位器必修项；原 edit01 FAIL、label-semantics01 启动 FAIL 与晚清理原件继续保留，修正后的真实 new-edit、layouts 和完整 D27 仍待各自验证。
