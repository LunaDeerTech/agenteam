# 前端页面设计总览

> 状态：布局设计稿；样式已定稿：Linear 视觉方向、石墨灰阶与低饱和偏蓝青蓝色。本文描述目标前端，不代表已有实现。

## 1. 目标与边界

agenteam 前端围绕用户自己的项目提供会议、工作规划、知识管理和项目配置。系统管理员另有系统配置入口。桌面优先，同时定义窄屏下的导航、侧栏和详情展示规则。

本轮完整描述布局、栏目、字段和入口；系统首页与项目首页只保留 Dashboard 容器。样式规范已定稿，固定 Linear 视觉方向、项目配色、字体与密度、控件细节及动效，见 [样式规范](styles/README.md)。临时预览仅用于样式试作，不作为正式布局依据，也不表示其信息组织与业务交互已确认。本目录不代表前后端实现，也不定义 HTTP wire contract。业务规则由对应架构文档负责，布局修订时同步必要的架构约定。

## 2. 页面地图

```text
账号入口（独立框架）
├── 登录
├── 邀请注册
├── 找回密码
└── 重置密码
已登录系统
├── Logo → 系统首页（Dashboard 占位）
├── 项目 → 当前用户拥有的项目列表
│   └── 项目工作台（系统导航 + 项目导航）
│       ├── 项目名 → 项目首页（Dashboard 占位）
│       ├── 会议 → 中央输入框准备 / 指定会议
│       ├── 任务 → Kanban / Explore
│       ├── 知识库 → 文档树 / 文档内容
│       └── 项目设置
├── 系统设置（仅系统管理员）
├── 搜索浮层
├── Inbox → 当前用户自己项目的统一待处理事项
└── 用户显示名 → 个人设置
```

## 3. 文档索引

| 布局文档 | 职责 |
| --- | --- |
| [应用框架](layouts/application-shell.md) | 系统导航、搜索、Inbox 入口、权限、滚动与通用状态 |
| [系统页面](layouts/system-pages.md) | 系统首页、项目列表、Inbox |
| [项目工作台](layouts/project-workspace.md) | 双导航、项目首页、子页面容器 |
| [会议](layouts/meetings.md) | 会话侧栏、新会议与消息内容 |
| [任务视图](layouts/tasks.md) | Kanban、Explore、详情与操作 |
| [知识库](layouts/knowledge-base.md) | 文档父子树、预览、编辑与删除 |
| [设置框架](layouts/settings-shell.md) | 悬浮侧栏、二级菜单、表单交互 |
| [系统设置](layouts/system-settings.md) | 系统配置栏目及字段 |
| [项目设置](layouts/project-settings.md) | 项目配置栏目及字段 |
| [个人设置](layouts/personal-settings.md) | 资料、偏好和账号安全 |
| [账号入口](layouts/account-entry.md) | 初始化、邀请、认证与恢复流程 |

独立展示：[组件控件前端](component-showcase/index.html)。集中查看已选样式的组件、状态与动效；展示骨架不作为正式布局依据，原业务试作页已移除。

样式文档：[样式总览](styles/README.md)、[颜色与主题](styles/colors-and-themes.md)、[字体与密度](styles/typography-and-density.md)、[控件与交互](styles/components-and-interactions.md)。

新增领域约定与尚需补齐的后端设计汇总在[架构衔接清单](architecture-follow-ups.md)。

## 4. 统一原则

- 一个 Project 只绑定一个 Owner，所有项目资源均校验当前用户为 Owner；系统管理员身份不授予其他用户项目访问权。
- 普通入口执行本文规定的默认选择；直接链接优先打开明确指定的对象，不用默认项覆盖对象链接。
- 系统导航与项目导航共存；不同页面复用布局框架，业务状态由各自领域管理。
- 系统导航右侧按“搜索 → Inbox → 用户显示名”排列；Inbox 汇总本人项目待办，业务动作仍由来源领域处理。
- Task 的规划、状态和排序使用现有 Work Management 契约；Meeting 的消息、执行、审批使用现有投影。
- 列表、树、面板、详情抽屉支持加载、空数据、请求失败、无权限、对象删除和版本冲突状态。
- 不编造 Dashboard 数据；不把尚未定义的功能显示为已有可用功能。

## 5. 验收场景

| 场景 | 预期 |
| --- | --- |
| 普通用户 / 管理员 | 系统设置可见性正确；项目列表均限本人项目 |
| 点击 Logo / 项目名 | 分别进入系统首页 / 当前项目首页 |
| 点击 Inbox | 进入本人项目的统一待处理页，默认 open，不保留项目导航 |
| 普通会议入口 / 指定会话链接 | 分别进入中央输入框准备 / 对应会话 |
| 会议控件与详情 | 加号、模式、编排与发送顺序正确；已开始右侧仅发送；详情卡片可折叠 |
| 首轮会议 finalize | 标题与四字段摘要共同提交，后续不改写标题 |
| 有 / 无 Current Sprint | 选择唯一 Current Sprint / 提示手动选择 |
| Kanban ↔ Explore | 保留 Sprint，Task 详情呈现方式随视图变化 |
| 展开知识节点 | 不改变选中文档；父文档可展示自身正文 |
| 删除父文档 | 明示子树并确认；取消不改变数据 |
| 普通设置入口 / 指定项链接 | 选择首个可访问叶子项 / 指定项 |
| 邀请 24 小时失效或撤销 | 记录删除，链接不可使用 |
| 已配置 / 未配置 SMTP | 邮件投递 / 后台日志获取链接 |
| 窄屏 | 导航可滚动、侧栏可打开、详情可返回，无页面级横向溢出 |

## 6. 架构依据

- [架构总览](../architecture/README.md)：Vue 3 + 自定义组件的技术基线。
- [安全与治理](../architecture/security-governance/README.md)：Owner 和系统资源边界。
- [Human Inbox](../architecture/platform-infrastructure/human-inbox.md)：统一待办投影、排序、历史与来源动作路由。
- [项目与工作管理](../architecture/project-work-management/README.md)、[会议](../architecture/meeting/README.md)、[知识文档](../architecture/knowledge-memory/knowledge-document-domain.md)：已有领域事实。
- [账号认证与邮件投递](../architecture/platform-infrastructure/authentication/README.md)：账号初始化、邀请注册、Session、密码恢复及 SMTP / 后台日志渠道。
