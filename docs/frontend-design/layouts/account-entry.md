# 账号入口布局

> 业务来源：[账号认证架构](../../architecture/platform-infrastructure/authentication/README.md)。本页仅定义可见页面、字段和反馈。

## 1. 页面职责与区域示意

登录、邀请注册、找回和重置使用独立居中面板，不显示已登录导航。

```text
产品标识 / 当前页面标题
┌────────────────────────────┐
│ 步骤说明                   │
│ 表单与字段错误             │
│ 主操作 / 返回登录           │
│ 投递结果或链接失效提示     │
└────────────────────────────┘
```

## 2. 栏目、字段与操作

| 页面 | 字段 | 操作及反馈 |
| --- | --- | --- |
| 登录 | 邮箱、密码 | 登录 / 找回密码；无公开注册入口 |
| 邀请注册 | 邀请邮箱只读、显示名、密码、确认密码 | 验证链接后显示表单，成功提示返回登录；失效提示联系管理员 |
| 找回密码 | 邮箱 | 提交请求、投递渠道说明、返回登录；不公开 token 或账号存在性 |
| 重置密码 | 新密码、确认密码 | 有效链接才可提交；成功返回登录，失效可重新发起找回 |

初始化、邀请有效期和撤销、注册消费、密码与 Session 规则均以[账号生命周期](../../architecture/platform-infrastructure/authentication/account-lifecycle.md)为准，不在布局中另行定义。

## 3. 默认选择与导航

登录成功进入系统首页或经重新授权的指定目标。管理员首次登录显示改密建议及个人设置密码页入口，可以继续使用系统。邀请邮箱不可通过表单修改；已登录账号访问另一身份邀请时提示身份不匹配，不自动绑定。

## 4. 投递、加载与异常

链接验证中显示加载，网络失败可重试；提交中禁重复操作。字段错误就近展示，成功后清空密码输入。

SMTP 投递显示发送结果和失败重试；未配置时展示后台日志获取及转交说明，不在公开页面展示恢复链接。两种渠道、发送失败规则与日志边界由[SMTP Delivery](../../architecture/platform-infrastructure/authentication/smtp-delivery.md)定义。投递失败保留邮箱，不显示凭据或 token。

## 5. 窄屏

面板单列适配可用宽度，长邮箱换行，按钮和返回入口保持可达，不产生页面级横向溢出。

## 6. 相关架构

[账号生命周期](../../architecture/platform-infrastructure/authentication/account-lifecycle.md)、[SMTP Delivery](../../architecture/platform-infrastructure/authentication/smtp-delivery.md)、[安全与治理](../../architecture/security-governance/README.md)。
