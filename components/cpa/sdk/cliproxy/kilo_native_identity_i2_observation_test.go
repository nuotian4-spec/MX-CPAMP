package cliproxy

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"golang.org/x/net/http2"
)

type i2Hello struct {
	Layer            string   `json:"layer"`
	Connection       string   `json:"connection"`
	SNI              string   `json:"sni"`
	Versions         []uint16 `json:"supported_versions"`
	CipherSuites     []uint16 `json:"cipher_suites"`
	Groups           []uint16 `json:"supported_groups"`
	PointFormats     []uint8  `json:"point_formats"`
	SignatureSchemes []uint16 `json:"signature_schemes"`
	ALPN             []string `json:"alpn_offered"`
}

type i2HTTP struct {
	Connection    string            `json:"connection"`
	Method        string            `json:"method"`
	Host          string            `json:"host"`
	Path          string            `json:"path"`
	Protocol      string            `json:"http_version"`
	TLSVersion    uint16            `json:"tls_version"`
	CipherSuite   uint16            `json:"cipher_suite"`
	ALPN          string            `json:"alpn_negotiated"`
	Resumed       bool              `json:"tls_resumed"`
	Machine       string            `json:"machine"`
	Headers       map[string]string `json:"headers"`
	CredentialOK  bool              `json:"synthetic_credential_matches"`
	ProxyAuthGone bool              `json:"proxy_auth_absent"`
}

type i2Trace struct {
	mu       sync.Mutex
	hellos   []i2Hello
	requests []i2HTTP
	outer    []i2HTTP
	settings map[string][]map[string]any
	conns    []net.Conn
}

func (o *i2Trace) hello(layer string) func(*tls.ClientHelloInfo) (*tls.Config, error) {
	return func(h *tls.ClientHelloInfo) (*tls.Config, error) {
		value := i2Hello{Layer: layer, Connection: h.Conn.RemoteAddr().String(), SNI: h.ServerName, Versions: slices.Clone(h.SupportedVersions), CipherSuites: slices.Clone(h.CipherSuites), PointFormats: slices.Clone(h.SupportedPoints), ALPN: slices.Clone(h.SupportedProtos)}
		for _, group := range h.SupportedCurves {
			value.Groups = append(value.Groups, uint16(group))
		}
		for _, signature := range h.SignatureSchemes {
			value.SignatureSchemes = append(value.SignatureSchemes, uint16(signature))
		}
		o.mu.Lock()
		o.hellos = append(o.hellos, value)
		o.conns = append(o.conns, h.Conn)
		o.mu.Unlock()
		return nil, nil
	}
}

func (o *i2Trace) close() {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, conn := range o.conns {
		_ = conn.Close()
	}
}

func i2HTTPObservation(r *http.Request) i2HTTP {
	value := i2HTTP{Connection: r.RemoteAddr, Method: r.Method, Host: r.Host, Path: r.URL.Path, Protocol: r.Proto}
	if r.TLS != nil {
		value.TLSVersion, value.CipherSuite, value.ALPN, value.Resumed = r.TLS.Version, r.TLS.CipherSuite, r.TLS.NegotiatedProtocol, r.TLS.DidResume
	}
	return value
}

func i2Log(t *testing.T, kind string, value any) {
	t.Helper()
	doc := ojson.NewDocString(`{}`)
	if err := doc.Set("kind", kind); err != nil {
		t.Fatal(err)
	}
	if err := doc.Set("observation", value); err != nil {
		t.Fatal(err)
	}
	t.Log(doc.String())
}

type i2BufferedTLS struct {
	*tls.Conn
	reader *bufio.Reader
}

func (c *i2BufferedTLS) Read(p []byte) (int, error) { return c.reader.Read(p) }

// Peek the client's real opening frame, then give the unchanged bytes to h2.
func i2ServeH2(t *testing.T, trace *i2Trace) func(*http.Server, *tls.Conn, http.Handler) {
	return func(server *http.Server, conn *tls.Conn, handler http.Handler) {
		reader := bufio.NewReaderSize(conn, 65536)
		header, err := reader.Peek(len(http2.ClientPreface) + 9)
		if err != nil {
			t.Errorf("h2 client preface: %v", err)
			return
		}
		if string(header[:len(http2.ClientPreface)]) != http2.ClientPreface {
			t.Error("negotiated h2 without its client preface")
			return
		}
		frameHeader := header[len(http2.ClientPreface):]
		length := int(frameHeader[0])<<16 | int(frameHeader[1])<<8 | int(frameHeader[2])
		wire, err := reader.Peek(len(header) + length)
		if err != nil {
			t.Errorf("h2 opening frame: %v", err)
			return
		}
		frame, err := http2.NewFramer(nil, bytes.NewReader(wire[len(http2.ClientPreface):])).ReadFrame()
		if err != nil {
			t.Errorf("h2 frame decode: %v", err)
			return
		}
		settings, ok := frame.(*http2.SettingsFrame)
		if !ok || settings.IsAck() {
			t.Error("h2 opening frame is not client SETTINGS")
			return
		}
		values := []map[string]any{}
		if err = settings.ForeachSetting(func(s http2.Setting) error {
			values = append(values, map[string]any{"id": uint16(s.ID), "name": s.ID.String(), "value": s.Val})
			return nil
		}); err != nil {
			t.Error(err)
			return
		}
		trace.mu.Lock()
		trace.settings[conn.RemoteAddr().String()] = values
		trace.mu.Unlock()
		serverH2 := &http2.Server{}
		serverH2.ServeConn(&i2BufferedTLS{Conn: conn, reader: reader}, &http2.ServeConnOpts{BaseConfig: server, Handler: handler})
	}
}

func i2Target(t *testing.T, f *kiloNativeFixture, trace *i2Trace) (*httptest.Server, *x509.CertPool) {
	t.Helper()
	modelCert := kiloProxyCertificate(t, "ai-gateway.kilo.ai")
	probeCert := kiloProxyCertificate(t, "api.ipify.org")
	roots := x509.NewCertPool()
	roots.AddCert(modelCert.Leaf)
	roots.AddCert(probeCert.Leaf)
	var calls atomic.Int32
	success := kiloWireSuccess(t, &calls)
	target := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host == "api.ipify.org" && r.Method == "GET" {
			f.probes.Add(1)
			_, _ = io.WriteString(w, "203.0.113.80")
			return
		}
		if r.Host != "ai-gateway.kilo.ai" || r.Method != "POST" || r.URL.Path != "/api/v1/responses" {
			t.Errorf("unexpected local observation target: %s %s %s", r.Method, r.Host, r.URL.Path)
			http.Error(w, "unexpected synthetic target", 400)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		f.requests <- body
		f.posts.Add(1)
		value := i2HTTPObservation(r)
		value.Machine = r.Header.Get("X-KILOCODE-MACHINEID")
		value.CredentialOK = r.Header.Get("Authorization") == "Bearer synthetic-kilo-token"
		value.ProxyAuthGone = r.Header.Get("Proxy-Authorization") == ""
		value.Headers = map[string]string{}
		for _, key := range []string{"User-Agent", "Accept", "Content-Type", "Accept-Encoding", "X-Session-Id", "X-KILOCODE-TASKID", "X-Session-Affinity"} {
			value.Headers[key] = r.Header.Get(key)
		}
		trace.mu.Lock()
		trace.requests = append(trace.requests, value)
		trace.mu.Unlock()
		success(w, r)
	}))
	target.EnableHTTP2 = true
	target.Config.TLSNextProto = map[string]func(*http.Server, *tls.Conn, http.Handler){"h2": i2ServeH2(t, trace)}
	target.TLS = &tls.Config{Certificates: []tls.Certificate{modelCert, probeCert}, NextProtos: []string{"h2", "http/1.1"}, GetConfigForClient: trace.hello("target")}
	target.StartTLS()
	t.Cleanup(func() { trace.close(); target.Close() })
	return target, roots
}

func i2ConnectProxy(t *testing.T, target string, trace *i2Trace, certificate *tls.Certificate, record bool) *httptest.Server {
	t.Helper()
	proxy := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "CONNECT" || (r.Host != "ai-gateway.kilo.ai:443" && r.Host != "api.ipify.org:443") {
			t.Errorf("unexpected local CONNECT: %s %s", r.Method, r.Host)
			http.Error(w, "unexpected CONNECT", 400)
			return
		}
		if record {
			trace.mu.Lock()
			trace.outer = append(trace.outer, i2HTTPObservation(r))
			trace.mu.Unlock()
		}
		remote, err := net.Dial("tcp", target)
		if err != nil {
			t.Error(err)
			return
		}
		client, buffered, err := w.(http.Hijacker).Hijack()
		if err != nil {
			_ = remote.Close()
			t.Error(err)
			return
		}
		trace.mu.Lock()
		trace.conns = append(trace.conns, remote, client)
		trace.mu.Unlock()
		_, _ = buffered.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		_ = buffered.Flush()
		go func() { _, _ = io.Copy(remote, buffered); _ = remote.Close(); _ = client.Close() }()
		_, _ = io.Copy(client, remote)
		_ = remote.Close()
		_ = client.Close()
	}))
	if certificate == nil {
		proxy.Start()
	} else {
		proxy.TLS = &tls.Config{Certificates: []tls.Certificate{*certificate}, NextProtos: []string{"h2", "http/1.1"}, GetConfigForClient: trace.hello("https-proxy")}
		proxy.StartTLS()
	}
	t.Cleanup(func() { trace.close(); proxy.Close() })
	return proxy
}

func TestI2NativeTransportObservations(t *testing.T) {
	isolatedHTTPS := os.Getenv("KILO_TEST_I2_HTTPS") == "1"
	for _, route := range []string{"direct", "http-connect", "https-connect", "socks5", "socks5h"} {
		if isolatedHTTPS && route != "https-connect" {
			continue
		}
		t.Run(route, func(t *testing.T) {
			if route == "https-connect" && !isolatedHTTPS {
				ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
				defer cancel()
				command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestI2NativeTransportObservations$/https-connect$", "-test.v")
				for _, entry := range os.Environ() {
					key, _, _ := strings.Cut(entry, "=")
					if !strings.EqualFold(key, "KILO_TEST_I2_HTTPS") && !strings.EqualFold(key, "GODEBUG") {
						command.Env = append(command.Env, entry)
					}
				}
				command.Env = append(command.Env, "KILO_TEST_I2_HTTPS=1", "GODEBUG=x509usefallbackroots=1")
				output, err := command.CombinedOutput()
				t.Logf("isolated HTTPS observation:\n%s", output)
				if err != nil {
					t.Fatalf("HTTPS observation failed: %v", err)
				}
				return
			}
			f := newKiloNativeFixture(t, kiloSuccessStream, true)
			trace := &i2Trace{settings: map[string][]map[string]any{}}
			target, roots := i2Target(t, f, trace)
			var proxy *httptest.Server
			var wire *kiloWireProxy
			proxyAddress := ""
			if route != "direct" {
				var certificate *tls.Certificate
				if route == "https-connect" {
					cert := kiloProxyCertificate(t, "proxy.i2.invalid")
					certificate = &cert
					outerRoots := x509.NewCertPool()
					outerRoots.AddCert(cert.Leaf)
					x509.SetFallbackRoots(outerRoots)
				}
				proxy = i2ConnectProxy(t, target.Listener.Addr().String(), trace, certificate, route == "http-connect" || route == "https-connect")
				f.cfg.Kilo.Proxies[0].URL = proxy.URL
				proxyAddress = proxy.Listener.Addr().String()
				if route == "https-connect" {
					_, port, _ := net.SplitHostPort(proxyAddress)
					f.cfg.Kilo.Proxies[0].URL = "https://proxy.i2.invalid:" + port
					proxyAddress = "proxy.i2.invalid:" + port
				}
				if route == "socks5" || route == "socks5h" {
					wire = newKiloWireSOCKS(t, f, route, false, "", proxy.Listener.Addr().String())
					f.cfg.Kilo.Proxies[0].URL = wire.URL
				}
			}
			transport := http.DefaultTransport.(*http.Transport)
			// Only synthetic trust and physical routing change. SNI and ALPN remain
			// the real transport's choices, unlike the older hostname-overridden fixture.
			transport.TLSClientConfig = &tls.Config{RootCAs: roots}
			transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
				if route == "direct" && (address == "ai-gateway.kilo.ai:443" || address == "api.ipify.org:443") {
					return (&net.Dialer{}).DialContext(ctx, network, target.Listener.Addr().String())
				}
				if proxy != nil && address == proxyAddress {
					return (&net.Dialer{}).DialContext(ctx, network, proxy.Listener.Addr().String())
				}
				return nil, fmt.Errorf("observation blocked nonlocal dial: %s", address)
			}
			id := m2Create(t, f, "")
			if route == "direct" {
				state := m2Request(t, f, "GET", kiloProxiesURL, "", 200)
				m2Request(t, f, "PUT", kiloAccountsURL+"/"+id+"/egress", fmt.Sprintf(`{"expected_revision":%d,"mode":"direct","exclusive_proxy_ids":[],"primary_proxy_id":""}`, ojson.GetBytes(state, "revision").Uint()), 200)
			}
			for request := 0; request < 2; request++ {
				result := ux2Request(context.Background(), f, "i2-"+route, `{"model":"gpt-6.1-sol","input":"local transport observation"}`)
				if result.err != nil || result.status != 200 {
					t.Fatalf("formal request %d: %+v", request+1, result)
				}
			}
			i1Released(t, f, 2)
			trace.mu.Lock()
			defer trace.mu.Unlock()
			if f.posts.Load() != 2 || len(trace.requests) != 2 || trace.requests[0].Connection != trace.requests[1].Connection {
				t.Fatalf("two formal requests did not reuse their target connection: %+v", trace.requests)
			}
			account, _ := f.manager.GetByID(id)
			modelHellos := 0
			for _, hello := range trace.hellos {
				i2Log(t, "client_hello", hello)
				if hello.Layer == "target" && hello.SNI == "ai-gateway.kilo.ai" {
					modelHellos++
					if hello.Connection != trace.requests[0].Connection || len(hello.Versions) == 0 || len(hello.CipherSuites) == 0 || len(hello.SignatureSchemes) == 0 {
						t.Fatal("target ClientHello not correlated with real requests")
					}
				}
				if hello.Layer == "https-proxy" && (hello.SNI != "proxy.i2.invalid" || !slices.Equal(hello.ALPN, []string{"http/1.1"})) {
					t.Fatalf("HTTPS outer identity/ALPN differs: %+v", hello)
				}
			}
			if modelHellos != 1 {
				t.Fatalf("target SNI/handshake count=%d", modelHellos)
			}
			for _, observed := range trace.requests {
				if !observed.CredentialOK || !observed.ProxyAuthGone || observed.Machine != coreauth.KiloMachineID(account) || observed.TLSVersion < tls.VersionTLS12 {
					t.Fatal("formal identity, credential or verified TLS missing")
				}
				i2Log(t, "target_http", observed)
				if observed.Protocol == "HTTP/2.0" {
					settings, ok := trace.settings[observed.Connection]
					if observed.ALPN != "h2" || !ok {
						t.Fatal("actual h2 request lacks negotiated h2 or SETTINGS evidence")
					}
					i2Log(t, "client_h2_settings", map[string]any{"connection": observed.Connection, "settings": settings})
				} else if observed.Protocol != "HTTP/1.1" || (observed.ALPN != "" && observed.ALPN != "http/1.1") {
					t.Fatalf("unexpected protocol observation: %+v", observed)
				} else {
					i2Log(t, "client_h2_settings", "not applicable: h2 not negotiated")
				}
			}
			for _, outer := range trace.outer {
				i2Log(t, "proxy_connect", outer)
				if route == "https-connect" && (outer.ALPN != "http/1.1" || outer.TLSVersion < tls.VersionTLS12 || outer.Protocol != "HTTP/1.1") {
					t.Fatal("HTTPS proxy outer protocol not recorded independently")
				}
			}
			if wire != nil {
				wire.mu.Lock()
				i2Log(t, "socks_wire", wire.observations)
				wire.mu.Unlock()
			}
			i2Log(t, "summary", map[string]any{"route": route, "model_posts": 2, "target_connections": modelHellos, "same_connection": true, "results": len(f.hook.snapshot()), "force_attempt_http2_base": transport.ForceAttemptHTTP2, "server_alpn": []string{"h2", "http/1.1"}, "official_client_comparison": "not performed; no dynamic sample"})
		})
	}
}
