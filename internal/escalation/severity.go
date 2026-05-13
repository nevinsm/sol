package escalation

// SeverityToPriority maps an escalation severity ("critical"/"high"/"medium"/
// "low") to the canonical priority used by both the inbox view and
// escalation-derived mail. Lower numbers = higher priority. Unknown
// severities default to "medium" (priority 3).
//
// Scale:
//
//	critical → 1
//	high     → 2
//	medium   → 3
//	low      → 4
//	unknown  → 3 (same as medium)
func SeverityToPriority(severity string) int {
	switch severity {
	case "critical":
		return 1
	case "high":
		return 2
	case "medium":
		return 3
	case "low":
		return 4
	default:
		return 3
	}
}
