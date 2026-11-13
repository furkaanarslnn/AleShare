package main

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"hotspot/core/internal/pairing"
)

// buildConf renders the WireGuard configuration of this computer. Its private
// key was generated here and is written only to a file nobody but the system
// and administrators can read. There is no pre-shared key: it would have had to
// cross the network.
func buildConf(res *pairing.Result, phoneIP string) string {
	var b strings.Builder
	b.WriteString("[Interface]\n")
	fmt.Fprintf(&b, "PrivateKey = %s\n", res.Private.Base64())
	addrs := []string{res.Grant.IPv4.String() + "/32"}
	if res.Grant.IPv6.IsValid() {
		addrs = append(addrs, res.Grant.IPv6.String()+"/128")
	}
	fmt.Fprintf(&b, "Address = %s\n", strings.Join(addrs, ", "))
	var dns []string
	for _, d := range res.Grant.DNS {
		dns = append(dns, d.String())
	}
	if len(dns) > 0 {
		fmt.Fprintf(&b, "DNS = %s\n", strings.Join(dns, ", "))
	}
	if res.Grant.MTU > 0 {
		fmt.Fprintf(&b, "MTU = %d\n", res.Grant.MTU)
	}
	b.WriteString("\n[Peer]\n")
	fmt.Fprintf(&b, "PublicKey = %s\n", res.Server.Base64())
	fmt.Fprintf(&b, "Endpoint = %s\n", net.JoinHostPort(phoneIP, strconv.Itoa(res.Grant.Port)))
	b.WriteString("AllowedIPs = 0.0.0.0/0, ::/0\n")
	return b.String()
}
