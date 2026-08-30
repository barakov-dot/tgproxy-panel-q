package models

// Setting mirrors the `settings` table in plan.md §4 — a flat key/value
// store, currently used only for the auto_issue toggle.
type Setting struct {
	Key   string
	Value string
}

// SettingAutoIssue is the settings.key used for the auto-issue toggle
// (plan.md §5/§9): "true" issues proxies automatically on request, "false"
// requires admin approval.
const SettingAutoIssue = "auto_issue"
