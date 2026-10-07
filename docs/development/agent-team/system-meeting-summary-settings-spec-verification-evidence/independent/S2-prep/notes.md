# S2 STATIC 准备（未审活动卡）

固定已提交 c210d249；精确 commit 与16项必要输入hash见 inputs.json。只读旧实现，不读取 `d09-system-meeting-summary-settings.md` 活动草稿，不判断其未冻结内容。无产品/Git写入、测试或资源。

1. **路由实际接缝**：`app/model.go` 已将完整 `/api/v1/system/model-selection` 和其 `/` 子路径原样交 Model handler；新增独立Summary path无需扩外层匹配。child `http.go` 当前仅旧四用途GET/PUT，新GET/PUT/HEAD与405/Allow、404、原始path/Origin/Host/CSRF/body上界及问题响应须在卡中精确定义。旧四用途body/schema不能纳入Summary字段。新DTO应对应S1独立 ID/version/nullable model；写请求不能接受clear/reasoning/Project覆盖。
2. **预算与终局**：现 `managementReadRequest` 仅覆盖credential metadata/deletion-impact；S1 Get自行3s在HTTP RequireSystem之后才开始。若S2承诺完整HTTP3s，须明确在鉴权前建立min(parent,now+budget)，GET/HEAD拒绝query/body/transfer编码，终局前不发布；PUT真实body/read/service/commit和取消须沿现有错误/Unknown规则，不能只看到AbortSignal就称实际完成。
3. **默认启动链**：account.go Secret成功并ctx检查后调用 `initializeModelsAndUsage(models.Initialize,usage.Initialize)`。S2应按Secret→旧Model singleton→Summary singleton→Usage检查共享原ctx/30s安全预算，逐步失败/Unknown/取消不监听，重启保留ID/version/model/旧receipts，不重置预算、不等configured、不选择默认、不新增worker。只接独立initializer；S3 Resolver/D24消费不提前绑定。卡白名单须覆盖所选精确helper/测试接缝，不以修改已接受Usage语义绕过。
4. **同kind两个selector**：旧client receipt已校验原command.kind、resource_id==原selector ID、version==expected+1、affected=0。Summary必须同样绑定本intent的ID/version；两区仍共享后端(kind,user,key)，不能发明新command kind或按kind一项缓存覆盖另一selector。lookup found只是当前授权下的历史观察，不证明本次body已接受；最终确认仍来自原key/原body/原version的显式重放。当前GET observation、保存draft、原intent及历史receipt必须分开。
5. **单Cookie owner，两区独立intent**：useSession `owner`/runAuthorized是唯一实际lane，visible promise超时/abandon不释放它；必须等实际body/cancel返回。旧selection已有read revisions与intent/uncertain/checked/keyConflict。新Summary不得另建owner/队列/域，且本区discard/取消/查证不能清掉另一块draft/intent；身份或CSRF失效时应明确清理所有相关旧材料。两区首读/参考链不能同时抢owner导致第二区永久busy；需要可恢复的有序调度。原请求材料仍仅私存在Session controller，不进View/URL/storage/log。
6. **同页离开与恢复**：目前App只安装一个selection navigation owner、logout confirm、mounted afterNavigation与dispose。若同页含两区，guard/confirmation必须覆盖两区dirty/uncertain；放弃不撤销远端，旧未完成观察退休成可显式重读状态。不能为新增区改写原四用途可选Reranker/Image规则。参考详情未完整返回或Model/Provider停用时保留保存ID，不猜已解绑；plain chat可用于Summary，memory仍json_schema。同页focus、dialog堆叠、取消和持续busy须可验证。
7. **有限验收代表**：冻结卡应给严格wire/schema/client正负边界、同kind错selector receipt拒绝、新旧双向key冲突、old route回归；当前Session/admin与实际COMMIT Unknown/read终局；真实默认根初始化与重启/失败/原预算；同页两区旧draft+新intent共存、迟返回/查证found并非确认、身份切换/导航/焦点和旧Cookie owner回归。沿完整import/embed/TestMain/dynamic build图，不因-run遗漏根构建；测试与资源预算/窗口待root按冻结卡授权。
8. **结果限制**：沿S1的真实schema20与独立选择库，S2只交设置HTTP/UI/根技术初始化；未配置明确状态且保存后无clear。S3模型Resolver与D24初始/更新/首轮标题生成仍未绑定，Execution Summary read model/compaction不改；Object runtime join、OpenAI tools独审、SPA并发发布原三停止与ready503不解除。辅助SQLseed身份仅能按已接受边界证明当前DB Session/role，不冒充Invitation/创建E2E。

上述是从固定现有实现得到的检查点，不是对活动规格的阻断或要求作者新增报告门槛。已向架构作者/root对齐dispatcher、receipt/单owner、预算与启动接缝。准备结束后停写，等待最终冻结卡及root正式完整STATIC派工。
