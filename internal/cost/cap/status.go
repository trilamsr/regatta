package costcap

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/trilamsr/regatta/internal/cost/spend"
)

// PrintStatus renders the plain-text status surface `regatta cost
// status` consumes (spec §6.1). Bypasses memoize so the operator
// always sees fresh numbers — status is rare, not a hot path.
//
// When CapMicro=0 the global ceiling is unset; the message explains
// that per-scope caps may still throttle individual spawns.
func PrintStatus(ctx context.Context, w io.Writer, e *Enforcer) error {
	if e == nil || e.CapMicro() == 0 {
		var spendMicro spend.USDMicro
		if e != nil {
			sm, _, _, _, _ := e.Snapshot(ctx)
			spendMicro = sm
		}
		if _, err := fmt.Fprintf(w, "24h spend : $%.2f\n", spendMicro.USD()); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(w, "daily cap : unset (no global ceiling)"); err != nil {
			return err
		}
		_, err := fmt.Fprintln(w, "state     : Active (per-scope caps may still throttle individual spawns)")
		return err
	}
	spendMicro, capMicro, state, resumeAt, err := e.Snapshot(ctx)
	if err != nil {
		return fmt.Errorf("cost status: snapshot: %w", err)
	}
	if _, err := fmt.Fprintf(w, "24h spend : $%.2f\n", spendMicro.USD()); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "daily cap : $%.2f\n", capMicro.USD()); err != nil {
		return err
	}
	switch state {
	case Active:
		head := capMicro - spendMicro
		pct := 0.0
		if capMicro > 0 {
			pct = float64(head) / float64(capMicro) * 100.0
		}
		if _, err := fmt.Fprintln(w, "state     : Active"); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "headroom  : $%.2f (%.0f%%)\n", head.USD(), pct); err != nil {
			return err
		}
	case Throttled:
		// eta is sub-minute precise — operator-readable not protocol-bound.
		eta := time.Until(resumeAt).Truncate(time.Minute)
		if _, err := fmt.Fprintln(w, "state     : Throttled"); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "auto-resume at: %s  (in %s)\n",
			resumeAt.In(e.TZ()).Format("2006-01-02 15:04 MST"), eta); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(w, "override  : run `regatta resume` to spawn now (counts against tomorrow's cap)"); err != nil {
			return err
		}
	}
	return nil
}
