---
name: agenteam-security
description: 在 agenteam 涉及身份与当前授权、Project 或 System 范围、Secret、受控出站或安全输出时使用，沿正式契约构造撤权、跨范围、恢复和泄露反例。
---

# agenteam 权限与安全验证

供 `security_reviewer` 和相关实现者使用。检查依据来自[安全架构](../../../docs/architecture/security-governance/README.md)、[基础身份与 scope 契约](../../../docs/development/work-items/d01-contracts/foundation.md)及目标工作项；不由本技能补造权限、审批或业务规则。审查独立性与文件写权遵循[团队流程](../../../docs/development/agent-team/README.md)。

## 触发与产物

适用于 Session/Actor、Project 与 System 权限边界、Secret 使用、授权缓存或 lease、受控网络、安全错误和审计输出。产物是与正式契约对应的访问矩阵、具体缺陷或修复、反例测试及适用证据；矩阵可保存在既有规格、测试用例或交接中，无需独立审批文件。

## 沿一次请求找检查位置

1. 标出入口携带的身份、目标资源与 scope、参与授权的当前事实、材料取得位置、写入或外部发出位置和返回内容。追到真实实现及装配，不能只检查接口声明或路由名称。
2. 对照契约逐格列出合法主体、未认证、已失效主体、跨 Project/错误 System scope 及资源生命周期状态。每格写预期结果与允许可见的内容；系统管理权限和 Project Owner 权限按正式规则分别核对。
3. 标明每次授权检查与实际使用之间有哪些等待、事务、缓存、lease 或异步阶段。使用 barrier 在这些间隔安排撤权、停用、资源替换或销毁，验证实现是否按契约重验当前事实。
4. 检查读取、修改、重放与 Unknown lookup 各入口。一次成功预检或历史回执不自动证明后续操作仍获授权；最终以该入口约定的事实、锁和输出规则判定。

## 构造可判定的反例

以一个合法请求作控制组，每次只改变一个关键事实，例如 Project ID、当前成员关系、Session 有效性、引用版本或出站目标。比较状态码/安全错误、响应字段、数据库事实和外部请求计数；授权拒绝时依契约断言未发生受保护读取、写入或发出。

检查成功与失败路径的错误包装、日志、序列化和审计。使用任务专用标记凭据验证输出未泄露其内容；测试报告只记录命中位置和安全分类，原始凭据不进入可提交材料。Audit 是否与命令同事务、记录哪些安全事实，以领域正式契约为准。

- [Account 说明](../../../docs/development/backend/account.md)用于 Session、HTTP 与当前身份事实。
- [Secret 说明](../../../docs/development/backend/secret.md)用于引用、lease、材料持有与撤销/退休竞争；确认受保护材料真正释放或 join，不能只看取消信号。
- [出站说明](../../../docs/development/backend/outbound.md)用于策略、DNS/IP、TLS、重定向及实际拨号边界。成功用例沿[现有安全测试](../../../scripts/test-security.sh)的任务所有私网 fixture 和精确规则验证，不改变生产地址分类器来适配测试。
- 同事务与锁竞争组合[数据库技能](../agenteam-database/SKILL.md)，可重复的 barrier/代理与故障注入组合[测试工程技能](../agenteam-test-engineering/SKILL.md)。

## 结论边界

每个发现给出触发条件、实际可观察事实、违反的正式条款及最小修复方向。未决产品含义或正式来源冲突交负责人处理；没有运行撤权/并发反例就明确未验证。参与实现的实例可以自查，但不能据此承担该实现的独立安全验收。

敏感输入只使用本任务生成的隔离材料。必要脱敏复现源进入[可恢复检查点](../../../.agent-state/README.md)；外部服务、真实用户数据或生产部署操作须落在既有授权内，技能本身不扩展权限。
