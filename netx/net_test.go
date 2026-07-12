package netx

import (
	"net"
	"testing"
)

func TestGetIntranetIP(t *testing.T) {
	ipStr := GetIntranetIP()

	// It's possible to get empty string if no network interfaces are available
	if ipStr == "" {
		t.Log("No intranet IP found (this might be expected in some environments)")
		return
	}

	ip := net.ParseIP(ipStr)
	if ip == nil {
		t.Errorf("GetIntranetIP returned invalid IP: %s", ipStr)
	}

	if ip.IsLoopback() {
		t.Errorf("GetIntranetIP returned loopback address: %s", ipStr)
	}

	if ip.To4() == nil {
		t.Errorf("GetIntranetIP returned non-IPv4 address: %s", ipStr)
	}

	t.Logf("Detected Intranet IP: %s", ipStr)
}
