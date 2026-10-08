# Project Owner UI 重建 edit03 成功与退役证据

2026-10-08。作者唯一获授权的 `new-edit/edit03` 实际整轮 **PASS**，其原件与 owned 资源退役经独立复核 **PASS**。浏览器 1 case / 5.7 秒，Go 顶层 `TestAccountProjectOwnerWebEditAndRename` PASS / 12.10 秒，直接命令 exit 0 / 57.939 秒。接受范围仅该作者实际编辑轮次及本轮退役；独审与本归档均未重跑 browser、schema/client、fixture 或既有检查。

历史 [edit01 失败及名称定位修正](project-owner-ui-recovered-edit01-verification.md)、[edit02 失败及身份定位修正](project-owner-ui-recovered-edit02-verification.md)、[read02 限定成功](project-owner-ui-recovered-read02-verification.md)继续保持原字节及各自范围。本次不等于独立真实 A/B、recovery/identity/layouts、旧 16 组或完整 D27 接受。

## 固定输入与实际终局

Go04/browser-v5 四个 harness 源由 Git `367156d89773660c4a671a4b73d5ea7a16e24f50` 定位；19 个 UI 源复用 Git `088f4d3490db4d86781090f0602299901c5f3247`。final04 只在 final03 闭包上覆盖已接受的 v5 单源，原 driver 字节不变；本档复用已提交闭包绑定，不重新扫描 955 源图或复制源码、资产、依赖实体。

| 原件 | SHA-256 |
| --- | --- |
| [作者 handoff](project-owner-ui-recovered-edit03-verification-evidence/originals/author/edit03-handoff.json) | `aac7480db2454b22932b84b0a96eb463c15c88f5c6be0b2a3df9c4c1620e5892` |
| [作者说明](project-owner-ui-recovered-edit03-verification-evidence/originals/author/edit03-handoff.md) | `71a09dc551689111f8ae2b5da774325c2f00ff03ee0cff3b2486a046bf3788d4` |
| final04 原准备输入 | `ab92204024887b840d45174b1d72250b6930bb967fba4936f10472d03052db4d` |
| [本轮 root 授权输入](project-owner-ui-recovered-edit03-verification-evidence/originals/run/frozen-input.json) | `0b724595ebc2fe77eb44e510d5161b0b256eea52f360a5ed53e93144ba91867a` |
| [final04 小 closure delta](project-owner-ui-recovered-edit03-verification-evidence/originals/input/final04/closure.delta.json) | `4b642b20b1fc1cca4460aa130517e4190356353e8bf19976a86dd6beeffdd35b` |
| browser-v5 freeze，复用已提交原件 | `a878bb0c423cc8820bc3e5060ef96fad290542226d45447ae6dfa0624965ff85` |
| 原 driver，路径换代 v04、字节不变 | `bb666f55bf39f6454a370f62459d3e72416d90767f14a75d2e99775b8c11b38e` |
| [实际 raw.log](project-owner-ui-recovered-edit03-verification-evidence/originals/run/raw.log) | `dca9a41ada39935128cf24d67a1ba4038af433bfa5d500846dd3cf728a1d3b8d` |
| [实际 result.json](project-owner-ui-recovered-edit03-verification-evidence/originals/run/result.json) | `982bc8961a974bd1dc32d01d4687d7434b6408a106d9180b4deb56be25cd8b2b` |

final04 原准备态的候选接受说明及 `root_authorized_resources=false` 保持原 bytes。本轮 true copy 只改变 `root_authorization`、`root_authorized_resources`、`status`，追加已完成的 v5/准备独审引用，仍只授权 `new-edit`；不追改先前准备记录。

[command.json](project-owner-ui-recovered-edit03-verification-evidence/originals/run/command.json)记录实际命令 `sh scripts/test-objects.sh -run '^(TestAccountProjectOwnerWebEditAndRename)$'` 和 PID/starttime、环境、时间与 direct wait。[launch 原件](project-owner-ui-recovered-edit03-verification-evidence/originals/author/edit03-launch.raw)及 handoff 记录外层 exec session `44429` 实际 exit 0、完成 chunk `48cd61`。45 秒 browser case、120 秒 top、6 分钟 package、75 秒 TCP tail 预算不变；其他 package 的 `[no tests to run]` 不计为新增业务用例通过。

[正式独审报告](project-owner-ui-recovered-edit03-verification-evidence/originals/independent/review.md) SHA `493166bdf861e450a01b05bf90227b5fc7b04b6090c37c677e0e24d10c5630ce`、[evidence](project-owner-ui-recovered-edit03-verification-evidence/originals/independent/evidence.json) SHA `a7c36740dfa3b7d41cd13a8fbeb2d84fecab31deb0864281e115fe9e4f7e426c` 及 [STOP manifest](project-owner-ui-recovered-edit03-verification-evidence/originals/independent/manifest.json)均冻结，无新增必修。

## 实际完成的编辑行为

冻结 case 已完成描述保留空格/换行的显式保存与清空、dirty 导航/取消、重名拒绝且保留草稿、版本冲突后明确重读/采用，以及改名后的 canonical URL 和项目导航断言。此前两个定位器修正均实际走过。

已确认写入后，proxy 受控注入当前 Get 失败；页面保留确认状态，随后显式重读。v5 第 689–705 行（旧 v4 第 684 行之后）无额外 PATCH、command/audit/event 事实不变、描述恢复、最终 `verifyBodies` 和 `complete` 均已执行。Go 在读取 `completed` 和全部必需结果后实际 PASS；该临时结果随私有 runtime 清理，持久证据为固定断言、top 日志和最终 checker 原件。没有另存完整 DOM 或截图。

这覆盖本轮受控读取失败后的处理，不代表真实后端故障、未运行的 unknown/lookup 场景或独立实际 A/B。正式 root 与 Account/Project API 链路的成功也不将私有静态托管、Skills preparation 或辅助生命周期事实扩大为生产绑定或生命周期运行验收。

## 同一原始响应的校验范围

56 份 run 原件共 355774 字节，逐项 bytes/SHA 与 handoff 一致。19 个 safe response sidecar 绑定 11 份原 body：GET 200 × 12、PATCH 200 × 5、PATCH 409 × 2。文件名、body SHA、input hash、source run 和唯一 request ID 均核合。

[schema 原结果](project-owner-ui-recovered-edit03-verification-evidence/originals/run/safe-http-evidence/TestAccountProjectOwnerWebEditAndRename/schema-validation.json)为实际进程 `status=0`、`stdout="19\n"`、stderr 空，覆盖全部 19 个响应；[同 body 原结果](project-owner-ui-recovered-edit03-verification-evidence/originals/run/safe-http-evidence/TestAccountProjectOwnerWebEditAndRename/same-body-validation.json)为 `schemas=19`、公开读取客户端 `resolve=1/get=6/list=0/problem=0`。

| 记录 | 在本轮证据中的范围 |
| --- | --- |
| 001–004 | fixture 准备 GET；计 schema，排除 browser 读取计数 |
| 005 | 实际 browser Resolve，同原 body 公开客户端解析 |
| 006/008/010/015/017/019 | 实际 browser Get，共 6 个，同原 body 公开客户端解析 |
| 012 GET / 013 PATCH | 外部更新 IPC helper；计 schema，排除 browser 读取计数 |
| 007/009/016/018 PATCH 200 | 实际页面写操作及 schema；不计额外 mutation 客户端重放 |
| 011/014 PATCH 409 | 名称占用 `RESOURCE_BUSY` 与 `VERSION_CONFLICT`，均 `not_committed`；不计公开 GET Problem decoder |

main 稳定 ID 为 `01a11a34-8d23-777f-8ff0-7adbf546e658`。013 helper 更新至 version 4；016/017 为改名后的 version 5；018 PATCH 与 019 GET 为同一 version 6、描述 `confirmed while Get is unavailable`，共用 body SHA `f60cd5346b12ad98017be7a390269ec7d95a865ddd24bfea4503e5715f3f46b3`。

固定 `observe/verifyBodies` 在实际运行中用原生 `X-Request-ID` 关联 browser 内存中的 URL、owner、status，核对 endpoint/status/hash 后从同一 bytes 构造 `Response`。Resolve 使用实际 URL 参数与 owner，Get 使用 sidecar project ID 与该 owner。完整 PASS 和最终 checker 原件证明此路径执行；逐请求 URL/owner 映射没有单独持久化，不能声称另有独立 browser 响应 transcript。受控失败 Get 没有正式后端 safe response 原件。本归档只解析 JSON 语法并核 hash，未再次执行 schema/client。

## 本轮退役与资产恢复

direct PID `184025/start1085900` 已 actual wait；[4 个 adopted wait](project-owner-ui-recovered-edit03-verification-evidence/originals/run/adopted-waits.json)为 `188485`、`188488`、`188489`、`188490`，starttime 均 `1089094`，实际 wait/exit 0。watchdog complete 且线程 joined；raw 保留 Node direct wait、proxy Serve/body handlers、Project preparation service/Guard 与正式 root join。

[observed-resources](project-owner-ui-recovered-edit03-verification-evidence/originals/run/observed-resources.json)记录 4 容器/3 网络的完整 ID、nonce 和 mount；[cleanup 两扫](project-owner-ui-recovered-edit03-verification-evidence/originals/run/cleanup.json)逐一 exact ID absent，owned、runtime、browser-runtime 与新增剩余资源为空，既有 Docker baseline 不变。monitor/cancel/forced action 均为 0；1165 个输入前后指纹相同，driver/授权输入不变。

[TCP 尾部原件](project-owner-ui-recovered-edit03-verification-evidence/originals/run/tcp-tail-observation.json)记录在 38.36864873800005 秒后两次 host TCP/tcp6 delta 为空，最后清扫 06:31:21.924414 UTC；这是补充轮询，不证明所有短连接或额外所有权。四个新增 PID1 daemon shim 单列非 owned、未 wait/未 signal：`184252/start1086113`、`184607/start1086346`、`184973/start1086538`、`185531/start1086752`。本轮 owned 退役不表示整机进程清零。

root 在 06:32:29.443034 UTC 恢复原 3 个 dist 文件，晚于本轮资源/TCP 退役；[restore-after-edit03 原件](project-owner-ui-recovered-edit03-verification-evidence/originals/root/restore-after-edit03.json) SHA `ce2ed5afc14af65f3f9798bfb408ca97bc2bc3cf2d5665ec85de19d848b82a88` 记录精确 bytes/SHA 恢复，53 个测试资产保留于 `/workspace/scratch/owner-ui-assets/test-dist-ui-v1`。该轮后恢复不算运行输入漂移；本档只读取这个固定记录，不混入后继资产或资源窗口。

## 归档检查与接受边界

[source-map](project-owner-ui-recovered-edit03-verification-evidence/source-map.json)记录 80 个逻辑原件引用，对应新增 66 个物理原件（428344 字节）和复用 12 个已提交物理原件；42 个 Git blob/SHA、final03 → final04 小差量及原准备 false/实际授权 true 分别绑定。没有复制产品源码、完整源图、依赖、资产或私有 material/key/Cookie/密码。

[格式例外](project-owner-ui-recovered-edit03-verification-evidence/format-exceptions.json)保留 33 个原件例外：19 个 sidecar、11 个 body 和 2 个最终校验 JSON 缺 EOF，raw.log 第 32/34/37 行原 trailing whitespace；无 CRLF 或独立 CR。[归档检查](project-owner-ui-recovered-edit03-verification-evidence/archive-checks.json)核对原 bytes/SHA、Git 引用、JSON 语法、链接及格式，新增派生文件为 UTF-8/LF/单个 EOF，不改动原件。

独立接受限于作者实际 edit03 和本轮 owned 退役。recovery 三态、identity/layouts、旧 16 组、独立实际 A/B、视觉及完整 D27 均未因此接受；生产未绑定/ready503、D08–D28/E01、Skills/生命周期边界与既有三停止保持。原 edit01/edit02 FAIL 不回填为成功。
