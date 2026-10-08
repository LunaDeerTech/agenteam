old16-batch01 已STOP：2 PASS / 1 FAIL / 13 NOT_RUN，已执行三轮全部实际退役。

| 轮次 | 结果 | top秒 | 原direct command秒 | adopted实际wait | TCP tail秒 |
| --- | --- | ---: | ---: | ---: | ---: |
| oldauthlife01 | PASS | 7.60 | 53.563 | 4 | 39.394 |
| oldauthrevoke01 | PASS（revocation/expiry） | 12.42 | 64.721 | 8 | 37.385 |
| oldprofile01 | FAIL | 8.54 | 58.746 | 4 | 38.399 |

最早失败为旧 personal-settings.spec.ts:441:27 的 await duplicate.json()，Chromium报 response.json: Protocol error (Network.getResponseBody): No data found for resource with given identifier。读取重复用户名响应body时抛错，field_errors/username/ALREADY_EXISTS断言及后续profile/avatar步骤未完成，无最终browser结果/Go事实结论。尚不归因于产品或旧测试；保留原错供独审。

每轮7实际ID、共21不同ID逐一双absent；3direct、16adopted实际wait与3watchdog join齐，owned/runtime/TCP双清，输入轮内及轮间同，monitor/cancel/forced均0。12新增daemon/PID1 shim非owned未wait单列，不称全机清零。114只是原16全跑计划，不是本次结果。

每轮24个原文件和独立launch raw均按原路径/SHA引用，未复制原件或重冻运行输入。旧域原断言单列；expiry仅私有调整一行过期时间后走真实GET/router，不称自然过期等待，也未借用任何新Project schema/client统计。余13组从oldtheme01起未启动，v04失败guard保持，无后继版本/重试/源码或预算修订。

全批无当前资源/reader，root拥有原3资产恢复；退役后恢复不算in-round输入漂移。批次及失败独审待进行，本实例已STOP。
