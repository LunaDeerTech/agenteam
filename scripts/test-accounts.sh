#!/bin/sh
# Fixed partitions retain the original six-minute per-package budget. Pure
# account libraries also run as complete packages; the fixture driver does not
# include them. Independent review-only probes remain in their own overlays.
set -eu
cd "$(dirname "$0")/.."
if [ "$#" -gt 1 ]; then
  printf '%s\n' 'Usage: test-accounts.sh [all|mutations|identity|mail|profile|avatar|avatar-recovery|http|app|library]' >&2
  exit 2
fi
agenteam_group=${1:-all}
case "$agenteam_group" in
  all|mutations|identity|mail|profile|avatar|avatar-recovery|http|app|library) ;;
  *) printf '%s\n' 'Unknown account test group.' >&2; exit 2 ;;
esac
# Serialize package compilation; concurrency inside a test remains unchanged.
GOFLAGS=${GOFLAGS:--p=1}
export GOFLAGS
agenteam_mutations='^(TestAccountResetPreparedBeforePasswordChangeCannotPublishOldVersion|TestAccountInvitationConcurrentUniquenessAndRevocation|TestAccountResetIPQuotaCountsBothExistenceClassesAndReplay|TestAccountChangedCookieForceWaitsActualUseJoin|TestAccountCleanupDurablePassDoesNotStarveTailBehindHundredProtectedCommands|TestAccountB02MutationUnknownKeepsOriginalStateAndFacts|TestAccountB02PlannedUnknownRecoveryWaitsOriginalWriter|TestAccountCaptchaOfficialVueActualBrowser|TestAccountChallengeQuotaRollbackAndRestart|TestAccountChallengeGlobalDatabaseQuota|TestPublicRotationKnownGeometry|TestPublicRotationOriginalCounterexamples|TestPublicRotationRejectsUnobservableGeometry|TestAccountChallengeRealGenerationConsumptionAndBinding|TestAccountDeliveryHandlerRealOutboxTransactionRollbackAndCanonical|TestAccountInvitationExplicitResendVersionAndHistoricalReceipt|TestAccountDeliveryPlanRejectsChangedSourceMapping|TestAccountInvitationAtomicSecretIntentEventAndRevocation|TestAccountInvitationRedeemCurrentProofAndCleanup|TestAccountExpiredLinkInvalidWhileActualMaterialUserKeepsLease|TestAccountPasswordChangeAtomicRevocationAndLostResponse|TestAccountPasswordResetConsumesTokenWithoutSessionAndSafeReplay|TestAccountPasswordResetDifferentCommandsHaveOneWinner|TestAccountResetPublicQueueAndAsynchronousMaterial)$'
agenteam_identity='^(TestAccountCurrentSessionRequiresRealHeldUserAndCurrentRole|TestAccountCrashHelper|TestAccountRecoveryExactChildDeathAndNoBootstrapReprint|TestAccountSessionIssuedLimitsAndTouchRemainCurrent|TestAccountResponseWindowEndsWithoutRevivingOrExtendingSession|TestAccountRecoveryProtectedHundredPlansCannotStarveJoinedReader|TestAccountCurrentHumanBindsSecretAndOutboundWithoutLateLocks|TestAccountBootstrapLoginReplayAndIndependentResponseLeases|TestAccountBootstrapConcurrentSingleUserAndOutput|TestAccountLogoutReplansOnlyHeaderAndRejectsOldPrepared|TestAccountLogoutReplanKeepsCurrentRevocationGate|TestAccountLogoutReplanUnknownSerializesBeforeRetry|TestAccountMigrationFreshAndNineUpgrade|TestAccountMigrationDDLAndJournalRollback|TestAccountAuditFailureRollsBackLoginAndDestroysUnconfirmedRead|TestAccountUsagePlansCannotBeForgedDowngradedOrBypassed|TestAccountReadWaitsExactCurrentUserGateAndRejectsRevocation|TestAccountMissingChallengeProviderAndFailureReceiptPrivacy|TestAccountCurrentRegularHumanAuditDoesNotGrantSystem|TestAccountKeyRegistryRequiresOldReferencedKeysAndRejectsReuse|TestAccountResponseRecoveryPreservesActiveUseAndDeletesAfterJoin|TestAccountResponseForceKeepsRealUseRegistered|TestAccountResponseReleaseRejectsChangedExactFence|TestAccountResponsePlanUnknownKeepsCauseAndConvergesAfterWriter|TestAccountLogoutAuditEventAndCurrentRevocationAtomic|TestAccountResponseCloseWaitsActualUseAndKeepsOtherLease|TestAccountReadWindowCurrentPasswordAndActivity|TestAccountUnknownConfirmationRetainsState|TestAccountFailedLoginUnknownKeepsFailureFactsAtomic|TestAccountLogoutUnknownKeepsEventAuditAndRevocationAtomic|TestAccountBootstrapUnknownDoesNotReprint|TestAccountUnknownNeverExposesUnconfirmedLoginMaterial)$'
agenteam_mail='^TestAccountMail'
agenteam_profile='^TestAccount(Profile|CurrentUserRoute)'
agenteam_avatar='^TestAccountAvatar(Cleanup|Reader|Body|Invalid|Published|Put|Original|Apply)'
agenteam_avatar_recovery='^TestAccountAvatar(Migration|Unknown|Crash|Recovery)'
agenteam_http='^TestAccountHTTP'
agenteam_app='^(TestB04App|TestAccountProcess)'
agenteam_run() {
  printf 'Account integration group: %s\n' "$1"
  sh scripts/test-objects.sh -run "$2"
}
agenteam_library() {
  AGENTEAM_GO=${AGENTEAM_GO:-go}
  GOTOOLCHAIN=local
  export AGENTEAM_GO GOTOOLCHAIN
  if [ "$("$AGENTEAM_GO" env GOVERSION)" != 'go1.27.1' ]; then
    printf '%s\n' 'Go go1.27.1 is required; set AGENTEAM_GO to that binary.' >&2
    exit 1
  fi
  printf '%s\n' 'Account integration group: library'
  "$AGENTEAM_GO" test -tags=integration -race -count=1 -timeout=6m ./internal/central/account/...
}
case "$agenteam_group" in
  mutations) agenteam_run mutations "$agenteam_mutations" ;;
  identity) agenteam_run identity "$agenteam_identity" ;;
  mail) agenteam_run mail "$agenteam_mail" ;;
  profile) agenteam_run profile "$agenteam_profile" ;;
  avatar) agenteam_run avatar "$agenteam_avatar" ;;
  avatar-recovery) agenteam_run avatar-recovery "$agenteam_avatar_recovery" ;;
  http) agenteam_run http "$agenteam_http" ;;
  app) agenteam_run app "$agenteam_app" ;;
  library) agenteam_library ;;
  all)
    agenteam_run mutations "$agenteam_mutations"
    agenteam_run identity "$agenteam_identity"
    agenteam_run mail "$agenteam_mail"
    agenteam_run profile "$agenteam_profile"
    agenteam_run avatar "$agenteam_avatar"
    agenteam_run avatar-recovery "$agenteam_avatar_recovery"
    agenteam_run http "$agenteam_http"
    agenteam_run app "$agenteam_app"
    agenteam_library
    ;;
esac
