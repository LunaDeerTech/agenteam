# 自动审批模型详细设计

> 状态：设计稿
>
> 上层架构：[安全与治理架构](../README.md)
>
> 相关详细设计：
> - [Approval Model Contract](./approval-model-contract.md)
> - [Approval Model Prompt Context](./approval-model-prompt-context.md)
> - [默认审批策略](../default-approval-policy.md)
> - [Approval Scope](../approval-scope.md)
> - [Audit](../audit.md)

## 设计范围

Auto Approval Model 是 approval_policy = auto 时的审批判断组件。

它只处理已经通过基础授权、且 Default Approval Rules 判定为 needs_approval、同时又没有 Reusable Approval 命中的 Tool Call。

本模块负责：

- 构造最小且结构化的 ApprovalEvaluationContext；
- 调用 Agent 明确配置的 approval_model_ref；
- 校验 Approval Model 输出；
- 把模型判断归一化为 AutoApprovalResult；
- 在任何模型调用或输出异常时 fail-safe 到用户审批；
- 固定 Approval Prompt、上下文组装方式和输入预算策略；
- 记录足够的审批 Audit 证据。

本模块不负责扩大 Agent 权限，也不能绕过 Agent Capability、Execution Policy、Project / Resource Scope 或平台固定安全规则。

## 调用前提

Auto 模式不会让 Approval Model 判断所有 Tool Call。

调用顺序由上层 Security / Governance 架构统一定义：

~~~text
基础权限校验
  -> Approval Match
      -> 已有匹配 Approval：直接执行
      -> 无匹配 Approval：Approval Policy
          -> auto
              -> Default Approval Rules
                  -> allow：直接执行
                  -> needs_approval：调用 Approval Model
~~~

因此 Approval Model 只处理：

> 没有已有 Approval 覆盖，并且 Default Approval Rules 原本要求用户审批的 Tool Call。

这样可以避免对普通低风险操作重复调用模型。

## 详细设计拆分

自动审批模型按“调用契约”和“Prompt / Context 构造”两个职责面拆分：

~~~text
Auto Approval Model
├── Approval Model Contract
│   ├── ApprovalEvaluationContext
│   ├── ApprovalModelDecision
│   ├── AutoApprovalResult
│   ├── fail-safe failure policy
│   └── audit contract
└── Approval Model Prompt Context
    ├── fixed system prompt
    ├── user message assembly
    ├── context serialization
    ├── stateless single-turn call
    └── input budget / truncation
~~~

职责边界：

- 输入 / 输出协议、失败语义和 Audit 见 [Approval Model Contract](./approval-model-contract.md)；
- Prompt、消息组装、输入预算和截断见 [Approval Model Prompt Context](./approval-model-prompt-context.md)。

## 第一阶段设计结论

第一阶段固定：

1. Approval Model 只处理 Default Approval Rules 原本要求审批的 Tool Call；
2. 使用独立 ApprovalEvaluationContext，不复用完整 Agent Model Context；
3. Approval Model 只输出 allow / needs_approval 和简短 reason；
4. Auto Approval Module 统一输出 decision / reason / source；
5. 所有调用失败、非法输出和不可可靠判断都 fail-safe 到 needs_approval；
6. Approval Prompt 固定在平台源码中，不允许 Project / Agent / 用户覆盖；
7. Context 先结构化构造，再序列化进入单轮 Model Call；
8. 安全关键字段不能为了塞入上下文而静默有损截断；
9. 上下文超限时退回用户审批，不通过额外 Model summary 强行继续；
10. Audit 只保存审批证据和版本信息，不重复复制完整高敏感 ApprovalEvaluationContext；
11. 应用层一次判断，不增加重试或备用模型；Adapter 透明网络重试仍受本次有限请求 timeout 限制并逐真实请求计量；
12. 转入人工后持久等待明确处理，不自动过期，不接受迟到模型结果覆盖人工流程。
