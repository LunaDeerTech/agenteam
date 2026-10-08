# Project Owner Audit UI 阶段验证

本档只封存 root 已接受的阶段组合：固定 21 技术路径版本链、必要离线与受控证据、三次原失败及修正，以及作者 read02／authority02／nav02 的实际结果。**完整 21 技术、旧 10 回归、独立本人 4 轮、README #21 与完整 22 路径仍待最终组合接受**；旧 10 与独立 4 不在本次归档范围。这里没有读取活动轮次，也没有新增业务运行。

[版本图 bba14d28](project-owner-audit-ui-verification-evidence/originals/bba14d28a3b50606-source-map.json)逐行固定 API5、state6、views6、JS2、Go2，卡顺序为 #1–20 与 #22；其中历史 pending 保持原文，本档仅用已封 nav02 与视觉末件补充阶段事实。17 个 web 源引用 root 给定 `055c727b5bd1fb2f2794d402ce214ef5f95bd38b:path`；JS2／Go2 尚用精确 SHA 与 freeze 链，未编造 Git 提交。归档未再次读取技术源码，最终一次 21 源绑定尚待完成。前置[rev2 规格](project-owner-audit-ui-spec-verification.md)与[正式 Audit HTTP](project-owner-audit-http-verification.md)原位复用；四处既有 409 的 schema 为 `b5158110`，以同一 `055c727b:api/openapi/project-audit.json` 固定，`d477a25e` 仅沿 root 的历史提交说明。

| 阶段 | 本档保留的接受范围与原限制 |
| --- | --- |
| [API 五源](project-owner-audit-ui-verification-evidence/originals/5669b67d4c3bf828-review.md) | 独立受控 71/71；作者 API 新 86 与原轮旧 102 按版本组合。原 185 PASS／3 FAIL 与测试前提修正保留。Project 31 输出动作／14 resource 与 53／25 过滤值分开；非真实 producer 或当前全应用通过。 |
| [state 原六源](project-owner-audit-ui-verification-evidence/originals/9ae45bdf4e11ad2a-review.md) → [View／auth-v2](project-owner-audit-ui-verification-evidence/originals/067080be0ba5211c-review.md) | state 独立 18＋2；真实 App 独立 9＋1，原失败与录证保留。auth 保 fullPath 关闭非法 query/hash 丢失后错误 Resolve/Get 的 D1。合法旧 A 页面重建可能产生一次 A 初读，不表述为导航全程无额外请求。 |
| [17 源离线](project-owner-audit-ui-verification-evidence/originals/280fa718ef5c61be-review.md) | 原 fullunit 2265 PASS／1 FAIL＋旧文件定点 20/20，未重标为修后 fresh 全套；API、state、App 各自原受控和类型／格式时点保持。 |
| [JS 两源](project-owner-audit-ui-verification-evidence/originals/f04af9bc5d4efeb8-review.md) | 原完整 STATIC、strict TS／格式及三模式枚举；枚举不执行业务。后续 spec `f6482404`→`16a3f734`→`a91d3ab7` 分别绑定 focus 与 Session 恢复前提修正。 |
| [Go 原 D1](project-owner-audit-ui-verification-evidence/originals/313a16f8b7233212-review.md) → [candidate03](project-owner-audit-ui-verification-evidence/originals/b9f2d99fc5fc5575-review.md) | held 的 joined 由 release／ModifyResponse 中间时点移到真实外层 handler 退出；协议和预算未扩。[D1 实际受控](project-owner-audit-ui-verification-evidence/originals/79e7b09ca2507602-review.md)为 1 top／5 组／13 关卡／8 终局，9.537433 s、direct wait／owned 双空；race-c、vet、两 cmd 编译仅为离线。 |

三次原业务失败按原 bytes 保留，退休接受不改其 FAIL：

| 原轮与修正 | 原实际边界 |
| --- | --- |
| [read01 dcc9e426](project-owner-audit-ui-verification-evidence/originals/dcc9e426a07d1b93-review.md) → [source-v02](project-owner-audit-ui-verification-evidence/originals/5164ee520fd7a10c-review.md) | top 22.90 s FAIL，actor_id focus 断言未通过，之前 invalid 已通过；无当时 activeElement／完整 DOM。修正保持 id invalid、新增 kind invalid 并严格检查首错 kind 焦点，后段由 read02 实际证明。 |
| [authority01 52eb76b5](project-owner-audit-ui-verification-evidence/originals/52eb76b51eb73303-review.md) → [source-v03](project-owner-audit-ui-verification-evidence/originals/de56c9b7b0bf1304-review.md) | top 30.36 s FAIL，401 后直接等待 Login 的前提过强；当时无 DOM，后段未到。单 hunk 改为明确 Session 恢复标题、按钮 enabled、显式 restore 后仍保原 Login／deniedBoth／finish。 |
| [nav01 cb849ce6](project-owner-audit-ui-verification-evidence/originals/cb849ce6daa5d4f9-review.md) → [canonical 修复 9b0453ea](project-owner-audit-ui-verification-evidence/originals/9b0453ea78e87ac9-review.md) | top 16.06 s FAIL 于第二个已登录大写 dotted 直达的当前 document native fact 等待；两份上游 body 不能补造 browser EOF／DOM／后段或 8 图。受控 RED 为大写 FAIL／同 gate 小写 PASS；作者 green01 48 PASS／1 新测试期待 FAIL＋green02 1 PASS／48 skip，独立最终同版本一文件 49/49 才补齐。 |

canonical 两源只在 raw source 严格为 Audit leaf 且目标 fullPath 正好为其 canonical path 时保留读取；跨 Project、非法 query/hash、其他 leaf 及真实 leave/unmount 仍退休。最终 View `3bb842a2`／test `480b2cfb` 与旧 View `58309a18`／test `0ba34fdf` 分别固定。[旧 build-v1](project-owner-audit-ui-verification-evidence/originals/ee7eb7aa4a01863c-freeze.json)为 59 文件／794400 B；[新 build-v02](project-owner-audit-ui-verification-evidence/originals/2be773aad6117e74-freeze.json)为 59 文件／794499 B。只封原执行、资产清单与输入指纹，不复制产物或称本次重新构建。

| 已接受作者实际轮 | 固定版本与实际结果 | 完整正文证据 |
| --- | --- | --- |
| [auditread02](project-owner-audit-ui-verification-evidence/originals/0f4b0ccc5faa1f0b-review.md) | spec `16a3f734`、旧 View／build；top 20.02 s，fixture 72.364 s | 19 GET sidecar／9 去重 body；19 native EOF 与同 body schema／公开 client 重放完成，18 成功＋1 Problem；不是额外 19 次网络请求。 |
| [auditauthority02](project-owner-audit-ui-verification-evidence/originals/6dce1378d4f69b4c-review.md) | spec `a91d3ab7`、旧 View／build；top 28.44 s，fixture 79.398 s | 47 GET，34 完整 native EOF（22 成功＋12 Problem）；13 未完成排除。10 显式权限探测与 24 页面请求分开，34 份同 body schema／client 完成。 |
| [auditnav02 39c88f8f](project-owner-audit-ui-verification-evidence/originals/39c88f8f9cd67e25-review.md) | spec `a91d3ab7`、修复 View／新 build；top 23.41 s，fixture 70.578 s | 18 GET／7 去重 body，17 完整 native EOF／schema／client，另 1 incomplete 排除；13 项导航完成标志与 8 PNG 已封。 |

持久原件保存安全上游 body、原 request ID／path／query／status／media、chunk 长度及同 bytes 的匹配 SHA；原生 chunk 逐字比较与公开 client 重放发生在各原 case 内，本档不伪造第二份完整 native chunks，也不重跑 schema。受控 cut／failure／hold、cursor／missing-ID 的明确 wire 改向保持原声明，不能冒充自然失败。EOF 不单独证明 Cookie owner finally；同 owner 顺序结论沿已接受的实际断言与实现。浏览器零写不否认 fixture 的正式准备写入，private Skills／lifecycle facts 不代表生产接入。

六个 run 均保实际 argv／安全 env／raw、input 前后、direct wait、adopted waits、资源与退休原件。每轮实际 7 ID 两次 absent、owned／Go runtime／browser runtime 双空；成功三轮及三失败的退役事实分别记账。每轮 4 个新增 PID1 shim 为 nonowned，未 wait／kill；进程观察数不冒充 wait 次数，TCP 尾部仅补充轮询，不代表完整短连接或全机清零。作者工具直接启动 driver，没有独立 launch 目录或可补造的 outer raw；fixture 实际 wait 见各 command，工具终局只沿原交接记录。

导航 [作者逐图视觉 018fe8c9](project-owner-audit-ui-verification-evidence/originals/018fe8c99778a01e-review.md)为本人对已封 8 PNG 的可见区有限 PASS；`39c88f8f` 独核只确认 PNG 存在、SHA 与尺寸，未看像素。本档只核归档 bytes 与 IHDR，不重复视觉验收。

| 图 | 尺寸 | 原件 |
| --- | --- | --- |
| project-audit-dark-1024.png | 1024×900 | [d2ed6330](project-owner-audit-ui-verification-evidence/originals/d2ed6330e261aa87-project-audit-dark-1024.png) |
| project-audit-dark-1440.png | 1440×900 | [16033eed](project-owner-audit-ui-verification-evidence/originals/16033eed9492b144-project-audit-dark-1440.png) |
| project-audit-dark-390.png | 390×900 | [9abbf101](project-owner-audit-ui-verification-evidence/originals/9abbf10190d6ff78-project-audit-dark-390.png) |
| project-audit-dark-768.png | 768×900 | [0c9c6da1](project-owner-audit-ui-verification-evidence/originals/0c9c6da1e2886985-project-audit-dark-768.png) |
| project-audit-light-1024.png | 1024×900 | [72dc600f](project-owner-audit-ui-verification-evidence/originals/72dc600f40e864cc-project-audit-light-1024.png) |
| project-audit-light-1440.png | 1440×900 | [b9ff2e2b](project-owner-audit-ui-verification-evidence/originals/b9ff2e2b25efb47e-project-audit-light-1440.png) |
| project-audit-light-390.png | 390×900 | [b794ee7d](project-owner-audit-ui-verification-evidence/originals/b794ee7d8bf56878-project-audit-light-390.png) |
| project-audit-light-768.png | 768×900 | [44c0cf26](project-owner-audit-ui-verification-evidence/originals/44c0cf26f143339e-project-audit-light-768.png) |

390／1024 为详情顶部，900 px 以下未完整呈现；768／1440 为筛选中部，屏外标题／列表及窄输入内部被截部分不扩验。未据图宣称完整输入值、全部滚动状态、真实键盘顺序或 WCAG 测量。

[来源映射](project-owner-audit-ui-verification-evidence/source-map.json)保存 806 个逻辑原件及 21 行历史版本映射；新增 673 个 SHA 去重实体／5344482 B，已有 5 个永久实体原位复用。六个明确 run 的 420 文件完整纳入，其中审查后置 manifest／handoff 导致本档计数大于原独审消费数，不冒充原审扩大过范围。原件相对链接及绝对路径保持原时点，由映射找永久实体。大输入图、源码树、缓存、binary、dist 不复制；[检查](project-owner-audit-ui-verification-evidence/checks.json)、[格式原例外](project-owner-audit-ui-verification-evidence/format.json)和[manifest](project-owner-audit-ui-verification-evidence/manifest.json)只描述本次 bytes／JSON／PNG 头／链接检查，无新 Go、Node、Git、浏览器或资源执行。

完整 21／22、旧 10、独立本人 4 与 README 仍 pending；本档不是当前 HEAD 一次 fresh 全套，不扩大为完整 D27、生产 ready 或 E01。Object runtime join、OpenAI tools、SPA concurrent publication 三停止及 Jina 边界保持。
