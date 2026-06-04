//revive:disable:exported
package approvals_shadow

import "log/slog"

const SystemActor = "system"

type ShadowMode string

const (
	ShadowModeOff ShadowMode = "off"
	ShadowModeOn  ShadowMode = "on"
)

type ReadMode string

const (
	ReadModeLegacy         ReadMode = "legacy"
	ReadModeSubstrateFirst ReadMode = "substrate_first"
)

type ShadowWriteConfig struct {
	Mode     ShadowMode
	ReadMode ReadMode
	Key      []byte
	KeyID    string
	TenantID string
	RunID    string
	Logger   *slog.Logger
}
