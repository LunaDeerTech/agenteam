# 公开账号入口规格原件

[正式规格记录](../../public-account-entry-spec-verification.md)区分可行性、规格通过、实施授权和未来验证；当前没有本卡业务验收结论。原件均保持原字节，原绝对路径及本地位置见 [original-map.json](original-map.json)。`acceptance/before.md` 与 `spec/rev1.md` 相同，映射到同一份物理字节。

- `feasibility/`：早期固定认证9a、个人设置尚未验后的有界分析，旧前置状态保留。
- `spec/`：rev1原卡、31 Git locator与作者自查，最终业务基线c54。
- `review/`：独立 STATIC PASS、检查和索引；没有动态结果。
- `acceptance/`：采纳前后记录和原行政patch，绑定接受提交e5a5ccf。
- `plan/`：独立后续风险计划与索引；最多2类pure+1真实组合是去重计划，尚未执行。

在仓库根运行以下只读检查，不复制源树、不安装依赖或运行产品：

```sh
python3 docs/development/agent-team/evidence/public-account-entry-spec-verification/verify_inputs.py --repo .
```

脚本核原件、31 Git blob/SHA/长度、独立索引引用、接受提交以及活卡技术§1–9。原卡内相对链接保持原工作项目录语境；本次只检查正式报告/活卡/状态页的有效链接，不改原件以迁移链接。`archive-checks.json` 记录实际命令/结果及文档检查；`whitespace-exceptions.json` 只列原patch的实际格式诊断，不能扩大排除。`SHA256SUMS`覆盖本目录除自身外全部文件。

本组仅规格及行政状态归档，没有Go/npm/browser/Docker/网络或Git写操作。下一阶段须绑定冻结实现与真实前置证据；生产SPA、完整D26/D27、Object/Artifact/Project阻断保持。
