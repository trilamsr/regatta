package substrate

import "context"

// ExportedRecordEventForTesting exposes recordEventAppended so the
// substrate_test package can drive the counter without the full
// AppendEvent fixture. Test-only seam; prod callers MUST use
// AppendEvent which routes through the same helper.
func ExportedRecordEventForTesting(ctx context.Context, kind EventKind) {
	recordEventAppended(ctx, kind)
}

// ExportedRecordDivergenceForTesting exposes recordDivergence so the
// enum-guard tests can drive the closed-enum fallthrough without
// inserting audit rows. Test-only seam.
func ExportedRecordDivergenceForTesting(ctx context.Context, programKind, layer string) {
	recordDivergence(ctx, programKind, layer)
}
