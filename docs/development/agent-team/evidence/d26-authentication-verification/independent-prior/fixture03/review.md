# D26 fixture03 selector 差量窄审

**STATIC PASS：已知 getByLabel exact 前置缺陷闭合，可按原组/原预算复验。** 本轮没有运行 TypeScript、npm、浏览器、Go、Docker 或网络。首轮真实三红及独立算法报告保持原字节；不将定位修正称为真实浏览器已通过。

固定 manifest `/workspace/agenteam-d26-auth-author-jrfhr6h1/evidence/fixture-input-03/manifest.json` SHA `34830f47406ccb2bc812e8c7cb30c5d0c8f8af93710e2cc7b5cf8caba8b91de9`，前版 fixture02 `75e43c7ddf26b9005c0ea0cde48a37e085221bd15a78f461315cd931f733743b`，基线仍为 `457b197`。前后21份源码逐 SHA 核对，唯一差量为 `tests/account-captcha-web/e2e/authentication.spec.ts`；独立从前后文本生成的统一 diff 与作者 delta `c75acec5ad214625ba06965cf1e156fddcd620058dc4c629bea0aa837bc63570` 字节相同。

邮箱全部采用 textbox + exact accessible name，避开原 label DOM 文本中的 aria-hidden 星号；密码使用实际 `#login-password`，在 openLogin/fill/注销后做 positive accessible-name 检查。清空值、键盘焦点及截图前不存在的断言都改为同一实际控件，不保留因错误 label selector 而总是空匹配的路径。密码不被错误当作有 textbox 隐式角色；原私有密码填写错误保护保留。

没有减少原 editable、标题/键盘 focus、真实状态码、Cookie、pass80/完整同 key 输入、Dashboard、主题/布局等断言；四 Go 顶层及六 browser case、45s/2min/worker1/retries0 等配置和20其他源码均未改。八项 dependency/dist manifest 声明前后相同；本轮只核固定声明，没有读作者活动构建目录，实际运行前后仍由执行者核其字节。

`browser-spec-type-04.log` 与 metadata 指纹匹配，作者实际 `tsc --noEmit ...` exit0、0.914783239s，原记录 env={}；这是作者的纯检查，不是我的执行或运行环境全集。静态 SHA/diff 结果在 `checks.json`，定位过程只执行 Python 文件读取/哈希和文本比对，没有 Git 写或仓库/作者原件修改。

完整真实行为待同原组复验。首轮日志只覆盖标题可聚焦后 label selector notfound，未覆盖登录/verify；此次修正不排除后续可能出现的新真实问题。独立增量仍保留一条真实失败焦点→新题/Session CSRF→注销链计划，待作者实际有效结果后去重。报告冻结后 all-stop。
