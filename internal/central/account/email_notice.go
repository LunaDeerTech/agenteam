package account

import (
	"context"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// queueOldEmailNotice commits the notification with the mailbox mutation. The
// canonical delivery-intent reconciler enqueues it after commit, including when
// the initiating HTTP response is lost. No notification contains an OTP/token.
func (s *Service) queueOldEmailNotice(ctx context.Context, x postgres.SQLExecutor, command c.CommandID, original emailRequest, version foundation.Version) error {
	noticeID, e := foundation.NewID[struct{}]()
	if e != nil {
		return unavailable(e)
	}
	jobID, e := foundation.NewID[c.MailJob]()
	if e != nil {
		return unavailable(e)
	}
	_, e = x.Exec(ctx, `INSERT INTO agenteam_account.email_requests(id,user_id,session_id,kind,original_email,target_email,user_version,password_version,passkey_version,phase,created_at,expires_at,last_delivery_at) SELECT $1,$2,$3,'old_email_notice',$4,$4,$5,$6,$7,'verified',n,n+interval '10 minutes',n FROM (SELECT clock_timestamp() n) z`, noticeID.String(), original.user, original.session, original.original, version, original.passwordVersion, original.passkeyVersion)
	if e != nil {
		return unavailable(e)
	}
	_, e = x.Exec(ctx, `UPDATE agenteam_account.commands SET resource_id=$2,attempt_id=$3 WHERE id=$1 AND phase='planned'`, command.String(), noticeID.String(), jobID.String())
	if e != nil {
		return unavailable(e)
	}
	cmd, e := loadCommand(ctx, x, command.String(), false)
	if e != nil {
		return e
	}
	return s.insertDelivery(ctx, x, cmd)
}

func isEmailNoticeCommand(name string) bool {
	switch name {
	case "email-change-confirm", "email-initialize-confirm", "operator-recovery-confirm":
		return true
	}
	return false
}
