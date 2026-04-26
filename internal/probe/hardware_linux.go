//go:build linux

// Linux hardware collector.
//
// Sources, in order of preference:
//
//   1. /sys/class/dmi/id/* — DMI/SMBIOS identity. World-readable on
//      modern kernels for non-sensitive fields (sys_vendor,
//      product_name, bios_*); the sensitive *_serial fields are
//      0400 root-only. Probe runs as root, so we can read them, but
//      we still tolerate a permission denial silently.
//
//   2. dmidecode -t 17  — memory DIMM details. Required because
//      /sys does not expose per-module manufacturer/part-number on
//      most boards. Runs only if the binary is on PATH; absent on
//      stripped-down container images.
//
//   3. /sys/block/* + /sys/block/*/device/* — disks. Self-contained,
//      no shell-out.
//
//   4. /sys/class/net/*/device/* — NIC vendor/product/driver, plus
//      /sys/class/net/*/speed for live link speed. ethtool is
//      avoided to keep the probe dependency-free.
//
//   5. lspci -mm -nn — GPU enumeration. Skipped if absent.
//
// Every source is best-effort; failures land in CollectionWarnings.

package probe

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func collectHardware(ctx context.Context) *Hardware {
	hw := &Hardware{}
	hw.System = readDMISystem(hw)
	hw.CPU = readLinuxCPUID(hw)
	hw.MemoryModules = readDMIMemoryModules(ctx, hw)
	hw.Storage = readLinuxStorage(hw)
	hw.NetworkAdapters = readLinuxNetworkAdapters(hw)
	hw.GPUs = readLinuxGPUs(ctx, hw)
	return hw
}

// readDMIFile returns the trimmed contents of /sys/class/dmi/id/<name>
// or "" if the file is missing or unreadable. Many of the *_serial
// fields are root-only; we silently ignore EACCES so a non-root probe
// degrades to "ProductName known, serial unknown" instead of crashing.
func readDMIFile(name string) string {
	b, err := os.ReadFile("/sys/class/dmi/id/" + name)
	if err != nil {
		return ""
	}
	s := strings.TrimSpace(string(b))
	// DMI vendors love to ship placeholder strings ("To Be Filled By
	// O.E.M.", "System Product Name", "Default string"). They're
	// noise — drop them so the UI's "—" fallback kicks in.
	if isDMIPlaceholder(s) {
		return ""
	}
	return s
}

func isDMIPlaceholder(s string) bool {
	low := strings.ToLower(s)
	switch low {
	case "", "to be filled by o.e.m.", "to be filled by oem",
		"system product name", "system manufacturer", "system version",
		"default string", "not specified", "not applicable",
		"o.e.m.", "oem", "0", "none":
		return true
	}
	return false
}

func readDMISystem(_ *Hardware) *HardwareSystem {
	sys := &HardwareSystem{
		Manufacturer:      readDMIFile("sys_vendor"),
		ProductName:       readDMIFile("product_name"),
		SerialNumber:      readDMIFile("product_serial"),
		ChassisType:       readDMIChassisType(),
		ChassisAssetTag:   readDMIFile("chassis_asset_tag"),
		BIOSVendor:        readDMIFile("bios_vendor"),
		BIOSVersion:       readDMIFile("bios_version"),
		BIOSDate:          readDMIFile("bios_date"),
		BoardManufacturer: readDMIFile("board_vendor"),
		BoardProduct:      readDMIFile("board_name"),
		BoardSerial:       readDMIFile("board_serial"),
	}
	// If literally nothing came back, drop the whole struct — happens
	// in containers where /sys/class/dmi doesn't exist at all.
	if *sys == (HardwareSystem{}) {
		return nil
	}
	return sys
}

// readDMIChassisType maps the SMBIOS chassis-type integer (0..N) to a
// readable label. The list comes from SMBIOS spec table 7.4.
func readDMIChassisType() string {
	raw := readDMIFile("chassis_type")
	if raw == "" {
		return ""
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return raw
	}
	switch n {
	case 1:
		return "Other"
	case 3:
		return "Desktop"
	case 4:
		return "Low-Profile Desktop"
	case 5:
		return "Pizza Box"
	case 6:
		return "Mini Tower"
	case 7:
		return "Tower"
	case 8, 9:
		return "Portable"
	case 10:
		return "Notebook"
	case 11:
		return "Hand Held"
	case 12:
		return "Docking Station"
	case 13:
		return "All in One"
	case 14:
		return "Sub Notebook"
	case 17:
		return "Main Server Chassis"
	case 18:
		return "Expansion Chassis"
	case 23:
		return "Rack Mount Chassis"
	case 28:
		return "Blade"
	case 29:
		return "Blade Enclosure"
	case 30:
		return "Tablet"
	case 31:
		return "Convertible"
	case 32:
		return "Detachable"
	case 35:
		return "Mini PC"
	case 36:
		return "Stick PC"
	default:
		return fmt.Sprintf("Type %d", n)
	}
}

// readLinuxCPUID populates the static CPU identity. We re-parse
// /proc/cpuinfo directly rather than reuse the gopsutil cpu.Info
// struct because cpu.Info conflates physical/logical counts and we
// want the unambiguous values from /proc.
func readLinuxCPUID(hw *Hardware) *HardwareCPU {
	f, err := os.Open("/proc/cpuinfo")
	if err != nil {
		hw.CollectionWarnings = append(hw.CollectionWarnings,
			"hardware/cpu: /proc/cpuinfo: "+err.Error())
		return nil
	}
	defer f.Close()

	out := &HardwareCPU{}
	physIDs := map[string]struct{}{}
	logical := 0
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		line := scan.Text()
		k, v, ok := splitKVColon(line)
		if !ok {
			continue
		}
		switch k {
		case "model name":
			if out.Model == "" {
				out.Model = v
			}
		case "cpu MHz":
			if n, err := strconv.ParseFloat(v, 64); err == nil && out.MHzNominal == 0 {
				out.MHzNominal = n
			}
		case "processor":
			logical++
		case "physical id":
			physIDs[v] = struct{}{}
		}
	}
	out.Threads = logical
	out.Cores = len(physIDs) // socket count; fall through below
	// /proc/cpuinfo's "cpu cores" tells us cores-per-socket. Multiply
	// by socket count to get total physical cores. Single-socket
	// fallback when "physical id" isn't present (some VMs).
	out.Cores = readCoresPerSocket() * max(out.Cores, 1)
	if out.Threads == 0 && out.Cores > 0 {
		out.Threads = out.Cores
	}
	return out
}

func readCoresPerSocket() int {
	f, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return 0
	}
	defer f.Close()
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		k, v, ok := splitKVColon(scan.Text())
		if !ok || k != "cpu cores" {
			continue
		}
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return 0
}

func splitKVColon(line string) (string, string, bool) {
	i := strings.IndexByte(line, ':')
	if i < 0 {
		return "", "", false
	}
	return strings.TrimSpace(line[:i]), strings.TrimSpace(line[i+1:]), true
}

// readDMIMemoryModules shells out to dmidecode. dmidecode requires
// CAP_SYS_RAWIO (or root); we expect root because the probe is
// installed as a system service, but if the binary is missing we
// just report a warning and skip — RAM details aren't critical.
func readDMIMemoryModules(ctx context.Context, hw *Hardware) []HardwareMemModule {
	bin, err := exec.LookPath("dmidecode")
	if err != nil {
		hw.CollectionWarnings = append(hw.CollectionWarnings,
			"hardware/memory: dmidecode not on PATH; DIMM details unavailable")
		return nil
	}
	out, err := exec.CommandContext(ctx, bin, "-t", "17").Output()
	if err != nil {
		hw.CollectionWarnings = append(hw.CollectionWarnings,
			"hardware/memory: dmidecode -t 17 failed: "+err.Error())
		return nil
	}
	return parseDMIMemoryOutput(string(out))
}

// parseDMIMemoryOutput splits dmidecode's "Memory Device" blocks. Each
// block starts at a "Handle 0x..., DMI type 17," header. Empty slots
// have Size=No Module Installed and we drop those — the UI cares only
// about populated DIMMs.
func parseDMIMemoryOutput(out string) []HardwareMemModule {
	var modules []HardwareMemModule
	blocks := strings.Split(out, "\n\n")
	for _, b := range blocks {
		if !strings.Contains(b, "Memory Device") {
			continue
		}
		mod := HardwareMemModule{}
		populated := false
		scan := bufio.NewScanner(strings.NewReader(b))
		for scan.Scan() {
			line := strings.TrimSpace(scan.Text())
			k, v, ok := splitKVColon(line)
			if !ok {
				continue
			}
			switch k {
			case "Size":
				// "8192 MB", "32 GB", or "No Module Installed".
				if size, ok := parseDMISize(v); ok {
					mod.SizeBytes = size
					populated = true
				}
			case "Form Factor":
				mod.FormFactor = v
			case "Type":
				if v != "Unknown" {
					mod.Type = v
				}
			case "Speed":
				// "3200 MT/s" or "Unknown".
				if mhz, ok := parseDMIMhz(v); ok {
					mod.SpeedMHz = mhz
				}
			case "Manufacturer":
				if v != "Unknown" && v != "Not Specified" {
					mod.Manufacturer = v
				}
			case "Serial Number":
				if v != "Unknown" && v != "Not Specified" {
					mod.SerialNumber = v
				}
			case "Part Number":
				if v != "Unknown" && v != "Not Specified" {
					mod.PartNumber = strings.TrimSpace(v)
				}
			case "Locator", "Bank Locator":
				// Prefer the chip locator ("DIMM_A1"), fall back to
				// the bank ("BANK 0") if that's all we have.
				if mod.Slot == "" {
					mod.Slot = v
				} else if k == "Locator" {
					mod.Slot = v
				}
			}
		}
		if populated {
			modules = append(modules, mod)
		}
	}
	return modules
}

func parseDMISize(s string) (uint64, bool) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(strings.ToLower(s), "no module") || s == "0" {
		return 0, false
	}
	parts := strings.Fields(s)
	if len(parts) < 2 {
		return 0, false
	}
	n, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		return 0, false
	}
	switch strings.ToUpper(parts[1]) {
	case "KB", "K":
		return n * 1024, true
	case "MB", "M":
		return n * 1024 * 1024, true
	case "GB", "G":
		return n * 1024 * 1024 * 1024, true
	case "TB", "T":
		return n * 1024 * 1024 * 1024 * 1024, true
	}
	return 0, false
}

func parseDMIMhz(s string) (int, bool) {
	parts := strings.Fields(s)
	if len(parts) == 0 {
		return 0, false
	}
	n, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, false
	}
	return n, true
}

// readLinuxStorage walks /sys/block/* and pulls model/serial/size.
// We deliberately skip loop, ram, sr (cdrom), and md (md-raid)
// devices — they're not "the disks in this machine" in the operator
// sense.
func readLinuxStorage(hw *Hardware) []HardwareDisk {
	entries, err := os.ReadDir("/sys/block")
	if err != nil {
		hw.CollectionWarnings = append(hw.CollectionWarnings,
			"hardware/storage: /sys/block: "+err.Error())
		return nil
	}
	var disks []HardwareDisk
	for _, e := range entries {
		name := e.Name()
		if isVirtualBlockDevice(name) {
			continue
		}
		base := "/sys/block/" + name
		dev := "/dev/" + name
		size := readSysSize(base + "/size") // sectors of 512B
		model := readSysFile(base + "/device/model")
		serial := readSysFile(base + "/device/serial")
		vendor := readSysFile(base + "/device/vendor")
		rot := readSysRotational(base + "/queue/rotational")
		bus := detectLinuxBus(name)
		disks = append(disks, HardwareDisk{
			Device:     dev,
			Model:      model,
			Serial:     serial,
			Vendor:     vendor,
			BusType:    bus,
			SizeBytes:  size,
			Rotational: rot,
		})
	}
	return disks
}

func isVirtualBlockDevice(name string) bool {
	switch {
	case strings.HasPrefix(name, "loop"),
		strings.HasPrefix(name, "ram"),
		strings.HasPrefix(name, "sr"),
		strings.HasPrefix(name, "md"),
		strings.HasPrefix(name, "dm-"),
		strings.HasPrefix(name, "zram"),
		strings.HasPrefix(name, "fd"):
		return true
	}
	return false
}

func detectLinuxBus(name string) string {
	switch {
	case strings.HasPrefix(name, "nvme"):
		return "nvme"
	case strings.HasPrefix(name, "sd"):
		// Could be SATA, SAS, USB, or iSCSI. Read the device's
		// transport file if available — the kernel populates it for
		// SCSI-class devices.
		if t := readSysFile("/sys/block/" + name + "/device/transport"); t != "" {
			return strings.ToLower(t)
		}
		return "scsi"
	case strings.HasPrefix(name, "vd"):
		return "virtio"
	case strings.HasPrefix(name, "xvd"):
		return "xen"
	case strings.HasPrefix(name, "mmc"):
		return "mmc"
	}
	return ""
}

func readSysFile(p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func readSysSize(p string) uint64 {
	s := readSysFile(p)
	if s == "" {
		return 0
	}
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0
	}
	return n * 512
}

func readSysRotational(p string) *bool {
	s := readSysFile(p)
	if s == "" {
		return nil
	}
	v := s == "1"
	return &v
}

// readLinuxNetworkAdapters walks /sys/class/net/*. It includes both
// physical and virtual NICs (lo, docker0, virbr0, ...) — the UI is
// expected to filter; the inventory should be honest.
func readLinuxNetworkAdapters(hw *Hardware) []HardwareNICInfo {
	entries, err := os.ReadDir("/sys/class/net")
	if err != nil {
		hw.CollectionWarnings = append(hw.CollectionWarnings,
			"hardware/network: /sys/class/net: "+err.Error())
		return nil
	}
	var nics []HardwareNICInfo
	for _, e := range entries {
		name := e.Name()
		base := "/sys/class/net/" + name
		mac := readSysFile(base + "/address")
		// Skip pure software constructs that produce no useful info.
		if name == "lo" {
			continue
		}
		nic := HardwareNICInfo{
			Name: name,
			MAC:  mac,
		}
		if speed := readSysFile(base + "/speed"); speed != "" {
			if n, err := strconv.Atoi(speed); err == nil && n > 0 {
				nic.SpeedMbps = n
			}
		}
		// The driver and PCI vendor/device IDs live behind device/.
		// device/ is a symlink to a /sys/devices/... node for real
		// hardware; for veth/bridge/etc. it doesn't exist.
		dev := base + "/device"
		if real, err := filepath.EvalSymlinks(dev); err == nil {
			if drv, err := filepath.EvalSymlinks(dev + "/driver"); err == nil {
				nic.Driver = filepath.Base(drv)
			}
			nic.BusInfo = pciBusInfoFromPath(real)
			nic.Vendor, nic.Product = lookupPCIVendorProduct(real)
		}
		nics = append(nics, nic)
	}
	return nics
}

// pciBusInfoFromPath extracts a B:D.F string from a /sys path. The
// last segment of the canonical path for a PCI device looks like
// 0000:01:00.0; for non-PCI buses (USB NICs) we just return "".
func pciBusInfoFromPath(p string) string {
	base := filepath.Base(p)
	if len(base) >= 12 && strings.Count(base, ":") == 2 && strings.Contains(base, ".") {
		return "pci@" + base
	}
	return ""
}

// lookupPCIVendorProduct reads the four-character PCI vendor and
// device IDs from sysfs and resolves them via /usr/share/hwdata or
// /usr/share/misc/pci.ids if present. Resolution failures fall back
// to the raw IDs ("0x10de:0x1c82") so the UI shows *something*.
func lookupPCIVendorProduct(devPath string) (string, string) {
	vendor := strings.TrimPrefix(readSysFile(devPath+"/vendor"), "0x")
	product := strings.TrimPrefix(readSysFile(devPath+"/device"), "0x")
	if vendor == "" {
		return "", ""
	}
	v, p := lookupPCIIDs(vendor, product)
	if v == "" {
		v = "0x" + vendor
	}
	if p == "" && product != "" {
		p = "0x" + product
	}
	return v, p
}

// pci.ids parser is intentionally minimal: we cache the file once per
// process, look up the two IDs, and fall back to the raw IDs if the
// file isn't there. Keeping it inline here avoids pulling in another
// dependency.
var pciIDFiles = []string{
	"/usr/share/hwdata/pci.ids",
	"/usr/share/misc/pci.ids",
	"/usr/share/pci.ids",
}

var pciIDOnce struct {
	loaded bool
	table  map[string]map[string]string // vendorID -> deviceID -> name; vendorID -> "" -> vendor name
	vname  map[string]string
}

func loadPCIIDs() {
	if pciIDOnce.loaded {
		return
	}
	pciIDOnce.loaded = true
	pciIDOnce.table = map[string]map[string]string{}
	pciIDOnce.vname = map[string]string{}
	for _, p := range pciIDFiles {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		var curVendor string
		scan := bufio.NewScanner(f)
		for scan.Scan() {
			ln := scan.Text()
			if ln == "" || strings.HasPrefix(ln, "#") {
				continue
			}
			if strings.HasPrefix(ln, "\t\t") {
				// subsystem record — ignore
				continue
			}
			if strings.HasPrefix(ln, "\t") {
				rest := strings.TrimLeft(ln, "\t")
				if len(rest) < 6 {
					continue
				}
				dev := rest[:4]
				name := strings.TrimSpace(rest[4:])
				if curVendor != "" {
					pciIDOnce.table[curVendor][dev] = name
				}
				continue
			}
			// vendor line: "10de  NVIDIA Corporation"
			if len(ln) < 6 {
				continue
			}
			curVendor = ln[:4]
			pciIDOnce.vname[curVendor] = strings.TrimSpace(ln[4:])
			pciIDOnce.table[curVendor] = map[string]string{}
		}
		f.Close()
		break
	}
}

func lookupPCIIDs(vendor, product string) (string, string) {
	loadPCIIDs()
	v := pciIDOnce.vname[vendor]
	p := ""
	if devs := pciIDOnce.table[vendor]; devs != nil {
		p = devs[product]
	}
	return v, p
}

// readLinuxGPUs runs `lspci -mm -nn` if it exists and grabs lines
// classified as VGA / 3D / Display controllers. Without lspci we just
// return nil — the absence of "GPUs: 0" vs "GPUs: <list>" is itself
// informative for an operator looking at a server.
func readLinuxGPUs(ctx context.Context, hw *Hardware) []HardwareGPU {
	bin, err := exec.LookPath("lspci")
	if err != nil {
		hw.CollectionWarnings = append(hw.CollectionWarnings,
			"hardware/gpu: lspci not on PATH; GPU details unavailable")
		return nil
	}
	out, err := exec.CommandContext(ctx, bin, "-mm", "-nn", "-D").Output()
	if err != nil {
		hw.CollectionWarnings = append(hw.CollectionWarnings,
			"hardware/gpu: lspci failed: "+err.Error())
		return nil
	}
	return parseLspciGPUs(string(out))
}

// parseLspciGPUs picks lines like:
//   0000:01:00.0 "VGA compatible controller [0300]" "NVIDIA Corp. [10de]" "TU106 [GeForce RTX 2060] [1f08]" -r0a "Gigabyte" "..."
// The fields are quoted and separated by spaces; split on `" "` after
// stripping the leading bus token.
func parseLspciGPUs(out string) []HardwareGPU {
	var gpus []HardwareGPU
	scan := bufio.NewScanner(strings.NewReader(out))
	for scan.Scan() {
		line := scan.Text()
		if line == "" {
			continue
		}
		// crude: only lines whose class is in the display range.
		if !(strings.Contains(line, "[0300]") ||
			strings.Contains(line, "[0301]") ||
			strings.Contains(line, "[0302]") ||
			strings.Contains(line, "[0380]")) {
			continue
		}
		bus := ""
		if i := strings.IndexByte(line, ' '); i > 0 {
			bus = line[:i]
		}
		fields := splitLspciFields(line)
		gpu := HardwareGPU{BusInfo: "pci@" + bus}
		if len(fields) >= 3 {
			gpu.Vendor = stripIDSuffix(fields[1])
			gpu.Product = stripIDSuffix(fields[2])
		}
		gpus = append(gpus, gpu)
	}
	return gpus
}

// splitLspciFields splits a line on `"`-quoted segments. lspci -mm
// guarantees this format.
func splitLspciFields(line string) []string {
	var out []string
	var cur strings.Builder
	inQ := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		if c == '"' {
			if inQ {
				out = append(out, cur.String())
				cur.Reset()
			}
			inQ = !inQ
			continue
		}
		if inQ {
			cur.WriteByte(c)
		}
	}
	return out
}

// stripIDSuffix removes a trailing "[1f08]" PCI ID hint from a vendor
// or product name.
func stripIDSuffix(s string) string {
	if i := strings.LastIndex(s, " ["); i > 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}
