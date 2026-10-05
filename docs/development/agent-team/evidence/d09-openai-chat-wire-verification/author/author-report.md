# OpenAI Chat wire 作者交接

2026-10-05。固定基线 `de00c610da62cb77cc03efe7c3cc842cf81f1ba5`，16 源 `candidate-rev1/manifest.json` SHA256 `1ed6b19184fa2e3cec928ed01e3b651eadc1704718b8d0f7547e1949dbf03f80`。main、执行 snapshot 与固定副本逐项相同；5 生产源与已交静审 production-rev1 未变。当前全部源、Go、Docker all-stop，窗口已交回 root。

实现限定文本 JSON/SSE、原始 usage、安全错误、精确路径和 D04 origin credential binding；64 全局/8 Project 的共享无队列 Budget；每 Exchange 独占真实 D04 Client，并以原 Client.Drain 成功退休 slot。无修改 D04/contract/driver/迁移/依赖。SDK 17 文件身份、字段声明、许可证与采用的已归档证据一致，见 `evidence/provenance-checks.json`。

## 实际检查

所有实际 argv/env/cwd/input SHA/exit/log 的映射在 `author-evidence.json`（SHA256 `955d2588d894ada2511e223320a04b2ec9a47ba84038c87501829f4d3885c52d`）；不是以空日志推成功。最终检查显式 GOENV=off/GOWORK=off、exact Go1.27.1、GOTOOLCHAIN=local、GOPROXY=off、只读已锁 modcache，构建/runtime 均在本私有 workspace。

| 检查 | 结果与元数据 |
| --- | --- |
| 生产首编译 | exit0；`first-compile.json`，当时尚无测试，不作为行为通过 |
| 适用纯 race | adapter 与原 D04 outbound 及 fixture 包 exit0；`pure-final-race.json` |
| 最终 adapter 全 unit/race | 15 顶层/38 子例通过；`pure-adapter-final.json` |
| 根纯测试全部包 | `pure-root.json` 中仅两个包因快照缺 API 输入失败；其余全过。恢复精确基线文件后二包 `pure-root-api-recheck.json` exit0 |
| 根 vet | `pure-vet.json` exit0 |
| integration race compile | `integration-compile-final.json` exit0，model/security/fixture 包，仅编译无行为运行 |
| Central 与 Runner | `central-build.json`、`runner-build.json` 分别 exit0 |
| 原真实 driver | `first-real.json` exit0；**13 顶层 = 3 新 wire + 10 旧 D04**，36 子例，无 FAIL/SKIP；model 包 9.470s |
| 格式/冻结 | `freeze-checks.json`：16 路径 gofmt/末尾空白、生产5冻结指纹通过 |

上述短文件名均位于本目录 `evidence/`，日志是对应 `.log`。早期迭代的实际命令及原结果也全部纳入索引，不覆盖原文件。

## 保留的首次失败

1. `first-unit` exit1：新测试误把 C0 `ModelError.Format` 的固定标签预期写为 `model_provider_error`；正式 Format 为 `model_model_error`。仅纠正测试，生产未因该失败修改。精确原失败文件保存在 `evidence/first-unit-input/errors_test.go`，与原 input SHA 相符。
2. `integration-compile-v1` exit1：新测试误用不存在的 `Decision.Allowed`；实际 API 是 `Reason`，成功为无拒绝 Reason。仅修测试并重新编译。原文件 `evidence/integration-compile-v1-input/openai_chat_wire_http_test.go` 与原 input SHA 相符。
3. `pure-root` exit1：私有最小快照遗漏 `api/openapi/account.json` 与 `api/openapi/common.json`，两旧 unit 报明确 no such file；从固定 de00 Git archive 精确恢复到私有 snapshot，命令/SHA 见 `snapshot-api-restoration.json`。未改旧测试/产品文件，受影响两包复验通过。

真实首轮没有失败，没有放宽断言、增加预算或重跑以掩盖红例。生产冻结前额外收紧原生 enum 精确匹配与不成对 UTF-16 转义，并有负例；这些都包含在 production-rev1，未发生冻结后生产 delta。

## 真实网络与边界

真实 Account Session/System 权限与 Audit 更新隔离 PG policy；owned 私网 TLS server 观察精确请求体、RequestURI、Bearer 合成 canary、一次发送计数。真实组覆盖拒绝前零发送、TLS 失败、POST 拒跳、HTTP 安全错误、默认 obfuscation、SSE 分片/断流/usage/refusal、D04 body cap、慢消费取消。晚返回 writer 通过标准 WroteRequest hook 阻塞**真实 D04 writer**，取消后 Close 超时且 Joined=false/8 slot 不退休；放行实际 writer 后才能复用 slot。两 adapter 共用 Budget 的 64 个实际请求与第 65 个/同 Project 第 9 个零 admission、Force/Drain 也通过。原 D04 旧组另覆盖持有响应头、取消和连接回收。

本结果是库级 conformance。未调用真实 Provider 账号，未装配 Model Runtime、消费者授权、Project Invocation、Secret Model resolve 或持久 Usage；合成材料不是业务 grant。

资源证据 `resource-final-checks.json` SHA256 `1c52778fe4b54ad1635427114162e8534e54ceb99385232697f8bf5b8071885e`：4 容器/3 网络 exact ID 两次实际 absent；原 2 容器/4 网络 ID/name/labels 相同；owned 进程0、runtime空、driver退出0、16源末检一致。仅使用新空 DOCKER_CONFIG 与本地固定 socket，未读取现存凭据或操作既有资源。尚待独立验收结论，不把作者结果当最终采纳。
