//go:build windows

package probe

import (
	"strings"

	"golang.org/x/sys/windows/registry"
)

// classifyNICOS reads the registry-backed network class root to map
// adapter names to their PnpInstanceID and walks the matching subkey
// to find the Characteristics / *PhysicalMediaType the NDIS driver
// registered. The "happy path" for the dashboards is wired vs.
// wireless, so we focus on those two values; everything else falls
// through to classifyNICName.
//
// Registry sources consulted:
//
//	HKLM\SYSTEM\CurrentControlSet\Control\Class\
//	  {4d36e972-e325-11ce-bfc1-08002be10318}\<index>
//	  -> values: NetCfgInstanceId, *PhysicalMediaType, *MediaType,
//	             AdapterType
//
// *PhysicalMediaType uses NDIS_PHYSICAL_MEDIUM enum (0=Unspecified,
// 1=WirelessLan, 8=Native802_11, 9=Bluetooth, 11=Ethernet, etc.).
// Older drivers populate *MediaType (NDIS_MEDIUM): 0=802.3, 1=802.4,
// 9=NdisMediumNative802_11.
//
// The interface "name" we receive from gopsutil is the friendly name
// shown in Network Connections (e.g. "Ethernet 2" or "Wi-Fi"). To map
// it to a class subkey we walk the network adapters list and match on
// the NetConnectionID stored alongside each NetCfgInstanceId.
func classifyNICOS(name string) string {
	if name == "" {
		return ""
	}

	const classRoot = `SYSTEM\CurrentControlSet\Control\Class\{4d36e972-e325-11ce-bfc1-08002be10318}`
	root, err := registry.OpenKey(registry.LOCAL_MACHINE, classRoot, registry.READ)
	if err != nil {
		return ""
	}
	defer root.Close()

	subkeys, err := root.ReadSubKeyNames(-1)
	if err != nil {
		return ""
	}

	target := strings.ToLower(name)
	for _, sk := range subkeys {
		k, err := registry.OpenKey(root, sk, registry.READ)
		if err != nil {
			continue
		}
		// NetCfgInstanceId is the {GUID} we want to compare by name —
		// but the friendly name lives under
		// HKLM\SYSTEM\CurrentControlSet\Control\Network\<class>\<guid>\Connection.
		// Cheaper path: most drivers also stash "DriverDesc" and
		// "AdapterType" / "*PhysicalMediaType" / "*MediaType" right
		// here, plus a "FriendlyName" or "DriverDesc" we can compare
		// case-insensitively.
		var friendly string
		if v, _, err := k.GetStringValue("FriendlyName"); err == nil {
			friendly = strings.ToLower(v)
		} else if v, _, err := k.GetStringValue("DriverDesc"); err == nil {
			friendly = strings.ToLower(v)
		}

		// We also try NetCfgInstanceId → Connection\Name.
		guid, _, _ := k.GetStringValue("NetCfgInstanceId")

		if friendly == "" && guid == "" {
			k.Close()
			continue
		}

		match := friendly == target
		if !match && guid != "" {
			connPath := `SYSTEM\CurrentControlSet\Control\Network\{4D36E972-E325-11CE-BFC1-08002BE10318}\` +
				guid + `\Connection`
			if ck, err := registry.OpenKey(registry.LOCAL_MACHINE, connPath, registry.READ); err == nil {
				if v, _, err := ck.GetStringValue("Name"); err == nil {
					if strings.EqualFold(v, name) {
						match = true
					}
				}
				ck.Close()
			}
		}

		if !match {
			// Cheap heuristic fallback — the friendly name is often a
			// prefix of the gopsutil name (e.g. "Intel(R) Wi-Fi 6 AX201
			// 160MHz" vs. just "Wi-Fi"). Skip — we'll rely on the name
			// heuristic if no entry matched.
			k.Close()
			continue
		}

		// Read NDIS physical-medium first; fall back to *MediaType.
		if v, _, err := k.GetIntegerValue("*PhysicalMediaType"); err == nil {
			k.Close()
			return ndisPhysicalMediumKind(uint32(v))
		}
		if v, _, err := k.GetIntegerValue("*MediaType"); err == nil {
			k.Close()
			return ndisMediumKind(uint32(v))
		}
		k.Close()
		return ""
	}
	return ""
}

// ndisPhysicalMediumKind translates NDIS_PHYSICAL_MEDIUM enum values
// to our four-bucket Kind. See:
//
//	https://learn.microsoft.com/windows-hardware/drivers/network/
//	  ndis-physical-medium
func ndisPhysicalMediumKind(v uint32) string {
	switch v {
	case 1, // WirelessLan
		8: // Native802_11
		return "wireless"
	case 11, // Ethernet (802.3)
		2, // CableModem
		3, // PhoneLine
		4, // PowerLine
		7: // BluetoothPan unused; treat unknowns as wired
		return "wired"
	case 9, // Bluetooth
		10: // Wman
		return "virtual"
	}
	return ""
}

// ndisMediumKind translates legacy NDIS_MEDIUM values.
func ndisMediumKind(v uint32) string {
	switch v {
	case 0, // 802_3
		1, // 802_5 (Token Ring)
		2: // FDDI
		return "wired"
	case 9: // Native802_11
		return "wireless"
	}
	return ""
}
