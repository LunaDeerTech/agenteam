# Wire 独立 probe 已就绪

固定基线 de00c610 + candidate-rev1 全 16 源（manifest `1ed6b19184fa2e3cec928ed01e3b651eadc1704718b8d0f7547e1949dbf03f80`）+ 独立 probe。没有修改仓库或作者源。全部 16 源逐 SHA 及编译后末检相符；先前 5 生产静审结论复用。

Probe：`snapshot/tests/model/openai_chat_wire_independent_test.go`，SHA `be61b134824ea022384c92834e53c0cd668484c1ebd182935b93c81e2f7565d4`；原字节副本 `probe.go.txt`。selector `^TestModelOpenAIChatWireIndependent$`，1 顶层 / 2 子例：

1. 真实 Do 未返回且 Decision=nil：正式 Resolver 的首次 Lookup 明确 entered/release，真实 policy/Client 未替换；取消后 Close/Result 不能误成未发送的证明或释放第八个 slot。7 个其他真实 held response 与第 9 次拒绝证明无排队；原 Do 真返、原 Client.Drain 后才可补位。借用材料一直有效。该增量补作者真实 writer 已返回 Decision 的不同窗口。
2. finish + final zero usage 后、DONE 前 hold：默认 obfuscation 不进正文，真实 handle/parser/连接仍活。分 release 后唯一 End/真 join，以及 40ms consumer wait 实际超时、错误后 EOF/不许迟到 End 两支；usage 保留且返值不别名。40ms 是原冻结 probe 等待预算，不能因失败提前 Release 或放宽。

作者真实 13 顶层 / 36 子例首轮 PASS 的原日志/metadata/16 输入均已独立核身份，覆盖 origin/escaped RequestURI/redirect、refusal、安全错误、writer 尾部、64/8 及 10 个旧 D04 顶层，故不重复新建 origin/refusal probe。旧 fixture diff 只增 Wire 闭集分支、原 path gate、request URI 与 handler 观察；十个原组包含计划六个以及 writer/旧连接/header/sent 拓展，足以覆盖本 delta 的旧行为接缝。

实际准备命令：Go1.27.1 `go test -c -race -tags=integration -mod=readonly -o <owned>/model-probe.test ./tests/model`；GOTOOLCHAIN=local、GOPROXY/GOSUMDB=off、GOENV/GOWORK=off，自有 GOCACHE/TMPDIR、复用公共锁定 GOMODCACHE、不复制缓存。2026-10-05 16:35:54–16:36:32 UTC，exit 0；没有执行测试 body，binary SHA `6e8113b1737a95939c96f7a39d0dd80917beb134720a445a89ffad4e7d4a8e34`。精确 argv/env/exit 在 `evidence/compile.json`，完整输入在 `verification-input.json`。

作者原三次失败保留：新测试 C0 Format 预期、测试 Decision.Allowed 非现 API、根 snapshot 缺精确基线 API 文件。根纯测不是一次首轮全绿；恢复精确输入后相关二包复验，加最终 adapter race/vet/compile/两 binary 是分段闭合。作者 15 对 metadata/log 指纹都已核；当前旧根纯测有三个随后改变的候选测试输入，最终相应检查/真实测试另有一致输入，不误称全部在同一最终输入首次通过。

此刻仅静审/编译准备通过，独立动态未执行；无 Docker、服务、网络或 Git 写动作。所有 Go 命令已结束，源码停写，等待 root 正式交唯一窗口。
