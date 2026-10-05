package worker

import "net"

func isBlockedIP(ip net.IP) bool {
	return !ip.IsGlobalUnicast() || ip.IsPrivate()
}
