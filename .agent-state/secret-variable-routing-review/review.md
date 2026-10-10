# Secret Variable 路由与 ordinary 隔离窄独审

有限接受，无本轮 must-fix。审查者未参与此片段实现；作者树基线 `87838e2f` 加下列冻结 11 路径，已由60e8a8实际核对，11个blob与保存提交`eaf209f5`逐项一致。没有修改作者产品、测试、current 或卡。

- 三个分发文件逆移除六个精确 Secret 分支后，全文逐字等于正式 `ce65714a`。旧 ordinary/Knowledge/Object/lifecycle 分支及独立 predicates 不扩义。
- ordinary Get/List/容量/UPDATE 四处增加 `type='variable'`，逆移除与包说明后全文仍等于正式基线。Create 的全局 ID 不复用规则、当前版本条件保持；ordinary 与 Secret 的名称冲突查询均故意跨 type，未把共享名称空间拆开。
- 新 Project helper 保持当前 Owner/Session 与 Mutate 先行，原 context、Tx、Entry/AppendKey 到真正 D10 provider。缺包私有同 Store/活 Tx mutation witness 时，公开安全元数据与 caller context 不能授予写事实。Outbox Project 与 producer 两域职责仍分开，Project dependencies 绑定完整 Actor（含 Session）、事件、issuer、purpose 和精确锁集。

`python3 -B .agent-state/secret-variable-routing-review/run.py --go` 实际 `73753→14aaea` exit0，Project 包两个新增独立 top / race 1.019s。第一进程 fresh UTC `2026-10-10T00:06:25.479310Z` / 5,568,966,656B；Go1.27.1、local/off、GOENV/GOWORKoff、p1、45s test timeout、原 Vars 独占 GOCACHE、原共享只读 modcache。实际控制核原 context/Tx/Entry/Key 透传至真实 D10 Authority 后拒绝；当前 SessionRevoked/Archived 阻断发生在该真实 provider 被调用前；同 User 新 Session、跨 Project authority issuer、少一锁的 dependencies 在 Store/Session IO 前拒绝。

Store/Session/Rows 使用原包明确 controlled fixture，不能称真实鉴权或 SQL。没有执行作者矩阵、活动 `projectvariable/secret_facts_test.go`、TestMain、PG/socket；只 overlay 自有新增 Project 测试，11 输入前后 blob 一致。独立控制不复制作者完整 matrix。`run.py` 可用 `REVIEW_GO`、`REVIEW_GOCACHE`、`REVIEW_GOMODCACHE` 替换执行环境。

eaf209f5同包另含作者secret_facts两路径，不计入本11路径独审：其PriorVersion序列化修正与新成功witness控制不改变本次拒绝路径，但尚未由本审查独验。

本结论不覆盖 00030 实际迁移/SQL、完整 Commands final Tx、成功私有 mutation witness/NewFact 持久后验、D04 两域实际组合或 Unknown。它们需作者后继控制与独立真实风险补集；未验不能因本片段接受而关闭。

按 root 要求记录冻结 blob，供稍后提交对齐；不是仓库全量 manifest：

| 路径 | Git blob |
| --- | --- |
| internal/central/project/audit_facts.go | 1f57e8fedcfd48dd4be304288f82b0a9c4363463 |
| internal/central/project/projectvariable_event_authority.go | c64e431e360e0a5ffa0b4cf4491a66da6dc4ecd5 |
| internal/central/project/audit_facts_test.go | 9fc786c562d058972e83bb2703a22c720d83016a |
| internal/central/project/secret_variable_facts.go | 9fa60cf9c47a4130b181ea9ffb1b587352ea9712 |
| internal/central/project/secret_variable_facts_test.go | a9fbdfdb107d39ebcb9c6700428daa1a02ba9f55 |
| internal/central/projectvariable/authority.go | 7dad3084512d16eeb6d6008289604601f9923b78 |
| internal/central/projectvariable/commands.go | a0e542b1193e5848372480c878116f3b0c2a721d |
| internal/central/projectvariable/reader.go | 3a5c8a2195facdd0a912e903a4836d56f9fde636 |
| internal/central/projectvariable/repository.go | d3185a8e876e1d619b0f1afc5935009312e3fe27 |
| .agent-state/current.md | 02703a707babf7d43d2439091efbcc698fd946bf |
| docs/development/work-items/d10-secret-variable-owner-service.md | 7cd663df1298db244b0529ad46031ca79d7e8039 |
