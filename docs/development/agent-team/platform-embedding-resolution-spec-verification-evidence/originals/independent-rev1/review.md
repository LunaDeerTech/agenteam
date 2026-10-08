Platform embedding Resolver rev1 完整 SPEC STATIC：CHANGES REQUIRED，D1/D2 两项，STOP。

固定卡 c531d6e746bdb266509f1a9719651c8d02455869a50cee199f0e2e3aabf39d17（23158B）；freeze fadabece6cbec323232cdfa8db526a2c3138b8779050aef460cc7b1502a9dadf。已通读全卡并核冻结四原件，复用前阶段 Resolver/S3/Embeddings 正式来源，只补必要源码；11个关键来源实际匹配作者来源指纹，未重扫49项或构造运行图。

D1，卡第57行：direct“只保持原Agent用途”错误收窄既有契约。已接受 recovery-d09-current-model-resolution.md:50 明确是合法 agent/tool chat 用途；C0 SelectionRequest.Validate 只排除 Memory/Meeting，ToolConsumer/ApprovalAuto 是原合法 direct，resolutionVariant 也没有 Agent-only 限制。最小修正为完整保留原 C0 合法 agent/tool（含 approval_auto），在本卡已列新 pure 的旧分支对照中核该代表；不增公共接口、生产绑定或写入路径。

D2，卡第69行：“InTx只核…不能…发I/O”需明确所禁为 Provider/外部网络 I/O。现 ResolveModelInTx:359 必须读 preparation，:399 completeResolution 必须落 snapshot/binding，:402 必须执行同Tx planned Secret Acquire；全卡也要求其原子提交。改为先核同Store/活Tx/完整持锁与绑定，沿调用者同一Tx执行必要SQL读写和Acquire，禁止补锁/内层Tx/Provider或外部网络I/O。此项是消除事务规则歧义，不是产品缺陷或新增能力。

其余规格未发现必修。两个 purpose/Actor/owner、Human Select与真正接受的层次、完整float profile（含MaxOutput=nil）、System与四用途version、当前授权及历史不可变、lease/Tx/Unknown均与既有契约接缝相符。prepared表述已准确：resolver.go:204–205仅在draft不同后增加Model plan_version；consumer-only mapping变化由authority重验，使旧plan失效而不保证Model版本递增。00017通用format1与现DTO/ResolvedModel支持无迁移前提，仍需以后实际持久化验证。

8技术＋README9白名单闭合；旧9个真实top、旧5个pure及两adapter复用top名称有实际来源。严格关系式fixture、正式身份、input/operation/current授权、同Tx副作用与失败检查目标、独立A/B增量、45s离线/120s含Cleanup/6m包/20s或更早内层及完整退休边界均明确。当前分析不证明新测试实现或执行通过。维度、serving、D13/D14 consumer、材料/网络/Invocation/生产Resolution仍未绑定，三停止和Jina边界不变。

未执行Go/Node/browser/SQL/网络/资源/Git，未修改产品或仓库文档，只写独立scratch。原rev1和作者检查原件保持；待两个窄文案及兼容断言要求修订STOP后，只复核其差量与最终组合。
