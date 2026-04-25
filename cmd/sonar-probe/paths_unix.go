//go:build !windows

package main

// On Linux/macOS the install one-liner drops config under /etc/sonar-probe
// and the systemd unit reads it from there.
var defaultConfigPath = "/etc/sonar-probe/agent.json"
