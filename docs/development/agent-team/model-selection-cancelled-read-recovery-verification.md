# Selection 取消读取后的状态恢复验收

状态：2026-10-06，作者检查与独立验收通过，主线程采纳四路径并提交推送 `debbb28deb7c883fd0b6b77a75354b9b5d7ece0b`，主线程核实远端一致。固定产品基线为 `fd32120eba4c76f67b649248377f4825a78d5d79`；[修复卡 rev1.1](../work-items/d27-model-selection-cancelled-read-recovery.md)在规格提交 `e4848151fabf50ee94087d5577fd948341379766` 的技术 §1–4保持原字节。本结果不接受后续 Account 六叶、三组组合，也不表示完整 D27 完成。

用户在当前配置或保存引用尚未读完时明确放弃编辑，controller 在同步退役边界将本批 `loading→error`，保留保存的 ID、已完整读取的 ready pair、既有 error 和可选 empty。旧代次/身份隔离、严格 canSave、历史命令恢复及真正 owner finally 释放规则保持；取消后不自动续读或产生新写，用户通过原入口明确重读/刷新恢复完整证据。

## 1. 固定输入与原件

[作者报告](model-selection-cancelled-read-recovery-verification-evidence/objects/016778a6be2df61ef243a5be19871cb4e926ce499d891a9d9c6cbed6c60079de)、[作者索引](model-selection-cancelled-read-recovery-verification-evidence/objects/e057942e5f5a2c82b0681171d3a504c5e60e92c26a0faab8b53051cf69745b8a)和[独立最终 v2](model-selection-cancelled-read-recovery-verification-evidence/objects/d57a0a6f8665fb252c90a773a731f67ef4d5e24864f4ce5fbc051ca794300433)保留实际 argv、cwd、env、退出、原 raw 与限制。作者 input01 manifest SHA256 为 `e05427019f96dd4e33dc3f51d4c4925e1cff8ce09a8ba28c8494cf5f5d0fe8e6`；四源与接受提交逐 SHA 匹配，31 dist仅保留指纹和构建原记录，1033 接受依赖直接从固定 Git 核对，不复制完整树、安装依赖或二进制。

| 接受路径 | SHA256 |
| --- | --- |
| `web/src/composables/useSystemModelSelection.ts` | `1a4dbe6f1c0eb7dbdf885fbdc7495c9468bc33a7b8fd7b5082eb45f6c2cc3883` |
| `web/src/tests/system-model-selection-state.spec.ts` | `5294842bb3fb093f5bcb5351b71b501f54b2c097903f490bc24cf4e322d2c245` |
| `web/src/tests/system-model-selection.spec.ts` | `5daedea590e9e08c24e927e1d9ee7e0ea36d7412f45e97bd508a462c99b96a08` |
| `tests/account-captcha-web/e2e/system-model-selection.spec.ts` | `24487a08ddd7d5a8f0d6b8a52471f649863610cfdf877284b28795d6abe25e42` |

## 2. 实际结果与保留失败

| 原检查 | 实际结果及边界 |
| --- | --- |
| 作者 pure-red01 | exit 1 / 4.777s；固定旧 controller 的 reference/current 两个正确终态断言均红，原 17 例通过。npm exec 消费了 `-t`，实际整文件跑 19 例，不称只执行两例。 |
| 作者 pure-green01、targeted01 | exit 0 / 3.151s，同两断言转绿；exit 0 / 6.139s，两目标文件 105 例通过，涵盖 Model/Provider 两段原生屏障、晚结果与身份/路由隔离、显式恢复和全部 ready 对照。 |
| 作者 check01 → check02 | 首轮 exit 1 / 24.747s，1043/1044，私有导出缺少已接受主题文档，build未执行；只补固定只读文档后 exit 0 / 25.796s，29 文件/1044 测试及 format/type/build通过。原失败不改。 |
| 作者离线准备 | 严格 TS、格式、三个 browser list1、Go1.27.1 integration/race空运行编译、精确顶层发现和三次输入门禁通过；发现/编译不计真实业务组。 |
| 作者 navigation01 | [原 raw](model-selection-cancelled-read-recovery-verification-evidence/objects/ea9799ebf4331e77b8dcc5b547e24bc79a8b241eda882da5aa96912eb35fe824)，exit 0 / 119.897s，顶层 15.56s、browser 11.8s；确认恢复后精确 Memory Provider GET确实仍被 hold，放弃后保留 Embedding、其余三项可重读错误、零自动续读/PUT；显式恢复后原保存/DB/焦点与八布局通过。 |
| 作者 regression01 | [原 raw](model-selection-cancelled-read-recovery-verification-evidence/objects/c48b106455ce29193d64620a4f0b3977d8e24890acc5c2a252f0f9bb0374c04f)，exit 0 / 84.293s；ReadAndPagination 18.74s、OutcomeRecovery 11.18s通过，未重复已通过的 Navigation。 |
| 独立 type01 → type02 | 私有最小闭包遗漏固定 RouteMeta 声明，exit 2 / 4.525s；只补私有声明后 exit 0 / 3.174s。保留原件，前提错误不记产品红。 |
| 独立 reference01、current01 | 两组共三例，exit 0 / 2.790s及2.847s。真实 App/Session、严格解析器与受控 native ReadableStream证明两类取消收尾、actual read/cancel尾部未结束仍 busy、零续发、明确重读恢复及完整 ready 保留。jsdom布局替代不冒充 Chromium焦点/几何。 |
| 独立真实 independent01 | [原 raw](model-selection-cancelled-read-recovery-verification-evidence/objects/23bbb65144032fa2e0eded3e2eb38474e3ee0d678a60ff3dbc5dc11d95fdf8f7)，首轮 exit 0 / 60.523s，`TestIndependentSelectionCancelledReadRecovery` 10.10s。公开确认重挂、同 Session新 GET 200 EOF、精确 Provider hold中放弃、ready/ID保留及零 PUT；显式恢复后一次正式 PUT和严格四字段 receipt，Go在浏览器结束后核 selector version+1、三引用、commands/audits/events/distinct keys各+1，零额外 embedding/downstream副作用。 |

真实三轮均维持45s browser、2m顶层、6m包、race/count1、worker1/retry0。服务器 hold结束只证明受控响应结束，actual native tail/owner join由专门纯屏障证明，不能互代。未变阶段按精确输入复用，未机械重跑其他领域。

## 3. 清理、图片与历史边界

| 实际轮 | exact资源 | 所属 PID/starttime | adopted实际wait | 终局 |
| --- | --- | --- | --- | --- |
| 作者 navigation01 | 7 | 130 | 4 | actual wait；两次资源 absent、所属进程空、runtime与短TMP清理；基线/输入不变，monitor 0 |
| 作者 regression01 | 7 | 100 | 8 | 同上 |
| 独立 independent01 | 7 | 85 | 4 | 同上，窗口交回 |

历史无关 PPID1 Z没有触碰，不计本轮回收或已wait。原命令、前后输入、exact ID、PID/starttime、两次清理均按原字节保存；没有为了归档再次启动产品或资源。

八图只有**作者逐图实际检查**及[其视觉记录](model-selection-cancelled-read-recovery-verification-evidence/objects/c716928fd67b9752c031a166f53ba5edffe744ea5ab1cbda0711d6c2381378f9)；主线程只读报告，没有亲自 view 本轮截图，独立验证者也未做本轮图审。[独立原 v1](model-selection-cancelled-read-recovery-verification-evidence/objects/894deacf60fdbb3777b0ef1d5beb94c2d7445bf2c60122a5951c135341fcd83c)的相反归属错误保留，以 v2仅勘误图审主体，功能、命令、清理与源 SHA未变。图片展示滚动内容顶端，不证明全部下方字段同时可见。

[早期混合输入诊断](model-selection-cancelled-read-recovery-verification-evidence/objects/c83432dbf6190c1f4a79938c3bdc777a5c047ce3d6a63fcd2642041459f4d9df)的 diagnostic02/04绿表示复现缺陷，不是修复通过；diagnostic01闭包导入、diagnostic03按钮名前提红原件保留。本卡另在已接受 fd32120基线上取得正确终态首红并修复。旧 Account oldselection01只有 Save disabled timeout，没有现场 refs/busy/请求序号、失败截图或trace；后续 oldselection02在四引用完整场景诊断通过，不得倒填原失败原因或抹去原红。归档保留这些历史 command/raw/result/probe/输入指纹；混合 Account完整闭包只保留原定位，不冒充本次固定接受基线或声称历史每版完整重建。

## 4. 归档与接续

[证据说明](model-selection-cancelled-read-recovery-verification-evidence/README.md)与[索引](model-selection-cancelled-read-recovery-verification-evidence/index.json)将271逻辑原件按 SHA去重为176物理对象；本修复26项原检查含三真实轮，另有早期诊断/旧Account交叉原件。原 raw/diff不做格式清理。离线脚本实际通过，仅核保存字节、四交付源、1033固定基线、原退出与清理，不运行产品：

```sh
env PYTHONDONTWRITEBYTECODE=1 GIT_NO_LAZY_FETCH=1 \
  python3 docs/development/agent-team/model-selection-cancelled-read-recovery-verification-evidence/verify_archive.py
```

后续 Account须以已接受的两个测试文件为基底，保留本次取消/显式恢复强断言，再重放原六叶菜单、三组增量并另验组合；不能整文件覆盖，也不能把本修复当作 Account整卡接受。SMTP、外部模型调用、Runtime、生产SPA/Vite、其他引擎/native zoom不在本结果内。完整D09/D26/D27及D08–D28/E01未完成、E01未开始；Summary待决、Object/tools原停止、Artifact/Project和生产未绑定、ready503边界保持。
