package web

import (
	"log/slog"
	"net/http"
)

// internalErrorMessage is the body served to clients on any internal
// error. We deliberately avoid revealing the cause — operator-visible
// detail lives in the server log via slog (R-MEGA-3 LIVE-5).
const internalErrorMessage = "internal server error"

// writeInternalError logs err (with op + request context) and writes a
// fixed 500 body that never exposes err.Error to the caller. Pass
// logger=nil to use slog.Default.
func writeInternalError(w http.ResponseWriter, logger *slog.Logger, op string, err error) {
	if logger == nil {
		logger = slog.Default()
	}
	logger.Error("web.internal_error", "op", op, "err", err.Error())
	http.Error(w, internalErrorMessage, http.StatusInternalServerError)
}
