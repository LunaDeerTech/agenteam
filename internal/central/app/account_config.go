package app

import (
	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/accountmail"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	"github.com/LunaDeerTech/agenteam/internal/central/recoverylog"
)

// Apply one deployment policy to every browser boundary and recovery channel.
func createAccountService(cfg config.Config, deps account.Dependencies) (*account.Service, error) {
	deps.DevMode = cfg.DevMode()
	return account.New(deps)
}

func createAccountMailWorker(cfg config.Config, deps accountmail.Dependencies) (*accountmail.Worker, error) {
	deps.DevMode, deps.PublicOrigin = cfg.DevMode(), cfg.PublicOrigin()
	return accountmail.New(deps)
}

func openAccountRecoveryLog(cfg config.Config) (*recoverylog.Sink, error) {
	return recoverylog.OpenWithOptions(cfg.AccountRecoveryLog(), recoverylog.Options{DevMode: cfg.DevMode()})
}
