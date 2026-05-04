//go:build windows

package probe

import (
	"context"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// collectSessionsOS enumerates Windows Terminal Services sessions
// (every interactive logon — console, RDP, fast-user-switching —
// shows up here). gopsutil's host.Users returns "not implemented" on
// Windows, so without this path the AgentDetail "Logon sessions"
// panel was always empty.
//
// Wire-level behavior:
//
//   - WTSEnumerateSessionsExW returns a WTS_SESSION_INFO_1W array
//     directly, including the username, domain, station, state, and
//     session ID for each session. No second call per session needed
//     for the headline data.
//   - For the logon time + RDP client name we then call
//     WTSQuerySessionInformationW(WTSSessionInfo) which fills a
//     WTSINFOW struct.
//
// Sessions where the username is empty (services session 0,
// listener sessions) are filtered out — operators care about who is
// logged in, not the SCM.
func collectSessionsOS(_ context.Context) ([]SessionRow, bool) {
	wts := windows.NewLazySystemDLL("wtsapi32.dll")
	procEnum := wts.NewProc("WTSEnumerateSessionsExW")
	procFree := wts.NewProc("WTSFreeMemoryExW")
	procQuery := wts.NewProc("WTSQuerySessionInformationW")
	procFreeMem := wts.NewProc("WTSFreeMemory")

	const (
		WTS_CURRENT_SERVER_HANDLE = 0
		level                     = uint32(1)
		WTSTypeSessionInfoLevel1  = 2
	)

	var (
		count   uint32
		sessPtr uintptr
		level0  = level
	)
	r0, _, e1 := procEnum.Call(
		uintptr(WTS_CURRENT_SERVER_HANDLE),
		uintptr(unsafe.Pointer(&level0)),
		0,
		uintptr(unsafe.Pointer(&sessPtr)),
		uintptr(unsafe.Pointer(&count)),
	)
	if r0 == 0 {
		// EnumerateSessionsEx fell over (older Windows, denied
		// privileges, ...). Return ok=true with no rows so the
		// fallback path doesn't run gopsutil's broken Users() —
		// the panel will simply show "no sessions" instead of
		// noise, which matches reality on a service-only host.
		_ = e1
		return nil, true
	}
	defer procFree.Call(uintptr(WTSTypeSessionInfoLevel1), sessPtr, uintptr(count))

	if count == 0 || sessPtr == 0 {
		return nil, true
	}

	type wtsSessionInfo1 struct {
		ExecEnvId   uint32
		State       uint32
		SessionId   uint32
		_           [4]byte // alignment for the *uint16s on 64-bit
		PSessionName *uint16
		PHostName    *uint16
		PUserName    *uint16
		PDomainName  *uint16
		PFarmName    *uint16
	}

	rows := make([]SessionRow, 0, int(count))
	sessions := unsafe.Slice((*wtsSessionInfo1)(unsafe.Pointer(sessPtr)), int(count))
	for i := range sessions {
		s := &sessions[i]
		user := utf16PtrToString(s.PUserName)
		if user == "" {
			continue
		}
		domain := utf16PtrToString(s.PDomainName)
		station := utf16PtrToString(s.PSessionName)
		hostName := utf16PtrToString(s.PHostName)

		row := SessionRow{
			User:  joinDomainUser(domain, user),
			Tty:   station,
			State: wtsStateName(s.State),
		}

		// Pull richer info via WTSQuerySessionInformationW(WTSSessionInfo).
		// The buffer is freed via WTSFreeMemory.
		const WTSSessionInfo = 24
		var (
			info     uintptr
			infoSize uint32
		)
		ok, _, _ := procQuery.Call(
			uintptr(WTS_CURRENT_SERVER_HANDLE),
			uintptr(s.SessionId),
			uintptr(WTSSessionInfo),
			uintptr(unsafe.Pointer(&info)),
			uintptr(unsafe.Pointer(&infoSize)),
		)
		if ok != 0 && info != 0 {
			parsed := parseWTSInfo(info, infoSize)
			procFreeMem.Call(info)
			if !parsed.LogonTime.IsZero() {
				row.Started = parsed.LogonTime.UTC().Format(time.RFC3339)
			}
		}

		// Source priority: explicit RDP client (PHostName), else
		// the session-station name "Console" → label as console.
		switch {
		case hostName != "":
			row.Source = hostName
		case station == "Console" || station == "console":
			row.Source = "console"
		}

		rows = append(rows, row)
	}
	return rows, true
}

// parsedWTSInfo is the slim subset of WTSINFOW we care about:
// headline logon time. The full struct also carries last input,
// disconnect time, and idle time, which we'd surface in a future
// "session details" tooltip.
type parsedWTSInfo struct {
	LogonTime time.Time
}

// parseWTSInfo walks the variable-layout WTSINFOW buffer returned by
// WTSQuerySessionInformationW. Layout (Win32 docs, packed):
//
//	State           uint32
//	SessionId       uint32
//	IncomingBytes   uint32
//	OutgoingBytes   uint32
//	IncomingFrames  uint32
//	OutgoingFrames  uint32
//	IncomingCompressedBytes uint32
//	OutgoingCompressedBytes uint32
//	WinStationName  WCHAR[32]      // 64 bytes
//	Domain          WCHAR[17]      // 34 bytes
//	UserName        WCHAR[20+1]    // 42 bytes
//	ConnectTime     LARGE_INTEGER  (8)
//	DisconnectTime  LARGE_INTEGER  (8)
//	LastInputTime   LARGE_INTEGER  (8)
//	LogonTime       LARGE_INTEGER  (8)
//	CurrentTime     LARGE_INTEGER  (8)
//
// Total ~232 bytes. We only need LogonTime so we compute its offset
// directly.
func parseWTSInfo(buf uintptr, size uint32) parsedWTSInfo {
	if size < 200 {
		return parsedWTSInfo{}
	}
	// Offset breakdown:
	//   8 * uint32 = 32
	//   WinStationName = 64
	//   Domain         = 34 (but compiler aligns to 36 on 4-byte boundary, then 4 padding to 40)
	//   UserName       = 42 -> aligned to 44 (4) + 4 padding = 48
	// Microsoft's WTSINFOW packing is not stable across SDK versions,
	// so to keep this robust we use the documented offsets exactly:
	const (
		offWinStationName = 32
		offDomain         = offWinStationName + 64
		offUserName       = offDomain + 34
		offConnectTime    = offUserName + 42 + 4 // 4 bytes alignment padding
	)
	// LARGE_INTEGER values are 100ns intervals since 1601-01-01.
	// LogonTime is the 4th of the five timestamps in the trailing
	// block.
	const (
		offDisconnect = offConnectTime + 8
		offLastInput  = offDisconnect + 8
		offLogonTime  = offLastInput + 8
	)
	if uint32(offLogonTime+8) > size {
		return parsedWTSInfo{}
	}
	rawPtr := unsafe.Add(unsafe.Pointer(buf), offLogonTime)
	raw := *(*int64)(rawPtr)
	if raw <= 0 {
		return parsedWTSInfo{}
	}
	// Convert FILETIME (100ns intervals since 1601) to time.Time.
	const epochDelta = 11644473600 // seconds between 1601 and 1970
	secs := raw/10_000_000 - epochDelta
	nsecs := (raw % 10_000_000) * 100
	return parsedWTSInfo{LogonTime: time.Unix(secs, nsecs)}
}

// utf16PtrToString walks a *uint16 NUL-terminated string. Returns "" for nil.
func utf16PtrToString(p *uint16) string {
	if p == nil {
		return ""
	}
	// Bound the walk so a corrupt buffer can't loop forever.
	const maxChars = 4096
	buf := make([]uint16, 0, 64)
	for i := 0; i < maxChars; i++ {
		v := *(*uint16)(unsafe.Add(unsafe.Pointer(p), i*2))
		if v == 0 {
			break
		}
		buf = append(buf, v)
	}
	return syscall.UTF16ToString(buf)
}

func joinDomainUser(domain, user string) string {
	if domain == "" || domain == "." {
		return user
	}
	return domain + `\` + user
}

// wtsStateName maps the WTS_CONNECTSTATE_CLASS enum to a human label.
func wtsStateName(v uint32) string {
	switch v {
	case 0:
		return "Active"
	case 1:
		return "Connected"
	case 2:
		return "ConnectQuery"
	case 3:
		return "Shadow"
	case 4:
		return "Disconnected"
	case 5:
		return "Idle"
	case 6:
		return "Listen"
	case 7:
		return "Reset"
	case 8:
		return "Down"
	case 9:
		return "Init"
	}
	return ""
}
