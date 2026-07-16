package netx

import (
	"math/rand"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"ojv/cog/log"
)

// GetIntranetIP returns the first non-loopback IPv4 address of the machine.
func GetIntranetIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		log.Errorf("get intranet ip failed: %v", err)
		return ""
	}
	var ips []string
	for _, address := range addrs {
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				ips = append(ips, ipnet.IP.String())
			}
		}
	}
	if len(ips) > 0 {
		return ips[0]
	}
	return ""
}

// === URL utilities ===

// IsLink checks whether a string is an http/https URL.
func IsLink(link string) bool {
	_, err := url.Parse(link)
	return err == nil && (strings.HasPrefix(link, "http://") || strings.HasPrefix(link, "https://"))
}

// BuildQueryString builds a URL-encoded query string from a map.
func BuildQueryString(params map[string]string) string {
	if len(params) == 0 {
		return ""
	}
	var builder strings.Builder
	first := true
	for k, v := range params {
		if first {
			first = false
		} else {
			builder.WriteByte('&')
		}
		builder.WriteString(url.QueryEscape(k))
		builder.WriteByte('=')
		builder.WriteString(url.QueryEscape(v))
	}
	return builder.String()
}

// === Cookie utilities ===

// CreateCookie creates an http.Cookie with the given attributes.
func CreateCookie(name, value, domain, path string, maxAge int, secure, httpOnly bool) *http.Cookie {
	return &http.Cookie{
		Name:     name,
		Value:    value,
		Domain:   domain,
		Path:     path,
		MaxAge:   maxAge,
		Secure:   secure,
		HttpOnly: httpOnly,
	}
}

// ParseCookies parses a Cookie header string into []*http.Cookie.
func ParseCookies(cookieStr string) []*http.Cookie {
	header := http.Header{}
	header.Add("Cookie", cookieStr)
	request := http.Request{Header: header}
	return request.Cookies()
}

// GetCookieByName finds a cookie by name in a list.
func GetCookieByName(cookies []*http.Cookie, name string) *http.Cookie {
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}

// CreateCookieJar creates a new in-memory CookieJar.
func CreateCookieJar() (http.CookieJar, error) {
	return cookiejar.New(nil)
}

// === User-Agent utilities ===

var defaultUserAgents = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/14.1.1 Safari/605.1.15",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:89.0) Gecko/20100101 Firefox/89.0",
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.114 Safari/537.36",
}

// GetRandomUserAgent returns a random User-Agent string.
func GetRandomUserAgent() string {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	return defaultUserAgents[r.Intn(len(defaultUserAgents))]
}

// === Request utilities ===

// GetIPAddress extracts the client's real IP address from an HTTP request,
// checking X-Forwarded-For, X-Real-IP, and X-Client-IP headers in order
// before falling back to RemoteAddr.
func GetIPAddress(r *http.Request) string {
	for _, header := range []string{"X-Forwarded-For", "X-Real-IP", "X-Client-IP"} {
		if val := r.Header.Get(header); val != "" {
			// X-Forwarded-For can contain multiple IPs; the first is the client
			parts := strings.SplitN(val, ",", 2)
			if ip := strings.TrimSpace(parts[0]); ip != "" {
				return ip
			}
		}
	}
	if ip, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return ip
	}
	return r.RemoteAddr
}
