package netx

import (
	"net"
	"net/http"
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

func TestGetIPAddress(t *testing.T) {
	tests := []struct {
		name     string
		headers  map[string]string
		remote   string
		expected string
	}{
		{"X-Forwarded-For simple", map[string]string{"X-Forwarded-For": "10.0.0.1"}, "192.168.1.1:1234", "10.0.0.1"},
		{"X-Forwarded-For multiple", map[string]string{"X-Forwarded-For": "10.0.0.1, 10.0.0.2"}, "192.168.1.1:1234", "10.0.0.1"},
		{"X-Real-IP", map[string]string{"X-Real-IP": "10.0.0.2"}, "192.168.1.1:1234", "10.0.0.2"},
		{"X-Client-IP", map[string]string{"X-Client-IP": "10.0.0.3"}, "192.168.1.1:1234", "10.0.0.3"},
		{"No headers fallback", map[string]string{}, "192.168.1.1:1234", "192.168.1.1"},
		{"Priority XFF > XReal", map[string]string{"X-Forwarded-For": "1.1.1.1", "X-Real-IP": "2.2.2.2"}, "192.168.1.1:1234", "1.1.1.1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &http.Request{
				Header:     make(http.Header),
				RemoteAddr: tt.remote,
			}
			for k, v := range tt.headers {
				r.Header.Set(k, v)
			}
			if got := GetIPAddress(r); got != tt.expected {
				t.Errorf("GetIPAddress() = %v, want %v", got, tt.expected)
			}
		})
	}
}
