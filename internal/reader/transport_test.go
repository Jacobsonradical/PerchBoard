package reader

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Exercise actual TLS protocol negotiation with the custom dialer in place.
func TestReaderNegotiatesHTTP2(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Protocol", r.Proto)
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()
	transport := client.Transport.(*http.Transport).Clone()
	defer transport.CloseIdleConnections()
	transport.TLSClientConfig = server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	called := false
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		called = true
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	response, err := (&http.Client{Transport: transport}).Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if !called || response.ProtoMajor != 2 || response.Header.Get("X-Protocol") != "HTTP/2.0" {
		t.Fatalf("custom dialer=%v protocol=%s", called, response.Proto)
	}
}
