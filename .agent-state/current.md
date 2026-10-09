# Secret Variable Audit 读端当前检查点

- 分支 `ai/secret-variable-audit`，正式main基线 `8cb0a953`。root将本结果从Knowledge交Variables接续；本次17源/文档全部冻结，Knowledge不再写本树。独立完整结果是三个Secret Variable Audit action的typed合同、Go/HTTP安全投影、OpenAPI与TS严格decoder；不是实际持久写入或事实提供者。
- 规格见 `docs/development/work-items/d10-secret-variable-audit-read.md`。已实现单独Secret action predicate/metadata和三处既有闭集入口，普通ProjectVariableAction未扩；真实row scanner及HTTP安全projection；两OpenAPI的精确filter/三record；TS parser/filter和正负测试。D04、Project事实、Outbox/root、SQL CHECK/迁移未改，00028未占号。
- 作者实际Go：21595/0097ba race0（contract新top3子＋真实row decoder新top3子，1.014/1.017s）；42309/907e65 race0（真实HTTP encoder＋标准Schema三正形21负控，1.954s）。范围不是真实PG/持久写入/权限provider。
- TS第一次31184/bfc94e actual1：旧穷举计数和metadata向量未扩，3失败/142通过；该轮与无语义Prettier格式化重叠，不作为稳定通过证据。补向量/范围后49374/25a5a2 actual1：2失败/146通过，剩余为project-audit-client旧fetch count78应随56+26同步；metadata测试以process.cwd定位Schema，命令误从repo而非web启动导致ENOENT。两次原FAIL保留，不记前端整体通过；新四例在两轮均通过。
- 首OpenAPI编辑519535被原格式等价保护assert阻止、没有写文件。实际原因audit.json保留\u2028/\u2029转义；修render保留后1d9655写入精确增量。6e9b7a diffcheck0；全部改动未独审，vet/typecheck/有限旧Go回归仍未运行。
- 只读复用 `/workspace/agenteam/web/node_modules`，本树web/package-lock逐字一致（0eac93）。本树 `web/node_modules` 是自建未跟踪symlink，**不要提交**；Vite cache在本树ignored output，无npm网络、Vite服务/browser/socket。17保存路径不含symlink或可重建output。

## 实际命令与接续

cwd本树，Go1.27.1，Knowledge独占cache（后续接手者按root分配私有cache，不并发写它）：

```sh
env PATH=/workspace/toolchains/go1.27.1/bin:$PATH GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOTELEMETRY=off GOMAXPROCS=2 GOMODCACHE=/workspace/agenteam/output/ai/model-ui-recovery/go-mod GOCACHE=/workspace/agenteam-knowledge/output/ai/knowledge/go-cache /workspace/toolchains/go1.27.1/bin/go test -mod=readonly -p=1 -race -count=1 -run '^TestProjectSecretVariable(AuditClosedReadContract|ActualRowDecoder)$' ./internal/central/audit/contract ./internal/central/audit
env PATH=/workspace/toolchains/go1.27.1/bin:$PATH GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOTELEMETRY=off GOMAXPROCS=2 GOMODCACHE=/workspace/agenteam/output/ai/model-ui-recovery/go-mod GOCACHE=/workspace/agenteam-knowledge/output/ai/knowledge/go-cache AGENTEAM_PROJECT_AUDIT_SCHEMA_PYTHON=/usr/bin/python3 /workspace/toolchains/go1.27.1/bin/go test -mod=readonly -p=1 -race -count=1 -run '^TestProjectSecretVariableAuditWireAndSchema$' ./internal/central/audit/http
```

ignored `output/ai/secret-variable-audit/vitest.config.mjs`可由下面内容重建，无其它必要未保存harness：

```js
export default {
  root: '/workspace/agenteam-secret-variable-audit/web',
  cacheDir: '/workspace/agenteam-secret-variable-audit/output/ai/secret-variable-audit/vite-cache',
  test: { environment: 'jsdom', include: ['src/tests/**/*.spec.ts'], restoreMocks: true, maxWorkers: 1 },
}
```

**后继TS应cwd web**，不要复用前两轮的错误cwd：

```sh
node /workspace/agenteam/web/node_modules/vitest/vitest.mjs run --config ../output/ai/secret-variable-audit/vitest.config.mjs src/tests/secret-variable-audit.spec.ts src/tests/project-audit-client.spec.ts src/tests/project-audit-metadata.spec.ts src/tests/system-audit-client.spec.ts
```

- 余下先修旧fetch计数与正确cwd，执行稳定受影响TS、typecheck/vet/有限旧合同回归、检查Schema边界与源差异，交未参与者独审。实际ProjectFacts路由仍拒新Secret action，不能将数据形状识别当append权限；SQL CHECK暂未扩，新action不能正式持久。新filter添加3Secret action＋既有project_variable resource，旧ordinary action filter未补，未顺便扩大任务。
- D05四路径与Secret A关闭两docs在另树保持冻结；本树交接不改变其输入。当前所有命令实际terminal，无自有真实资源。Git仅root。
