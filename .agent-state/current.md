# Knowledge 当前 Human Owner 有界正文 HTTP

- 本树 `/workspace/agenteam-knowledge-content-http`，分支 `ai/knowledge-content-http`，基线 main `3b7ed9da35844e3a367cc5e9da0cf424ab36499a`。Variables UI 是唯一作者；root 管 Git/真实资源，Work 是 SPEC/领域单点未参与者审查。新正式 commandhttp 独立19文件不作为本树实现依赖，不强行更新base。
- SPEC rev1 已获 Work 独立有限接受，无must-fix：唯一 `/{document_id}/content` GET/HEAD，默认64KiB/max1MiB与规范byte_offset/max_bytes；ReadDocument负责当前权限/真实canonical reader同步Close。PDF/DOCX仅dependency_unbound，不是原文件下载；HEAD同GET完整消费但不写body。2s原预算含SQL/read/Close/HTTP callback实际尾，Object Runtime join停止项保持。
- 第一可恢复源码片段：新增contenthttp/{handler,query,wire,io}.go与api/openapi/knowledge-content.json，source.go只把ReadDocument/OpenCanonical两次已授权tombstone出口NotFound改ResourceDeleted。后一次可首次观察delete，不能由HTTP泛转404；缺行/foreign/权限/Object错误、打开后VersionConflict+原Close、SourceResolver/preview未改。
- IO从本main已验metadata方法适配；新包仅正常且实际join后清deadline，异常保deadline并abort，不开无界fallback。wire显式12字段+text或dependency_unbound、实际字节/原offset溢出校验、7MiB编码上限；query严格两个参数、decoded重复/整数上限。Schema独立，不修改旧metadata/POST/schema/root/SQL/依赖。
- 已做 be96df 卡本地链接/LF/diffcheck0；b5e0cf第一源码diffcheck0；b86a63新Schema JSON和Draft202012组件结构校验0，尚无实际handler向量。不曾运行Go编译/gofmt/tests/vet/PG/native/socket或新cache；源码格式与所有产品行为仍待后继实际检查，不把静态检查当编译。
- 本片段8路径统一freeze交root保存（本current、卡、4个新contenthttp源、新Schema、source.go）。新增相邻content_source_test.go及本包query/wire/handler/I/O/Schema/native、真实PG测试尚待写；不能以未实现控制声称验收。下一步先补必要定向测试和源码自检，Go/真实资源各等root明确窗口；复用未变B02和原HTTP证据，不默认重跑整个旧矩阵。
