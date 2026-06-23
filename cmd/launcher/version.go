package main

// version is the launcher version, stamped at release time via
// -ldflags "-X main.version=...". The launcher keeps the bare vX.Y.Z tag
// namespace; see .github/workflows/release.yml (issue #25).
var version = "dev"
