# Knowledge B02 有限独立补集恢复点

本树 `/workspace/agenteam-knowledge-independent`，`ai/knowledge-service-independent`，产品/原作者测试基线 `f3ce0c7a`。独立执行者 Skills 未参与 Knowledge 产品实现。本轮仅新增 `tests/knowledge/b02_independent_content_test.go`、`tests/knowledge/b02_independent_tree_reference_test.go` 和本摘要；不改产品、原 fixture或旧作者测试；后续root另授权本树唯一两top harness增量，范围见下节。前序作者记录保留在基线 Git 历史及 Knowledge 作者树，不将其作者 PASS 改写为独验。

## 当前片段

两 exact top 为 `TestKnowledgeB02IndependentContent`、`TestKnowledgeB02IndependentTreeReference`，各三 direct 子组，selector `^TestKnowledgeB02Independent(Content|TreeReference)$`。原race-c45237 actualexit0；9ccae9 discovery actual0 精确两 top；vet67704 actualexit0；gofmt 与 56e9b6 diffcheck actual0。原候选 `output/ai/knowledge-independent/knowledge-independent-race.test` 为 36,862,944 B，绑定两新测试2578a9ef，工具冻结f285be16。首次真实89530整体FAIL、全部实际资源尾完整，见下节；失败摘要已root保存b7798cd5。后续仅最后一子窄修的新candidate67286已离线编译，未PG、待未参与者窄审，见修复节；当前无执行在途。首 race-c91846 因 Content 未使用 oc import 编译 FAIL，移除后才通过，原结果保留。

- Content：有效 DOCX ZIP 原字节/实际 canonical reader EOF+Close，声明短长长度和 SHA 错误不产生发布事实；真实 D05 Send 完成、real Outbox PrepareAppend 返回后，同 User EX 锁撤销上游 Session，final gate 必须拒绝，后继有效 Session 用新源恢复原 key；真实发布 Event 与 Delete Audit 正控，对公共合法 Event 缺原 command_event、精确公共 Audit 缺原 Tx 私有 witness 均拒绝。
- TreeReference：真实正文替换上传后插入真实 Move，final/replay 不覆盖当前 parent；preview 成员真实移出/移入、count 同值但旧 scope 拒绝，fresh scope 只删当前成员；真实 PublishVerifiedInTx 返回的原 opaque receipt 在替换后不能 Consume/Attach 复活，精确 Cleanup cause 重放与 wrong operation/reason 对照。
- 正向 canonical/reference/upload/Audit/Event/command facts 来自真实 Knowledge+Object+Audit+Outbox API。直接 SQL 仅读取事实，以及明确上游 Account/Project seeds 与持 User 锁 Session 撤销刺激；不造 canonical、claim、receipt、cleanup authority 或 private witness。复用已审 `newPublicationFixture(nil Runtime)` 同 Store 组合，来源实读/Close、原 calls Drain、reader lease/work active 零均检查；不声称 Runtime/ProcessGuard/Project Create/真实 Login。
- 允许失败后的 planned command、上传/退休记录保留，检查的是无新增已发布事实，不用总库零行否认正式恢复账本。普通 source.Close 收尾不替代原业务结果。最后一子原计划以 Create 原 receipt Consume 和正文替换的 ExistingOwner active Attach 为正控，再拒绝被撤销旧 upload；89530暴露其把 Publish 返回结果当作有效 receipt 的前置假设错误，后续正负例均未到，不能认该计划已实现有效验证。

## 首次真实结果：89530 整体 FAIL，完整尾已释放

恢复期间几次 queued grant 均在启动前撤销，没有真实 exec；只读恢复18c819确认无本树PG目录、无自有live。收到新的明确grant后，首actualexec456209在同process检查 available=5,566,189,568 B、候选大小、固定MinIO SHA、Go1.27.1和完整offline/readonly env，再exec下方原root入口。没有重编或改运行输入；沿用Go6m/root540+60+3/TCP75/七资源。

- Content三子全部PASS，top2.92s；TreeReference正文/parent竞争与同count成员交换两子PASS，top3.67s，但`revoked_original_receipt_cannot_reopen_reference`在239行`capture was not exact committed original upload`失败。只到Create、源Close和publicationFacts；尚未到原Consume/Attach、正文替换及被撤销upload、错cleanup cause、该子末尾joined检查。五个已过子不能认整补集接受。
- Go PID1129999 actualWait1，driver PID1128018 actualWait1；outer89530由295e37取得actualexit1。04ccfd实读完整尾：七个精确ID各两次absent、private双absent、runtime双empty、owned descendants双[]、exact2tops/actual_test_wait真、HOST_TCP两次delta_empty、inputs_unchanged=True；supervisor100.984s terminal1。root已接受窗口释放，无自有live。原日志为`output/ai/knowledge-independent/pg/pg-0b6312f929ee40b88a378716c8dfd519.log`；忽略目录仅保留owned/request和已空runtime，不复制原日志入Git。
- 只读源码定位：Knowledge `commands.go`先`storePublishedDocument`再`PublishVerifiedInTx`，`object_authority.go`因此返回ExistingOwner；D05 `upload.go`将这同一个原prospective upload转attached，按正式行为返回空Receipt。Knowledge随后已校验Put.Meta.ID与canonical一致；新测试错误地假设其仍返回可用原receipt。原239未分采两个布尔，不能回填其动态值；尚未改变产品或测试，也未把后继替换upload与原upload混同。下一步只核真实可达的原receipt获取方式，不通过SQL补造receipt/claim/witness，不把无法到达的前置改成通过。

## 最后一子白盒前置修复（仅离线候选）

root明确授权只读真实原uploads行，用公开`NewUploadReceipt`投影原持久身份；它不是声称API曾返回的receipt，也不插入/改写事实或私有witness。原D05 `LookupPut`在attached状态同样不返回receipt，不能用换Lookup掩盖原错误。依据D05设计§5和实际`references.go`，同owner/原稳定actor/creation cause/当前正式授权下，已canonical且未revoked的prospective上传可携原身份幂等Consume；bare Attach本来要求receipt而Forbidden，因此不能把原bare Attach拒绝当撤销证明。

只改`tests/knowledge/b02_independent_tree_reference_test.go`最后一子，改名`revoked_persisted_public_receipt_identity_and_old_attachment`：

- 正式Create A返回Object必须与canonical相同，且PutResult.Receipt显式为空。只读真实uploads与真实Create command相联的receipt_id/upload_id/object_id/creation_cause，核原User/Project/Document/command key/completed和prospective+attached+committed，再调用公开constructor投影。没有造新receipt ID或写SQL。
- 先在实际完整Access plan/原Store Tx和Knowledge authority下成功Consume A，原同一canonical/reference/业务事实不增。A的bare Attach保持正式Forbidden，明确不将此当撤销正控。
- 正式Update B为ExistingOwner，实际Attach B成功且不改事实；再用一次必要Update C真正撤销B，使随后旧A Consume和旧B Attach各自都有同一对象的撤销前正向。均需精确ResourceDeleted，不能用任意拒绝冒充撤销。
- 保留原A的真实cleanup cause重放、错operation/reason拒绝及原上传精确绑定；两个旧对象references0/revoked/cleaning与单一cleanup事实不再变，最终C唯一canonical、三次发布各一次Audit/Event/command、实际source Close/current reader EOF+Close/lease与call Drain尾保持。

67286（faf08c/c4755d）race-c actual0，新候选`output/ai/knowledge-independent/knowledge-independent-receipt-fixed-race.test`为36,877,449 B，旧45237未覆盖；3e4853 integration vet actual0。f4e507实际list精确两top，并核原Content三子和TreeReference前两子源码逐字未变、产品/迁移无diff。使用本页完整Go1.27.1/offline/readonly/独占cache env，构建命令仅将`-o`换新候选路径；3b2587 gofmt/diffcheck0。未执行任何修后PG，原89530五子证据只复用未变范围，整个独立补集仍未接受。

本树工具仍只封闭原两top组合，没有暗加单top/子selector或skip。后续只补最后一子实际所需的精确工具映射由root另协调，原七资源/预算/全尾不放宽。当前仅最后一子源与本摘要两路径冻结供保存及未参与者窄审；没有自有live或真实资源。

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

## 唯一两 top harness 映射（有限独审接受）

root保存两新test/current为2578a9ef；其后授权仅本独立树工具增量，作者Knowledge树未动。原 `root_chain_driver.py` TARGETS新增 literal `^TestKnowledgeB02Independent(Content|TreeReference)$` → tests/knowledge；supervisor expected新增同两top，并只对该新selector要求实际顶层出现次数恰2，拒绝重复，旧selector不变。没有开放各独立单top、通配、错序、缺项或更多top，没有failfast。Go6m/root540+60+3/默认123+3/TCP75/七精确资源、实际Wait、双private/runtime/desc、闭集TCP诊断及input门不变。

`python3 .agent-state/knowledge-independent/selector-controls.py` 125bd3 actual0/32控：逆除必要增量两工具全文逐字2578a9ef、所有selector两表相同、原预算；实际configuration正向+五个拒例均不创建runtime；实际observe_root_chain在受控精确owned manifest下1正5负（缺项/多项/重复/缺actualWait/错selectorWait），每格14次资源观测与双private/runtime完整，未把mock资源当真实回收。新test binary36,862,944 B保持45237原编译输入，不重编。

Runner未参与者独审e060b4原32控与9b3408六补控actual0，反序/子RUN正向及重复、absent=false、Wait PID0、private/runtime残留负例全部符合原门槛，每格14次观察/第二尾可达；两工具逆投影逐字2578a9ef。仅接受封闭映射，不代表业务两top通过。四路径随后由root保存f285be16。

独立树初始无MinIO cache；只建立忽略路径 `output/ai/deps-minio/bin/minio` → `/workspace/agenteam-skills/output/ai/deps-minio/bin/minio` 本地只读复用链接，没有复制大文件或下载。1d0cbb实际SHA核为 `dc5298474f0bc87a068f0b1135c583bb1278c17c11c512212ed7644a238c89c8`，actualconfiguration再次读取该固定身份通过。它是二进制缓存入口，不是fixture/runtime资源，真实启动前仍检查固定身份。

未来真实入口如下，仅在root明确freshgrant后才可执行。第一实际exec应在同一完整env先statvfs≥5368709120，否则exit78且不建资源，再exec下方监督器；此处只记录命令，没有启动。

```sh
# cwd /workspace/agenteam-knowledge-independent/tests/knowledge
env -u AGENTEAM_PG_FIXTURE -u AGENTEAM_PG_UNSUPPORTED_FIXTURE \
 -u AGENTEAM_OBJECT_FIXTURE -u AGENTEAM_OUTBOUND_FIXTURE \
 PATH=/workspace/toolchains/go1.27.1/bin:$PATH \
 GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOTELEMETRY=off GOMAXPROCS=2 GOFLAGS=-mod=readonly \
 GOMODCACHE=/workspace/agenteam/output/ai/model-ui-recovery/go-mod \
 GOCACHE=/workspace/agenteam-project-variables-independent/output/ai/project-variables-independent/gocache \
 GOTMPDIR=/workspace/agenteam-knowledge-independent/output/ai/knowledge-independent/tmp \
 AGENTEAM_GO=/workspace/toolchains/go1.27.1/bin/go \
 AGENTEAM_MINIO_BINARY=/workspace/agenteam-knowledge-independent/output/ai/deps-minio/bin/minio \
 python3 /workspace/agenteam-knowledge-independent/.agent-state/task-planning-recovery/pg_only_supervisor.py \
 --root-chain \
 --driver /workspace/agenteam-knowledge-independent/.agent-state/work-owner-http/root_chain_driver.py \
 --binary /workspace/agenteam-knowledge-independent/output/ai/knowledge-independent/knowledge-independent-race.test \
 --run '^TestKnowledgeB02Independent(Content|TreeReference)$' \
 --output /workspace/agenteam-knowledge-independent/output/ai/knowledge-independent/pg
```

工具与两业务新源继续保持f285be16/2578a9ef，不修写运行输入；本次先冻结本摘要供root保存失败和实际缺项。

## 复用与未扩大边界

Knowledge P1/P2 原独立15866结论复用，不重复旧纯测。作者 Process50756完整 PASS（Go2.40s/test1079008 Wait0、driver1077003 Wait0、outer726572 exit0、7IDs14absent/private/runtime/desc/TCP/input全尾，原日志位于作者树 `output/ai/knowledge/pg/pg-d776f88b697049b1853a3f24616ca380.log`）；本人7f14ba实读原日志、2ec24f核实际测试源，原PID Guard busy、SIGKILL及真实Wait、旧claim保留而非graceful退休、Guard death后唯一新attempt/fence和canonical/replay事实已走到，无新已知缺口，不重复进程矩阵，不回填旧91700 FAIL。其它作者 10top35sub、先前 Runtime/Cleanup 结果仍绑定各自实际源与 binary，不称本补集 PASS。

下一步：先保存89530失败和未到范围，再只修有真实源码证据的独立测试前置；后续实际补验需新grant，不自动重跑，不重复作者矩阵。Work新增Blocker普通完成方法独审另在Skills树进行，不回填Work07；此处不复制其报告。
