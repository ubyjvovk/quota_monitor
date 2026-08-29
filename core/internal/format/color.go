package format

// Colorize wraps text in the ANSI colour for a usage severity. Normal and
// unknown severities remain uncoloured.
func Colorize(severity, text string) string {
	switch severity {
	case "warning":
		return "\x1b[33m" + text + "\x1b[0m"
	case "critical":
		return "\x1b[31m" + text + "\x1b[0m"
	default:
		return text
	}
}
