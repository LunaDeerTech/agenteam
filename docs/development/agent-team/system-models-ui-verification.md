# System Model 管理 UI：完整卡接受与证据

状态：2026-10-06，`directory_frontend`（frontend_worker）完成、`recovery_verification` 独立最终 PASS，主线程采纳26路径并提交推送 `bc17167c42ee5d5fc1427adaac099ff888ca959b`，主线程核远端一致。本报告绑定 [Model rev2](../work-items/d27-system-model-management-ui.md)的四类Model配置、只读删除影响、受当前事务再次裁决的替代删除、管理员权限/命令恢复及页面验收。技术§1–7 SHA256 `0f459d3631d85e86ab9b0912a99c33a470ba9eebc4c940d98bf5625f5789d96c` 保持原字节。

固定产品基线为 Provider `f465f45b899e21c225e7d4107c099b256538239c`，后端 `9b3201547f9b7b61fd9716a6ba6540084961496c`及迁移1–19未变。最终25个受测源/测试路径对应input03；README为独立通过后补的第26路径。此接受不包含后继Selection UI、Runtime调用、非空Model参数编辑或外域adapter接入。

## 1. 输入、复用与末件

| 原件 | SHA256 | 确切用途 |
| --- | --- | --- |
| [作者最终索引](system-models-ui-verification-evidence/objects/5bb955c1389bc78c3c73b3f6ea33dff9327635913936f54786ef7d7d28f51404.json) / [作者原报告](system-models-ui-verification-evidence/objects/554019a0acb8c03a099a7b4c08f2b31bdd979f365c24adc51cc3f7e6c3cc48c5.txt) | `5bb955c1389bc78c3c73b3f6ea33dff9327635913936f54786ef7d7d28f51404` | 索引保存全部阶段/原失败/四真实轮和八图；原报告当时pending、README deferred字样保持。 |
| [input01](system-models-ui-verification-evidence/objects/e1491bcd239f354a360c0e64fd6ae786d7824d9f74084dcaf797b7f294ed39ec.json) | `e1491bcd239f354a360c0e64fd6ae786d7824d9f74084dcaf797b7f294ed39ec` | 25源/29dist；409错误预期与运行期基线仅36路径由静审阻断，未作为真实红运行。 |
| [input02](system-models-ui-verification-evidence/objects/2cf6565245e301da3272853a4422f15180277c80592c0c98688a331aeee07aae.json) | `2cf6565245e301da3272853a4422f15180277c80592c0c98688a331aeee07aae` | 只改新浏览器spec的禁用替代Model预期为400/INVALID_ARGUMENT/not_started及字段复核；driver另补1030基线和观察器门禁。 |
| [input03](system-models-ui-verification-evidence/objects/d12df3fe63fdb9a405db2ef72864acb48a6de3280d7938559f78272128754ff6.json) | `d12df3fe63fdb9a405db2ef72864acb48a6de3280d7938559f78272128754ff6` | 只改Go fixture的DB→wire快照投影，保留全DTO比较、既有值及额外字段负例；driver另加四个失败组的精确选择。 |
| [独立最终报告](system-models-ui-verification-evidence/objects/7e96eff7312d1f5f686e0326d02036f743e92493f7ce84232fa4cbe7a351099a.txt) / [JSON](system-models-ui-verification-evidence/objects/1d6db0051e20168f5ae1218a3b40f132576c4f48d2ec4827e769fdae8c7eedb1.json) | `7e96eff7312d1f5f686e0326d02036f743e92493f7ce84232fa4cbe7a351099a` | API/owner/page复用及真实两代表组、实际退出/双清；[25源清单](system-models-ui-verification-evidence/objects/fac9dd248cea4cae71f9e421183bb4bab4d32ebf361dd8b7177cef06453aaf42.json)绑定同input03。 |
| [README最终清单](system-models-ui-verification-evidence/objects/586bc3aefebfa5a20d3709f674ab4f9a5b1bf643d9010a90a8cc05b9a849a0ec.json) | `586bc3aefebfa5a20d3709f674ab4f9a5b1bf643d9010a90a8cc05b9a849a0ec` | 26路径与接受Git逐字节相同，README SHA `b58f95adda6671245ad57fee3fcafa9582b089f34ff4f01b8fb8789bd24ad961`。 |

三输入的19个Web源及29个dist哈希完全相同；API、owner、page阶段按其精确输入和最终源字节复用。新六组只复用new01的Lifecycle/Outcome，其余四组在input03/new02通过，不称同一轮全绿。1030个基线源从固定Git重建，25候选与历次纯测/私有probe只存必要版本；依赖树、dist实体、缓存和二进制不入档。

README保留[首份final-delivery原清单](system-models-ui-verification-evidence/objects/11d6ea4832c212b23dd14211af386481ef539169e117138ffd0d66ff6a99a0ef.json)及[最终单处差量](system-models-ui-verification-evidence/objects/bf8658f71f93dcd8875850b9ef47a206be23f35d38ea56dade51beab9406fabe.txt)：主线程只收紧“引用变化必然拒绝”的表述。相同Model版本下新增合法引用可以改变最终affected_refs，DELETE仍在事务内重新发现并裁决；显式版本发现或版本/候选不兼容拒绝才要求对应复核。无业务源变化，不把首稿描述成整份被拒；最终README本地26链接、解析、原16组选择器和格式检查通过。

## 2. 实际执行与终局

所有原command均保存实际argv/cwd/选定env覆盖、开始结束、actual wait及退出码；raw保留原样。下表PID按所属PID/starttime观测计数，wait为实际adopted wait；各轮均7个exact资源双absent、原2容器/4网络基线不变、源码/dist/1030基线及验证输入不变、monitor0、所属进程清空、runtime与短浏览器目录终局为空/移除。

| 轮次与原命令 | 实际退出/秒 | 真实顶层结果 | 所属PID / wait |
| --- | --- | --- | --- |
| [作者new01 / input02](system-models-ui-verification-evidence/objects/db2712e120657e7cf800699f7bfa8cf8cafabb66c0a1fd5be7d6b6de02d25aaf.txt) / [command](system-models-ui-verification-evidence/objects/8221ac8f93b210b90d04e0d29a5b147db17077f767277fd499a623195f1be065.json) | 1 / 201.587 | Lifecycle32.17s、Outcome15.43s PASS；Deletion、Read、Authority、Navigation四个全DTO比较原红保留。 | 188 / 24 |
| [作者new02 / input03](system-models-ui-verification-evidence/objects/184c63e218b7eff9eeddc8d179d2e355a54b36054f21b53629c46c78cbe1458f.txt) / [command](system-models-ui-verification-evidence/objects/2fae5f95e50cc5c7bff4f28a8c2d7b490cf0781adc7dddc2fb461356ac777e20.json) | 0 / 124.100 | 原四组分别24.65s、16.62s、16.10s、15.53s PASS。 | 124 / 16 |
| [作者old-core01](system-models-ui-verification-evidence/objects/6b3a6d2901f65c6ba8f9f5501cf74f8642c5389e71e215409c7ba53a4080a618.txt) / [command](system-models-ui-verification-evidence/objects/faae5564716ca9ec75f41535867709ff717bd940a25b638725bebe06afda8d93.json) | 0 / 125.801 | 公开入口1、认证1、个人设置1、邀请2、系统用户目录2，共7 PASS。 | 169 / 28 |
| [作者old-provider01](system-models-ui-verification-evidence/objects/cf0ac3c9476ce977acdf2f43271bef1181b8043c4cef04d545254702bbcfadce.txt) / [command](system-models-ui-verification-evidence/objects/e450ba9e7190baf1a7bd07e499a7c1152fc601beb21993afdae189012f8208f9.json) | 0 / 88.181 | CredentialReplacement、OutcomeRecovery、NavigationAndLayouts，共3 PASS。 | 112 / 12 |
| [独立independent01](system-models-ui-verification-evidence/objects/8a003a57724ce1b4db90d552e620752ade02391aee780230585052003a4e2cdb.txt) / [command](system-models-ui-verification-evidence/objects/e84fd23bf41462310067d929207b826ae5bf5ec80b00b0feb0f1da39a6fa565d.json) | 0 / 155.994 | Replay9.58s/browser5.0s、Replacement10.44s/browser6.0s，首轮两组均PASS。 | 212 / 8 |

独立原raw SHA256 `8a003a57724ce1b4db90d552e620752ade02391aee780230585052003a4e2cdb`；[cleanup](system-models-ui-verification-evidence/objects/a6f4b46dceb76bded757632939650ab65b3aa977c7cec60fb4ef131e998d9677.json)两扫时间为11:21:16.827356与11:21:17.524922 UTC。[终局result](system-models-ui-verification-evidence/objects/dca6d20cd1defada2fdcb8ac54d5427bbf3be110a712c8ccc5e6f69d08e2a3e6.json)记录无活动资源，窗口已释放。历史既有PPID1 Z按原身份保留且未触碰，不算本轮已wait或被回收；双清只覆盖各轮自有资源与进程。

Replay实际经历none预览→正式新增可选reranker引用且Model版本不变→接受响应截断→Session/lookup/当前GET404均不作receipt→显式原key/body/CSRF Execute重放返回affected_refs1。Replacement实际禁用跨Provider候选后收到400/not_started且无部分写入；明确复核、同身份Session503再200恢复待决确认与原生焦点/Tab，再以新命令完成required memory原子替代。Go在浏览器实际wait后核安全receipt、唯一command/Audit/event及selector/反向索引；只记录安全相等布尔值，不保留材料值或hash。text/plain503不声称reader EOF，成功恢复200按其实际证据判断。

作者唯一外域未绑定负例仅在自有隔离库、正式目标Model下插入一条Model自有反向索引以模拟agent引用：UI真实blocker且零DELETE，fixture另作正式HTTP DELETE503并核零副作用；精确所属行条件清理，不声称Agent产品或外域adapter存在。平台selector引用全经正式GET/PUT，不以SQL伪造。该负例与独立两个正式selector代表场景分列。

## 3. 纯测、静审与原失败

作者[最终web-check02 raw](system-models-ui-verification-evidence/objects/c31e94c78eeab7ca8a505a7155bf6876107f21d8193d74c8def8b1557e338a09.txt)为829测试/26文件及format/type/build通过；harness类型、编译、vet、baseline/observer/serializer检查保留原结果。独立[API 16/16](system-models-ui-verification-evidence/objects/7af8f51724395f555534570ac5e7f32a8929c4e555643386d428e170347bee0a.txt)、[owner 8/8](system-models-ui-verification-evidence/objects/01ee2ddface188662ce54e17a15bd9d0abca238af9fa00bd1a6d83322b98af2a.txt)、[page/App 5/5](system-models-ui-verification-evidence/objects/f53f7ab119fbabb96e8637b98cc9bdd19ab798d8480eef2ca04c516a33b0d39e.txt)按未变源复用。API真实reader/cancel、超界600001/最大600000字节与owner实际Promise尾部屏障属纯受控传输证据；jsdom页面布局替身不等于原生焦点。原生焦点/Tab来自上述真实代表组，浏览器观察器本身不替代owner-finally证明。

| 原失败/阻断 | 分类及修复后证据 |
| --- | --- |
| [page-state01](system-models-ui-verification-evidence/objects/fe74e95dc97b381c2c348855d394e046778ab4ba2d2a9b32cff88897cde83a95.txt) | 两处产品红：陈旧替代候选有效性未撤销、已离场编辑器的晚到复核发布旧版本。另有“可见取消”和实际尾部观测前提；不得合并删掉产品红。 |
| [page-state02](system-models-ui-verification-evidence/objects/e0a27f0af0bce5f19f5faf3f7946696b2ee5b2460ff67a927e4738d15d9714d3.txt) | 第三处产品红：首次删除预览被当成显式版本复核，静默采用更新版本。[原断言page-state03绿](system-models-ui-verification-evidence/objects/664ecd8a5837cfe7a62f0c0f876a22739c9dcdc2cd23b207c8a81186331b334c.txt)、完整829及独立page通过；独立未重现旧产品红。 |
| [harness static01](system-models-ui-verification-evidence/objects/b3d31499fcb39a7f8a413c772c4782d25f834d275222f9937310057eb890c1b6.txt) | 错误409预期与运行期只绑定36源两项静态前提；[static02](system-models-ui-verification-evidence/objects/a4eb739a7117675bf71054445c719ff3166ea2c821e9414005d4eedc2bf6aa4f.json)修400/not_started和完整1030基线后PASS。两项都不是已执行的真实产品失败。 |
| 作者new01四个全DTO比较 | 原轮未记录现场字段级差异。后续[真实httpModelDTO serializer03](system-models-ui-verification-evidence/objects/0f0bbeea81a43875773cbbd2e461a0e37847bef464de6385041bb7d6f56e0413.txt)五种形状及固定源码证明缺省count/nil slice与wire必需null/array的差异；[static03](system-models-ui-verification-evidence/objects/f2cacc67d6f3e321733594128ee964b44136e81801c850e8e0f5daee70ff5cb6.json)核input03仅投影该形状、全DTO断言未弱化。后证不得改称原轮现场诊断。 |
| 初期API/owner/page pure、类型及旧页面检查 | year0000/UTF8边界、union/ES2022类型、cursor/restore-busy、label/resetSession、SessionView字面量以及旧目录返回项等原前提/类型失败均保留input/source/raw/result，按阶段索引定位；与三处页面产品红分开。 |
| [独立type-bound01原红](system-models-ui-verification-evidence/objects/0d6d47ccdaf3b38777d46a9fa8b7e52d8cf2a98f7a1a5c712b06b65168a42c63.txt) | 私有probe的unknown enum收窄准备错误；保留bound01/02/03源，闭合enum修正后类型/编译/list通过。它不是产品红或真实浏览器红；独立真实首轮即两组通过。 |

早期作者pure/check没有完整同期env快照，作者确认无额外现存原件；保留原argv/cwd/raw和单独锁/工具版本，不拼装完整历史env。原README末件之前报告中的pending状态不覆盖本页绑定的最终接受。

## 4. 图像与适用边界

作者实际查看new02全部8张light/dark × 1440/1024/834/390图；主线程实际接受代表[light1440](system-models-ui-verification-evidence/objects/113a70ac006bf913f49c0f0301f71c2a17ecc6d5d4853320db5a0ad93c1658d4.png)与[dark390](system-models-ui-verification-evidence/objects/f821fd3b353e4c7fc0a37178249f123c29b24d64948f83e52d1167bff9debba2.png)，其余图在索引中。长页下部采用正常纵向滚动，真实几何/可达性断言通过；本归档未再运行截图或浏览器，不声称独立新增图片、native zoom、真实BFCache、其他引擎、生产SPA部署或真实Vite代理验证。

本卡只接受既定配置和管理结果，不覆盖外部Provider调用、凭据取回、外域Agent/Project生产者、完整SMTP或全UI重跑。完整D09/D26/D27及D08–D28/E01仍未完成，E01未开始；Summary待决，Object/tools历史任务继续停止，Artifact/Project及生产未绑定、ready503等旧边界保持。

## 5. 离线复核

[证据说明](system-models-ui-verification-evidence/README.md)与[索引](system-models-ui-verification-evidence/archive-index.json)保存794个逻辑原件、302个去重对象，共4515988字节。原路径仅为来源标签；离线脚本只读档案和本地固定Git，核26交付源、1030基线、三版输入、阶段/原失败源与五轮实际退出/双清，不读随后Selection活动树，不执行保存的命令或产品。

```sh
env PYTHONDONTWRITEBYTECODE=1 GIT_NO_LAZY_FETCH=1 python3 docs/development/agent-team/system-models-ui-verification-evidence/verify_archive.py
```

本轮实际离线结果：PASS。报告、入口与卡页首检查UTF-8/LF/尾换行、空白、相对链接和技术正文保持；旧文档原正文原字节保留。
