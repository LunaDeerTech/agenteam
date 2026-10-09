# Knowledge 文档树管理命令 HTTP 接续

- 工作树 `/workspace/agenteam-knowledge-tree-http`，分支 `ai/knowledge-owner-tree-http`，正式基线 `ce65714aac6eb4995a43fc427a2c77e6497470a7`。唯一负责人 `/root/work_ui`；Git/真实资源调度归 root。
- [SPEC rev1](../docs/development/work-items/d12-knowledge-owner-tree-http.md)已获 Runner 未参与者有限接受。首批四个 commandhttp 产品源（handler/input/wire/io）已落盘，尚未编译或行为验证；不能记接受。范围为 title-only Update、Move、PrepareDeleteSubtree、DeleteSubtree 与原意图 Lookup 的独立 HTTP adapter。
- 已与 Knowledge read HTTP 负责人确认路由无交叠；新 `internal/central/knowledge/commandhttp` 独立构造、IO/DTO/Schema，不依赖或修改其未验 read 实现。只消费正式 B02/Account；不改领域产品/迁移/共享 root，不含上传、正文或生产生命周期绑定。
- 唯一新增写域见卡 §6；首批四个产品源与本 current 已 freeze 交 root 保存；其余新测试/Schema可独立接续。root已批准原Work缓存上的定向pure/race/vet，首次同进程fresh磁盘≥5GiB才启动；不新增GB cache，不启动 PG/browser/socket。
- 后续顺序：有限 pure/标准 Schema验证及返修、作者 native/真实 PG 与独立风险补集。全部真实命令须原 Wait 与资源尾齐，旧 B02 与本实例 Work/Timeline 的结果不能代本任务验收。
- 原 Work UI/Timeline 技术输入继续冻结；Work09 未获本次运行授权，不能因新树存在而改其输入或重开旧失败轮。
