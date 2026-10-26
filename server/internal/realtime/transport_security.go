package realtime

import "net"

func netSplitHostPort(addr string) (string, string, error) { return net.SplitHostPort(addr) }
func parseIPLoopback(host string) bool                     { ip := net.ParseIP(host); return ip != nil && ip.IsLoopback() }
