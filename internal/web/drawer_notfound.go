package web

import "net/http"

// drawerNotFoundHTML is the styled fragment htmx swaps into the drawer
// slot when the requested row no longer exists. Replaces the raw
// "404 page not found" text/plain leak that bled the http.NotFound
// stdlib output into the operator UI (R-MEGA-2 P5).
const drawerNotFoundHTML = `<div class="drawer-empty" role="status">Item not found.</div>`

// writeDrawerNotFound serves the styled 404 fragment so htmx swaps a
// readable message rather than the stdlib text/plain leak.
func writeDrawerNotFound(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte(drawerNotFoundHTML))
}
