# 旧16组串行准备（STOP，仅交接）

原卡§9.3的16个top与固定groups及既有list-tops01原raw逐名对应。本次只读原件并写两份小交接，不运行新check/Go-list/filegate/资源、不读取globaldist；root_authorized_resources=false、executable_now=false。

| 顺序 | 原group | 运行短名 | 精确top | expected IDs |
| --- | --- | --- | --- | ---: |
| 1 | old-auth-lifecycle | oldauthlife01 | TestAccountAuthenticationWebSessionLifecycle | 7 |
| 2 | old-auth-revocation | oldauthrevoke01 | TestAccountAuthenticationWebRevocationAndExpiry | 7 |
| 3 | old-personal-profile | oldprofile01 | TestAccountPersonalSettingsWebProfileAndAvatar | 7 |
| 4 | old-personal-theme | oldtheme01 | TestAccountPersonalSettingsWebThemeAndNavigation | 7 |
| 5 | old-personal-password | oldpassword01 | TestAccountPersonalSettingsWebPasswordRotation | 7 |
| 6 | old-invitations | oldinvite01 | TestAccountSystemInvitationsWebOutcomeRecovery | 7 |
| 7 | old-providers | oldproviders01 | TestAccountSystemProvidersWebCredentialReplacement | 7 |
| 8 | old-models | oldmodels01 | TestAccountSystemModelsWebOutcomeRecovery | 7 |
| 9 | old-selection | oldselection01 | TestAccountSystemModelSelectionWebOutcomeRecovery | 7 |
| 10 | old-summary-recovery | oldsummaryrec01 | TestAccountSystemMeetingSummaryWebRecovery | 7 |
| 11 | old-summary-authority | oldsummarynav01 | TestAccountSystemMeetingSummaryWebAuthorityNavigation | 7 |
| 12 | old-account-security | oldaccountsec01 | TestAccountSystemAccountSecurityWebOutcomeRecovery | 7 |
| 13 | old-smtp-settings | oldsmtpsettings01 | TestAccountSystemSMTPSettingsWebOutcomeRecovery | 7 |
| 14 | old-smtp-delivery | oldsmtpdelivery01 | TestAccountSystemSMTPDeliveryWebTestAndRetry | 9 |
| 15 | old-outbound-policy | oldoutbound01 | TestAccountSystemOutboundPolicyWebMutationAndRecovery | 7 |
| 16 | old-public-entry | oldpublic01 | TestAccountPublicEntryWebIdentityNavigation | 7 |

每轮独立nonce与资源ID：通常4容器3网络=7；只有SMTPDelivery再加SMTP容器/网络，共9。InvitationOutcomeRecovery mode=outcome不加SMTP，SMTPSettings不加；全16若均执行预计114不同ID，此数字是计划而非执行结果。

保持各旧top原内部case预算、测试body与重试策略；外层top120s含cleanup/package6m/TCPtail75s不变，每轮实际开窗重新核fresh≥5GiB/livePID-starttime/Docker/TCP基线。不能机械把新Project45s覆盖到旧config。

只有前轮精确top和外层driver实际PASS、direct/adopted wait及watchdogjoin、对应7/9ID双absent、owned/runtime/TCP双清、inputsame和完整原件均完成才可next。任一FAIL或未完成先实际退役保原件，再STOP全批；不继续、重试或调预算。

复用final04/v5/driver-v04 exactBB/955仓库源/66目录/1165SHA与既有工具、实际依赖图和29legacyJS输入，不生成大索引或wholefreeze。绝对run/runtime/screenshot/evidence目录和未来命令模板已列JSON；模板不授权执行。root须另授truecopy及唯一53资产窗口，当前global原3已恢复。

新5作者实际通过为分版组合：read02用v3，其余新4用v5，product19/Go04不变；新3原件及视觉独验仍进行，旧16本任务未执行，不称整卡或独立实际A-B通过。所有历史失败保持。本代两文件写毕STOP。
