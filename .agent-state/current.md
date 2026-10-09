# Knowledge 文档树管理命令 HTTP 接续

- 工作树 `/workspace/agenteam-knowledge-tree-http`，分支 `ai/knowledge-owner-tree-http`，正式基线 `ce65714aac6eb4995a43fc427a2c77e6497470a7`。唯一负责人 `/root/work_ui`；Git/真实资源调度归 root。
- 当前只有 [SPEC rev1](../docs/development/work-items/d12-knowledge-owner-tree-http.md)，待未参与者独审，没有产品实现、编译或动态通过。范围为 title-only Update、Move、PrepareDeleteSubtree、DeleteSubtree 与原意图 Lookup 的独立 HTTP adapter。
- 已与 Knowledge read HTTP 负责人确认路由无交叠；新 `internal/central/knowledge/commandhttp` 独立构造、IO/DTO/Schema，不依赖或修改其未验 read 实现。只消费正式 B02/Account；不改领域产品/迁移/共享 root，不含上传、正文或生产生命周期绑定。
- 唯一新增写域见卡 §6；当前只写本卡/current，两者形成首可恢复片段后 freeze 交 root 保存。磁盘协调期间不编译、不新增 cache，不启动 PG/browser/socket。
- 后续顺序：root 安排 SPEC 独审，闭合必要返修后实施、有限 pure/标准 Schema、作者 native/真实 PG 与独立风险补集。全部真实命令须原 Wait 与资源尾齐，旧 B02 与本实例 Work/Timeline 的结果不能代本任务验收。
- 原 Work UI/Timeline 技术输入继续冻结；Work09 未获本次运行授权，不能因新树存在而改其输入或重开旧失败轮。
