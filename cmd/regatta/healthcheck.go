// healthcheck: distroless containers ship without sh or wget, so the
// docker-compose HEALTHCHECK invokes this in-binary subcommand instead.
// Exit 0 on HTTP 200 from /healthz, exit 1 otherwise.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const subcmdHealthcheck = "healthcheck"

func runHealthcheck(args []string) int {
	return runHealthcheckWith(args, doHealthcheck)
}

func runHealthcheckWith(args []string, probe func(ctx context.Context, url string) error) int {
	fs := flag.NewFlagSet(subcmdHealthcheck, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	url := fs.String("url", "http://localhost:8080/healthz", "healthz URL to probe")
	timeout := fs.Duration("timeout", 2*time.Second, "per-request timeout")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	if err := probe(ctx, *url); err != nil {
		fmt.Fprintf(os.Stderr, "healthcheck: %v\n", err)
		return 1
	}
	return 0
}

func doHealthcheck(ctx context.Context, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status=%d", resp.StatusCode)
	}
	return nil
}
