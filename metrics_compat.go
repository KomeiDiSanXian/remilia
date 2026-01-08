package remilia

import "github.com/KomeiDiSanXian/remilia/infra/metrics"

// formatAttempt is kept for backward compatibility with existing tests.
func formatAttempt(attempt int) string { return metrics.FormatAttempt(attempt) }
