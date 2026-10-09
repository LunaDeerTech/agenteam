# Knowledge B02 有限独立补集恢复点

本树 `/workspace/agenteam-knowledge-independent`，`ai/knowledge-service-independent`，产品/原作者测试基线 `f3ce0c7a`。独立执行者 Skills 未参与 Knowledge 产品实现。本轮仅新增 `tests/knowledge/b02_independent_content_test.go`、`tests/knowledge/b02_independent_tree_reference_test.go` 和本摘要；不改产品、原 fixture、旧作者测试或 harness。前序作者记录保留在基线 Git 历史及 Knowledge 作者树，不将其作者 PASS 改写为独验。

## 当前片段

两 exact top 为 `TestKnowledgeB02IndependentContent`、`TestKnowledgeB02IndependentTreeReference`，各三 direct 子组，候选 selector `^TestKnowledgeB02Independent(Content|TreeReference)$`。当前已完成两新源候选：race-c45237 actualexit0；9ccae9 discovery actual0 精确两 top；vet67704 actualexit0；gofmt 与 56e9b6 diffcheck actual0，实际仅本三路径 dirty。新候选 `output/ai/knowledge-independent/knowledge-independent-race.test` 为 36,862,944 B。尚未实际 PG、尚未增加 harness 映射；无测试/资源执行在途。首 race-c91846 因 Content 未使用 oc import 编译 FAIL，原结果保留，移除后才通过；未执行任何用例。

- Content：有效 DOCX ZIP 原字节/实际 canonical reader EOF+Close，声明短长长度和 SHA 错误不产生发布事实；真实 D05 Send 完成、real Outbox PrepareAppend 返回后，同 User EX 锁撤销上游 Session，final gate 必须拒绝，后继有效 Session 用新源恢复原 key；真实发布 Event 与 Delete Audit 正控，对公共合法 Event 缺原 command_event、精确公共 Audit 缺原 Tx 私有 witness 均拒绝。
- TreeReference：真实正文替换上传后插入真实 Move，final/replay 不覆盖当前 parent；preview 成员真实移出/移入、count 同值但旧 scope 拒绝，fresh scope 只删当前成员；真实 PublishVerifiedInTx 返回的原 opaque receipt 在替换后不能 Consume/Attach 复活，精确 Cleanup cause 重放与 wrong operation/reason 对照。
- 正向 canonical/reference/upload/Audit/Event/command facts 来自真实 Knowledge+Object+Audit+Outbox API。直接 SQL 仅读取事实，以及明确上游 Account/Project seeds 与持 User 锁 Session 撤销刺激；不造 canonical、claim、receipt、cleanup authority 或 private witness。复用已审 `newPublicationFixture(nil Runtime)` 同 Store 组合，来源实读/Close、原 calls Drain、reader lease/work active 零均检查；不声称 Runtime/ProcessGuard/Project Create/真实 Login。
- 允许失败后的 planned command、上传/退休记录保留，检查的是无新增已发布事实，不用总库零行否认正式恢复账本。普通 source.Close 收尾不替代原业务结果。Create prospective upload 在有效时 bare Attach 原已 Forbidden；原 receipt Consume 是正控，正文替换的 ExistingOwner active upload 是 Attach 正控，替换后原 upload 两入口都应 ResourceDeleted。

## 固定离线命令

已实核以下两个缓存目录存在。GOCACHE 此时由本执行者独占；modcache 复用固定现有缓存，禁止下载，Go 路径前置继承原 PATH。先创建本树 `output/ai/knowledge-independent/tmp`，不复制或清理其它树 cache。

```sh
PATH=/workspace/toolchains/go1.27.1/bin:$PATH \
GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOTELEMETRY=off GOMAXPROCS=2 \
GOMODCACHE=/workspace/agenteam/output/ai/model-ui-recovery/go-mod \
GOCACHE=/workspace/agenteam-project-variables-independent/output/ai/project-variables-independent/gocache \
GOTMPDIR=/workspace/agenteam-knowledge-independent/output/ai/knowledge-independent/tmp \
/workspace/toolchains/go1.27.1/bin/go test -mod=readonly -tags=integration -p=1 -race -c \
-o output/ai/knowledge-independent/knowledge-independent-race.test ./tests/knowledge
```

当前没有 PG/browser/socket/Git 网络授权；唯一真实窗口归 root 调度。本树 candidate 后续应只增加上述唯一封闭两 top 映射并独立窄审，真实执行仍需 fresh grant、原 Go6m/root540+60+3/TCP75/7 精确资源/actual Wait/全部尾；不依据 offline compile 认六组实际通过。

## 复用与未扩大边界

Knowledge P1/P2 原独立15866结论复用，不重复旧纯测。作者 Process50756 已报告完整 PASS（Go2.40s/test1079008 Wait0、driver1077003 Wait0、outer726572 exit0、7IDs14absent/private/runtime/desc/TCP/input全尾，原日志位于作者树 `output/ai/knowledge/pg/pg-d776f88b697049b1853a3f24616ca380.log`）；本独验尚未复核该日志，不重复新进程矩阵，也不回填旧91700 FAIL。其它作者 10top35sub、先前 Runtime/Cleanup 结果仍绑定各自实际源与 binary，不称本补集 PASS。

下一步：两新源+本摘要已冻结交 root 保存；随后优先 Work 普通完成方法 `5a49197a..0ad6e5b6` 四 TS/三 CJS 限定技术独审。独立补集真实窗口另排。
