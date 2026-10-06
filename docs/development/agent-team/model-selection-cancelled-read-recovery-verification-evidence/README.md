# Selection 取消读取恢复：不可变证据

对应[正式验收报告](../model-selection-cancelled-read-recovery-verification.md)。接受产品为 `debbb28deb7c883fd0b6b77a75354b9b5d7ece0b`，基线 `fd32120eba4c76f67b649248377f4825a78d5d79`，规格 `e4848151fabf50ee94087d5577fd948341379766`。

[index.json](index.json)以 `artifacts` 将逻辑名称映射到 `objects/<sha256>`，记录原字节数与原定位；`objects`按 SHA去重。271逻辑原件、176物理对象，共6,354,610字节。原文和 raw/diff不改写，原件中的临时路径及相对链接保留当时含义，当前读取使用本索引，不执行对象中的脚本。

- `author/`：固定三源阶段、四源input01、正确终态首红/修后与全部15项离线检查、两个真实轮和八图/作者视觉记录。
- `independent/`：三源阶段、原type01前提失败与后续实际纯屏障、私有真实输入/overlay、四项真实准备检查、首轮独立代表及实际终局。`verification-report.md/json`为保留的v1，`verification-report-v2.md/json`仅纠正图片审阅主体。
- `history/`：早期混合输入诊断的四轮原command/raw/result/probe和输入指纹、旧Account两轮command/raw/result/cleanup。诊断绿是复现缺陷；混合Account完整闭包只引用原定位，不复制或冒充本次接受基线。

固定依赖用原input01中的1033项基线及Git读取，不再生成第二份全量基线索引或复制源码树。作者各轮只归档非基线源版本，独立纯验证以固定Git加候选controller/私有探针重建。31dist、已安装依赖和运行时二进制仅保存原指纹，不归档实体；这不宣称当前安装内容或生产绑定经过本次执行。

```sh
env PYTHONDONTWRITEBYTECODE=1 GIT_NO_LAZY_FETCH=1 \
  python3 docs/development/agent-team/model-selection-cancelled-read-recovery-verification-evidence/verify_archive.py
```

[verify_archive.py](verify_archive.py)只读本包和固定Git，核对象SHA/长度、四交付源、1033基线、规格技术正文、26项原检查（含三真实轮）及原清理记录。它不读取 scratch或活动Account源码、不安装依赖、不运行归档脚本、浏览器、服务或fixture；没有以离线复核冒充新的业务验收。历史PPID1 Z及原现场字段缺口保持。
