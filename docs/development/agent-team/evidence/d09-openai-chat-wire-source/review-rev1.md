# D09 OpenAI Chat wire rev1 独立规格静审

结论：范围可行，建议完成下列两项窄修后采纳；当前 rev1 仍有确定的协议阻断/终态歧义。本轮仅静审，没有业务实现或动态验收通过声明。

## 固定输入与独立性

- 卡：`docs/development/work-items/recovery-d09-openai-chat-wire.md` rev1，SHA-256 `ea343789b5861bcc67193a7e7f91c4cae1ffeb9adfeb50f4d653d06f4e49dcbc`；副本 `card-rev1.md`。
- 已提交依赖：`de00c610da62cb77cc03efe7c3cc842cf81f1ba5`。只读 D04 Client/HTTP/Profile/Policy/Target、C0 Model/SecretMaterial、D09 rev4、Chat Runtime/C0文档及两旧 outbound fixture；17 个 scoped Git 文件和卡绑定于 `inputs.json`。不读活动 Artifact。
- SDK：官方 `openai/openai-python@becc1d20eed83c1b8d85e15dc131a372d9dc7813`（`_version.py` 为 3.24.0）。作者恢复，审查者只读本地固定 17 源；`sdk-source-manifest.json` SHA-256 `14de67c09ce263b11414505fcd52f125b48684dfafa23374281a6b066d317760`。独立计算每源 SHA-256、Git blob SHA1、commit 原对象 SHA1，并以只读 `git ls-tree` 核路径/blob 归属，17/17 一致；记录 `sdk-identity-checks.json`、`sdk-ls-tree.txt`。没有由审查者发起网络访问。
- 未参与本卡或产品实现。本轮只写此自有 tmp 目录；无仓库写入、Git 写、Go/Docker、SQL/fixture或再委派。

## 两项必须修订

### W1：固定 SDK 默认 SSE 字段被严格 envelope 拒绝

卡第105行仅发 `stream_options.include_usage=true`；第114行 envelope 闭集无 `obfuscation`，并要求未知键拒绝。固定 SDK `src/openai/types/chat/chat_completion_chunk.py:294–298` 明确 `obfuscation: Optional[str]` 默认包含；`chat_completion_stream_options_param.py:13–21` 也明确只有显式 `include_obfuscation=false` 才关闭。因此本卡原闭集会拒绝此官方默认形状，不能按“兼容厂商未知字段”处置。

最小修订：仅 SSE chunk envelope 接受 absent/null/string 的 obfuscation；其原始事件仍服从本卡1 MiB事件上限，累计网络读取服从真实 D04 `Profile.Limits.ResponseBodyBytes`（D04 streaming默认上限256 MiB，可收紧）。不另设未定义的“总输入”预算。不投影正文、Usage、错误或日志；保留供应商默认，不增加 `include_obfuscation=false`。纯与真实 conformance 条款补默认字符串通过、错误类型/超事件上限安全失败；现有纯超限测试与真实带字段用例可按风险分工，不增加顶层/文件/API。

### W2：非空 refusal 的公开终态存在歧义

卡第114行把 refusal 与只允许 null 的不适用 optional 字段一起描述；第116行又要求非空 refusal 映射 content_filter，未明确是错误还是 Result/StreamEnd。SDK `chat_completion_message.py:60–64` 与 chunk `ChoiceDelta:87–88` 证实 refusal 是可选字符串；C0 同时有 ErrorCategory 和 FinishReason 的 content_filter，故不能留给实现猜测。

最小修订（root已选定方向）：合法非空 refusal 返回安全 `ModelError{Category: content_filter, Retryable:false}`，不转发其正文。JSON 不返回成功 Result；SSE 返回安全错误一次后 EOF，绝不 StreamEnd。保留此前合法 usage；PartialOutput 只来自实际已交付文本前缀，Dispatched 仅投影真实 Decision.Sent（符合 C0 `PartialOutput => Dispatched`）。结构/类型错误仍按协议错误。正常原生 finish_reason=content_filter 且无非空 refusal 的既定 End 映射不必改变。补 JSON/SSE 两类正反断言即可，无业务身份或API扩张。

## 已核可复用结论

1. **D04配置/策略/材料绑定可落地。** `client.go:95–107` 正式 NewClient 拒零 Policy/Trust；`profile.go:158–241` 允许 final Origin 的 HeaderCredential + CredentialBinding + Model Profile/CallContext。Policy 实际 gate/地址分类/首写校验留 D04。nil resolver 的正式默认构造会读固定 `/etc/resolv.conf`（dns.go:46–76），卡未承诺无此配置读取；不应把构造校验当策略授权或健康证明。卡禁止 policy SQL/socket/后台预授权，无要求修改 D04。
2. **Do与实际终局清楚分离。** `http.go:487–510` writer登记后 goroutine 真退出才减数，Do取消可先返回；`client.go:156–175` Drain 核 active、writers，StopAdmission 后还核实际 connections。卡先登记 Exchange/slot，再创建独占 Client/Do；本地 parser/body/Do真退出 + 原 Client.StopAdmission/Drain成功才 Joined/释放槽，覆盖晚到response。取消ctx的只读Drain探测只有已空才成功，ctx错误不可冒充join。该设计静态可实现，动态并发/尾部尚待作者与独立V。
3. **共享预算来自既有契约。** D09 rev4 §7 第249行已有全局64/Project8；wire无队列，后继 Runtime独占128业务队列；满额零Do、取消中保槽、Force先全取消再共用剩余预算一致。每Exchange一Client限制复用代价，避免通过私有D04状态伪造每call join。显式Overall≤120s、ReadIdle≤min(60s,Overall)没有继承D04 streaming默认30min。
4. **C0 ModelError无需伪 InvocationID。** `types.go:395–415` ModelError仅安全category/code/requestID/retry/dispatched/partial字段；Invocation身份属于 ModelResponse/Frame。卡第128行明确 DoStarted=true/Decision=nil 为unknown，Dispatched=false本身不是not_sent证明；不能以HTTP错误、EOF或ctx推发送事实。后继Runtime仍须消费Observation，wire不签调用receipt。
5. **usage原映射有确切来源。** 固定 `completion_usage.py:37–53` 中 cached_tokens及cache_write_tokens确实存在（后者43行），不是Anthropic字段误植；completion details有reasoning_tokens（22行）。D09原§6第223行同口径可保留。其他audio/image/text/prediction细分仅按固定manifest严格校验后忽略；不累加、不估算、不把缺失改0。`stream_options.py:24–33` 明确最终空choices usage为全请求统计，其他chunk usage为null且中断可缺失。
6. **源码足够限定本文本profile，不证明账号/型号/执行。** resources `completions.py:1317–1373` 证明POST相对路径与body字段；`_client.py:620–636` Bearer，`_base_client.py:477–481,560–570` base raw路径拼接；message/choice/chunk/usage源覆盖采用类型；`_streaming.py:70,383–426` 证明DONE标记、多data行与注释。严格完整DONE、单choice、拒额外能力、JSON/UTF8/深度/队列上限是本项目profile限制，不声称SDK本身强制同一规则。SDK中的metadata/moderation等未开放能力仍可明确拒绝；不由Optional推导供应商必然发送。W1例外是源码明确默认包含的字段。
7. **范围与fixture接缝有界。** 路径展开16个=14新+2旧，基线只有两fixture已存在；8本地链接存在。固定fixture原nonce/exact资源校验、请求16MiB读取/受控私网及release可扩窄Wire scenario，不需改driver、D04、C0、锁文件或迁移。real server计数/handler返回与Client.Drain是不同证据；合成SecretMaterial与实际Account/System policy只验证协议，不证明Project consumer/Secret Model lease/read已绑定。

## 实际检查与限制

执行的命令类别均为只读：`sha256sum`/`sed`/`rg`，`git show de00c610:<scoped path>`、scoped `git ls-tree`/`git cat-file -e`，以及 Python hashlib/Markdown路径展开与链接检查。源字节/commit/blob一致、16路径分类、8链接检查通过，详情见JSON证据。首次固定输入脚本曾使用不存在的 `chat-runtime.md`（git show exit128）；随后按卡真实链接改读 `chat-model-runtime.md`，输入完整，不涉及产品执行失败。

未运行：Go compile/unit/race/vet、两binary构建、真实D04/PG/MinIO/server、Provider账号smoke、wire conformance、Runtime/Invocation/Usage/Secret授权和根装配。两项修订须由唯一作者修卡并冻结delta后复审；本结论不代表D09或新库已验收。SDK小型逐文件/字段manifest应随规格/实现证据持久归档，不提交SDK checkout/cache。
