// Package version holds the application version that the API, the web UI and
// the startup log report.
package version

// Version is the application version, following semantic versioning.
//
// Bump it with every change: a new feature or other user-visible addition gets
// a minor bump, a bug fix a patch bump, and a breaking change a major bump.
// `internal/server` reports it from GET /api/info and `main` logs it at
// startup, so there is exactly one place to edit.
const Version = "1.12.0"
