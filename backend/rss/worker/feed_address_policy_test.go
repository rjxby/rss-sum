package worker

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"testing"

	"golang.org/x/net/dns/dnsmessage"
)

func TestFeedDestinationIPValidation(t *testing.T) {
	cases := []struct {
		address string
		blocked bool
	}{
		{"0.0.0.0", true},
		{"10.0.0.1", true},
		{"127.0.0.1", true},
		{"169.254.1.1", true},
		{"172.16.0.1", true},
		{"192.168.1.1", true},
		{"224.0.0.1", true},
		{"255.255.255.255", true},
		{"::", true},
		{"::1", true},
		{"fc00::1", true},
		{"fd00::1", true},
		{"fe80::1", true},
		{"ff02::1", true},
		{"::ffff:10.0.0.1", true},
		{"::ffff:127.0.0.1", true},
		{"8.8.8.8", false},
		{"93.184.216.34", false},
		{"100.64.0.1", false},
		{"192.0.0.9", false},
		{"198.18.0.1", false},
		{"203.0.113.1", false},
		{"::ffff:8.8.8.8", false},
		{"64:ff9b::a00:1", false},
		{"64:ff9b:1::808:808", false},
		{"2001:db8::1", false},
		{"3fff::1", false},
		{"2001:4860:4860::8888", false},
	}
	for _, tt := range cases {
		t.Run(tt.address, func(t *testing.T) {
			ip := net.ParseIP(tt.address)
			if got := isBlockedIP(ip); got != tt.blocked {
				t.Fatalf("blocked = %v, want %v", got, tt.blocked)
			}
			feedURL := "https://" + net.JoinHostPort(tt.address, "443") + "/feed"
			if err := validateFeedURL(context.Background(), feedURL); (err != nil) != tt.blocked {
				t.Fatalf("direct URL validation error = %v, want blocked = %v", err, tt.blocked)
			}
			withLookupIP(t, func(context.Context, string) ([]net.IP, error) { return []net.IP{ip}, nil })
			if err := validateFeedURL(context.Background(), "https://feed.example/feed"); (err != nil) != tt.blocked {
				t.Fatalf("DNS URL validation error = %v, want blocked = %v", err, tt.blocked)
			}
			client := safeFeedHTTPClient()
			t.Cleanup(client.CloseIdleConnections)
			for _, target := range []string{feedURL, "https://feed.example/feed"} {
				request, err := http.NewRequest(http.MethodGet, target, nil)
				if err != nil {
					t.Fatal(err)
				}
				if err := client.CheckRedirect(request, nil); (err != nil) != tt.blocked {
					t.Fatalf("redirect validation error = %v, want blocked = %v", err, tt.blocked)
				}
			}
			// An invalid network prevents a connection even if the policy regresses.
			conn, err := safeDialContext(context.Background(), "feed-policy-test", net.JoinHostPort(tt.address, "443"))
			if conn != nil {
				_ = conn.Close()
				t.Fatal("unexpected connection")
			}
			if err == nil || strings.Contains(err.Error(), "blocked IP") != tt.blocked {
				t.Fatalf("direct dial error = %v, want blocked = %v", err, tt.blocked)
			}
			if !tt.blocked && !strings.Contains(err.Error(), "unknown network") {
				t.Fatalf("public address did not reach dialer: %v", err)
			}
		})
	}
}

func TestFeedDestinationRejectsInvalidIP(t *testing.T) {
	for _, ip := range []net.IP{nil, {1, 2, 3}} {
		if !isBlockedIP(ip) {
			t.Fatalf("invalid IP %v accepted", ip)
		}
	}
}

func TestFeedDestinationRejectsMixedDNSAnswers(t *testing.T) {
	for _, blocked := range []string{"10.0.0.1", "192.168.1.1", "fc00::1", "::1"} {
		t.Run(blocked, func(t *testing.T) {
			withLookupIP(t, func(context.Context, string) ([]net.IP, error) {
				return []net.IP{net.ParseIP("8.8.8.8"), net.ParseIP(blocked)}, nil
			})
			if err := validateFeedURL(context.Background(), "https://feed.example/feed"); err == nil {
				t.Fatal("mixed public/nonpublic DNS answers accepted")
			}
			request, err := http.NewRequest(http.MethodGet, "https://feed.example/feed", nil)
			if err != nil {
				t.Fatal(err)
			}
			client := safeFeedHTTPClient()
			t.Cleanup(client.CloseIdleConnections)
			if err := client.CheckRedirect(request, nil); err == nil {
				t.Fatal("redirect with mixed public/nonpublic DNS answers accepted")
			}
		})
	}
}

func TestFeedDestinationDialValidatesDNSAnswers(t *testing.T) {
	for _, tt := range []struct {
		address string
		blocked bool
	}{
		{"10.0.0.1", true},
		{"192.168.1.1", true},
		{"100.64.0.1", false},
		{"198.18.0.1", false},
		{"fc00::1", true},
		{"::1", true},
		{"2001:db8::1", false},
		{"64:ff9b::a00:1", false},
		{"8.8.8.8", false},
		{"2001:4860:4860::8888", false},
	} {
		t.Run(tt.address, func(t *testing.T) {
			withFeedPolicyResolver(t, netip.MustParseAddr(tt.address))
			conn, err := safeDialContext(context.Background(), "feed-policy-test", "feed.example:443")
			if conn != nil {
				_ = conn.Close()
				t.Fatal("unexpected connection")
			}
			if err == nil || strings.Contains(err.Error(), "resolved to blocked IP") != tt.blocked {
				t.Fatalf("DNS dial error = %v, want blocked = %v", err, tt.blocked)
			}
			if !tt.blocked && !strings.Contains(err.Error(), "unknown network") {
				t.Fatalf("public DNS answer did not reach dialer: %v", err)
			}
		})
	}
}

func TestFeedRedirectStopsBeforeNonpublicRequest(t *testing.T) {
	for _, tt := range []struct {
		target  string
		blocked bool
	}{
		{"http://10.0.0.1/feed", true},
		{"http://100.64.0.1/feed", false},
		{"https://[fc00::1]/feed", true},
		{"https://[2001:db8::1]/feed", false},
		{"https://[64:ff9b::a00:1]/feed", false},
		{"http://feed.example/feed", true},
		{"https://192.0.0.9/feed", false},
		{"http://[2001:4860:4860::8888]/feed", false},
	} {
		t.Run(tt.target, func(t *testing.T) {
			withLookupIP(t, func(context.Context, string) ([]net.IP, error) {
				return []net.IP{net.ParseIP("10.0.0.1")}, nil
			})
			client := safeFeedHTTPClient()
			client.CloseIdleConnections()
			var requested []string
			client.Transport = feedPolicyRoundTripper(func(request *http.Request) (*http.Response, error) {
				requested = append(requested, request.URL.String())
				response := &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader("feed response")),
					Request:    request,
				}
				if len(requested) == 1 {
					response.StatusCode = http.StatusFound
					response.Header.Set("Location", tt.target)
				}
				return response, nil
			})
			response, err := client.Get("https://8.8.8.8/feed")
			if response != nil {
				_ = response.Body.Close()
			}
			if (err != nil) != tt.blocked {
				t.Fatalf("redirect request error = %v, want blocked = %v", err, tt.blocked)
			}
			wantRequests := 2
			if tt.blocked {
				wantRequests = 1
			}
			if len(requested) != wantRequests {
				t.Fatalf("requested URLs = %v, want %d requests", requested, wantRequests)
			}
		})
	}
}

type feedPolicyRoundTripper func(*http.Request) (*http.Response, error)

func (f feedPolicyRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func withFeedPolicyResolver(t *testing.T, address netip.Addr) {
	t.Helper()
	original := net.DefaultResolver
	net.DefaultResolver = &net.Resolver{
		PreferGo: true,
		Dial: func(context.Context, string, string) (net.Conn, error) {
			client, server := net.Pipe()
			go func() {
				defer func() { _ = server.Close() }()
				var length [2]byte
				if _, err := io.ReadFull(server, length[:]); err != nil {
					return
				}
				packet := make([]byte, binary.BigEndian.Uint16(length[:]))
				if _, err := io.ReadFull(server, packet); err != nil {
					return
				}
				var query dnsmessage.Message
				if err := query.Unpack(packet); err != nil {
					return
				}
				response := dnsmessage.Message{
					Header:    dnsmessage.Header{ID: query.ID, Response: true, RecursionAvailable: true},
					Questions: query.Questions,
				}
				for _, question := range query.Questions {
					var body dnsmessage.ResourceBody
					if question.Type == dnsmessage.TypeA && address.Is4() {
						body = &dnsmessage.AResource{A: address.As4()}
					}
					if question.Type == dnsmessage.TypeAAAA && address.Is6() {
						body = &dnsmessage.AAAAResource{AAAA: address.As16()}
					}
					if body != nil {
						response.Answers = append(response.Answers, dnsmessage.Resource{
							Header: dnsmessage.ResourceHeader{Name: question.Name, Type: question.Type, Class: dnsmessage.ClassINET, TTL: 60},
							Body:   body,
						})
					}
				}
				packet, err := response.Pack()
				if err != nil {
					return
				}
				binary.BigEndian.PutUint16(length[:], uint16(len(packet)))
				_, _ = server.Write(append(length[:], packet...))
			}()
			return client, nil
		},
	}
	t.Cleanup(func() { net.DefaultResolver = original })
}
