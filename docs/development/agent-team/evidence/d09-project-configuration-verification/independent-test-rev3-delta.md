# D09 首红归因 rev3 窄差量复核

结论：此前唯一测试证据补点已在静态层闭合，可执行真实复验；未发现需要进一步更改生产或扩大测试范围的阻断。原首轮 FAIL 不变，仍无本审核动态通过声明。

- rev3 manifest SHA-256：`6d122a4a34b9eb31421c0b795edfb22155dc26d5a50e93b02a3a1f964dee68d6`，21 项逐一核验。
- 作者 rev2→rev3 patch SHA-256：`cea94bc620cb4319759ddeb0999c1962cfa18bf7bc546d1a345f1cbebb0023f0`。
- 独立比较只有 `tests/model/project_configuration_integration_test.go`：新增 bytes import，以及 fields 反例原错误/六类计数断言之后约 11 行完整 ProviderView 复读比较。13 生产和其余 7 测试与 rev2 完全相同。
- 新断言调用真实 `GetProjectProvider`，以原 Project/Owner/ProviderID 读取；比较修改前后完整 JSON bytes，并拒绝读取或序列化错误。既有 ProviderView 含 ID、Scope、完整 Input、Version、CreatedAt、UpdatedAt；因此覆盖仅计数不能发现的旧行内容泄漏。读取路径和比较不引入 fake Authority，也没有取消 Forbidden 或持久计数断言。
- 未改等待预算、fixture 配置或实际 final COMMIT selector。下一真实轮仍必须实际通过撤销、精确 LockFailed/55P03、忽略 missing-held-lock 后 poison、typed-valid Audit 反例以及此旧行回滚断言。
- 依据为本目录 `rev3/` 固定源和 `independent-rev2-to-rev3.patch`；原详审 `review.md` 保持冻结（SHA `f603c79580b33dbbe16cffef74545f5b30220d6cab7c52197cd8c161ee528d6f`），其中“未闭合”状态由本窄复核更新。

只运行固定文件读取、SHA-256 和文本差异；无 Go/SQL/Docker/Git 写入，无仓库写入、无后台命令、无 fixture 占用。Object 13 源保持 all-stop。
