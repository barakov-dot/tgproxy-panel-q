package models

// Setting mirrors the settings key/value table.
type Setting struct {
	Key   string
	Value string
}

// SettingAutoIssue is the settings key for the auto-issue toggle.
const SettingAutoIssue = "auto_issue"

// AutoIssueEnabled reports whether value enables automatic proxy issuance.
func AutoIssueEnabled(value string) bool {
	return value == "true"
}
