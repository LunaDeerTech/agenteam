# Project Owner UI 重建 edit02 失败与退役证据

2026-10-08。root 单独授权的 `new-edit/edit02` 已实际 **FAIL 并退役**：Go 顶层 `TestAccountProjectOwnerWebEditAndRename` 为 FAIL 13.04 秒，直接命令 exit 1 / 58.451 秒，外层 exec session `9115` 已实际返回 exit 1。旧协调文档的“准备、尚未开始”记录不代表本轮终局；本档以冻结的作者原件及独立审查为准。

独立审查已正式 STOP：54 份原件完整性及 owned 退役复核 **PASS**，本轮实际 FAIL 保留，必修仅为 #21 `ready()` 身份 facts 的定位范围。此次归档没有重跑业务、Go、browser、PG、网络、schema/client 或原有检查。历史 [edit01 失败与 v4 窄修](project-owner-ui-recovered-edit01-verification.md)、[read02 限定成功](project-owner-ui-recovered-read02-verification.md)、[read01 失败](project-owner-ui-recovered-read01-verification.md)及 [UI 受控验收](project-owner-workspace-ui-controlled-verification.md)保持各自输入和接受范围。

## 本轮固定输入

四个 Go/browser harness 源由 Git `f09f311256a88dd535a4315b65e378d3b3682465` 定位，其中 #21 为 browser-v4；19 个 UI 源复用 Git `088f4d3490db4d86781090f0602299901c5f3247`。本轮不采用任何后继活动 selector 修订。共同原件按 SHA 复用已提交档，final03 只在原闭包上覆盖已接受的 v4 单源，未重新扫描 955 源图或复制源码实体、资产、依赖。

| 固定原件 | SHA-256 |
| --- | --- |
| [作者 handoff](project-owner-ui-recovered-edit02-verification-evidence/originals/author/edit02-handoff.json) | `c150baaeeb4bde32cba5f90437bd2a4d5d34854e4b6bfb6e36dcd3e47ac7b74c` |
| [作者说明](project-owner-ui-recovered-edit02-verification-evidence/originals/author/edit02-handoff.md) | `637ddd404f16b633161c833c64e0f0bacc49d6f1d5c1b6ff19df4e736639419a` |
| final03 原准备输入 | `99d71e028ded80f7b7979f8ee9a32ef4fe88c1a1c21b8a415ae665faa2290232` |
| [实际 root 授权输入](project-owner-ui-recovered-edit02-verification-evidence/originals/run/frozen-input.json) | `0e4d117ae2d882299f21172237f4b09563d3dfd35055e197ed0df0909b21dbcf` |
| [final03 小 closure delta](project-owner-ui-recovered-edit02-verification-evidence/originals/input/final03/closure.delta.json) | `006f0a4f0c5f184c53350f27bcf6b943083f1f758954ec3d51059379cc260a5a` |
| browser-v4 freeze，复用已提交原件 | `9688d909627f3e8253c68e989ebecd7cac8b6f16d84b9196b9297ba854f006d9` |
| 原 driver，路径换代为 v03、字节不变 | `bb666f55bf39f6454a370f62459d3e72416d90767f14a75d2e99775b8c11b38e` |
| [实际 raw.log](project-owner-ui-recovered-edit02-verification-evidence/originals/run/raw.log) | `10f7e26ea47a00ce22aefdaa10f18147a407c0e0376d7f4bf768ba1355f50460` |
| [实际 result.json](project-owner-ui-recovered-edit02-verification-evidence/originals/run/result.json) | `1331b6afd61a3e0cad44ea3b9cfe5e357c3b17d920639e49c0b5f4674c68078f` |

final03 准备原件的 `root_authorized_resources=false` 保持不变；本轮 root true copy 仅改变 `root_authorization`、`root_authorized_resources`、`status`，两份输入均只选 `new-edit`。原准备态与后来的单次执行授权分列，不追改前者。

[command.json](project-owner-ui-recovered-edit02-verification-evidence/originals/run/command.json)保留实际命令 `sh scripts/test-objects.sh -run '^(TestAccountProjectOwnerWebEditAndRename)$'`、PID/starttime、环境和开始/结束时刻；[launch 原件](project-owner-ui-recovered-edit02-verification-evidence/originals/author/edit02-launch.raw)及 handoff 记录外层完成 chunk `1f9dae`。45 秒 browser case、120 秒 top、6 分钟 package、75 秒 TCP tail 预算未延长；本轮失败为 strict locator 歧义，不是 edit01 的名称定位超时。其他 package 的 `[no tests to run]` 不计为业务用例通过。

## 失败位置与已到界限

原调用栈为 browser-v4 `ready():249 → confirmed():272 → edit 正文:683`。失败断言 `expect(page.locator(".project-facts")).toContainText(id)` 触发 strict mode，raw 明确列出两个匹配 `dl`：一个含 `Project ID`/`Owner`，另一个含“当前名称 Owner.Renamed / 当前描述”。日志中的两个元素片段属于实际错误证据；没有另存完整 DOM snapshot 或截图。

[正式独审报告](project-owner-ui-recovered-edit02-verification-evidence/originals/independent/review.md) SHA `2a3dedd33b5754c6205784a6a059acdaf55dc2503944ff7052211d55a7348f3f`、[evidence](project-owner-ui-recovered-edit02-verification-evidence/originals/independent/evidence.json) SHA `498c13d5368200497a433d1f8615d3019ea22efc8fb823905e7ffc791f94b98e` 及 [STOP manifest](project-owner-ui-recovered-edit02-verification-evidence/originals/independent/manifest.json)保留归因与边界。两个列表在固定页面中合法共用样式 class，测试将它假定为唯一身份块。必修是同时包含精确 `dt`“Project ID”和“Owner”的 `dl.project-facts` 身份目标，保留 strict/唯一性及稳定 ID 断言；不使用 `.first()`、不去掉 ID、不改变其他无内容检查或产品状态逻辑。

固定源码说明：重命名后当前值为 version 5；下一写入回执为 version 6，随即当前读故意失败，仍 ready 的旧 editor 保留草稿。显式重试的 `readCurrent()` 默认不自动采用；读取新版本可进入冲突核对状态，因此当前值 review 与稳定身份 facts 可以并存。017 GET version 5、018 PATCH/019 GET version 6 支持这一先后事实；内部 editor 标志转移仅为源码推导，不冒充运行时状态快照。独审未运行新 probe，原 strict 输出已直接证明定位歧义。

此前改名后的 canonical URL 和项目导航名称断言已经完成；写入后的“命令已确认”与“项目信息读取失败”两个 heading 均已走过，随后点击“重新读取项目”。第 683 行再次进入 `confirmed()`，其 heading 与 form 可见断言已完成，但 `ready()` 的 ID 断言失败。后续 `ready()` 的重读按钮断言、`confirmed()` 的保存禁用断言，以及正文第 684 行起的“没有额外 PATCH”、`factsUnchanged`、恢复后的描述值、最终 `verifyBodies` 和完成结果均未执行，不能称完整 edit 通过。

## 原始 HTTP 证据

54 份作者 run 原件共 358083 字节，逐项 bytes/SHA 与 handoff 一致。19 个 safe response sidecar 绑定 11 份原 body，均保留原字节，method/status 为 GET 200 × 12、PATCH 200 × 5、PATCH 409 × 2。所有 sidecar 使用本轮授权 input hash 与 `TestAccountProjectOwnerWebEditAndRename` source run；统计包含 fixture 与 IPC helper，不能作为浏览器专属读取数量。

| 原记录 | 已保存的响应事实 |
| --- | --- |
| 001–004 | 准备 GET，分别为 main、duplicate、archived、archiving |
| 005–006 | 初始 Resolve/Get，main version 1 |
| 007–008 | PATCH/Get，version 2，描述保留空格和换行 |
| 009–010 | PATCH/Get，version 3，描述清空 |
| 011 | 名称占用 PATCH 409，`RESOURCE_BUSY/not_committed` |
| 012–013 | GET/PATCH helper，准备 version 4 的外部更新 |
| 014–015 | PATCH 409 `VERSION_CONFLICT/not_committed`，随后 GET version 4 |
| 016–017 | rename PATCH/Get，version 5、`Owner.Renamed` |
| 018–019 | PATCH/Get，version 6、`confirmed while Get is unavailable` |

main 稳定 ID 为 `01a11a1f-b866-7338-aa31-ce2794d5e288`。019 的 GET 与 018 PATCH 共用 body SHA `562195e72feaf655bc48fb73efa90cbea74b8c6f077fbd30105f195296c7e1f1`。表格只说明保存的 native 响应；不凭空补造中间失败 GET 的 body，也不把这些响应与此前已完成的顺序断言合并成整轮 PASS。

本轮无最终 `schema-validation.json` 或 `same-body-validation.json`，截图为 0。归档中的 JSON 语法解析与 body/sidecar SHA 核对只检查完整性，没有执行 schema/public client 校验。

## 退役与资产恢复

直接进程 PID `162247/start949335` 已 actual wait；[4 个 adopted wait](project-owner-ui-recovered-edit02-verification-evidence/originals/run/adopted-waits.json)为 `166721`、`166715`、`166718`、`166719`，starttime 均 `952583`，实际 wait/exit 0。watchdog complete 且线程 joined；raw 保留 Node direct wait、proxy Serve/body handlers、Project preparation service 及正式 root join 记录。

[observed-resources](project-owner-ui-recovered-edit02-verification-evidence/originals/run/observed-resources.json)保留 D03/D04/D05 的 7 个完整 container/network ID、nonce 与 mount；[cleanup 两扫](project-owner-ui-recovered-edit02-verification-evidence/originals/run/cleanup.json)逐 ID absent，owned/process、两处 runtime entries 与新增剩余资源为空，既有 Docker baseline 不变。monitor error、cancellation、forced tail action 均为 0。1165 个显式输入前后相同，原 driver 与冻结验证输入保持一致。

[补充 TCP 尾部原件](project-owner-ui-recovered-edit02-verification-evidence/originals/run/tcp-tail-observation.json)记录 host TCP/tcp6 全状态 delta 在 39.38028535700141 秒后两次为空，未扩大原 75 秒预算。轮询不证明所有瞬时短连接或所有权。四个新增 daemon/PID1 shim zombie 另列：`162472/start949562`、`162827/start949801`、`163195/start949987`、`163694/start950173`；非 driver owned、未 wait、未发送 signal，不称整机进程清零。

root 于 06:09:18.662340 UTC 恢复原 3 个全局 dist 文件，晚于 06:08:37.815244 UTC 的最后 TCP 清扫；[restore-after-edit02 原件](project-owner-ui-recovered-edit02-verification-evidence/originals/root/restore-after-edit02.json) SHA `f2c5902158de4690ced39abde0a83f22c2bb01b293de97eac72f30228ce6601c` 保留逐文件 bytes/SHA 及 `original_restored_exactly=true`。53 个测试资产保留于 `/workspace/scratch/owner-ui-assets/test-dist-ui-v1`。退役后的 root 恢复不是本轮输入漂移，也不授权后继执行。

## 归档及未验范围

[source-map](project-owner-ui-recovered-edit02-verification-evidence/source-map.json)绑定 104 个逻辑原件引用：原失败、准备输入与独审 77 个，文末后继窄修 27 个；对应新增 76 个物理原件（439029 字节）与复用 18 个已提交物理原件。43 项 Git blob/SHA 区分旧 f09f/browser-v4 运行与新 367/browser-v5 修正；final03 小差量和 root 单次授权单独绑定。只归档必要原 bytes 与 metadata，不复制全树、依赖、资产、私有 material、key、Cookie 或密码。

[格式例外](project-owner-ui-recovered-edit02-verification-evidence/format-exceptions.json)记录 32 个原件：19 个 sidecar/11 个 body 缺 EOF，edit02 raw.log 第 32/34/36/37/39/45/49/50/61/62/64/65 行原 trailing whitespace，以及后继 v5-list01 raw.log 第 25 行原 EOF 空行；无 CRLF 或独立 CR。全部按 SHA 保持原 bytes。[归档检查](project-owner-ui-recovered-edit02-verification-evidence/archive-checks.json)核对原件、Git 引用、JSON 语法、链接与格式，新增派生文件保持 UTF-8/LF/单个 EOF；这些检查不执行原业务或 schema/client。

原 edit02 FAIL 保持；修正后的真实 edit、其余三个新分组、旧 16 组、独立真实 A/B、视觉、recovery 三态及完整 D27 均未因此接受。私有 Skills prepared、辅助生命周期事实、生产未绑定/ready503、D08–D28/E01 与既有三停止边界不变。

## 已交付的身份定位窄修：独立后继输入

root 随后已提交并核远端相同的 `367156d89773660c4a671a4b73d5ea7a16e24f50`，只在 #21 新增 `projectIdentityFacts(page)` 并替换 `ready()` 一处目标。helper 从 `dl.project-facts` 同时筛选含精确 `dt`“Project ID”和“Owner”的身份区域，保留 strict、稳定 ID、form 可见及重读 enabled 断言。7 个名称 helper 调用、31 个描述 exact label、其他全 class 无内容断言和产品源均保持。

源码只由 Git `367156d8` 定位，SHA `3cb33cf8505356c6fdb971fc75af7f47b57f960de8181fbc52a2bb02ea0ef009`；[v5 freeze](project-owner-ui-recovered-edit02-verification-evidence/originals/followup-v5/author/browser-v5/freeze.json) SHA `a878bb0c423cc8820bc3e5060ef96fad290542226d45447ae6dfa0624965ff85`，[原 delta](project-owner-ui-recovered-edit02-verification-evidence/originals/followup-v5/author/browser-v5/delta.patch) SHA `19b9c8d9e3dea653a4d4a13e09db10077d83e62dc37508265e2b3db58fef7bfa` 与 Git f09f → 367 差量完全相同。13 项显式输入只改变 #21；共同 imports/tools 与已有原件按 SHA 复用，未重扫完整闭包。

[独立 v5 验收报告](project-owner-ui-recovered-edit02-verification-evidence/originals/followup-v5/independent/review.md) SHA `82f890192ee86f926afda73c789b55c9fed482f16c86476c1ad37979f1f931e0`、[evidence](project-owner-ui-recovered-edit02-verification-evidence/originals/followup-v5/independent/evidence.json)及 [STOP manifest](project-owner-ui-recovered-edit02-verification-evidence/originals/followup-v5/independent/manifest.json)保留原 bytes。作者 [checks](project-owner-ui-recovered-edit02-verification-evidence/originals/followup-v5/author/browser-v5/checks.json)记录 format-check exit 0 / 0.731 秒、strict TypeScript/checkJs exit 0 / 1.669 秒、五个 mode 各列举一项 exit 0 / 4.349 秒；三组 command/raw/result/前后 inputs 均已归档或复用，实际 direct wait、输入同、owned 双空均有原记录。

此独立接受只关闭身份 facts 定位器缺陷，没有新 browser probe、业务资源或检查重跑。列举测试不等于执行测试；原 edit02 FAIL、未到断言、最终 schema/client 未执行及无完整 DOM 的边界不变。修正后的真实 edit/layouts 仍待后续单独验证，未回填本轮结果。
