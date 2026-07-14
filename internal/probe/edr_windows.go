//go:build windows

package probe

import (
	"context"
	"sort"
	"strings"

	"github.com/shirou/gopsutil/v4/process"
	"golang.org/x/sys/windows/svc/mgr"
)

// edrSignature matches a running process or service name fragment to a product label.
type edrSignature struct {
	needle  string
	product string
}

var edrProcessNeedles = []edrSignature{
	{needle: "csfalconservice", product: "CrowdStrike Falcon"},
	{needle: "csfalconcontainer", product: "CrowdStrike Falcon"},
	{needle: "msmpeng", product: "Microsoft Defender"},
	{needle: "sense", product: "Microsoft Defender for Endpoint"},
	{needle: "sentinelagent", product: "SentinelOne"},
	{needle: "sentinelstaticengine", product: "SentinelOne"},
	{needle: "cb", product: "Carbon Black"},
	{needle: "carbonblack", product: "Carbon Black"},
	{needle: "cylancesvc", product: "Cylance"},
	{needle: "cylance", product: "Cylance"},
	{needle: "tanium", product: "Tanium"},
	{needle: "elastic-endpoint", product: "Elastic Endpoint"},
	{needle: "elasticagent", product: "Elastic Agent"},
	{needle: "sophos", product: "Sophos"},
	{needle: "symantec", product: "Symantec"},
	{needle: "sep", product: "Symantec Endpoint Protection"},
	{needle: "mcafee", product: "McAfee"},
	{needle: "trellix", product: "Trellix"},
	{needle: "fireeye", product: "FireEye"},
	{needle: "xagt", product: "FireEye HX"},
}

var edrServiceNeedles = []string{
	"csagent", "csfalconservice", "windefend", "sense", "sentinelagent",
	"carbonblack", "cylancesvc", "tanium", "elastic agent", "elastic-endpoint",
	"sysmon", "sysmon64",
}

// CollectEDR detects common EDR products and Sysmon on Windows.
func CollectEDR(ctx context.Context) (products []string, sysmon *bool) {
	found := map[string]struct{}{}
	procs, err := process.ProcessesWithContext(ctx)
	if err == nil {
		for _, p := range procs {
			name, err := p.NameWithContext(ctx)
			if err != nil {
				continue
			}
			low := strings.ToLower(name)
			for _, sig := range edrProcessNeedles {
				if strings.Contains(low, sig.needle) {
					found[sig.product] = struct{}{}
				}
			}
			if strings.Contains(low, "sysmon") {
				v := true
				sysmon = &v
			}
		}
	}
	for _, svc := range edrServiceNeedles {
		select {
		case <-ctx.Done():
			break
		default:
		}
		if serviceExists(ctx, svc) {
			if strings.Contains(svc, "sysmon") {
				v := true
				sysmon = &v
				continue
			}
			label := strings.Title(strings.ReplaceAll(svc, "-", " "))
			found[label] = struct{}{}
		}
	}
	for p := range found {
		products = append(products, p)
	}
	sort.Strings(products)
	return products, sysmon
}

func serviceExists(ctx context.Context, name string) bool {
	select {
	case <-ctx.Done():
		return false
	default:
	}
	m, err := mgr.Connect()
	if err != nil {
		return false
	}
	defer m.Disconnect()
	svc, err := m.OpenService(name)
	if err != nil {
		return false
	}
	defer svc.Close()
	return true
}
