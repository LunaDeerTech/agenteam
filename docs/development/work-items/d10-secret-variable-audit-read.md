# D10 Secret Variable Audit 合同与读端兼容

状态：SPEC，实施中；基线正式main `8cb0a953`。来源是已有限独审的[Secret Owner rev2](d10-secret-variables-owner.md) §6/§6.2，A纯合同/Schema已交付。本结果只完成新增审计记录的严格表示、解析和现有读端兼容；SQL CHECK未扩展，因此不宣称新action已经能够持久写入。

## 1. 闭集与结果

新增恰三个action：`project.secret_variable.create`、`project.secret_variable.update`、`project.secret_variable.delete`；producer沿`projectvariable`，resource沿`project_variable`且ID等于metadata.variable_id。仅Project scope、Human Actor、Success、空associations；稳定摘要沿现`Audit event`。AppendKey沿原command canonical digest、ordinal 0，不新增producer或原材料摘要。

安全metadata恰`variable_id,version,changed_fields`：create version=1、fields恰`[created]`；update version≥2、fields是`description/name/value`的非空有序去重子集；delete version≥2、fields恰`[deleted]`。不含name/description正文、明文、值长度/摘要、CredentialRef或mask。严格拒绝未知/重复/null成员、错大小写、错数值形状、未知action、actor/resource/scope关联不符；公开错误只固定schema path。

单独Secret action predicate/metadata构造器与投影保持类型闭集；原`ProjectVariableAction`仍只表示普通变量三action，不能因读端扩展让旧事实authority替Secret签发授权。公共Entry/Metadata都是数据形状，不是native事实proof。`secret.create/update/delete`仍属于D04真实Credential值变更；metadata-only/no-op不因此产生D04值Audit，本域真实变化Audit的写入责任仍在后继D10事务。

Go `DecodeMetadata/NewEntry`→真实SQL row scanner→现Project Audit HTTP安全projection→OpenAPI和TS实际decoder贯通新三分支，原记录继续严格解析。共享query filter仅增加这三个action和必要既有resource；不改变System Audit记录自身的scope/actor闭集，不为新分支开放未知兜底。

## 2. 唯一写域

`/root/knowledge`在独立树`ai/secret-variable-audit`拥有：

- 本卡与`.agent-state/current.md`。
- 新`internal/central/audit/contract/projectsecretvariable.go`及测试；原`contract/types.go`、`contract/metadata.go`仅接闭集。
- `internal/central/audit/http/project_wire.go`及新`project_secret_variable_test.go`；必要真实row scanner测试新`internal/central/audit/project_secret_variable_test.go`，不改查询权限/分页算法。
- `api/openapi/project-audit.json`、必要`api/openapi/audit.json`仅精确action/filter及安全record分支。
- `web/src/api/project-audit-metadata.ts`、`web/src/api/client.ts`仅精确decoder/filter；新`web/src/tests/secret-variable-audit.spec.ts`，必要旧穷举测试表同步新记录。

不写Project事实路由、projectvariable Authority/commands、Outbox Catalog装配、D04、SQL/迁移或生产root。后继D10 Owner结果拥有secret_commands/history/canonical、private discovery及实际D04返回的同Tx证明；这些不能拆成没有真实事实却成功返回的provider。D04专用存储由Model独占。00028不占号。

## 3. 有限验收

1. 三分支typed构造/严格反序列化/shape与metadata字段边界，unknown/null/重复/错version/错actor/resource/scope/associations拒绝；ordinary predicate不接受Secret，producer与AppendKey原闭集保留。
2. 实际SQL row decoder和HTTP encoder接受三正形、拒篡改；canary字段不能进入安全JSON/Fault。真实标准JSON Schema对同一Go输出和负控执行，不用文本搜索代替。
3. 实际TS metadata/record/query parser正负控，旧普通/Project/Secret值Audit集合有限回归；类型检查和相关格式检查。复用只读node_modules，不npm网络，不起Vite/browser/socket。
4. 离线Go小包race/vet、既有受影响读端合同测试。不存在PG写入/同Tx事实/Owner授权集成证明；不依假SQL provider通过扩大结论。根安排未参与者独审后才正式交付。

没有新增产品决定；任何发现要求扩大既有事实权限或改变strict行为，先冻结该变化交独审。当前技术片段已实现并冻结交接，Go定向race/schema已过；TS整组仍有两项夹具/运行入口失败，详见current，不宣称完整结果已验收。
