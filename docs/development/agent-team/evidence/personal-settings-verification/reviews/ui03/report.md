# 个人设置 UI03 独立窄复核

**PASS：关闭 UI-F2，UI-F1 修复保持。** 未发现本次固定差量中的新增确定阻断。只做独立静审及作者固定原红/修后证据核对，没有再次运行纯测试或真实资源；不宣告完整个人设置/D26验收。

固定 `ui-review-03/manifest.json` SHA `89a165c4f692efda9a58ef838a0a5719a4b8daaf31a689e5893a2e6f2d0962a7`，delta SHA `1e7eaf05c7a6cf98096491d803db9143999d37afda86147e501554a38b6cc3bc`；19文件原字节SHA/size匹配，相对UI02恰usePersonalSettings.ts与personal-settings.spec.ts改变，生成的两文件diff与冻结delta逐字匹配。三核心仍等core02，业务基线仍9a710f272026b41ef69852bbeb41cb7670b500a8。

## 身份边界

固定owner:75–79用私有passwordFeedback保存完整承接identity、origin及requestGeneration，沒有增加公开敏感状态或新请求。ownsPasswordProgress:587–595分别比较包含epoch的两个identity和原请求代号；load:268/293及show:599均消费这一所有权判断，不再按userID重建反馈。

- 原A收到严格确认时，stopPassword:615–631只在原identity完整相同时建立绑定；本次A→B在stopIdentity:549–574要求原owner身份仍等progress.origin，且既有binding有效，才一次继承安全反馈到B。
- B的checking只临时卸载，回到同B identity不resetOwner，绑定保留；合法叶切换及后续Profile读取失败仍投影已确认事实、另附读错误，原UI-F1不退回。
- 后续真正B→C时，B已不同于origin A，resetOwner:153–166清binding且不能继承。旧progress即使仍存在于协调器，owns判断失败，load不能复活。不同用户、失效、明确放弃、真正离页/dispose的原清理路径保持；progress=null也令binding=null。新命令按新的完整origin/requestGeneration重建，不复用上一命令绑定。

## 作者证据

四份check JSON及原raw日志SHA全部核对，精确定位见index.json。

| 记录 | 核实结果 |
| --- | --- |
| ui-f2-red-01 | 1 failed / 12 skipped，失败于新C下仍有旧成功提示；原owner字节等UI02，原两源SHA与实际input一致。原测试到终版仅later对象排版，无断言或预算变化。 |
| ui-f2-format-01 | 两授权文件prettier --write，exit0；不把它称为完整format check。 |
| ui-f2-final-01 | 两文件32 pure通过，19输入逐SHA匹配UI03；包括新增A→B成功、同B重验保留、C新epoch清理以及原UI-F1失败后显式检查和后续Profile503保留确认的断言。 |
| ui-f2-type-01 | exit0，19输入逐SHA匹配UI03。 |

原红日志SHA `d48fe9c1ff177a3ce80c4e6c4a3638795fcdc586ab57bbb515aa2a569fdd0148`；终版32日志SHA `df44c40324f8a886dd2cdc0ebd05fc9af94a83d9ecce2133bf6524aede5f09d2`。原red-input manifest SHA `1437800304a682874101856f075dba8dbb2ad1beebaf611a8992bb7d374507c0`，原件保留。UI02本实例原同probe一次PASS属于UI02，未冒称本实例在UI03再次运行；UI03关闭依据固定差量审查及上述作者受影响32例。

仍复用已验core02与UI01其他有界审查；UI01 web-check108仅归属其原版，不写成UI03全工程重跑。作者其余五fixture、完整真实四组及认证受影响回归/最终资产绑定待后续固定交验。没有浏览器、Docker、Go/npm、网络或新增pure运行，未读活动fixture、未写业务/仓库/Git，无委派；仅本私有report/index，all-stop。
