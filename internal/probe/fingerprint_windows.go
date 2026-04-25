//go:build windows

package probe

import (
	"os/exec"
	"regexp"
	"strings"
)

// machineGuidRE pulls the GUID value out of `reg query` output of the
// form:
//
//	HKEY_LOCAL_MACHINE\Software\Microsoft\Cryptography
//	    MachineGuid    REG_SZ    a1b2c3d4-...
//
// Using `reg.exe` keeps the agent dependency-free (no winregistry
// imports), at the cost of one subprocess per probe startup.
var machineGuidRE = regexp.MustCompile(`(?i)MachineGuid\s+REG_SZ\s+([0-9A-Fa-f-]{8,})`)

// machineID returns the per-install Windows MachineGuid, or "" if it
// can't be read. The MachineGuid is created during OS install and is
// the canonical "this machine" identifier on Windows.
func machineID() string {
	out, err := exec.Command("reg", "query",
		`HKLM\Software\Microsoft\Cryptography`, "/v", "MachineGuid").Output()
	if err != nil {
		return ""
	}
	if m := machineGuidRE.FindStringSubmatch(string(out)); len(m) == 2 {
		return strings.TrimSpace(m[1])
	}
	return ""
}
