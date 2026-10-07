# System 运行信息只读 UI 验证记录

状态：本卡 **35 路径已独立验收、主线程采纳并提交推送**，产品提交 `e1f8cefbeaf3203c4e0388f9ea9dbf11e2e2959b`，主线程核远端一致。交付为管理员受保护的 Runtime 缓存信息读取、显式重新读取/取消、当前身份和权限处理及正式导航；不触发健康探测、不新增写口或自动轮询。本文保存原验收证据，不执行产品复验。

[工作卡 rev2](../work-items/d27-system-runtime-information-ui.md)技术§1–7 SHA `85b3d003c8f257e12fa29e0becd2f23b1f77ea4f64efcc521179567c50f27400` 原字节保持；[原规格档](system-runtime-information-ui-spec-verification.md)仍记录当时阶段，不追写历史。上游 [Runtime HTTP](system-runtime-information-http-verification.md)产品 `9b074f8` 与 [Audit UI](system-audit-ui-verification.md)产品 `a0e73bd` 已接受。

## 1. 固定输入与永久原件

实际运行基线为 `a0e73bd8fc7fa40e1f424f5817e7b1b3b281def1` 的私有冻结输入。作者最终 input04 SHA `1e41477c2810d5ba78e78021528f7048563ce43245c8de88e50ecb4973f9b9e7`，绑定 34 技术源、42 dist 指纹、957 固定 Git 文件及 2892 实际外部源/工具依赖。独立 execution03 SHA `7126260480887e6db740f4c5316f0163398c1da3347b6a71c485c0ce910125f1` 保持同一候选/产物/固定 Git，另含 3127 外部指纹和 47 私有文件。这些是不同层次的闭包，不相加冒充一个统一源码计数。

最终 [作者交付清单](system-runtime-information-ui-verification-evidence/objects/f28f3680dbf8e0086e4189c11e331791eed269db9ff9014f3d07e1e1fd13c049)含第 35 路径 README，SHA `f28f3680dbf8e0086e4189c11e331791eed269db9ff9014f3d07e1e1fd13c049`；README after 为 `8a050cfc721530114c8d29f734bbeb8962e1ed9d7a9a9430aaae1ec0d7feb72f`。README 原文基线为 `93d5863`，34 技术源和 42 dist 不变。提交 e1f8cef 的精确 35 路径逐 SHA 对应交付；这不把原私有运行改称该提交上的主树动态测试。

[证据索引](system-runtime-information-ui-verification-evidence/index.json)将九份准备映射的 1102 个逻辑引用归并为 1079 个原件路径：610 个新内容寻址对象，共 36,152,599 字节；71 个原件路径复用固定 Git 文件或既有永久对象。35 个交付路径的 53 个已保存字节版本可定位。原 command/cwd/env、输入指纹、raw、result、cleanup、实际 wait、探针/runner/差量及图片均保持原字节；不复制工具、依赖树、cache、dist 实体或可执行产物。scratch 仅作历史来源，离线校验不依赖它继续存在。

## 2. 阶段与分版本复用

| 阶段 | 实际证据 | 保留的限制与原失败 |
| --- | --- | --- |
| API A 三源 | 作者 59/59、type、format 首轮通过；独立两组合 actual0/0.228668s、39 输入、2 PID 双清 | Node 受控原生流不是 TCP/browser；16KiB 填充边界不是 DTO 最大语义长度；Go 仅静态来源，无 Go oracle |
| owner/state B 三增量 | 最终作者 40、type/format；独立两组合 actual0/0.251321s、81 输入、2 PID 双清 | 原 pure01 39 PASS/1 FAIL 因 SMTP 前提漏正式 GET/未 dispatch，原 type01 exit2 为两处 nullable signal；只改测试前提/类型，两产品从首 pure 起同 SHA；不能倒称原失败已有 cancel/join |
| 页面 C 与旧导航 | 原 App 8 PASS/5 skip、完整 49 文件 1875 测试及 type/build；独立两组合 actual0/2.208942s、2449 输入、2 PID 双清 | App/jsdom、虚拟 30s 与 native/browser 分层。旧 browser CLI 缺 DOM.Iterable/Node types，修命令后通过；Outbound 原格式 warning、Audit 新长行、provenance/AST 首辅助错误均保留；最后仅 Audit format 通过，未称全 browser 格式通过 |
| D 四 harness 与执行准备 | 最终 type/list/format/race 编译/vet、普通 BrowserContext baseURL 自审差量；独立 STATIC PASS | 仅 list/编译不等于业务执行，原二进制未执行。首次 recorder dist 路径错误是 pre-spawn 叙述/tool 记录，不补造监督 raw。input01 缺 external Go 指纹，input02 精确补 2098 项；race runtime-list02 实际 460 包，原 458 误述保留 |
| 最终独立准备 | 原六 offline 绑定 input03；input04 只增加作者 browser 四行后作 gate02 actual0/1.131720s、2 PID 双清 | 两独立测试体/Go/driver/overlay 未变，不宣称六项全部在 input04 重跑。execution01/source01 的未执行历史及一次取消谓词修订保持 |

A/B/C 生产源冻结后保持。后两次 read 修订只修改新 browser spec；最终 21 web 与原 1875 轮相同，旧 Audit browser 最后仅换行/可选尾逗号，正确 AST 比较证明等价后复用原 type/list。1875 全量不改称最终全树重跑；完整结果由适用的原阶段证据和下列真实轮组成。

## 3. 七轮真实原记录

| 原轮次 | 输入 | 实际退出 / 外层秒 | top / browser 秒 | 原终局 |
| --- | --- | --- | --- | --- |
| 作者 read01 | input02 | **1 / 109.734** | 原 read top FAIL | 7 IDs、119 PID、4 adopted waits，双清 |
| 作者 read02 | input03 | **1 / 64.776** | 原 read top FAIL | 7 IDs、84 PID、4 adopted waits，双清 |
| 作者 read03 | input04 | 0 / 57.871 | 12.33 / 7.9 | 7 IDs、88 PID、4 adopted waits，双清 |
| 作者 navigation01 | input04 | 0 / 65.161 | 11.31 / 7.0 | 7 IDs、82 PID、4 adopted waits，双清 |
| 作者 old-audit-navigation01 | input04 | 0 / 74.353 | 20.53 / 15.7 | 7 IDs、85 PID、4 adopted waits，双清 |
| 作者 old-outbound-navigation01 | input04 | 0 / 78.249 | 22.23 / 19.3 | 7 IDs、83 PID、4 adopted waits，双清 |
| 独立 independent01 | execution03 | 0 / 118.972 | 9.37、8.60 / 4.6、4.1 | 两 top 同轮 PASS；7 IDs、136 PID、8 adopted waits，双清 |

原 browser45s/top2m/package6m、race/count1、worker1/retry0 保持。各轮实际 command wait、前后输入一致、monitor0、精确所属资源双 absent、PID/starttime 双空及 runtime/browserTMP 删除有原件。其他包 `[no tests to run]` 不计业务 PASS。基线为 2 container/4 network；原 raw MinIO Mount 数组顺序差异保留，比较完整对象多重集合后 canonical 同值，重复项和所有属性都未丢弃。独立第一扫 raw 有顺序差异、第二扫 raw 相同，不将它冒称清理失败或擦除原差量。历史 PPID1 僵尸及 Outbound PID89884/start540243 未由本任务回收。

read01 原 observer ended/eof 为 true，旧测试却要求取消必须 !eof。原安全 raw 未投影 snapshot/abort，后续 401/member 段未到，不能用后续通过回填。read02 已到严格 Runtime401 屏障，随后错等 login；原 raw 没有现场 route/DOM。独立静态确认 unavailable 需要用户点击“检查当前会话”，input04 在既有登录链前只加标题/空观察/按钮可用/点击四行。read03 首次完整走到后续链，原两个 FAIL 永久保持。

## 4. 独立结果与视觉范围

[最终独立报告](system-runtime-information-ui-verification-evidence/objects/9eb837460d6e5618d895dfa752dd9a1fcff2ef560e4dadee9b890f3c05deb9aa) SHA `9eb837460d6e5618d895dfa752dd9a1fcff2ef560e4dadee9b890f3c05deb9aa`，delivery34 `f0260d0e4fa9621659f522d00b9202fbfc2eecabf13aa13fccb3a83476a5740a`。R1 对同次正式 GET 的严格 DTO 与页面四区域/四时间/原样版本，覆盖 503 清旧观察与显式恢复、普通账户正式 403 和新 member 页面。R2 覆盖 busy pageshow 不检查 Session、真实在途读取取消、原生 read 结束/代理 handler join 后才释放 owner、空闲 pageshow503 的旧实例退休、同身份恢复仅一次新 GET 与新焦点；零 Runtime 写入、零业务 dialog。

代理 hold 发生在完整上游响应之后，不证明真实 DB 事务尾或 TCP precommit 零字节。取消允许 native read.done=true，成功仍需合法完整 DTO/EOF；这两种终局不能混同。前阶段 Node/jsdom 焦点不冒真实浏览器焦点，真实部分由本轮和作者 navigation 支撑。不扩到真实 BFCache、production SPA、额外健康 probe 或完整旧域矩阵。

保存 24 张 light/dark×390/768/1024/1440 原 PNG 及三份 visual-review。作者新 Runtime 八张全部实际 view，旧 Audit/Outbound 各只 view light390、dark1440，合计 12 张；主线程仅看新 light390、dark768 两张。所有图都是 900px 高局部视口，768 的换行/密度及截断处按原报告保留；不声称覆盖截图外字段、长列表或下方错误。独立验证员和本归档者未新增图审，图审不替代几何/键盘/焦点断言。

## 5. 历史证据限制与未完成边界

所需失败与最终受测源均有冻结字节。C 三个分析/AST helper 虽有原 argv/raw 和后来冻结源，逐轮门禁未列脚本 SHA；事后 manifest 不能补作执行前绑定。独立 C 的准备路径/打印错误仅有原叙述，D 初始 recorder 错误也无单独监督 raw；不重构、不补跑或伪造历史文件。原 input01 外部 Go 指纹缺项、格式写入轮 input_unchanged=false、原警告和辅助错误均保持。

本卡接受不表示完整 D08–D28 或 E01 完成；E01 未开始，Summary 初值仍待决定，ready 保持 503。Object join、OpenAI tools、SPA publication 三项停止边界不解除。Embedding B 是另线活动范围，本归档未读取或据此更新其状态。

## 6. 离线核验与文档范围

从仓库根运行：

```sh
env PYTHONDONTWRITEBYTECODE=1 GIT_NO_LAZY_FETCH=1 python3 docs/development/agent-team/system-runtime-information-ui-verification-evidence/verify_archive.py
```

私有 payload 使用 `--repo /workspace/agenteam --documents <payload-root>`。校验器只读永久对象和固定 Git：核原字节/35 路径、957 基线、源版本、原 command/raw/result/输入关系、七轮退出与双清、图片指纹、技术正文和限定行政差量/本地链接；不运行保存脚本、Go/Node、业务测试、浏览器或资源。通过仅证明档案完整和这些原记录一致，不创造新的产品测试结果。

本次文档范围为本报告/证据、AGENTS、团队 tasks、开发计划的新增结果入口、接续§37及本卡接受页首。旧规格档、接续§1–36、其余历史正文和技术§1–7保持原字节；前端 README 已随产品交付，本次不再修改。自身提交由文件历史定位，整合由主线程执行。
