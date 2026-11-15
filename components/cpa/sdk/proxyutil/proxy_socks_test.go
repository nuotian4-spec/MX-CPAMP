package proxyutil

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"
	"time"
)

func TestBuildHTTPTransportSOCKSHandshakeCancellationClosesSocket(t *testing.T) {
	for _, scheme := range []string{"socks5", "socks5h"} {
		for _, phase := range []string{"greeting", "authentication", "connect"} {
			for _, timeout := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/timeout=%t", scheme, phase, timeout), func(t *testing.T) {
					listener, err := net.Listen("tcp", "127.0.0.1:0")
					if err != nil {
						t.Fatal(err)
					}
					connections := make(chan net.Conn, 1)
					t.Cleanup(func() {
						_ = listener.Close()
						select {
						case conn := <-connections:
							_ = conn.Close()
						default:
						}
					})
					ready, closed := make(chan error, 1), make(chan error, 1)
					go func() {
						conn, err := listener.Accept()
						if err != nil {
							ready <- err
							return
						}
						connections <- conn
						read := func(n int) ([]byte, error) { b := make([]byte, n); _, err := io.ReadFull(conn, b); return b, err }
						greeting, err := read(2)
						if err != nil {
							ready <- err
							return
						}
						if greeting[0] != 5 {
							ready <- fmt.Errorf("SOCKS version=%d", greeting[0])
							return
						}
						if _, err = read(int(greeting[1])); err != nil {
							ready <- err
							return
						}
						if phase != "greeting" {
							if _, err = conn.Write([]byte{5, 2}); err != nil {
								ready <- err
								return
							}
							auth, err := read(2)
							if err != nil {
								ready <- err
								return
							}
							if auth[0] != 1 {
								ready <- fmt.Errorf("authentication version=%d", auth[0])
								return
							}
							if _, err = read(int(auth[1])); err != nil {
								ready <- err
								return
							}
							n, err := read(1)
							if err != nil {
								ready <- err
								return
							}
							if _, err = read(int(n[0])); err != nil {
								ready <- err
								return
							}
							if phase == "connect" {
								if _, err = conn.Write([]byte{1, 0}); err != nil {
									ready <- err
									return
								}
								header, err := read(5)
								if err != nil {
									ready <- err
									return
								}
								if header[0] != 5 || header[1] != 1 || header[3] != 3 {
									ready <- fmt.Errorf("SOCKS CONNECT header=%v", header)
									return
								}
								if _, err = read(int(header[4]) + 2); err != nil {
									ready <- err
									return
								}
							}
						}
						ready <- nil
						_, err = read(1)
						closed <- err
					}()
					transport, mode, err := BuildHTTPTransport(scheme + "://user:pass@" + listener.Addr().String())
					if err != nil || mode != ModeProxy {
						t.Fatalf("transport=%v mode=%v", err, mode)
					}
					defer transport.CloseIdleConnections()
					ctx, cancel := context.WithCancel(context.Background())
					if timeout {
						cancel()
						ctx, cancel = context.WithTimeout(context.Background(), time.Second)
					}
					defer cancel()
					done := make(chan error, 1)
					go func() {
						conn, err := transport.DialContext(ctx, "tcp", "synthetic.invalid:443")
						if conn != nil {
							_ = conn.Close()
						}
						done <- err
					}()
					select {
					case err = <-ready:
						if err != nil {
							t.Fatal(err)
						}
					case <-time.After(3 * time.Second):
						t.Fatal("SOCKS stage not reached")
					}
					if !timeout {
						cancel()
					}
					select {
					case err = <-done:
						if err == nil {
							t.Fatal("interrupted SOCKS dial succeeded")
						}
					case <-time.After(3 * time.Second):
						t.Fatal("SOCKS dial retained cancelled context")
					}
					select {
					case err = <-closed:
						if !errors.Is(err, io.EOF) {
							t.Fatalf("proxy did not observe peer EOF: %v", err)
						}
					case <-time.After(3 * time.Second):
						t.Fatal("SOCKS peer stayed open after cancellation")
					}
					t.Log("formal SOCKS DialContext returned and proxy observed EOF before fixture cleanup")
				})
			}
		}
	}
}
