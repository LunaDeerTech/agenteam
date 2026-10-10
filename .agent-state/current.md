# Skills Owner 读取 UI 当前状态

- 工作树 `/workspace/agenteam-skills-owner-ui`，分支 `ai/skills-owner-ui`，基线 `33903460`。本文件仅记录本树；全局任务台账由 coordination 维护，Git 由 root 操作。
- 目标与接口：[D10 卡的 Owner 读取 UI SPEC rev1](../docs/development/work-items/d10-skills-owner-read-http.md#owner-读取-ui接口与首条链路)。首条结果为项目设置中的真实 Skills 目录与详情；未实现安装、创建、版本写入或 Agent 分配，生产 Project initializer 仍 unbound。
- 当前仅冻结领域卡规格与本文件，尚无技术源、测试、构建或浏览器结果。root 已授本域 API/controller/view/自有测试、router/auth/index 与 Settings 窄入口；共享 `client.ts`、`useSession.ts` 尚未交写权。
- content 当前独写 KnowledgeRename 的两个共享文件；双方确认新增具名 capabilities 尾 options，其先放 knowledgeCommands，我后继同对象扩 skills，旧参数 0..15 不变。待其 freeze、root 精确基线交接后再改共享接线，不新增另一 Session owner。
- 下一步：skills_http 对本卡新段做非作者描述级审；按授权完成独立 UI 功能与必要正常/失败自测。真实 PG/browser/socket 必须另获 root 窗口，当前无资源运行。
- 先前 Work Planning06 wholeFAIL 与完整尾保存在原 Work 树 `2d2d5e11`，该模块冻结，本树不返修或重跑。
