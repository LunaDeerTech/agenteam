# D26 个人设置有限验收

个人设置卡的精确 24 路径已独立 **PASS**，主线程已采纳并提交推送 `c54f73f3324caa11608d84e5d207141985eb6074`；远端一致由主线程确认。实际验证输入是 `9a710f272026b41ef69852bbeb41cb7670b500a8` 加最终 24 路径与其正式 dist，**不是整个接受提交上的其他并行能力**。本记录不宣称完整 D26、生产 SPA hosting、实际 Vite 代理或 Object 修复完成。

## 1. 固定来源与交付

[个人设置卡](../work-items/d26-personal-settings.md)交付真实基本资料、头像、外观主题、密码更新与 Session/CSRF 续接、草稿和页面导航。上游是已验 Account API 和 [认证前端](d26-authentication-verification.md)；产品规则仍以卡片及正式设计为准。

| 来源 | 固定指纹与定位 |
| --- | --- |
| 作者最终输入 | [fixture-input-01](evidence/personal-settings-verification/author/evidence/fixture-input-01/manifest.json)，SHA-256 `20d8fc5e3ec3ad3f5eb477345fd76d002eec057b4a0a2d0fad875e354c1a99fd`；24 源、11 固定依赖与 9 dist 文件 |
| 作者原终报 | [report.md](evidence/personal-settings-verification/author/evidence/author-final/report.md)，`9a99271765d7b99041e8ffb4c1e3de9003532aac8669d0c8c4ec2982198513c1`；[manifest](evidence/personal-settings-verification/author/evidence/author-final/manifest.json) `f5cd17fe1d6cf573bbe01666c33cc6bf56c8abe54bf5158c5c3e2c4a08f0c142` |
| 独立最终结论 | [final-report.md](evidence/personal-settings-verification/independent/final-report.md)，`ada59561329eb0c5d8b9b28720e6a548601a09b27966e04df4ea40897ae92b9c`；[final-index](evidence/personal-settings-verification/independent/final-index.json) `2fd1ab76be5bc9597c9126df58b2828211cd785c6cb84fd6f80b4b309f5fb960` |
| 原件与源码定位 | [归档入口](evidence/personal-settings-verification/README.md)、[原件映射](evidence/personal-settings-verification/original-map.json)、[源码定位](evidence/personal-settings-verification/source-locators.json)；最终源从接受提交逐项取出，历史差量、独立探针另存精确字节 |

原终报中“待独立/待主线程提交”保留当时事实；当前采纳状态以本报告为准。作者三份前端验后文档另组停写，其 [rev02 清单](evidence/personal-settings-verification/afterdocs/rev02/manifest.json)仅是文档证据，不作为业务运行输入。

## 2. 实际验证与复用

| 验证 | 实际结果及范围 |
| --- | --- |
| 作者工程检查 | `npm run check --prefix web` 的最终 `web-check-02`：9 文件、110 pure，格式、vue-tsc、Vite 正式 build 通过。受影响 Go integration race compile/vet、最终 browser TS/config 通过；原命令、环境、exit 与 input 均保留 |
| 作者新增真实组 | `personal-profile-theme-01` 与 `personal-password-authority-01`：4 完整 Go 顶层/4 browser case 通过，使用真实 Account、PG、MinIO 和同源正式 dist |
| 旧认证兼容 | 4 个顶层名/6 browser case 的**组合证据**：lifecycle、desktop 在首父组通过；该组 keyboard 取体失败导致父组/driver FAIL。原样 keyboard 的单独后续授权运行通过；revocation、expiry、layouts 在后续原组通过。不能称原父组一次全绿 |
| 独立静审与 pure | Core01→02、UI01→03、固定 fixture 静审完成。UI-F1 的独立 1 pure 原红→同探针修后 PASS；UI-F2 的独立发现为静态，动态修复证据来自作者，未伪称独立原动态红 |
| 独立真实增量 | `TestIndependentPersonalSettingsLiveOwnerRevocation`，1 Go 顶层/1 browser case：`independent-live-owner-02` driver 0，PW 5.1 s、Go 顶层 10.56 s、account 11.591 s；原始 [raw](evidence/personal-settings-verification/independent/logs/independent-live-owner-02.log) 与 [整轮索引](evidence/personal-settings-verification/independent/evidence/independent-live-owner-02/index.json)保留 |

真实组使用原 `scripts/test-objects.sh`、`-race -count=1 -timeout=6m`；Go 顶层 2 分钟、PW 45 秒、worker 1、retry 0 的预算未放宽。其他包 `no tests to run` 不计覆盖；归档没有运行新的业务检查。

独立链证明：A 会话未保存资料和真实 File/Blob，在同 Session pageshow 200 后保留且数据库资料未被改写；B 在另一页面真实改密码后取得新 Session/CSRF，资料 PATCH 真实使用新 CSRF；A 真正收到 401 后，同一个 SPA 文档清除旧草稿，不被脏表单拦截，原 `URL.revokeObjectURL` 由透明计数包装调用恰好一次；重新登录不会恢复旧身份草稿或头像。只读 PG 核对 profile 1、password 1、avatar 0、Audit 2、version 3 和旧 Session 撤销/新 Session 当前事实。该 URL 调用计数不证明 JavaScript GC 或 Object 共享 guard。

## 3. 原失败与修复边界

- Core F1：密码结果 Unknown 且原实际 owner 已结束时，不能借旧 CSRF 退出；修后先用原密码意图确认 Session。Core F2：Unknown 后一次普通 not_started 错误不能丢掉同 key、完整原输入及 File；确认、身份失效或明确放弃之前保留原意图。作者两项原红及修后命令/源码指纹保留。
- UI F1：密码 200 后 GET 失败，再显式检查成功时不能用资料重载擦掉已确认密码反馈；独立原 pure 实际失败，同一 probe 在 UI02 通过。UI F2：同 User 的后续新 Session 不能复活旧反馈；原身份/requestGeneration 与一次 A→B 继承、B→C 退役由 UI03 固定差量及作者 32 pure/type 闭合。
- 原 TS 分支类型、按钮 selector、URL 清理时机、browser TS 类型路径等准备失败均保留。两份历史源是**事后按原 SHA 精确重建**，不是当时备份：[Core F2 provenance](evidence/personal-settings-verification/author/evidence/core-f2-fix-reconstructed/reconstruction.json)、[fixture TS provenance](evidence/personal-settings-verification/author/evidence/fixture-ts-01-reconstructed/provenance.json)。
- 旧 auth 首轮 keyboard 的 `first.json()` 报 `Network.getResponseBody: No data found`，未取得 `CHALLENGE_REQUIRED` 的目标断言。原 spec 该点在读取前没有 HTTP status 断言，因此该轮不能补称已观察到 401。[原只读归因](evidence/personal-settings-verification/cdp-diagnosis/review.md)保留“原因未知”；后续完全相同旧子例通过，没有改产品、locator、断言、预算或引入 clone/tee，不能将它称为已修产品故障。
- 独立 TS 首红是 Window observer 类型声明，修正私有类型后通过。独立真实 run01 已启动 PG/MinIO/完整应用前置，但命中既有 helper 的 runtime 路径长度门槛，Node/browser 未启动；清理后仅改自有 runtime 路径与记录组的 plan02 再获授权。24 源、3 probe、dist、observer 与预算未变，后续 run02 才构成独立 browser 行为通过。

## 4. 资源与材料

作者五轮、独立两轮都保存实际 4 容器/3 网络存活 exact ID、nonce/label 及两次 exact absent，原 2 容器/4 网络基线不变；owned runtime/tmp、所属进程最终清零并交还窗口。作者**首轮 Node PID/starttime 未被 observer 采到**，只有真实 Go launcher 的 `cmd.Wait` 0 作为该部分证据，后四轮采样不能回填该缺口。独立成功轮实际记录 Node 2、Chromium 12、Crashpad 2 的 PID/starttime，4 个 adopted wait；两轮清理及原输入前后检查均保留。

归档只收既有安全原件、合成测试输入、源码、数量/布尔/ID、命令和资源记录；不收真实运行中的 Session/CSRF/password/avatar 请求 payload、邀请秘密、凭据、trace/video、缓存或编译二进制。Core F1 原 pure 日志的 mock 值与随机测试 key 是合成值，原字节未改；[窄屏截图](evidence/personal-settings-verification/author/evidence/browser-images-01/personal-settings-narrow.png)只含受控 fixture 的静态展示数据。

## 5. 尚未证明与归档检查

Unknown/迟到响应主要是 pure 验证，本项没有真实服务器 COMMIT/网络故障注入。布局中的 200% 是 **CSS `zoom=2`，不是浏览器原生缩放**。正式 build 由任务自有同源服务器供真实 Account 使用，不等于生产 SPA hosting 或真实 Vite 开发代理验收。完整 D26/D27、Model consumer/Runtime、Project 及 Artifact 共享 guard 的阻断不因个人设置通过而关闭。

归档检查只读 Git 对象和固定原字节，见 [archive-checks](evidence/personal-settings-verification/archive-checks.json)。最终 24、实际前后输入、关键失败/修后源、独立 probe 的重建入口为 [verify_sources.py](evidence/personal-settings-verification/verify_sources.py)；早期中间命令存在仅记录 SHA、没有完整原字节的范围，逐文件/命令见 [source-limitations](evidence/personal-settings-verification/source-limitations.json)，这些范围不承担最终源码可重建证明。最终 dist 的 9 个原件 SHA 已核，不在本次重新 build 或复制 bundle；历史 UI01 build 资产仅保留原清单指纹，不作为 browser 通过证据。原 patch/log 格式诊断按 [精确例外](evidence/personal-settings-verification/whitespace-exceptions.json)保留，不修改原字节。
