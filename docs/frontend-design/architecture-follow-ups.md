# 前端设计与架构衔接清单

> 本文区分已确认的业务规则与尚需落实的工程契约。正式规则以架构专题为准，字段、签名、参数和真实验收仍待责任模块；本文不代表接口已有实现。

## 1. 已有契约直接复用

| 能力 | 来源与边界 |
| --- | --- |
| 基础约定 | [基础契约](../architecture/platform-infrastructure/foundation-contracts.md)：稳定身份、错误、游标分页、版本冲突、幂等及取消方向 |
| 项目访问 | [安全与治理](../architecture/security-governance/README.md)：一个 Owner，无人类成员矩阵；管理员不能据角色访问其他用户项目 |
| 任务页面 | [Work Management](../architecture/project-work-management/README.md)：强制层级、唯一 Current Sprint、正式状态机、version、rank、Timeline |
| 会议内容 | [Meeting](../architecture/meeting/README.md)：长期会话、结构化引用、Contribution、按需展开运行详情 |
| 模型与 Agent | [模型配置](../architecture/platform-infrastructure/model-system/model-configuration.md)、[Agent](../architecture/agent-management.md)：配置字段、模型替代、能力和审批边界 |
| 项目技能 | [Agent Skills](../architecture/agent-skills.md)：标准包安装、项目库、分配、固定版本及下一轮生效 |
| MCP | [MCP Config](../architecture/mcp-integration/mcp-server-config-lifecycle.md)：Config / Connection / Credential Binding 分离 |
| 知识正文 | [KnowledgeDocument](../architecture/knowledge-memory/knowledge-document-domain.md)：当前版本、文件预览、索引异步、文档授权与删除 |
| 统一待办 | [Human Inbox](../architecture/platform-infrastructure/human-inbox.md)：open / resolved / dismissed、排序、忽略限制、来源动作路由与实时更新 |
| 账号认证 | [Authentication](../architecture/platform-infrastructure/authentication/README.md)：初始化、登录、邀请注册、资料与密码恢复 |
| 认证邮件 | [SMTP Delivery](../architecture/platform-infrastructure/authentication/smtp-delivery.md)：SMTP 配置与凭据、测试、重试及后台恢复日志 |

## 2. 已归位规则与待补齐契约

会议输入、首轮一次性标题、历史回复替换后的摘要待更新、固定输入和删除边界已归位 [Meeting 架构](../architecture/meeting/README.md)。账号、SMTP、项目生命周期和知识目录方向也已确定；布局只引用规则并描述可见交互，不重复列为产品待选。

| 子系统 | 已有架构来源 / 确认内容 | 尚需补齐 |
| --- | --- | --- |
| 账号安全实现 | [账号生命周期](../architecture/platform-infrastructure/authentication/account-lifecycle.md)：密码规则、期限默认值/配置、Session 撤销、统一恢复和登录挑战均已定 | D07/D26 固定哈希参数、请求字段、配置生效与并发保护，完成真实认证验收 |
| SMTP 实现 | [投递契约](../architecture/platform-infrastructure/authentication/smtp-delivery.md)：TLS/STARTTLS/none、渠道边界和有限重试已定 | D07 固定 timeout、重试默认值/上限与安全拨号适配，验证持久投递及失败恢复 |
| 头像 | [账号资料](../architecture/platform-infrastructure/authentication/account-lifecycle.md#7-资料与界面偏好)：仅静态 JPG/PNG/WebP、只保留当前头像 | D05/D07 固定媒体库、大小/尺寸限制及正式对象端口，验证并发替换与清理 |
| Project 资料与生命周期 | [Project](../architecture/project-work-management/README.md)：Owner 名称唯一、可读路径、归档只读及不可恢复永久删除已定 | D01/D08 固定字段、查询、停止/清理端口及竞争；后续模块完成真实绑定 |
| 知识文档结构 | [KnowledgeDocument](../architecture/knowledge-memory/knowledge-document-domain.md)：当前单父关系、懒加载、全项目标题定位；移动不推正文 version、不重建索引、不留结构历史 | D12/D27 固定查询/游标、祖先路径、命令及移动竞争，验证输入保留和定位 |
| 文档子树删除 | 明示范围、变化重新确认及全部后代删除已定 | D12 固定一致性与删除事务、索引/存储清理和历史引用投影，验证部分失败 |
| Skills 管理 | [技能规则](../architecture/agent-skills.md)：安装不自动分配、显式更新、默认启用可禁用及运行中新增已定 | D10/D22/D27 固定包/分配字段和状态投影，验证并发、固定版本与下一轮绑定 |
| Meeting / Runtime 展示 | [Meeting Summary](../architecture/meeting/meeting-context-summary.md)、[Runtime View](../architecture/agent-executor/runtime-view.md)：摘要实际覆盖/待更新，恢复实际已包含内容 | D22/D24/D25/D27 固定输入身份、投影/订阅契约并验证缺帧、重启和迟到事件 |
| 系统运行信息 | 管理员只读版本及依赖状态 | 授权 read model，不暴露部署凭据 |
| 本人 Inbox | [Human Inbox](../architecture/platform-infrastructure/human-inbox.md)：本人全部项目、来源权威、必处理项不可忽略且人工审批不自动过期 | D01/D25/D26 固定聚合查询、稳定分页及跨本人项目实时订阅并真实验证 |

## 3. 布局层实现约定

- 当前视图、选中对象、树展开、抽屉状态由前端管理；项目主页 `/{username}/{project_name}` 已定，可直接链接的叶子路由和页面参数由 D26/D27 固定，不能把全部 URL 重新列为待选。
- 通用入口按页面默认规则，显式链接优先；对象权限和存在性仍由服务端验证。
- 三类设置复用容器、二级菜单、显式保存、未保存提示、加载 / 冲突处理，不复制领域授权。
- 搜索本轮仅确定浮层容器，检索对象和接口后续单独设计。

## 4. 尚未定稿内容

Dashboard 继续只保留容器；全局搜索的对象范围、排序与接口另立产品设计工作项，不纳入当前已定业务验收。知识目录 version / indexing 边界、账号政策及 2026-10-01 样式 tokens 均已确定，不重新选型或修改样式。认证、媒体、协议库与运行参数等工程细节仍按上表完成规格和真实验收，不能把方向确认视作实现完成。

## 5. 后续验证要求

领域专题完成后复核本目录字段、操作可见性和失败状态；前端实现阶段再做真实页面、双导航、窄屏、键盘与焦点验证。本轮验证仅覆盖 Markdown 文档、相对链接和契约一致性。
