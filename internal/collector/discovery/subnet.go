package discovery

import (
	"fmt"
	"net"
)

// IPv4Hosts enumerates assignable addresses inside an IPv4 CIDR (excluding the
// network and broadcast addresses). Enumeration stops after maxHosts.
func IPv4Hosts(cidr string, maxHosts int) ([]string, error) {
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, err
	}
	network := ipnet.IP.Mask(ipnet.Mask).To4()
	broadcast := lastIPv4(ipnet).To4()
	if network == nil || broadcast == nil {
		return nil, fmt.Errorf("discovery: only IPv4 CIDRs supported for now")
	}
	start := ipToUint32(network) + 1
	end := ipToUint32(broadcast) - 1
	if end < start {
		return nil, fmt.Errorf("discovery: CIDR too small")
	}
	var out []string
	for n := start; n <= end && len(out) < maxHosts; n++ {
		out = append(out, uint32ToIP(n).String())
	}
	return out, nil
}

func ipToUint32(ip net.IP) uint32 {
	ip = ip.To4()
	if ip == nil {
		return 0
	}
	return uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3])
}

func uint32ToIP(n uint32) net.IP {
	return net.IPv4(byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
}

func lastIPv4(n *net.IPNet) net.IP {
	ip := n.IP.To4()
	if ip == nil {
		return nil
	}
	mask := net.IP(n.Mask).To4()
	out := make(net.IP, 4)
	for i := range out {
		out[i] = ip[i] | ^mask[i]
	}
	return out
}
