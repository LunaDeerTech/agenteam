# noSecretRead 单处 SQL 候选：STOP

唯一候选是 tests/model/platform_embedding_resolution_fixture_test.go 的 noSecretRead 查询追加 `AND metadata->>'consumer'='model'`，期待仍严格为0；原file cd738f93保持仓库未写。反向移除此片段精确还原所有原bytes，未增测试、imports或其他改动。

依据：audit/contract/metadata.go:26、86、146–150正式 consumer=model 的JSON字段；Secret两个材料读取入口在 secret/lease.go:319、secret/model_usage.go:112共用同一writer，model_usage.go:182–195用grant.Consumer生成secret.resolve。Model resolution_secret.go:181给sc.Model；Account secret_authority.go:392给sc.System。正式identity helper三Login/两invite worker能产生system记录。旧实际仅总count5，未保存逐行归属，不声称五条实际全来自Account。

过滤仅按正式consumer，覆盖System/Project全部Model凭据、所有actor与历史/当前lease；不按project/actor/当前lease缩窄，不减常量5，也不采全库baseline。五作者top末尾与独立A/B两处均复用此helper，无ref路径仍允许零条，有ref/历史/Unknown所留任何Model材料审计都会被计入。仍只是既有持久审计行断言，未新增对回滚内读取尝试的独立证明。

原Selection FAIL、原cleanupfalse与独立临时目录退休成功原件均不变。候选未安装、未gofmt/Go/driver/资源运行。接受后仅需必要包编译/vet及共享Go单hash差量，实际Selection/其余top/A-B仍待root逐轮授权。sources/impact/patch由freeze固定，STOP后不写本目录。
