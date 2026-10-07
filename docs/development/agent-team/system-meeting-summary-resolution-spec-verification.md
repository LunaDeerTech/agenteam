# 系统会议 Summary current-resolution 规格验收

2026-10-07，rev1 获独立完整有界 STATIC PASS 并由主线程采纳。最终[工作卡](../work-items/d09-system-meeting-summary-resolution.md)已提交推送 `bd94a1841a6978a1909b9fa3625bbd2c83ef8d0d`，远端一致由主线程核实。此结果仅接受 S3 规格，没有测试、资源或产品接受，也不完成 S2/S3/D24、完整 D09 或 E01。

固定结论见 [独审报告](system-meeting-summary-resolution-spec-verification-evidence/objects/7c6a9805c2cff3099910cac604705bce9663e931f7aab02c9d2df2236d74c15a) 与 [结果原件](system-meeting-summary-resolution-spec-verification-evidence/objects/3399eb65e165ad6444f2eea9bdd8dce50a81a968a45f5a242406bbb5ca23dbfb)，实际产品依赖为已接受 S1 `c210d249600d98871513c56fb9a8fff7c50a4c34`。本归档只读取冻结原件和指纹，没有再次读取活动 S3 产品候选形成结论，没有运行 Go/Node、编译、应用、listener、PG、容器或浏览器。

## 固定版本与原断言失败

| 输入 | SHA256 |
| --- | --- |
| 独立受审 rev1 候选 | `00f54620a1013e8cb121736c0d125b23c6afa851950d6bb11b05a06ca22af3d8` |
| root 采纳后的最终卡 | `bb124b229914855a99980f21542f988b69a26942a0a264405d4e7f3bf98a2d1f` |
| 独审最终报告 | `7c6a9805c2cff3099910cac604705bce9663e931f7aab02c9d2df2236d74c15a` |
| 独审结果 JSON | `3399eb65e165ad6444f2eea9bdd8dce50a81a968a45f5a242406bbb5ca23dbfb` |

[受审卡](system-meeting-summary-resolution-spec-verification-evidence/objects/00f54620a1013e8cb121736c0d125b23c6afa851950d6bb11b05a06ca22af3d8) 与 [采纳卡](system-meeting-summary-resolution-spec-verification-evidence/objects/bb124b229914855a99980f21542f988b69a26942a0a264405d4e7f3bf98a2d1f) 原件仅页首状态不同，[页首差量](system-meeting-summary-resolution-spec-verification-evidence/objects/aa881c7eea48709b311c436088c25257a9bd8d3b2a05ec64db5801f196d6bd90) 保存实际差量；从 §1 起完整技术正文 SHA `f34de8333b80d8e6d68ad702ea42918fa22950278b2e9367f40c1a8cf215745b` 相同。作者 [作者freeze](system-meeting-summary-resolution-spec-verification-evidence/objects/81faf1cd9e33e9fae7fc8afe205a12d88a8030a60490b671b058e227582ca7ed) 另行绑定原候选、最终卡和页首 diff。

独审收尾“候选全字节未变”断言曾得到 `AssertionError, top-level Python line5`：root 已授权的页首采纳更新在收尾时到达，原断言不再成立。[close-first-failure.json](system-meeting-summary-resolution-spec-verification-evidence/objects/1d8b548a5ba4ebc8da694705803ecfd74796bf67cbad27c0385987f5b54ab114) 与 [页首更新前报告](system-meeting-summary-resolution-spec-verification-evidence/objects/4f13ad616b41ac1a3381b121e2640ae15e7e5f7f5928ff909b1cbe893b50a560) 保留该失败及原报告，随后核实技术正文完全不变，再绑定最终卡。它是检查时点与授权文档更新的差量，不能写成产品失败，也不能冒称候选全字节始终不变。另一次猜测的 `project_fixture_test.go` 不存在，原 discovery 记录保留，后定位实际 `project_configuration_fixture_test.go`；没有规格返修要求。

## 被接受的规格边界

本卡只扩现有 current-resolution 库：两个 Meeting Purpose（initial/update）从 `platform.meeting_summary` 读取独立 selector/version，在真实当前 Project 授权、受信 Meeting/Operation 事实及完整事务锁内生成不可变 snapshot、consumer binding 和适用 Secret lease。首轮标题属于 initial 的同次普通 text 生成，不新增 Purpose、selector 或 Agent Execution。

四个 Resolver 方法、ConsumerAuthority 公共口及持久 DTO `format_version=1` 保持；迁移前缀仍为 00001–00020，无新迁移、字段或 backfill。旧 direct/memory 和 `project_summary` 的原拒绝语义不改，不做 Project override、默认复制或 fallback。原未初始化 Summary 的旧配置场景继续支持其原分支；S3 不把新 initializer 接入旧 Initialize。

审查确认的关键闭合点：

- 新分支复用完整 Summary loader，区分技术 singleton 缺失、已初始化未配置及坏事实；校验 Summary 独立 version。旧 singleton SH 仅用于 loader 所读的 owner ID 技术校验，旧四用途 configured/value/version 不成为 Summary 前置。
- 普通 text 请求不等于强制 text-v1 snapshot。保留真实 capabilities：plain 使用已接受 text-v1，含 json_schema 使用已接受 structured-v1，未来 Summary 请求仍为 text。原 protocol/profile 子集限制和明确错误保持，不删 capabilities、不扩 wire、不缩窄 S1 配置准入来掩盖不支持。
- Select 只提供当前 Human Session/Project Read 的安全投影，不证明 Meeting/Operation 存在或允许生成。实际 Resolve 仍要 ConsumerAuthority 在当前 Tx 验证事实。管理员配置权、Project Owner 或一次旧 Select 都不能替代消费授权。
- 两个锁入口加入 Summary SH，同时保留 loader 实际所需的旧 singleton SH。Project/User/Model/Provider/references/consumer/Secret 完整锁并集、统一排序、同 Tx 当前授权及缺锁/弱锁/错 issuer 反例不减少。
- 相同 CallID 解析单位不能更换 Purpose、Meeting、Operation 或 initiator。prepared 沿原规则重新规划，committed 重入恢复原不可变 snapshot；同用户新 Session 必须当前授权并重新规划，新逻辑调用才读取当前选择。
- planned Secret Acquire 保持 exact-Tx active/verified witness、canonical lease、无凭据零调用、原子回滚及 provisional 发布边界。Unknown 计划只声称真实事务后的结果装饰、writer 屏障和后态，明确不冒称物理 COMMIT ACK 丢失。

S3 不读取 Secret 材料、不发 Provider 请求，不创建 Meeting/Message/Turn/Invocation/Usage 或后台恢复。D24 仍负责真实 Meeting provider、输入捕获、逻辑调用身份、有限生成/校验重试、标题/四字段解析、Turn finalizing 与原子提交；生产 `Authorizations.Resolution` 继续真正 nil，`Invocations`、ready503 和三个停止任务保持原边界。

## 候选路径、验收门槛与并行所有权

[静态增量输入](system-meeting-summary-resolution-spec-verification-evidence/objects/7631a816750f98a151b231afad9a555faac783e63a044826649f6846ec96fece) 固定16个技术路径：10个既有、6个新测试。5个新真实顶层与8个既有回归已在规格中明确；旧测试名称和接缝可定位，新测试仍只是规格要求。正式身份工作流、受信当前 Meeting 行事实及旧 convenience fixture 的局限写明，不把 always-allow fixture 当 D24 provider 已实现。

未来实施须冻结完整 import/embed/TestMain/动态命令闭包，offline 每命令45秒、新top每项120秒且含 Cleanup、原包6分钟预算，独立两组、唯一资源窗口、实际 wait 与双清均是后续门槛。本次没有执行这些测试，也没有证明其运行通过。

[S2/S3所有权原件](system-meeting-summary-resolution-spec-verification-evidence/objects/b4d5f8409d27eff3b2bc9a5576a2ee20db49dc62aa4f8f36933b1f256a844ab2) 针对 S2 冻结 rev1 SHA `1734a1a7e0074db41cc1e3171f805e7107a35efc789d6c2b78437cfb54ae9772` 的31候选路径，与 S3 16技术路径交集为空，并由 S2 独审者确认。两者仍共享 `tests/model` 的实际编译包：验证前必须冻结完整共同包及动态图，不能读取另一作者活动测试；文件无交集不授重叠资源窗口。backend README 是共享末件，须由 root 单独串行交还，未计入 S3 16路径权限。S2 若调整路径，只补核变化后的交集。

协调时点另记：STATIC 接受之后，root 已另行授权 `next_frontier` 唯一负责 S3 16路径实施与准备，未授权资源。该后续工程调度不改变本次 STATIC 证据，不表示实现、编译、测试或资源已接受；此归档不把原规格时点的“未授实施”写成当前永久未授权。

## 原件与归档检查

[source-map.json](system-meeting-summary-resolution-spec-verification-evidence/source-map.json) 按来源和 SHA 去重保存候选/最终卡、页首差量、准备输入、静审输入增量、所有权核对、最终报告和原失败。必要产品输入沿独审的固定 `c210d249` Git读取/hash记录定位；未复制源码树。四份关联固定规格/架构原件只列指纹，避免重复文档全文。

独审实际执行的是冻结 hash、固定 Git 文件读取/字节对照、8个本地链接存在性及源码接缝/测试名称静态核对。本归档另做[数据与格式自查](system-meeting-summary-resolution-spec-verification-evidence/archive-checks.json)，不重复独审的产品读取或动态验证；本归档未修改产品、卡片或入口，未执行 Git 操作。
