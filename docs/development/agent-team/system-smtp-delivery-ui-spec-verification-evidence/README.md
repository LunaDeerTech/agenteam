# SMTP 投递 UI 规格静态证据

关联[规格报告](../system-smtp-delivery-ui-spec-verification.md)。这是规格与固定接缝审查，非产品验收。

[index.json](index.json)把30个逻辑原件映射到原来源、SHA256、字节数及`objects/<sha>`或固定Git。29对象保留原字节；最终正式卡可由`git show 8bdfb006b32fbbc8889a190d7829f05e93e29787:docs/development/work-items/d27-system-smtp-delivery-ui.md`读取。`git_sources`为49次引用去重后的47个628612c文件，不复制原源码或运行闭包。

逻辑分组：`author/`保存rev0.1/rev0.2、精确差量、scope17、seams与自查；`independent/`保存review、原静态script/command/raw/绑定和准备勘误；`formal-rev1/`保留页首原版及提取前提失败；`formal-rev1-v2/`保存删旧时态单句与最终冻结。原绝对scratch路径只是历史定位，不要求运行时存在。

```bash
env PYTHONDONTWRITEBYTECODE=1 GIT_NO_LAZY_FETCH=1 python3 docs/development/agent-team/system-smtp-delivery-ui-spec-verification-evidence/verify_archive.py
```

只读本档案和固定Git，校验原件／技术正文／17范围／原静态实际退出；不执行保存的脚本、测试或服务。五新／五旧用例仍是规格要求，无动态验收声明。原retry状态码准备误述、页首提取失败保留；同完整identity与原CSRF仍合法约束不放宽。
