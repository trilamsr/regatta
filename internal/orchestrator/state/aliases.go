package state

import "github.com/trilamsr/regatta/internal/orchestrator/state/approvals_shadow"

type ShadowWriteConfig = approvals_shadow.ShadowWriteConfig //nolint:revive // re-export shim; godoc lives on origin
type ShadowMode = approvals_shadow.ShadowMode               //nolint:revive // re-export shim; godoc lives on origin
type ReadMode = approvals_shadow.ReadMode                   //nolint:revive // re-export shim; godoc lives on origin

const (
	ShadowModeOff          = approvals_shadow.ShadowModeOff          //nolint:revive // re-export
	ShadowModeOn           = approvals_shadow.ShadowModeOn           //nolint:revive // re-export
	ReadModeLegacy         = approvals_shadow.ReadModeLegacy         //nolint:revive // re-export
	ReadModeSubstrateFirst = approvals_shadow.ReadModeSubstrateFirst //nolint:revive // re-export
)
