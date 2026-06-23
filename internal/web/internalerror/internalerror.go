// Package internalerror writes a generic 500 to the client while logging
// the full error server-side. Prevents err.Error leakage (R-MEGA-3 LIVE-5).
package internalerror

import (
	"log/slog"
	"net/http"
)

// genericMessage is the body served to clients on any internal error. We
// deliberately avoid revealing the cause — operator-visible detail lives
// in the server log via slog.
const genericMessage = "internal server error"

// Write logs err (with op + request context) and writes a fixed 500 body
// that never exposes err.Error to the caller. Pass logger=nil to use
// slog.Default.
func Write(w http.ResponseWriter, logger *slog.Logger, op string, err error) {
	if logger == nil {
		logger = slog.Default()
	}
	logger.Error("web.internal_error", "op", op, "err", err.Error())
	http.Error(w, genericMessage, http.StatusInternalServerError)
}
