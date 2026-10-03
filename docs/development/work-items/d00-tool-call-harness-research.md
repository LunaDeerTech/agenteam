# P14 工具调用与超时：公开 harness 调研

日期：2026-10-03。关联 [AT-0005](../agent-team/tasks.md) / [P14-01 决策记录](d00-audit-decisions.md)。状态：调研完成，用户已选择修订 A，工具按需提供可选超时；不增加平台统一强制期限，人工审批持续等待明确处理。本报告保留调研过程与候选对比，不是 D18 接口冻结或实现验收。

## 1. 问题与调研边界

用户要求先参考其他 harness，再决定工具参数和执行选项怎样定义。此前提出的统一 `{arguments, execution}` 外层和保留字段 `__runtime` 都未获选择。

调研时的 [Tool Execution §7](../../architecture/tool-system/tool-execution.md#7-timeout) 同时要求：Agent 可以为任意一次工具调用选择 timeout；不填时没有统一 Operation deadline；timeout 不进入 ToolSpec 或业务 arguments。现有 ModelToolCall 没有可由模型填写的独立执行选项通道。研究必须区分这三个问题：

1. 模型看到并填写什么参数；
2. 程序内部如何传递调用上下文、取消信号和 SDK 配置；
3. timeout 是命令运行期限、MCP 请求等待期限，还是仅等待输出后交还控制权。

任务卡 AT-0005/P14-R1：主线程核查 Codex、OpenCode 并整合建议；一个 `verification_worker`（`gpt-6-astra` / `max`）只读核查 Claude Agent SDK、OpenAI Agents Python SDK，使用 `agenteam-verification` 技能。研究副本仅存 `/tmp`，不安装或运行下载的软件，不修改产品实现。验收为固定来源可复核、语义分层准确、建议明确标出架构变化。

## 2. 来源快照

以下是本次实际读取的版本，不将开发分支快照宣称为稳定发布版。

| 项目 | 本次快照 | 证据范围 |
| --- | --- | --- |
| OpenAI Codex | `6326163b9abd7802c0e57be4e326e5f898bbba75` | Rust 工具 schema、handler、调用上下文、MCP 绑定 |
| OpenCode | `907b3bc518fa48e90e8ec24dd327d13eee71c36c`；该快照 package 声明 `1.18.34` | Shell schema/执行、工具 Context、MCP 转换/配置 |
| OpenAI Agents Python SDK | `v0.23.1` / `c4a1d047b22488234b2ba81056e1b9b8db2127a5` | FunctionTool、Provider 投影、timeout 执行和 MCP client 配置 |
| Claude Agent SDK | TypeScript 官方包 `0.3.288`；公开仓库 `36836f000b931056f7de6471bcc90b31d658d4e9`；Python `v0.2.163` / `1ef6d8c71bb0e44a6b33fe61497864f21e17fdb7` | 官方类型声明、公开 SDK 实现和发行说明；不声称完整审计 Claude Code 私有 runtime |

Claude TypeScript 声明来自 [官方 npm 固定版本包](https://registry.npmjs.org/@anthropic-ai/claude-agent-sdk/-/claude-agent-sdk-0.3.288.tgz)，归档 SHA-256：`eb97c0f5a7d96189bceaf1986c2751a024b7e13844ba843531cc55bb0b4fac54`。其中 `sdk-tools.d.ts` L801–809 是 Bash 输入；`sdk.d.ts` L602–645 是 SDK MCP server timeout，L9716 附近是自定义工具定义函数。

## 3. 比较结果

| 项目 | 模型可见参数 | timeout / 执行控制 | 对原 P14 候选的启示 |
| --- | --- | --- | --- |
| Codex | 命令工具直接使用 `cmd` 等工具字段 | 交互命令用 `yield_time_ms`；一次性命令变体用 `timeout_ms`；内部 Invocation 单独带上下文和取消信号 | 同一工具族也需要区分“先返回”与“到时终止”，不以统一模型参数外层解决全部问题 |
| OpenCode | Shell 直接使用 `command`、可选 `timeout`、`workdir` | Shell 自己处理期限/终止；MCP `timeout` 由配置传到客户端请求选项 | 工具专用参数和 MCP 客户端控制处于不同层 |
| OpenAI Agents Python SDK | FunctionTool 的 `params_json_schema` 投影为模型 function parameters | 开发者用 `@function_tool(timeout=...)` / `timeout_seconds` 配置异步工具调用；Context 单独传入 | SDK 的“每次调用都有期限”不等于“模型每次能选择期限” |
| Claude Agent SDK | Bash 声明含平铺 `command`、`timeout`；自定义 MCP tool 声明自己的 input schema | SDK MCP server timeout 是开发者配置，独立于 tool args | 内置命令工具可让模型指定 timeout，不代表所有 MCP 工具获得通用模型控制字段 |

本次核查的常规函数/MCP 调用路径没有把每个工具的模型输入统一包成 `{arguments, execution}`，也没有普遍注入 `__runtime`。这是限定来源内的结论，不是对所有扩展、模式和 harness 的穷尽证明。内部调用对象独立携带 Context，并不要求模型输入也采用同一种外层结构。

### 3.1 Codex

- [交互命令 schema](https://github.com/openai/codex/blob/6326163b9abd7802c0e57be4e326e5f898bbba75/codex-rs/core/src/tools/handlers/shell_spec.rs#L24)：`cmd`、`workdir`、`tty`、`yield_time_ms` 等直接位于输入对象。描述说明等待输出后可返回仍在运行的 session。
- [一次性命令变体](https://github.com/openai/codex/blob/6326163b9abd7802c0e57be4e326e5f898bbba75/codex-rs/core/src/tools/handlers/unified_exec/exec_command.rs#L482)：移除 `tty`/`yield_time_ms`，加入 `timeout_ms`，描述明确超时或取消后终止、不可恢复。不能只看反序列化结构里同时出现两个字段，就声称模型总能同时使用两者。
- [运行分支](https://github.com/openai/codex/blob/6326163b9abd7802c0e57be4e326e5f898bbba75/codex-rs/core/src/tools/handlers/unified_exec/exec_command.rs#L300)：Interactive 不创建 completion timeout；OneShot 使用调用值或默认值。它是命令工具的语义，不是给所有工具施加同一种期限。
- [ToolInvocation](https://github.com/openai/codex/blob/6326163b9abd7802c0e57be4e326e5f898bbba75/codex-rs/core/src/tools/context.rs#L69) 包含 session、turn、cancellation token、call ID、tool name、payload 等运行字段。
- [MCP PreparedMcpCall](https://github.com/openai/codex/blob/6326163b9abd7802c0e57be4e326e5f898bbba75/codex-rs/codex-mcp/src/binding.rs#L291) 将 `arguments` 与调用方 `timeout` 分开，后者可收紧 server timeout；[本次核对的模型 MCP 执行路径](https://github.com/openai/codex/blob/6326163b9abd7802c0e57be4e326e5f898bbba75/codex-rs/core/src/mcp_tool_call.rs#L450) 传入 `requested_timeout = None`。内部 API 有 timeout 槽，不等于模型有对应参数。

### 3.2 OpenCode

- [Shell 参数](https://github.com/anomalyco/opencode/blob/907b3bc518fa48e90e8ec24dd327d13eee71c36c/packages/opencode/src/tool/shell/prompt.ts#L15) 直接声明 `command`、可选毫秒 `timeout` 和 `workdir`。
- [工具 Context](https://github.com/anomalyco/opencode/blob/907b3bc518fa48e90e8ec24dd327d13eee71c36c/packages/opencode/src/tool/tool.ts#L36) 含 session、agent、abort signal 等；执行接口为 `execute(args, ctx)`。
- [超时处理](https://github.com/anomalyco/opencode/blob/907b3bc518fa48e90e8ec24dd327d13eee71c36c/packages/opencode/src/tool/shell.ts#L533) 在进程退出、abort、期限之间竞争；超时调用 `handle.kill`，向结果加入终止说明。该版本有默认运行期限；本项目不因参考它就自动引入相同默认值。
- [MCP 转换](https://github.com/anomalyco/opencode/blob/907b3bc518fa48e90e8ec24dd327d13eee71c36c/packages/opencode/src/mcp/catalog.ts#L42) 将 tool input schema 交给模型；调用时 `name/arguments` 与客户端 `timeout/signal/resetTimeoutOnProgress` 分开。[期限来源](https://github.com/anomalyco/opencode/blob/907b3bc518fa48e90e8ec24dd327d13eee71c36c/packages/opencode/src/mcp/index.ts#L661) 是 MCP 配置。启用进度重置意味着不能直接当成不可延长的 wall-clock 总期限。
- 该转换同时强制对象根和 `additionalProperties: false`。这里借鉴的是参数与执行层的分工，不照搬可能改变第三方 schema 接受集合的转换；本项目仍须验证 Provider 适配不静默削弱/改变业务契约。

### 3.3 OpenAI Agents Python SDK

- [FunctionTool](https://github.com/openai/openai-agents-python/blob/c4a1d047b22488234b2ba81056e1b9b8db2127a5/src/agents/tool.py#L455) 分别声明 `params_json_schema`、`on_invoke_tool` 和 `timeout_seconds`。[Provider 转换](https://github.com/openai/openai-agents-python/blob/c4a1d047b22488234b2ba81056e1b9b8db2127a5/src/agents/models/openai_responses.py#L2204) 使用 `parameters: tool.params_json_schema`，没有添加本题的统一外层。
- [实际执行](https://github.com/openai/openai-agents-python/blob/c4a1d047b22488234b2ba81056e1b9b8db2127a5/src/agents/tool.py#L2226)：Context 与参数字符串分开；`timeout_seconds` 非空时通过 `asyncio.wait_for` 等待异步 handler。超时可返回模型可见错误，也可按开发者配置抛出异常。
- [ShellActionRequest](https://github.com/openai/openai-agents-python/blob/c4a1d047b22488234b2ba81056e1b9b8db2127a5/src/agents/tool.py#L1424) 另有模型调用携带的 `timeout_ms`。这是特定 Shell 能力，不是 FunctionTool/MCP 的通用参数槽。字段存在也不保证 executor 已实现进程终止。
- 这是开发者配置的值，并非模型可填写的通用隐藏字段。SDK 可对 function schema 做 strict 归一化，“不包统一外层”也不等于原 schema 字节不变。
- [MCP client 配置说明](https://github.com/openai/openai-agents-python/blob/c4a1d047b22488234b2ba81056e1b9b8db2127a5/src/agents/mcp/server.py#L925) 将 `client_session_timeout_seconds` 定义为 ClientSession read timeout；它不应与工具业务参数、整个 Agent run 的期限混为一谈。

### 3.4 Claude Agent SDK

- 官方 TypeScript 包 `sdk-tools.d.ts` 的 `BashInput` 直接声明 `command` 和可选 `timeout`；注释单位毫秒，前台命令上限 600000。这里核对了公开输入契约，没有执行 Bash 超时行为测试。
- `sdk.d.ts` 的 `tool(name, description, inputSchema, handler, ...)` 让开发者声明自定义 schema；handler 的 `args` 和额外运行信息分开。公开 Python SDK 的 [schema 构造](https://github.com/anthropics/claude-agent-sdk-python/blob/1ef6d8c71bb0e44a6b33fe61497864f21e17fdb7/src/claude_agent_sdk/__init__.py#L410) / [tools list](https://github.com/anthropics/claude-agent-sdk-python/blob/1ef6d8c71bb0e44a6b33fe61497864f21e17fdb7/src/claude_agent_sdk/__init__.py#L580) 也由工具自身输入定义生成 inputSchema。
- `createSdkMcpServer({timeout})` 的类型说明将其定义为该 server 的每次调用 wall-clock 限制，进度通知不延长，fallback 为环境变量；这与 OpenCode 本次 MCP 路径的 progress reset 策略不同。它仍是开发者的 server 配置，不是模型逐次参数。
- [官方发行说明](https://github.com/anthropics/claude-agent-sdk-typescript/blob/36836f000b931056f7de6471bcc90b31d658d4e9/CHANGELOG.md#L287) 记录 SDK server timeout 配置的加入；本报告这项 public API 来自 TypeScript 包，不将其直接套到 Python SDK。
- TypeScript 的 `SDKControlMcpCallRequest` 确实另有 `arguments` 和 `timeout_ms`（`sdk.d.ts` L4649–4669），但声明明确它是 **without a model turn** 的 host 控制调用。它不能证明模型已拥有相同的通用控制槽。
- TypeScript 声明、Python SDK 和实际 Claude Code runtime 的证据范围不能混同；本报告不从类型注释推断远端进程一定终止、外部副作用一定回滚。
- 本次 TS 包对应 runtime `2.1.288`，Python 包绑定 runtime `2.1.286`，不将两个 SDK 快照视为同一 runtime 版本。

## 4. 对本项目的修订建议与选择

建议先决定是否保留“任意 Tool 都能让 Agent 逐次指定统一 Operation timeout”这个产品要求，再决定参数载体。原先直接推荐统一外层，过早把这个要求当成必须保留；本次证据不支持把它称为通用惯例。

**修订 A（用户已选择）：按工具需要定义模型可控超时，保留工具自身参数结构。**

- 普通工具使用自身 schema。命令执行等确实需要模型控制期限的工具，可以在自己的输入契约中声明可选 timeout；需要先返回继续运行的工具另外明确等待/查询/取消语义，不把 yield 叫作终止期限。
- MCP 工具维持第三方输入语义，不统一塞入 `__runtime` 或外层 `execution`。如果 MCP 服务自己定义了名为 `timeout` 的业务字段，仍按它自己的 schema 处理，不能剥离或冒充平台控制。
- 内部调用仍分开保存参数与可信运行上下文、取消信号。工具显式声明的期限由已定义的适配规则执行；不能从任意参数同名字段、自然语言或 Provider metadata 猜测期限。
- 保留“不填时不增加统一 Operation deadline”“没有 Project 统一 timeout 页面”“人工 Approval/Decision 等待不自动过期”。必要的网络、RPC、SDK 超时是适配器可靠性机制；本题不借调研新增人类为每个工具配置期限的要求。
- **代价/变化：** 不再承诺模型能为每一种工具逐次选择平台级期限；允许特定工具在自己的输入 schema 中声明期限，因此要明确修改旧文档中绝对的“timeout 不进入 ToolSpec / arguments”。这不是单纯的字段换名。

**修订 B（未采用）：保留所有工具通用的逐次 Agent timeout，采用项目自己的统一模型投影外层。**

- 继续原先较强的产品要求；在模型可见投影上采用 `{arguments, execution}`，解包后才校验/传递原业务参数及独立 Runtime options。
- 这是为本项目需求设计的适配层；需验证 Provider schema 子集、嵌套 `$ref`、调用记录、审批 fingerprint、重试/恢复和固定 Execution 投影等，不以别的 SDK 内部 Context 作为其已兼容证明。
- 原来“业务参数平铺再加保留字段”的候选暂不推荐；第三方 schema 碰撞与组合约束成本仍在。

两种选择均维持取消后的 outcome 判断：发出取消或发生 timeout 不能证明操作未生效；结果未知时不自动重试非幂等副作用。不能直接复制其他 harness 的默认时长或把进程等待超时当成整个 Agent Execution 失败。

需同步的责任范围：D01 的调用/取消边界，D09 模型投影，D16 命令执行，D18 ToolSpec/Runtime/结果，D20 MCP adapter，D21 工具目录和 D22 Loop/checkpoint；人工审批持久等待与可见状态涉及 D19/D25/D27。用户已选择 A，架构原文按 [AT-0005 同步任务卡](d00-decision-sync.md)归位，本报告保留调研时的候选和证据，不实现产品。具体字段、单位、支持工具清单及必要行为验收留各工作项规格。

## 5. 检查与局限

- 实际使用 `urllib.request` 读取 GitHub 页面与固定 SHA 的 raw 文件，SDK 执行者使用 `git ls-remote`、官方 codeload 和 npm 固定发行包；用 `rg -n` / 小段源码阅读核对 schema、投影和执行入口。GitHub API 及部分官方 docs 入口返回 403，采用仍可访问的一手源码/发行包，没有用失败请求推断产品能力。
- Codex/OpenCode 的部分旧路径返回 404，通过固定提交目录树定位迁移后的文件；报告仅引用实际成功读取的路径。
- 没有运行这些 harness、模型请求、MCP server 或超时取消集成测试；结论是接口和源码调研，不是本平台实现验收。没有引入依赖、Git 推送或进入下一模块。
- SDK 执行者只读复核本报告对应版本、SDK 结论和修订 A/B，无实质误述；补充的 Shell 专用参数、host 控制通道和 runtime 版本差异已纳入。主线程检查本轮 3 份文档 UTF-8/LF/whitespace、16 个本地链接及标题 fragment，通过；`git diff --check` 通过。执行者及后台命令已结束。
- 用户已选择修订 A，并重申无平台统一强制期限、人工审批不自动过期且持续显示等待。本文随 AT-0005 讨论文档维护，全部审计决定已完成核对，正式归位与验收进度见 [同步任务卡](d00-decision-sync.md)。
