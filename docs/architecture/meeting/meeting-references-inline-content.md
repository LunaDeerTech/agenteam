# Meeting References & Inline Content 详细设计

> 上层架构：[Meeting 架构](./README.md)
>
> 相关设计：
> - [Meeting Domain Model](./meeting-domain-model.md)
> - [Meeting Context & Summary](./meeting-context-summary.md)
> - [Meeting Timeline & Realtime](./meeting-timeline-realtime.md)
> - [Object Storage](../platform-infrastructure/object-storage.md)
> - [Artifact Builtin Tools](../tool-system/artifact-tools.md)
> - [Knowledge Base 与 Agent Memory](../knowledge-memory.md)

本文定义 Meeting 中长期 References、MeetingMessage Inline Content、资源选择、Pin to References，以及 file 与 Object Storage / Artifact Tools 的完整边界。

## 1. 设计目标

Meeting 需要同时支持两类资源关联：

1. **Meeting References**：长期固定到 Meeting 的资源，每次 Agent Execution 都能看到其稳定引用；
2. **Message Inline References**：某条 MeetingMessage 中临时提到的资源，只随该 Message 进入后续会话 Context。

同时，MeetingMessage 不能只依赖纯文本解析来表达结构化内容。至少需要支持：

- text；
- mention；
- task；
- knowledge；
- link；
- file。

核心原则：

> UI 可以使用 `@`、`#`、chip、拖拽上传等自然交互，但持久化必须保存稳定的结构化 identity，不能依赖显示文本重新解析业务对象。

## 2. 三种不同关系

必须区分：

```text
Meeting.origin
= Meeting 从哪里创建
= 单一、不可变 provenance

MeetingReference
= 哪些资源长期固定到 Meeting
= 多个、可 Pin / Unpin

MeetingMessage Inline Node
= 这条消息具体表达了什么
= text / mention / task / knowledge / link / file
```

三者不能合并成一个 `source_reference`。

### 2.1 Origin

Origin 只回答：

> 这个 Meeting 最初由什么业务入口 / 对象创建？

例如：

```text
origin = agent_execution / exec_123
origin = task / task_456
```

Origin：

- 最多一个；
- 创建后不可修改；
- 不因为存在 origin 就自动进入 Meeting References；
- 不自动加载来源对象内容。

如果业务入口希望从 Task 创建 Meeting 后持续把该 Task 作为 Meeting Context，应显式同时创建一个 Task MeetingReference。

## 3. Reference Type

Meeting Reference 与 Message Resource Reference 使用相同的资源类型集合。

第一阶段：

```text
ReferenceType
├── task
├── knowledge
├── link
└── file
```

对应稳定 identity：

| Type | Stable Identity | 具体内容如何获取 |
| --- | --- | --- |
| `task` | `task_id` | Task Tools |
| `knowledge` | canonical `document_id` | Knowledge Tools |
| `link` | normalized `https/http URL` | 不自动获取；Agent 按能力决定是否访问 |
| `file` | `artifact_id` | Artifact Builtin Tools |

这里统一使用用户可理解的类型名 `knowledge`；其 target 实际指向 Knowledge Base 的 canonical document。

## 4. Canonical Reference Identity

为了让不同 reference type 使用同一套 Pin / Unpin 与去重逻辑，定义 canonical `reference_key`。

概念规则：

```text
task:<task_id>
knowledge:<document_id>
file:<artifact_id>
link:<normalized_url>
```

例如：

```text
task:task_123
knowledge:doc_789
file:artifact_456
link:https://example.com/design
```

`reference_key` 是服务端计算值，不信任客户端直接提交。

MeetingReference 唯一约束：

```text
UNIQUE(meeting_id, reference_key)
```

Message 内允许同一个资源出现多次；每个 inline node 都可以指向同一个 MessageReference，或者按实现简单性建立多个等价 MessageReference。第一阶段建议在单条 Message 内同样按 reference_key 复用。

## 5. MeetingReference

MeetingReference 表达长期固定资源。

概念模型：

```text
MeetingReference
├── id
├── meeting_id
├── reference_type
│   ├── task
│   ├── knowledge
│   ├── link
│   └── file
├── reference_key
├── target_id?
├── target_url?
├── display_label_snapshot?
├── pinned_by_participant_id?
├── sort_order
├── created_at
└── updated_at?
```

字段规则：

### task

```text
reference_type = task
target_id = task_id
target_url = null
```

### knowledge

```text
reference_type = knowledge
target_id = canonical document_id
target_url = null
```

不能引用：

- chunk id；
- embedding/vector id；
- retrieval result id；
- 索引版本 id。

### file

```text
reference_type = file
target_id = artifact_id
target_url = null
```

Meeting 不直接引用：

- StoredObject ID；
- storage key；
- MinIO URL；
- Signed URL。

### link

```text
reference_type = link
target_id = null
target_url = normalized http/https URL
```

Link 不是 Object Storage 对象，不需要下载或 materialize 后才能引用。

## 6. Link Normalization

Link reference 只允许：

- `https://`；
- `http://`。

拒绝：

- `javascript:`；
- `data:`；
- `file:`；
- 带 username/password credential 的 URL；
- 其他未允许 scheme。

服务端进行最小规范化，用于 reference identity：

- scheme / host 统一小写；
- 移除默认端口；
- 规范空 path；
- 去除 fragment 是否参与 identity 可在实现时固定，建议 fragment 保留，因为可能表达具体文档锚点；
- 不擅自移除 query 参数。

不要对 URL 做可能改变资源语义的激进 canonicalization。

## 7. MeetingMessage Content Model

MeetingMessage content 使用 ordered inline node list，而不是单一纯文本字符串作为唯一 canonical 表达。

概念结构：

```text
MeetingMessageContent
└── nodes[]
    ├── text
    ├── mention
    ├── task
    ├── knowledge
    ├── link
    └── file
```

第一阶段不要求实现完整 Rich Text Document Model。

不在第一阶段加入：

- paragraph tree；
- heading；
- list；
- table；
- bold / italic 等复杂 mark；
- arbitrary nested node。

如果未来需要完整富文本，可以在保留这些 inline semantic node 的前提下扩展外层 block model。

## 8. Inline Node Schema

统一 discriminated union：

```text
MeetingInlineNode
├── type
└── payload
```

### 8.1 text

```text
{
  type: "text",
  text: "我觉得这里应该调整"
}
```

用于普通自然语言。

### 8.2 mention

```text
{
  type: "mention",
  participant_id: "participant_agent_a",
  display_label_snapshot: "@Agent A"
}
```

Mention 指向 MeetingParticipant，而不是直接指向 Agent / User 主表。

这样即使 Agent 后续删除，历史 Message 仍能保留 mention identity 与显示。

Mention 不支持 **Pin to References**。

### 8.3 task

```text
{
  type: "task",
  reference_id: "message_ref_1"
}
```

对应 MeetingMessageReference：

```text
task / task_123
```

支持 **Pin to References**。

### 8.4 knowledge

```text
{
  type: "knowledge",
  reference_id: "message_ref_2"
}
```

对应：

```text
knowledge / doc_789
```

支持 **Pin to References**。

### 8.5 link

```text
{
  type: "link",
  reference_id: "message_ref_3"
}
```

对应：

```text
link / https://example.com/design
```

支持 **Pin to References**。

### 8.6 file

```text
{
  type: "file",
  reference_id: "message_ref_4"
}
```

对应：

```text
file / artifact_456
```

支持 **Pin to References**。

### 8.7 Node 创建来源

上述 Inline Node schema 对 User Message 和 Agent Message 都成立，但创建方式不同。

User Message：

- 由 Composer 的 `@` / `#` / Link / File UI 创建；
- 服务端验证所有 ID / URL，以及 Task / Knowledge / Artifact 是否属于当前 Meeting Project 后持久化。

Agent Message：

- 只有当 Agent Execution 已经持有可验证的稳定资源 identity 时，才能生成对应结构化 node；
- 例如 Task Tool 返回 task id、Knowledge Tool 返回 document id、Artifact Tool 返回 artifact id；
- Agent 不能只凭自然语言“猜一个 ID”就创建结构化 resource node；
- 无法验证的引用保持普通 text，不伪造业务引用。

Agent Message 的结构化 node 同样不会自动 Pin 到 Meeting References。

## 9. MeetingMessageReference

task / knowledge / link / file inline node 的持久化资源 identity 统一由 MeetingMessageReference 表达。

概念模型：

```text
MeetingMessageReference
├── id
├── message_id
├── reference_type
│   ├── task
│   ├── knowledge
│   ├── link
│   └── file
├── reference_key
├── target_id?
├── target_url?
├── display_label_snapshot?
├── created_at
└── resolution_state?
    ├── available
    └── unavailable
```

Inline node 的 type 必须与 MeetingMessageReference.reference_type 一致。

例如：

```text
node.type = task
node.reference_id = ref_1
ref_1.reference_type = task
```

服务端必须验证这一约束。

## 10. 为什么不保存纯文本 #xxx

以下做法不允许作为 canonical persistence：

```text
"请看 #AUTH-123"
```

然后在后端重新解析 `#AUTH-123`。

原因：

- Task display key 可能变化；
- Knowledge title 可以重命名；
- 文件名可以重复；
- Link label 不等于 URL；
- 资源名可能存在歧义；
- Agent 无法可靠判断是否真的是业务引用。

UI 展示文字只是 snapshot。

业务 identity 始终是：

- Participant ID；
- Task ID；
- Document ID；
- URL；
- Artifact ID。

## 11. Composer 交互

### 11.1 @ Mention

输入：

```text
@
```

弹出当前 Meeting active Participants。

选择后插入：

```text
mention node
```

对于 Agent Participant，mention 同时参与 Turn target resolution。

规则：

- 如果 User 没有 Agent mention / picker selection：默认全部 active Agent Contributions；
- 如果存在 Agent mention 或显式 Agent picker selection：本 Turn 只选择显式目标 Agent；
- mention 在正文中第一次出现的顺序可以作为默认 selection order；
- 如果 UI 中另外调整了 Agent selection order，则显式 selection order 优先；
- mention User Participant 不影响 Agent target selection。

Turn Runtime 消费的是结构化 participant IDs，不解析显示文本。

### 11.2 # Resource Picker

输入：

```text
#
```

打开统一 Resource Picker。

第一阶段分类：

```text
Resources
├── Tasks
├── Knowledge
└── Files
```

Link 不必依赖 `#` 搜索；通过 URL paste / link action 创建。

搜索结果只返回当前 Project 内有权访问的资源。

### 11.3 Link

以下交互可以创建 link node：

- 粘贴 URL；
- 选中文本后执行 “Add link”；
- 显式 Link action。

URL 显示文本可以与 URL 不同，但 canonical identity 始终使用 normalized URL。

### 11.4 File

File node 可以通过：

- 上传新文件；
- 从已有 Project Artifacts 中选择；
- 拖拽文件到 Composer；
- 从已有 Tool / Execution Artifact 插入。

最终都必须获得稳定 `artifact_id` 后才能提交 MeetingMessage。

## 12. File Resource Architecture

Meeting 的 `file` 类型不直接绑定 StoredObject。

完整链路：

```text
MeetingReference / MeetingMessageReference
    -> artifact_id
        -> ToolArtifact
            -> stored_object_id
                -> StoredObject
                    -> ObjectStorageService
                        -> MinIO
```

理由：

- StoredObject 只有存储语义；
- ToolArtifact 提供 Project scope、名称、media type 和来源；
- Meeting 不直接读取 Object Storage，而是通过 ToolArtifact 保持统一 Project 文件边界；
- Agent 已经有统一 Artifact Tools。

## 13. User File Upload

用户在 Meeting Composer 上传文件：

```text
User selects / drops file
    -> Meeting/File Upload API
    -> ObjectStorageService.put(stream)
    -> StoredObject
    -> ToolArtifact(kind = user_upload)
    -> artifact_id
    -> MeetingMessageReference(type = file)
    -> file inline node
```

上传 API 属于业务 API，不要求浏览器直接访问 MinIO credential。

可以使用 ObjectStorageService 的受控 upload contract；后续需要 multipart / resumable upload 时也不改变 Meeting Reference 模型。

上传完成前，Composer 可以显示 local uploading node，但未获得 `artifact_id` 的文件不能成为已提交 MeetingMessage 的 canonical file node。

## 14. Agent File Tools

Agent 不直接操作 ObjectStorageService。

继续复用 Artifact Builtin Tools：

```text
list-artifacts
create-artifact
read-artifact
```

用途：

### list-artifacts

查询 Project 内可访问文件 / Artifact metadata。

### create-artifact

Agent 创建或持久化新文件，相当于 Agent-facing 的受控文件写入 / 上传能力。

返回稳定：

```text
artifact_ref
file_ref / image_ref
```

### read-artifact

根据 artifact reference 读取文本范围、metadata 或得到 file/image reference。

因此 Meeting Context 中：

```text
file / artifact_456
```

足以让 Agent 决定调用 `read-artifact`。

不需要把 Signed URL 注入模型。

## 15. ToolArtifact 对 User Upload 的支持

ToolArtifact 不只允许 Agent 创建。

第一阶段应允许：

```text
ToolArtifact
├── ...
├── kind
│   ├── generated
│   ├── user_upload
│   └── ...
├── created_by_agent_id?
├── created_by_user_id?
└── ...
```

一个 Artifact 只需要属于合法 Project，不要求必须源自 Tool Execution。

`ToolArtifact` 这一名字沿用现有工具系统模型；如果未来 Artifact 演化成更通用的 Project File 领域对象，可以在不改变 Meeting 的 `file -> artifact_id` 引用语义前提下迁移。

## 16. Pin to References

以下 inline node 支持：

```text
Pin to References
├── task
├── knowledge
├── link
└── file
```

不支持：

- text；
- mention。

流程：

```text
Message inline resource node
    -> resolve MeetingMessageReference
    -> compute canonical reference_key
    -> create-or-get MeetingReference
    -> return pinned state
```

Pin 是幂等操作。

如果已经存在：

```text
UNIQUE(meeting_id, reference_key)
```

则直接返回已有 MeetingReference。

Pin：

- 不修改原 MeetingMessage；
- 不修改 inline node；
- 不删除 MeetingMessageReference。

Unpin：

- 只删除 / 解除 MeetingReference；
- 不修改历史 Message；
- 不影响 Message Inline Reference；
- 不删除底层 Task / Knowledge / Artifact / Link。

## 17. Meeting Info References UI

Meeting 页面提供：

```text
Meeting Info
├── Participants
├── Summary
└── References
    ├── Tasks
    ├── Knowledge
    ├── Links
    └── Files
```

References 可以：

- 搜索并直接 Pin；
- Unpin；
- reorder；
- 打开资源；
- 显示 unavailable / deleted 状态。

直接从 References 区域 Pin 的资源不要求先出现在某条 Message 中。

## 18. Context Injection

MeetingContextProvider 每次注入全部当前 MeetingReference，但只注入稳定 identity，不自动读取内容。

概念序列化：

```xml
<meeting_references>
  <task id="task_123" />
  <knowledge id="doc_789" />
  <link url="https://example.com/design" />
  <file artifact_id="artifact_456" />
</meeting_references>
```

这是 Prompt Serialization 示例，不是数据库格式。

Agent 根据需要：

- task → Task Tools；
- knowledge → Knowledge Tools；
- file → Artifact Tools；
- link → 根据自身可用 Tool / 网络能力决定是否访问。

MeetingContextProvider 不因为 Reference 存在而自动执行 retrieval。

## 19. Inline Node Context Serialization

MeetingMessage nodes 按原顺序序列化。

例如持久内容：

```text
text("请 ")
mention(participant_agent_a)
text(" 看一下 ")
task(ref_task_123)
text(" 和 ")
file(ref_file_456)
```

可序列化为：

```xml
请 <mention participant_id="participant_agent_a">@Agent A</mention>
看一下 <task id="task_123">AUTH-123</task>
和 <file artifact_id="artifact_456">trace.zip</file>
```

knowledge：

```xml
<knowledge id="doc_789">Authentication Architecture</knowledge>
```

link：

```xml
<link url="https://example.com/design">design reference</link>
```

Display label 可以进入序列化以保持自然语言可读性，但它不是 authoritative identity。

## 20. Message Immutability

MeetingMessage 发送后继续保持不可变。

因此：

- inline nodes 不允许后续原地编辑；
- label snapshot 不因资源重命名自动重写历史 Message；
- Pin / Unpin 不修改历史 Message；
- 资源被删除时不改写原 Message。

UI 可以在 render 时根据当前资源状态补充：

- 当前标题；
- unavailable；
- deleted。

历史 snapshot 仍保留。

## 21. Resource Resolution State

Task / Knowledge / File 可能后续被删除。

Reference 不应因此静默从历史 Message 消失。

可以在读取时解析：

```text
resolution_state
├── available
└── unavailable
```

其中：

- `available`：当前可以解析；
- `unavailable`：资源不存在 / 已删除。

该状态可以是 read-time projection，不要求长期持久化。

Project 是单用户结构，因此 Meeting Reference 不建立资源级用户 ACL。

创建 / 解析 Task、Knowledge、File Reference 时统一校验：

```text
resource.project_id == meeting.project_id
AND
meeting.project.owner_user_id == current_user.id
```

跨 Project Resource 直接拒绝引用。

## 22. Link 安全

Link node / Reference 只表达 URL identity。

平台：

- 不自动抓取 URL；
- 不把 URL 内容自动加入 Meeting Context；
- 不因为 Link 被 Pin 就认为内容可信；
- UI 外链打开应使用标准安全策略；
- Agent 若访问 URL，仍通过其实际可用能力和安全策略。

## 23. File Project Scope 与生命周期

File Reference 只允许引用当前 Meeting 所属 Project 的 ToolArtifact：

```text
artifact.project_id == meeting.project_id
```

用户侧访问只需要 Project Owner 校验，不再增加 ToolArtifact 级的人类 ACL。

Pin file：

- 不复制 ToolArtifact；
- 不复制 StoredObject；
- 不新增 Signed URL。

Hard delete Meeting：

- 删除 MeetingReference / MeetingMessageReference；
- 不自动删除 ToolArtifact / StoredObject。

删除 ToolArtifact：

- Meeting Reference / Message Reference 可以变为 unavailable；
- 不自动删除历史 MeetingMessage。

这样避免 Meeting 成为文件 ownership / GC 系统。

## 24. Knowledge Project Scope 与生命周期

Knowledge Reference：

```text
knowledge -> canonical document_id
```

Document 更新：

- Reference ID 不变；
- Agent 下次通过 `read-doc` 获取最新版；
- Message label snapshot 保留发送时显示名称。

Document 删除：

- Reference 变为 unavailable；
- 历史 Message 不删除。

## 25. Task Project Scope 与生命周期

Task Reference：

```text
task -> task_id
```

Task title / status 变化：

- 不修改 Message snapshot；
- Agent 使用 Task Tool 获得当前状态。

Task 删除：

- Reference 变为 unavailable；
- 历史 Message 保留。

## 26. Mention 与 Turn Runtime

Mention 是 Message semantic node，同时可以参与 User Turn 的 Agent target selection。

处理顺序：

```text
MeetingMessageContent
    -> collect unique Agent mention participant_ids
    -> merge / resolve explicit picker selection
    -> MeetingTurn target Agent Contributions
```

Runtime 保存最终 target participant IDs。

后续 Runtime 不再重新解析 Message display text。

Mention 本身不会进入 Meeting References。

## 27. API / Command 边界

Meeting 至少需要以下 reference-related commands：

```text
PinMeetingReference
UnpinMeetingReference
ReorderMeetingReferences
ResolveMeetingReference
SearchMeetingReferenceTargets
```

Message 发送命令包含：

```text
SendMeetingMessage
├── content_nodes[]
├── referenced_resource_payloads / reference ids
├── agent selection
└── idempotency_key
```

服务端负责：

- 验证 inline node；
- 验证 resource scope；
- 计算 reference_key；
- 创建 MeetingMessageReference；
- 建立不可变 Message content；
- 解析 Agent mentions 供 Turn Runtime 使用。

## 28. Idempotency

Pin：

```text
(meeting_id, reference_key)
```

天然唯一。

User upload 建议使用 upload / artifact idempotency key，避免网络重试重复创建 Artifact。

SendMeetingMessage 继续使用 Meeting command idempotency key，避免：

- Message 重复；
- MeetingMessageReference 重复；
- Turn 重复；
- Contributions 重复。

## 29. 第一阶段实现边界

第一阶段实现：

1. MeetingReference：task / knowledge / link / file；
2. MeetingMessage content nodes：text / mention / task / knowledge / link / file；
3. `@` Participant Picker；
4. `#` Task / Knowledge / File Resource Picker；
5. URL paste / link action；
6. User file upload -> ToolArtifact -> StoredObject；
7. Pin to References；
8. Meeting Info References；
9. Context serialization；
10. Artifact Tools 集成；
11. Reference Project scope / unavailable 状态。

第一阶段不要求：

- 完整富文本 block document model；
- arbitrary nested nodes；
- 自动抓取 / summarize Link；
- 自动读取 pinned Resource 内容；
- file content 自动注入 Context；
- Reference 自动生成 Knowledge chunk；
- Meeting-owned 文件 GC；
- Agent 自动 Pin Reference。

## 30. Source of Truth

```text
Meeting long-lived resource set
-> MeetingReference

Meeting immutable semantic content
-> MeetingMessage content nodes

Message resource identity
-> MeetingMessageReference

Participant identity
-> MeetingParticipant

Task
-> Task Domain

Knowledge
-> canonical Knowledge Document

File
-> ToolArtifact
    -> StoredObject
        -> ObjectStorageService

Link
-> normalized URL identity
```

Meeting 的 Reference 系统只建立关联与语义，不复制其他 Domain 的业务状态或资源内容。
