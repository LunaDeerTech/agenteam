# D04 Request / Intent 独立有限审查

仅审 `/workspace/agenteam-secret-variable-storage` 的 `92aca721` 两源：`internal/central/secret/contract/project_variable.go`、`project_variable_test.go`。正式依据为受审树 `faca8b33` 的 `docs/development/work-items/d04-secret-variable-storage.md` §2、正式 D10 A 契约及 `docs/development/backend/secret.md` 的同步材料借用规则。Skills 未参与这两个源的实现或设计；作者两包 race57406/477472 未变范围复用。未审作者后继 prepared/stage 活动源，不改产品。

有限接受，无本范围 must-fix。请求只含原 Human/Session、Project/Variable、D10 原 CommandIdentity、操作与 expected；工厂校验不代表当前权限。identity 必须是 `projectvariable`、恰一个原 Project owner、完整 `project.secret_variable.create/update/delete`；expected 指针、metadata 指针与 identity owners 的公开投影均复制。D04 名称检查只负责结构/大小，D10 正式名称、保留名和共享 namespace 校验仍由后继持锁 Owner 承担，不能据此认 authority 已绑定。

Intent 创建持有独立 SecretMaterial；普通 Go handle 复制共享该持有状态，显式 Clone 复制独立材料。Destroy 清自有持有副本并禁止后续材料借用；它不销毁原调用者材料，也不表示旧 Use 回调已经退出。已发给活跃同步回调的私有副本在实际返回或 panic 后清零，遵循原 SecretMaterial 合同；本结论不承诺擦除 Go/消费者自行复制的所有字节。无值的 update/delete 没有材料可清，不能将其 Destroy 外推为 prepared capability 的整体退休。

独立控制位于本目录 `intent_test.go` 与 `run.py`。命令：

```sh
# cwd /workspace/agenteam-skills
python3 .agent-state/secret-storage-intent-review/run.py
```

脚本只读受审树，以 92aca721 实际核对 Secret/ProjectVariable 两 contract 包已有 Go 源；排除新活动 Go 文件，将本独立测试以临时 overlay 加入实际 package。使用固定 Go1.27.1、`GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOTELEMETRY=off GOFLAGS=-mod=readonly GOMAXPROCS=2`、继承 PATH；GOMODCACHE 为 `/workspace/agenteam/output/ai/model-ui-recovery/go-mod`，GOCACHE 为 `/workspace/agenteam-project-variables-independent/output/ai/project-variables-independent/gocache`，GOTMPDIR 为本树已有 `output/ai/skills/compile/tmp`。没有新 cache、PG、browser、socket 或网络。

35587 / 21c8a0 actual exit0，四 top race1.017s：

- channel 明确卡住实际 UseValue 回调时销毁原材料及 Intent alias：新借用/Validate/Clone 拒绝，原回调真实返回后借用清零；独立 Clone 不受影响。panic 后清零，回调改借用字节不改持有内容，nil callback 拒绝。
- 12 goroutine 并发 Validate/Fields/Use/Clone/Destroy 的数据竞争与副本内容；nested map/slice/private struct、fmt 六种 verb、JSON、slog 与解码失败的安全表面，不泄露测试材料、name/description、原 key/Project；拒绝反序列化不损坏原有效对象。
- 实际 D10 Update→D04 Intent 适配，销毁 D10 与临时材料后仍保 UTF-8 原字节、空白/组合字符及 expected/presence；Delete 不可借材料；超长/NUL/坏 UTF-8/代理码点等结构失败拒绝且不反射输入。
- 原 authority request 的 Actor/Session/identity/expected 投影不受 caller 修改；普通 Variable command、错 operation、错 namespace、零/双/外 Project owners、合法 Service Actor 全拒；旧 `Purpose.Valid` 不扩。

首 27188 / 744b02 为独验探针使用不存在的 `identity.NewProjectScope` 的 setup 编译失败；改正式 `identity.InProject` 后才有上述通过，不是产品反例。所有工具已实际 terminal；受审源码未写。

本结论仅覆盖无材料 Request 与 owned Intent，未证明专用 authority/provider、同 Store/锁/当前权限、prepared issuer、原语义摘要/加密、SQL、私有 Audit 或整个 D04 producer。后继跨用途存储/legacy write 隔离仍需真实实现及独立验收，不能用本纯契约检查代替。
