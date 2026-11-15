package bps

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestM3CBPSNewClientSOCKSTransport(t *testing.T) {
	for _, reject := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "authentication-rejected"}[reject], func(t *testing.T) {
			var requests atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.Header.Get("Proxy-Authorization") != "" {
					t.Error("proxy credential leaked")
				}
				_, _ = io.WriteString(w, "synthetic BPS transport")
			}))
			defer target.Close()
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			done := make(chan struct{})
			go func() {
				defer close(done)
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				defer conn.Close()
				read := func(n int) ([]byte, error) { b := make([]byte, n); _, err := io.ReadFull(conn, b); return b, err }
				header, err := read(2)
				if err != nil {
					return
				}
				if _, err = read(int(header[1])); err != nil {
					return
				}
				_, _ = conn.Write([]byte{5, 2})
				auth, err := read(2)
				if err != nil {
					return
				}
				user, err := read(int(auth[1]))
				if err != nil {
					return
				}
				size, err := read(1)
				if err != nil {
					return
				}
				password, err := read(int(size[0]))
				if err != nil {
					return
				}
				if string(user) != "bps-user" || string(password) != "bps-pass" {
					t.Error("BPS SOCKS credentials missing")
					return
				}
				if reject {
					_, _ = conn.Write([]byte{1, 1})
					return
				}
				_, _ = conn.Write([]byte{1, 0})
				header, err = read(5)
				if err != nil {
					return
				}
				if header[3] != 3 {
					t.Errorf("BPS target ATYP=%d", header[3])
					return
				}
				address, err := read(int(header[4]) + 2)
				if err != nil {
					return
				}
				if string(address[:len(address)-2]) != "bps-transport.invalid" || binary.BigEndian.Uint16(address[len(address)-2:]) != 80 {
					t.Error("BPS target differs")
					return
				}
				remote, err := net.Dial("tcp", strings.TrimPrefix(target.URL, "http://"))
				if err != nil {
					t.Error(err)
					return
				}
				defer remote.Close()
				_, _ = conn.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 0})
				go func() { _, _ = io.Copy(remote, conn); _ = remote.Close() }()
				_, _ = io.Copy(conn, remote)
			}()
			client := NewClient("socks5://bps-user:bps-pass@" + listener.Addr().String())
			if client.initErr != nil {
				t.Fatal(client.initErr)
			}
			req, _ := http.NewRequestWithContext(context.Background(), "GET", "http://bps-transport.invalid/", nil)
			response, err := client.httpClient.Do(req)
			if reject {
				if err == nil || requests.Load() != 0 {
					t.Fatalf("BPS auth rejection=%v target=%d", err, requests.Load())
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				raw, err := io.ReadAll(response.Body)
				_ = response.Body.Close()
				if err != nil || string(raw) != "synthetic BPS transport" || requests.Load() != 1 {
					t.Fatalf("BPS real transport=%q %v target=%d", raw, err, requests.Load())
				}
			}
			client.httpClient.CloseIdleConnections()
			<-done
			t.Log("BPS NewClient retained real authenticated SOCKS construction; only loopback target, no OAuth invocation")
		})
	}
}
