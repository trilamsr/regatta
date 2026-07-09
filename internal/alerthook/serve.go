package alerthook

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// ServeOptions wires the in-process variant. W3 supervisor (regatta
// serve) calls Serve with the operator's AlarmWebhook config block.
// Both binaries (cmd/regatta-alarm-webhook and cmd/regatta serve) end
// up driving the same Handler so behaviour stays byte-equal.
type ServeOptions struct {
	ListenAddr string
	GHRepoOwner string
	GHRepoName  string
	GHToken     string
	GHBaseURL   string
	Logger      *slog.Logger
}

// Serve runs the AlertManager receiver until ctx is cancelled, then
// gracefully shuts the listener down. The function blocks; callers
// goroutine-launch it.
func Serve(ctx context.Context, opts ServeOptions) error {
	if opts.ListenAddr == "" {
		return errors.New("alerthook: ListenAddr empty")
	}
	if opts.GHRepoOwner == "" || opts.GHRepoName == "" {
		return errors.New("alerthook: gh repo owner+name required")
	}
	if opts.GHToken == "" {
		return errors.New("alerthook: gh token empty")
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	client := NewHTTPGitHubClient(opts.GHToken, opts.GHRepoOwner, opts.GHRepoName, opts.GHBaseURL)
	h := &Handler{Client: client, Logger: logger}
	stopReaper := h.startReaper(0)
	defer stopReaper()

	mux := http.NewServeMux()
	mux.Handle("/webhook", h)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := &http.Server{
		Addr:              opts.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("alerthook.listen", "addr", opts.ListenAddr,
			"repo", opts.GHRepoOwner+"/"+opts.GHRepoName)
		err := srv.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		// Use a detached context with a 10s deadline so Shutdown can
		// drain in-flight handlers even though ctx is already cancelled.
		sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(sctx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
		return nil
	case err := <-errCh:
		return err
	}
}
