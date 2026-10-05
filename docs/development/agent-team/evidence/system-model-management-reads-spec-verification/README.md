# System Model 管理读口规格证据

[正式记录](../../system-model-management-reads-spec-verification.md)区分规格通过、实施授权和未来业务验证。`spec/`、`review/`、`plan/`、`feasibility/` 保留原字节；[原件映射](original-map.json)记录原绝对路径及重复字节的别名，不依赖临时目录才能核验。

`acceptance/card.md` 是提交 bfae86b 的原卡；两个 administrative patch 是归档时从明确前后文件计算的差量，非历史命令输出。`implementation-status.md` 固定本次行政接续，避免校验索引绑定以后会更新的活卡。原卡 §1–9、独立报告和原检查不倒写；Secret Unknown 的准确限定在原后续计划及正式记录中。

仓库根可执行：

```sh
python3 docs/development/agent-team/evidence/system-model-management-reads-spec-verification/verify_inputs.py --repo .
```

检查仅校验本目录指纹、13 件原始文件/3 个同字节别名、47+1 个固定 c54 Git 对象、bfae86b 卡字节及三版本技术 §1–9，不复制 Git 源码或依赖树，不运行业务。原 Markdown 内的链接保持原路径语境；文档检查核正式记录、活卡、三状态页和本 README，不修改原件链接。

`SHA256SUMS` 覆盖本目录除自身外全部文件；`archive-checks.json` 记录本次真实只读检查。格式诊断与精确例外另列，不将旧原检查改成覆盖新归档。没有源码验收、运行资源或 Git 写操作。
