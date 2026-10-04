package object

import (
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
)

type runtimeConfig struct {
	storage  StorageConfig
	transfer TransferEndpoint
	download DownloadKeyring
	spool    string
}
type RuntimeConfig struct{ data func() runtimeConfig }

func LoadRuntimeConfig(lookup func(string) (string, bool), cursors cursor.Keyring, secrets secret.Keyring) (RuntimeConfig, error) {
	if lookup == nil {
		return RuntimeConfig{}, invalid()
	}
	storage, err := LoadStorageConfig(lookup)
	if err != nil {
		return RuntimeConfig{}, err
	}
	transfer, err := LoadTransferEndpoint(lookup, storage)
	if err != nil {
		return RuntimeConfig{}, err
	}
	raw, _ := lookup("AGENTEAM_CENTRAL_OBJECT_DOWNLOAD_KEYRING")
	download, err := LoadDownloadKeyring(raw, cursors, secrets)
	if err != nil {
		return RuntimeConfig{}, err
	}
	spool, ok := lookup(EnvironmentPrefix + "SPOOL_DIR")
	if !ok {
		spool = "/var/lib/agenteam/object-spool"
	}
	if !validSpoolPath(spool) {
		return RuntimeConfig{}, invalid()
	}
	d := runtimeConfig{storage, transfer, download, spool}
	return RuntimeConfig{func() runtimeConfig { return d }}, nil
}
func validSpoolPath(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path && path != "/" && !strings.ContainsAny(path, "\x00\r\n")
}
func (c RuntimeConfig) Validate() error {
	if c.data == nil {
		return invalid()
	}
	d := c.data()
	if d.storage.Validate() != nil || d.transfer.data == nil || d.transfer.data().Validate() != nil || d.download.Validate() != nil || !validSpoolPath(d.spool) {
		return invalid()
	}
	return nil
}
func (c RuntimeConfig) Storage() StorageConfig {
	if c.data == nil {
		return StorageConfig{}
	}
	return c.data().storage
}
func (c RuntimeConfig) TransferEndpoint() TransferEndpoint {
	if c.data == nil {
		return TransferEndpoint{}
	}
	return c.data().transfer
}
func (c RuntimeConfig) DownloadKeyring() DownloadKeyring {
	if c.data == nil {
		return DownloadKeyring{}
	}
	return c.data().download
}
func (c RuntimeConfig) SpoolDirectory() string {
	if c.data == nil {
		return ""
	}
	return c.data().spool
}
func (RuntimeConfig) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "object_runtime_config") }
func (RuntimeConfig) MarshalJSON() ([]byte, error) { return []byte(`"object_runtime_config"`), nil }
func (*RuntimeConfig) UnmarshalJSON([]byte) error  { return invalid() }
func (RuntimeConfig) LogValue() slog.Value         { return slog.StringValue("object_runtime_config") }
