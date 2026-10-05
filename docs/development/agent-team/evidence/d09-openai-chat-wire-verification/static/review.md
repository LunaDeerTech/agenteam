# OpenAI Chat wire：production-rev1 独立静审

结论：固定 5 个生产文件的有限静审未发现确定硬阻断，可以继续完成作者真实测试并冻结完整 16 源，再安排独立行为验收。本结论不等于产品或真实 Provider 验收通过。

## 固定输入

- 作者只读副本：`/workspace/agenteam-openai-wire-o11zl625/production-rev1`；manifest SHA-256 `df11905c95ee6009011db6a3c7426ffe3be49d9bac61349137a5c5d092e09df9`。基线 `de00c610da62cb77cc03efe7c3cc842cf81f1ba5` 加该 5 源，已在本目录逐文件复制并初检/末检相等。
- 已采纳规格：`9d648575d07ce791dd623eb45eb6604cc6c908b5` 的 wire rev2；复用原已审 SDK `becc1d20eed83c1b8d85e15dc131a372d9dc7813` 17 文件身份。本次仅重读 usage/message/completion/chunk 四个采用源码，字段类型与当前闭集相符，包括 cache_write_tokens、service_tier 的 fast、默认 obfuscation。
- 源 SHA 见 `manifest.json`，指定已提交依赖/SDK 输入见 `dependency-inputs.json`，作者纯测身份核对见 `checks.json`。未读取活动测试稿、Artifact/Object 活动源，也未读取共享树中的这 5 个候选作为结论输入。

## 已核实现与证据

| 风险 | 固定实现证据与结论 |
| --- | --- |
| 受限请求与 origin | `openai_chat.go:127–234` 同步校验 C0 Snapshot/Message/Tool/格式后，只编码原生文本字段；unsupported 在预算登记/Do 前返回。编码总量 16 MiB，正文成为独占 bytes.Reader。使用 EscapedPath 保留内部转义/双斜线/点段，只处理边界 slash；由最终 ParseTarget.Origin 创建真实 CredentialBinding，普通 header 不放 Authorization。POST、req.Close=true、每 Exchange 新真实 Client，无 adapter 重试。实际 RequestURI/origin/redirect 仍待受控网络验证。 |
| JSON/usage 闭集 | `errors.go:80–432` 检查 UTF-8/UTF-16 配对、重复 key、深度 32、尾随 JSON 与逐层允许键；单 index=0、assistant、已知 finish。非文本能力安全失败。Token 直接解析非负 int64，零有指针、缺失/null 为 nil；原值映射，不推算 total 或累计 detail。SDK 其他正式 detail 先验证类型再忽略。没有把未核型号、公共 Invocation 或业务 receipt 造出来。 |
| SSE/refusal/obfuscation | `sse.go:18–146` 有界跨 Read、LF/CRLF、多 data 行、1 MiB 原事件、16 MiB 总文本；64 KiB UTF-8 分片，transport 队列最多 32 项/256 KiB，并由原 ctx 解除背压。合法 finish 后还必须收到 DONE；usage 空 choices 只在 finish 后一次。后续错误保留已观察 usage。`errors.go:299–432` 仅 SSE 接受 obfuscation 缺失/null/string，受原 event 与 D04 总响应字节限额，忽略不投影；不关闭供应商默认。JSON 非空 refusal 无成功 Result；SSE 在该合法阶段返回 content_filter/不可重试、错误后 EOF、无 StreamEnd；未消费正文不造 PartialOutput。 |
| 真实 Do/body/parser/Client 所有权 | `transport.go:43–61,62–117,151–202`：Budget 登记后才异步运行；perform 的真实 Do、parser、response.Close 及 request.Body.Close 均已返回后，run 调用原 Client.StopAdmission，再标本地 ioDone。只有原 Client.Drain 成功才能标 joined 并归还 slot。对照固定 D04 `client.go:156–171`，Drain 真检查 active/writers/停止后的 connections；取消不是成功。D04 Response.Close 是正式同步 finish，当前端口自身返回 nil；没有依赖伪造 Close 成功来代替 Drain。run 尾部仅状态/栈返回，不含遗漏的 I/O。 |
| 消费、取消与预算 | 正常 JSON Result 与 SSE StreamEnd 经 `consumerJoin` 后发布；错误/取消可先返回，但未 join 的 handle/原 slot 留在共享 Budget。Close 使用调用者预算，run 用原 attempt ctx，没有新超时、WithoutCancel 或无限后台 Drain。Joined 的已取消 ctx 只作正式零等待复核。`budget.go:23–42,77–116` 原子限制全局 64/Project 8、无排队；Force 先停止准入、取消原 handle 再按原 caller 预算 Drain。Budget.Joined 要求先 stopped，避免把暂时空集合当最终停止。 |
| 观察与安全值 | `transport.go:68–80,118–149,217–228` 仅实际 Do 前登记 DoStarted，仅复制真实 Response/NetworkError Decision；Decision=nil 保留 unknown。ModelError.Dispatched 仅投影已知 Sent，不能充当 nil Decision 的 not_sent 证明。C0 ModelError 不要求 InvocationID；PartialOutput 依实际已交付 TextDelta 且蕴含 Dispatched。请求/结果/事件/句柄默认格式固定安全标签，错误 code 闭集、原错误正文最多丢弃 64 KiB、不带 cause；request ID 仅实际 header 中 1–256 ASCII safe token。Observation/usage/end 返回复制值，SecretMaterial 借用至真实 Joined，不由库 Destroy。 |

以上是源码与正式依赖的静态调用链结论，不声称已动态复现所有取消、网络和并发窗口。

## 作者已有证据的有限复用

读取并保存作者 `evidence/pure-race-v2.json` 与对应原日志。实际 argv 为：

```text
/workspace/toolchains/go1.27.1/bin/go test -race -count=1 -v -timeout=6m ./internal/central/model/adapter ./internal/central/outbound ./tests/testsupport/outbound/...
```

元数据记录在作者 snapshot、de00c610 基线执行，exit 0；其中 5 生产指纹与本次固定 manifest 完全匹配。原日志 SHA `651cf707c9798e252c419093e2789c77672d4614fd445d5f3e6ecf4b9a6d923e`，adapter 1.205s、原 outbound 7.690s，合计 41 个顶层及 158 个子例 PASS；三个 testsupport fixture 包为 `[no test files]`，不能算真实 fixture 行为通过。测试源本轮仍活动，未审其当前内容，不把元数据中 16 路径视为最终完整冻结输入。本人没有执行这条命令或其他 Go 命令。

本轮实际只读命令包括固定 `git show de00c610:<依赖>`、`git show 9d64857:<规格>`、固定副本 `nl/sed/rg`、Python 字节指纹与纯日志统计；依赖路径通过 scoped `git ls-tree/git grep` 定位。两次试探路径 lifecycle.go/model_error.go 不存在，随后按实际 client.go/http.go/types.go 定位，属于文件定位结果，不是产品或测试失败。只写本自有临时目录，没有网络、Docker、Go、仓库写入或 Git 写操作。

## 最小独立补证与下一门槛

沿已冻结准备计划 `/tmp/agenteam-openai-wire-verification-prep-6fjevpb5/plan.md`，保持候选 1 个新增顶层、3 个风险组，不因静审增加整套重复测试：

1. 正式 Resolver 内真实 Do 晚返回：共享同 Project 8 个 slot 已进入 Lookup，取消一个后 Close/Joined 不得提前退休，第 9 个立即 ResourceBusy/零新 Lookup；放行原 Do、原 Client 真 Drain 后才可补位。证明实际 Do 尾部，不能冒称阻塞 socket writer 已动态验证。
2. 真实 SSE 分片与默认 obfuscation：finish→最终 usage（含 0）→DONE 前 hold，原 handle 未 join、无提前 End；放行后恰一 End/EOF。缺 DONE 和文本前缀后的 refusal 分别安全错误、usage/实际 PartialOutput 正确，材料与正文不泄漏。
3. 转义 base path 与最终 credential origin：真实 RequestURI/Bearer、POST redirect 原 origin 一请求/目的零请求、真实 Sent；安全错误与 policy 拒绝零请求的 Decision 对照，不把 nil Decision 窗口降成未发送。

先等作者完整 16 源固定与最终纯/race/vet/两 binary/三个新真实顶层及受影响旧 fixture 结果。两个旧 fixture 未冻结，本轮不判断其 delta；后续按实际 delta 选原计划 6 个旧回归，并复用确实匹配的作者证据。唯一资源窗口由 root 单独交付。本轮没有最终真实验收、Provider 账号 smoke、Model Runtime/Secret/Invocation/Usage/root 装配完成声明。

报告、5 源副本及证据已冻结，all-stop。
