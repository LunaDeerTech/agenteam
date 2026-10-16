// Synthetic project directory. IDs and metadata are references, never credentials.
export const models = [
  { id: "model-project-chat", label: "Project Chat", provider: "Atlas OpenAI", source: "项目", available: true, efforts: ["low", "medium", "high"] },
  { id: "model-review-chat", label: "Review Chat", provider: "System Anthropic", source: "系统", available: true, efforts: [] },
  { id: "model-system-reasoning", label: "System Reasoning", provider: "System OpenAI", source: "系统", available: true, efforts: ["medium", "high", "xhigh"] },
  { id: "model-retired-chat", label: "Legacy Chat", provider: "Legacy Provider", source: "系统", available: false, efforts: ["low", "high"] },
];
export const runnerChoices = [
  { id: "runner-atlas", label: "Atlas Runner" },
  { id: "runner-research", label: "Research Runner" },
];
export const secretChoices = [
  { id: "secret-research", label: "RESEARCH_API_KEY", description: "查询项目订阅的研究资料。" },
  { id: "secret-repository", label: "REPOSITORY_TOKEN", description: "访问已授权的项目代码仓库。" },
  { id: "secret-observability", label: "OBSERVABILITY_TOKEN", description: "读取项目监控与诊断数据。" },
  { id: "secret-design", label: "DESIGN_LIBRARY_KEY", description: "读取设计资源库。" },
  { id: "secret-delivery", label: "DELIVERY_TOKEN", description: "发布经过审批的交付产物。" },
  { id: "secret-translation", label: "TRANSLATION_KEY", description: "调用文档翻译服务。" },
];
export const skillChoices = [
  { id: "skill-research", name: "Research", description: "整理来源、核对事实与形成研究结论。", revision: "3", source: "项目", protected: false },
  { id: "skill-review", name: "Review", description: "审查变更与记录需要跟进的问题。", revision: "5", source: "项目", protected: false },
  { id: "skill-writing", name: "Writing", description: "组织项目文档与交付说明。", revision: "2", source: "项目", protected: false },
  { id: "skill-planning", name: "Planning", description: "拆分工作、核对依赖与验收条件。", revision: "1", source: "项目", protected: false },
  { id: "skill-testing", name: "Testing", description: "编写针对行为的验证与复现步骤。", revision: "4", source: "项目", protected: false },
  { id: "skill-platform", name: "Add Skills", description: "发现并安装适合当前任务的项目技能。", revision: "1", source: "系统内置", protected: true, default: true },
  { id: "skill-safety", name: "Workspace guide", description: "工作目录与资源使用说明。", revision: "1", source: "系统内置", protected: true, default: false },
];
export const toolChoices = [
  { id: "web-search", name: "Web Search", source: "Builtin", purpose: "搜索公开资料与来源。", access: "read", risk: "low", available: true },
  { id: "install-skill", name: "Install Skill", source: "Builtin", purpose: "安装项目技能包。", access: "write", risk: "medium", available: true },
  { id: "run-command", name: "Run Command", source: "Runner", purpose: "在授权工作目录内执行命令。", access: "write", risk: "high", available: true },
  { id: "read-file", name: "Read File", source: "Runner", purpose: "读取 Agent 已挂载目录中的文件。", access: "read", risk: "low", available: true },
  { id: "write-file", name: "Write File", source: "Runner", purpose: "写入 Agent 已挂载目录中的文件。", access: "write", risk: "medium", available: true },
  { id: "mcp:github:list_issues", name: "GitHub · List issues", source: "MCP", purpose: "读取 Atlas GitHub 连接中的 Issues。", access: "read", risk: "low", available: true },
  { id: "mcp:github:create_issue", name: "GitHub · Create issue", source: "MCP", purpose: "通过 Atlas GitHub 连接创建 Issue。", access: "write", risk: "medium", available: true },
  { id: "mcp:docs:search", name: "Docs · Search", source: "MCP", purpose: "搜索项目文档连接。", access: "read", risk: "low", available: true },
  { id: "legacy-fetch", name: "Legacy Fetch", source: "Builtin", purpose: "旧资料读取工具，已停用。", access: "read", risk: "low", available: false },
];
export const coreTools = ["query-doc", "read-doc", "recall", "retain", "reflect", "list-mcp-resources", "read-mcp-resource", "list-artifacts", "create-artifact", "read-artifact"];
export const presets = [
  { id: "preset-research", name: "Research assistant", description: "收集资料、核对来源并形成可追溯的研究结论。", tagColor: "#879582", modelID: "model-system-reasoning", reasoningEffort: "high", instructions: "先明确研究问题，再读取一手来源。区分证据与推断，保留出处和读取时间。", injectAgentsMD: "yes", allowedToolIDs: ["web-search", "legacy-fetch"] },
  { id: "preset-review", name: "Review assistant", description: "审查变更、核对影响范围并整理验证结论。", tagColor: "#9b8f7a", modelID: "model-retired-chat", reasoningEffort: "high", instructions: "先理解变更目的与调用关系，再检查正确性和回归风险。每个结论包含证据和下一步。", injectAgentsMD: "yes", allowedToolIDs: ["web-search"] },
  { id: "preset-blank-model", name: "Writing assistant", description: "整理项目文档与交付说明。", tagColor: "#87929b", modelID: "", reasoningEffort: null, instructions: "使用简洁明确的语言，先写结果，再说明依据与限制。", injectAgentsMD: "yes", allowedToolIDs: [] },
];
export const dateText = (value, detailed = false) => value ? new Intl.DateTimeFormat("zh-CN", { month: "short", day: "numeric", ...(detailed ? {year: "numeric", hour: "2-digit", minute: "2-digit"} : {}), timeZone: "UTC" }).format(new Date(value)) : "—";
