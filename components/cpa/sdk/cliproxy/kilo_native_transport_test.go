package cliproxy

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MoeForge/moego/base64"
	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
)

type kiloWireObservation struct {
	Target        string
	Method        byte
	ATYP          byte
	Authenticated bool
	ALPN          string
}

type kiloWireProxy struct {
	URL           string
	listener      net.Listener
	forward       string
	auth          bool
	rejectAuth    bool
	rejectConnect bool
	pauseTarget   string
	pauseAfter    int32
	pauseCalls    atomic.Int32
	rejections    atomic.Int32
	paused        chan struct{}
	closed        chan error
	mu            sync.Mutex
	connections   []net.Conn
	observations  []kiloWireObservation
}

// This proxy speaks real SOCKS on its client leg. Its only destination is the
// existing loopback CONNECT fixture, which forwards to the synthetic TLS target.
func newKiloWireSOCKS(t *testing.T, f *kiloNativeFixture, scheme string, auth bool, pauseTarget string, forward ...string) *kiloWireProxy {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(f.cfg.Kilo.Proxies[0].URL)
	p := &kiloWireProxy{listener: listener, forward: u.Host, auth: auth, pauseTarget: pauseTarget, paused: make(chan struct{}), closed: make(chan error, 1)}
	if len(forward) > 0 {
		p.forward = forward[0]
	}
	p.URL = scheme + "://" + listener.Addr().String()
	if auth {
		p.URL = scheme + "://wire-user:wire-pass@" + listener.Addr().String()
	}
	t.Cleanup(func() {
		_ = listener.Close()
		p.mu.Lock()
		defer p.mu.Unlock()
		for _, c := range p.connections {
			_ = c.Close()
		}
	})
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			p.mu.Lock()
			p.connections = append(p.connections, conn)
			p.mu.Unlock()
			go p.serveSOCKS(t, conn)
		}
	}()
	if len(forward) == 0 {
		f.cfg.Kilo.Proxies[0].URL = p.URL
	}
	return p
}

func (p *kiloWireProxy) serveSOCKS(t *testing.T, conn net.Conn) {
	defer conn.Close()
	read := func(n int) ([]byte, error) { b := make([]byte, n); _, err := io.ReadFull(conn, b); return b, err }
	header, err := read(2)
	if err != nil {
		return
	}
	methods, err := read(int(header[1]))
	if err != nil {
		return
	}
	if header[0] != 5 {
		t.Errorf("SOCKS version=%d", header[0])
		return
	}
	method := byte(0)
	if p.auth {
		method = 2
	}
	found := false
	for _, value := range methods {
		found = found || value == method
	}
	if !found {
		t.Errorf("SOCKS authentication method %d not offered: %v", method, methods)
		return
	}
	if _, err = conn.Write([]byte{5, method}); err != nil {
		return
	}
	authenticated := false
	if p.auth {
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
		if auth[0] != 1 || string(user) != "wire-user" || string(password) != "wire-pass" {
			t.Error("SOCKS RFC1929 credential mismatch")
			return
		}
		if p.rejectAuth {
			p.rejections.Add(1)
			_, _ = conn.Write([]byte{1, 1})
			return
		}
		if _, err = conn.Write([]byte{1, 0}); err != nil {
			return
		}
		authenticated = true
	}
	header, err = read(5)
	if err != nil {
		return
	}
	if header[0] != 5 || header[1] != 1 || header[3] != 3 {
		t.Errorf("SOCKS requires remote domain CONNECT: %v", header)
		return
	}
	address, err := read(int(header[4]) + 2)
	if err != nil {
		return
	}
	target := net.JoinHostPort(string(address[:len(address)-2]), fmt.Sprint(binary.BigEndian.Uint16(address[len(address)-2:])))
	if target != "api.ipify.org:443" && target != "ai-gateway.kilo.ai:443" {
		t.Errorf("unexpected SOCKS target=%s", target)
		return
	}
	p.mu.Lock()
	p.observations = append(p.observations, kiloWireObservation{Target: target, Method: method, ATYP: header[3], Authenticated: authenticated})
	p.mu.Unlock()
	if p.pauseTarget == target && p.pauseCalls.Add(1) > p.pauseAfter {
		close(p.paused)
		_, err = read(1)
		p.closed <- err
		return
	}
	if p.rejectConnect {
		p.rejections.Add(1)
		_, _ = conn.Write([]byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	remote, err := net.Dial("tcp", p.forward)
	if err != nil {
		t.Error(err)
		return
	}
	defer remote.Close()
	_, _ = fmt.Fprintf(remote, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target)
	reader := bufio.NewReader(remote)
	response, err := http.ReadResponse(reader, &http.Request{Method: "CONNECT"})
	if err != nil || response.StatusCode != 200 {
		t.Errorf("loopback forwarding failed: response=%v err=%v", response, err)
		return
	}
	if _, err = conn.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 0}); err != nil {
		return
	}
	go func() { _, _ = io.Copy(remote, conn); _ = remote.Close() }()
	_, _ = io.Copy(conn, reader)
}

func kiloWireSuccess(t *testing.T, calls *atomic.Int32) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Proxy-Authorization") != "" {
			t.Error("proxy credentials leaked to model target")
		}
		doc := ojson.NewDocString(kiloTerminal)
		if err := doc.Set("response.id", fmt.Sprintf("resp-wire-%d", calls.Add(1))); err != nil {
			t.Error(err)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "data: %s\n\n", doc.String())
	}
}

func TestKiloNativeTransportSOCKSAuthentication(t *testing.T) {
	for _, tc := range []struct {
		scheme string
		auth   bool
	}{{"socks5", false}, {"socks5", true}, {"socks5h", true}} {
		t.Run(fmt.Sprintf("%s/auth=%t", tc.scheme, tc.auth), func(t *testing.T) {
			var calls atomic.Int32
			f := newKiloNativeFixture(t, kiloWireSuccess(t, &calls), true)
			wire := newKiloWireSOCKS(t, f, tc.scheme, tc.auth, "")
			id := m2Create(t, f, "")
			status, body := f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","input":"SOCKS production request"}`)
			if status != 200 {
				t.Fatalf("SOCKS inference=%d %s", status, body)
			}
			f.assertReleased(t, 1)
			wire.mu.Lock()
			defer wire.mu.Unlock()
			if f.posts.Load() != 1 || f.probes.Load() != 1 || f.connects.Load() != 1 || len(wire.observations) != 2 {
				t.Fatalf("SOCKS wire counts=%d/%d/%d observations=%+v", f.posts.Load(), f.probes.Load(), f.connects.Load(), wire.observations)
			}
			for _, observation := range wire.observations {
				if observation.ATYP != 3 || observation.Authenticated != tc.auth {
					t.Fatalf("SOCKS authentication/domain=%+v", observation)
				}
			}
			result := f.hook.snapshot()[0]
			if result.AuthID != id || !result.Success {
				t.Fatalf("SOCKS result=%+v", result)
			}
			t.Logf("formal %s: RFC1929=%t remote-domain ATYP3; health=1 POST=1 CONNECT=1 result=1; both leases released", tc.scheme, tc.auth)
		})
	}
}

func TestKiloNativeTransportSOCKSProbeDeadlineClosesSocket(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	wire := newKiloWireSOCKS(t, f, "socks5", true, "api.ipify.org:443")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- helps.ProbeKiloProxy(ctx, wire.URL) }()
	m3bAwait(t, wire.paused)
	if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("probe deadline=%v", err)
	}
	select {
	case err := <-wire.closed:
		if !errors.Is(err, io.EOF) {
			t.Fatalf("probe peer EOF=%v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed-out Probe retained SOCKS socket")
	}
	if f.posts.Load() != 0 || f.probes.Load() != 0 {
		t.Fatal("held SOCKS handshake reached target")
	}
	t.Log("formal Probe deadline returned and proxy observed EOF before fixture cleanup")
}

func TestKiloNativeTransportSOCKSRequestCancellationClosesSocket(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	wire := newKiloWireSOCKS(t, f, "socks5h", true, "ai-gateway.kilo.ai:443")
	m2Create(t, f, `,"allow_direct_when_proxy_full":true`)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "POST", f.server.URL+"/v1/responses", strings.NewReader(`{"model":"gpt-6.1-sol","input":"cancel SOCKS handshake"}`))
	req.Header.Set("Authorization", "Bearer synthetic-kilo-key")
	req.Header.Set("Content-Type", "application/json")
	done := make(chan struct{})
	go func() {
		defer close(done)
		response, err := f.server.Client().Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
		}
	}()
	m3bAwait(t, wire.paused)
	if f.executor.KiloProxySnapshot().Proxies[0].Active != 1 {
		t.Fatal("formal request did not hold its proxy admission")
	}
	cancel()
	m3bAwait(t, done)
	select {
	case err := <-wire.closed:
		if !errors.Is(err, io.EOF) {
			t.Fatalf("request peer EOF=%v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled Kilo request retained detached SOCKS dial")
	}
	f.assertReleased(t, 1)
	if f.posts.Load() != 0 || f.probes.Load() != 1 || f.hook.snapshot()[0].Success {
		t.Fatalf("cancel result/posts/probes=%+v %d %d", f.hook.snapshot(), f.posts.Load(), f.probes.Load())
	}
	t.Log("real Router/Manager/KiloDo cancellation: health1 modelPOST0 result1, proxy EOF observed, both admissions released")
}

func newKiloWireHTTP(t *testing.T, f *kiloNativeFixture, certificate *tls.Certificate) *kiloWireProxy {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	scheme := "http"
	if certificate != nil {
		listener = tls.NewListener(listener, &tls.Config{Certificates: []tls.Certificate{*certificate}, NextProtos: []string{"h2", "http/1.1"}})
		scheme = "https"
	}
	u, _ := url.Parse(f.cfg.Kilo.Proxies[0].URL)
	p := &kiloWireProxy{listener: listener, forward: u.Host, auth: true}
	p.URL = scheme + "://wire-user:wire-pass@" + listener.Addr().String()
	t.Cleanup(func() {
		_ = listener.Close()
		p.mu.Lock()
		defer p.mu.Unlock()
		for _, c := range p.connections {
			_ = c.Close()
		}
	})
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			p.mu.Lock()
			p.connections = append(p.connections, conn)
			p.mu.Unlock()
			go func() {
				defer conn.Close()
				alpn := ""
				if secure, ok := conn.(*tls.Conn); ok {
					if err := secure.Handshake(); err != nil {
						p.rejections.Add(1)
						return
					}
					alpn = secure.ConnectionState().NegotiatedProtocol
					if alpn != "http/1.1" {
						t.Errorf("proxy ALPN=%q", alpn)
						return
					}
				}
				reader := bufio.NewReader(conn)
				req, err := http.ReadRequest(reader)
				if err != nil {
					return
				}
				if req.Method != "CONNECT" || (req.Host != "api.ipify.org:443" && req.Host != "ai-gateway.kilo.ai:443") {
					t.Errorf("HTTP proxy target=%s %s", req.Method, req.Host)
					return
				}
				if req.Header.Get("Proxy-Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte("wire-user:wire-pass")) {
					t.Error("HTTP proxy Basic credential mismatch")
					return
				}
				p.mu.Lock()
				p.observations = append(p.observations, kiloWireObservation{Target: req.Host, Authenticated: true, ALPN: alpn})
				p.mu.Unlock()
				if p.rejectAuth {
					p.rejections.Add(1)
					_, _ = io.WriteString(conn, "HTTP/1.1 407 Proxy Authentication Required\r\nContent-Length: 0\r\n\r\n")
					return
				}
				remote, err := net.Dial("tcp", p.forward)
				if err != nil {
					t.Error(err)
					return
				}
				defer remote.Close()
				_, _ = fmt.Fprintf(remote, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", req.Host, req.Host)
				remoteReader := bufio.NewReader(remote)
				response, err := http.ReadResponse(remoteReader, &http.Request{Method: "CONNECT"})
				if err != nil || response.StatusCode != 200 {
					t.Errorf("forward CONNECT=%v %v", response, err)
					return
				}
				if _, err = io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
					return
				}
				go func() { _, _ = io.Copy(remote, reader); _ = remote.Close() }()
				_, _ = io.Copy(conn, remoteReader)
			}()
		}
	}()
	transport := http.DefaultTransport.(*http.Transport)
	dial := transport.DialContext
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address == listener.Addr().String() {
			return (&net.Dialer{}).DialContext(ctx, network, address)
		}
		return dial(ctx, network, address)
	}
	f.cfg.Kilo.Proxies[0].URL = p.URL
	return p
}

func TestKiloNativeTransportHTTPBasic(t *testing.T) {
	var calls atomic.Int32
	f := newKiloNativeFixture(t, kiloWireSuccess(t, &calls), true)
	wire := newKiloWireHTTP(t, f, nil)
	m2Create(t, f, "")
	status, body := f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","input":"HTTP Basic"}`)
	if status != 200 {
		t.Fatalf("HTTP Basic inference=%d %s", status, body)
	}
	f.assertReleased(t, 1)
	wire.mu.Lock()
	defer wire.mu.Unlock()
	if f.posts.Load() != 1 || f.probes.Load() != 1 || f.connects.Load() != 1 || len(wire.observations) != 2 {
		t.Fatalf("HTTP counts=%d/%d/%d %+v", f.posts.Load(), f.probes.Load(), f.connects.Load(), wire.observations)
	}
	for _, o := range wire.observations {
		if !o.Authenticated {
			t.Fatal("Basic authentication missing")
		}
	}
	t.Log("HTTP CONNECT Basic verified on health and inference; target received no Proxy-Authorization; POST1/result1/released")
}

func kiloProxyCertificate(t *testing.T, host string) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: host}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IsCA: true, BasicConstraintsValid: true}
	if ip := net.ParseIP(host); ip != nil {
		cert.IPAddresses = []net.IP{ip}
	} else {
		cert.DNSNames = []string{host}
	}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}
}

func TestKiloNativeTransportHTTPSLayers(t *testing.T) {
	selected := os.Getenv("KILO_TEST_HTTPS_LAYER")
	if selected == "" {
		for _, name := range []string{"success", "proxy-untrusted", "proxy-hostname", "target-untrusted"} {
			t.Run(name, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestKiloNativeTransportHTTPSLayers$", "-test.v")
				env := make([]string, 0, len(os.Environ())+2)
				for _, entry := range os.Environ() {
					key, _, _ := strings.Cut(entry, "=")
					if !strings.EqualFold(key, "KILO_TEST_HTTPS_LAYER") && !strings.EqualFold(key, "GODEBUG") {
						env = append(env, entry)
					}
				}
				cmd.Env = append(env, "KILO_TEST_HTTPS_LAYER="+name, "GODEBUG=x509usefallbackroots=1")
				output, err := cmd.CombinedOutput()
				t.Logf("isolated synthetic-root HTTPS %s:\n%s", name, output)
				if err != nil || !strings.Contains(string(output), "--- PASS: TestKiloNativeTransportHTTPSLayers") {
					t.Fatalf("HTTPS layer child failed: %v", err)
				}
			})
		}
		return
	}
	var calls atomic.Int32
	f := newKiloNativeFixture(t, kiloWireSuccess(t, &calls), true)
	certHost := "127.0.0.1"
	if selected == "proxy-hostname" {
		certHost = "wrong.synthetic.invalid"
	}
	certificate := kiloProxyCertificate(t, certHost)
	roots := x509.NewCertPool()
	if selected != "proxy-untrusted" {
		roots.AddCert(certificate.Leaf)
	}
	// The child process uses only this synthetic fallback; the OS trust store is untouched.
	x509.SetFallbackRoots(roots)
	wire := newKiloWireHTTP(t, f, &certificate)
	if selected == "target-untrusted" {
		http.DefaultTransport.(*http.Transport).TLSClientConfig.RootCAs = x509.NewCertPool()
	}
	m2Create(t, f, `,"allow_direct_when_proxy_full":true`)
	status, body := f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","input":"HTTPS layers"}`)
	if selected == "success" {
		if status != 200 {
			t.Fatalf("verified HTTPS=%d %s", status, body)
		}
		f.assertReleased(t, 1)
		wire.mu.Lock()
		defer wire.mu.Unlock()
		if f.posts.Load() != 1 || f.probes.Load() != 1 || len(wire.observations) != 2 {
			t.Fatalf("HTTPS counts=%d/%d %+v", f.posts.Load(), f.probes.Load(), wire.observations)
		}
		for _, o := range wire.observations {
			if o.ALPN != "http/1.1" || !o.Authenticated {
				t.Fatalf("HTTPS ALPN/Basic=%+v", o)
			}
		}
		t.Log("two verified TLS layers, proxy ALPN http/1.1, Basic CONNECT health+inference, POST1/result1/released")
	} else {
		if status != 503 || f.posts.Load() != 0 || f.probes.Load() != 0 {
			t.Fatalf("rejected TLS reached target: status=%d POST=%d healthGET=%d body=%s", status, f.posts.Load(), f.probes.Load(), body)
		}
		f.assertReleased(t, 0)
		wire.mu.Lock()
		defer wire.mu.Unlock()
		if selected == "target-untrusted" && len(wire.observations) != 1 {
			t.Fatal("target TLS failure did not follow accepted outer TLS/Basic CONNECT")
		}
		if selected != "target-untrusted" && (wire.rejections.Load() != 1 || len(wire.observations) != 0) {
			t.Fatal("proxy TLS failure reached CONNECT")
		}
		t.Logf("%s certificate rejected before target HTTP; POST0, no direct/replay, admissions0", selected)
	}
}

func TestKiloNativeTransportProxyRejections(t *testing.T) {
	for _, kind := range []string{"http-basic", "socks-auth", "socks-connect"} {
		t.Run(kind, func(t *testing.T) {
			f := newKiloNativeFixture(t, kiloSuccessStream, true)
			var wire *kiloWireProxy
			if kind == "http-basic" {
				wire = newKiloWireHTTP(t, f, nil)
				wire.rejectAuth = true
			} else {
				wire = newKiloWireSOCKS(t, f, "socks5", true, "")
				wire.rejectAuth = kind == "socks-auth"
				wire.rejectConnect = kind == "socks-connect"
			}
			m2Create(t, f, `,"allow_direct_when_proxy_full":true`)
			status, body := f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","input":"reject auth"}`)
			if status != 503 || f.posts.Load() != 0 || f.probes.Load() != 0 || f.connects.Load() != 0 || wire.rejections.Load() != 1 {
				t.Fatalf("rejection=%d POST=%d health=%d connects=%d rejects=%d body=%s", status, f.posts.Load(), f.probes.Load(), f.connects.Load(), wire.rejections.Load(), body)
			}
			f.assertReleased(t, 0)
			t.Log("formal authentication/CONNECT rejection: one handshake, zero model POST/direct/replay/results; both admissions0")
		})
	}
}

func kiloWireInference(t *testing.T, f *kiloNativeFixture, label string) []byte {
	t.Helper()
	status, body := f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", fmt.Sprintf(`{"model":"gpt-6.1-sol","input":%q}`, label))
	if status != 200 {
		t.Fatalf("inference %s=%d %s", label, status, body)
	}
	return body
}

func TestKiloNativeTransportStrictReuseAndIsolation(t *testing.T) {
	for _, kind := range []string{"same", "account", "proxy-id", "direct", "url"} {
		t.Run(kind, func(t *testing.T) {
			var calls atomic.Int32
			var mu sync.Mutex
			var peers []string
			success := kiloWireSuccess(t, &calls)
			f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				peers = append(peers, r.RemoteAddr)
				mu.Unlock()
				success(w, r)
			}, true)
			f.allowDirect = true
			wire := newKiloWireSOCKS(t, f, "socks5h", true, "")
			id := m2Create(t, f, "")
			kiloWireInference(t, f, "reuse-first")
			f.assertReleased(t, 1)
			var other *kiloWireProxy
			switch kind {
			case "account":
				m2Request(t, f, "PATCH", kiloAccountsURL+"/"+id, `{"enabled":false}`, 200)
				m2Create(t, f, "")
			case "proxy-id":
				view := m2Request(t, f, "POST", kiloProxiesURL, fmt.Sprintf(`{"expected_revision":1,"url":%q}`, wire.URL), 201)
				pid := ojson.GetBytes(view, "proxies.1.id").String()
				m2Request(t, f, "PUT", kiloAccountsURL+"/"+id+"/egress", fmt.Sprintf(`{"expected_revision":2,"mode":"pool","exclusive_proxy_ids":[%q],"primary_proxy_id":%q}`, pid, pid), 200)
			case "direct":
				m2Request(t, f, "PUT", kiloAccountsURL+"/"+id+"/egress", `{"expected_revision":1,"mode":"direct","exclusive_proxy_ids":[],"primary_proxy_id":""}`, 200)
			case "url":
				other = newKiloWireSOCKS(t, f, "socks5h", true, "", wire.forward)
				m2Request(t, f, "PATCH", kiloProxiesURL+"/local-proxy", fmt.Sprintf(`{"expected_revision":1,"url":%q}`, other.URL), 200)
			}
			kiloWireInference(t, f, "reuse-second")
			if kind == "proxy-id" {
				m3Wait(t, func() bool {
					return len(f.hook.snapshot()) == 2 && f.executor.KiloProxySnapshot().Proxies[1].Active == 0
				})
			} else {
				f.assertReleased(t, 2)
			}
			mu.Lock()
			defer mu.Unlock()
			if len(peers) != 2 || f.posts.Load() != 2 {
				t.Fatalf("request counts=%d peers=%v", f.posts.Load(), peers)
			}
			if kind == "same" {
				if peers[0] != peers[1] || f.connects.Load() != 1 {
					t.Fatalf("same-client connection not reused: peers=%v CONNECT=%d", peers, f.connects.Load())
				}
			} else if peers[0] == peers[1] {
				t.Fatalf("%s crossed isolation boundary: peers=%v", kind, peers)
			}
			wantConnect := int32(2)
			if kind == "same" || kind == "direct" {
				wantConnect = 1
			}
			if f.connects.Load() != wantConnect {
				t.Fatalf("%s inference CONNECT=%d want=%d", kind, f.connects.Load(), wantConnect)
			}
			results := f.hook.snapshot()
			if (results[0].AuthID == results[1].AuthID) != (kind != "account") {
				t.Fatalf("AuthID isolation=%+v", results)
			}
			if other != nil {
				wire.mu.Lock()
				before := len(wire.observations)
				wire.mu.Unlock()
				other.mu.Lock()
				after := len(other.observations)
				other.mu.Unlock()
				if before != 2 || after != 2 {
					t.Fatalf("URL change reached old destination: old=%d new=%d", before, after)
				}
			}
			t.Logf("strict %s: actual target peers=%v POST2 CONNECT%d; completed response reuse/isolation verified", kind, peers, f.connects.Load())
		})
	}
}

func TestKiloNativeTransportCancelPreservesOtherInflight(t *testing.T) {
	started, finish := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-finish:
		default:
			close(finish)
		}
	}()
	f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) { close(started); <-finish; kiloSuccessStream(w, r) }, true)
	wire := newKiloWireSOCKS(t, f, "socks5", true, "ai-gateway.kilo.ai:443")
	wire.pauseAfter = 1
	m2Create(t, f, "")
	first := make(chan int, 1)
	go func() {
		status, _ := f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","input":"keep live connection"}`)
		first <- status
	}()
	m3bAwait(t, started)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "POST", f.server.URL+"/v1/responses", strings.NewReader(`{"model":"gpt-6.1-sol","input":"cancel second pending dial"}`))
	req.Header.Set("Authorization", "Bearer synthetic-kilo-key")
	req.Header.Set("Content-Type", "application/json")
	done := make(chan struct{})
	go func() {
		defer close(done)
		response, err := f.server.Client().Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
		}
	}()
	m3bAwait(t, wire.paused)
	if f.executor.KiloProxySnapshot().Proxies[0].Active != 2 {
		t.Fatal("two same-client requests did not share live admission")
	}
	cancel()
	m3bAwait(t, done)
	select {
	case err := <-wire.closed:
		if !errors.Is(err, io.EOF) {
			t.Fatalf("cancel EOF=%v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("pending socket not closed")
	}
	m3Wait(t, func() bool { return f.executor.KiloProxySnapshot().Proxies[0].Active == 1 })
	select {
	case status := <-first:
		t.Fatalf("CloseIdleConnections terminated live request: %d", status)
	default:
	}
	close(finish)
	if status := <-first; status != 200 {
		t.Fatalf("retained request=%d", status)
	}
	f.assertReleased(t, 2)
	if f.posts.Load() != 1 || f.connects.Load() != 1 || f.probes.Load() != 1 {
		t.Fatalf("cancel counters=%d/%d/%d", f.posts.Load(), f.connects.Load(), f.probes.Load())
	}
	results := f.hook.snapshot()
	successes := 0
	for _, r := range results {
		if r.Success {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("results=%+v", results)
	}
	t.Log("same AuthID/proxy/mode client: cancelled pending SOCKS socket EOF, other active connection remained admitted and completed200; one success/one cancellation, slots0")
}

func TestKiloNativeTransportReusedPOSTFailureNeverReplays(t *testing.T) {
	var calls atomic.Int32
	var mu sync.Mutex
	var peers []string
	f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		mu.Lock()
		peers = append(peers, r.RemoteAddr)
		mu.Unlock()
		if n == 2 {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = conn.Close()
			return
		}
		doc := ojson.NewDocString(kiloTerminal)
		_ = doc.Set("response.id", fmt.Sprintf("resp-post-close-%d", n))
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "data: %s\n\n", doc.String())
	}, true)
	newKiloWireSOCKS(t, f, "socks5", true, "")
	m2Create(t, f, `,"allow_direct_when_proxy_full":true`)
	kiloWireInference(t, f, "post-before-close")
	f.assertReleased(t, 1)
	status, body := f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","input":"post-confirmed-once"}`)
	if status != 502 {
		t.Fatalf("accepted second POST disconnect=%d %s", status, body)
	}
	f.assertReleased(t, 2)
	mu.Lock()
	if len(peers) != 2 || peers[0] != peers[1] {
		t.Fatalf("failure was not on reused connection: %v", peers)
	}
	mu.Unlock()
	if f.posts.Load() != 2 || f.connects.Load() != 1 || f.hook.snapshot()[1].Success {
		t.Fatalf("failed POST replay/result=%d/%d %+v", f.posts.Load(), f.connects.Load(), f.hook.snapshot())
	}
	seen := 0
	for i := 0; i < 2; i++ {
		raw := <-f.requests
		if strings.Contains(string(raw), "post-confirmed-once") {
			seen++
		}
	}
	if seen != 1 {
		t.Fatalf("business marker received %d times", seen)
	}
	view := m2Request(t, f, "GET", kiloProxiesURL, "", 200)
	m2Request(t, f, "POST", kiloProxiesURL+"/local-proxy/recheck", fmt.Sprintf(`{"expected_revision":%d}`, ojson.GetBytes(view, "revision").Uint()), 200)
	kiloWireInference(t, f, "independent-after-close")
	f.assertReleased(t, 3)
	mu.Lock()
	defer mu.Unlock()
	if peers[2] == peers[1] || f.posts.Load() != 3 || f.connects.Load() != 2 {
		t.Fatalf("new independent request=%v POST%d CONNECT%d", peers, f.posts.Load(), f.connects.Load())
	}
	t.Log("second complete POST received exactly once on reused connection then target closed; 502/result failure once, no direct/replay; explicit recheck permits independent POST on fresh connection")
}

func TestKiloNativeTransportTruncatedThenCleanRequest(t *testing.T) {
	var calls atomic.Int32
	f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		if n == 1 {
			_, _ = io.WriteString(w, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"truncated-wire\"}}\n\ndata: {\"residual\"")
			return
		}
		doc := ojson.NewDocString(kiloTerminal)
		_ = doc.Set("response.id", "resp-clean-wire")
		_, _ = fmt.Fprintf(w, "data: %s\n\n", doc.String())
	}, true)
	newKiloWireSOCKS(t, f, "socks5h", true, "")
	id := m2Create(t, f, "")
	status, _ := f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","input":"truncated-once"}`)
	if status != 502 {
		t.Fatalf("truncated=%d", status)
	}
	f.assertReleased(t, 1)
	status, _ = f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","input":"cooled-account"}`)
	if status != 503 || f.posts.Load() != 1 {
		t.Fatal("truncated-event account cooldown was bypassed")
	}
	// Keep the existing failure cooldown; another explicit account is a new
	// independent request, not a continuation of the truncated exchange.
	m2Request(t, f, "PATCH", kiloAccountsURL+"/"+id, `{"enabled":false}`, 200)
	other := m2Create(t, f, "")
	body := kiloWireInference(t, f, "clean-independent")
	f.assertReleased(t, 2)
	if ojson.GetBytes(body, "id").String() != "resp-clean-wire" || strings.Contains(string(body), "residual") || f.posts.Load() != 2 {
		t.Fatalf("residual contamination: POST%d body=%s", f.posts.Load(), body)
	}
	if f.hook.snapshot()[0].Success || !f.hook.snapshot()[1].Success {
		t.Fatalf("results=%+v", f.hook.snapshot())
	}
	if f.hook.snapshot()[0].AuthID != id || f.hook.snapshot()[1].AuthID != other || id == other {
		t.Fatal("independent account boundary missing")
	}
	t.Log("truncated response failed once; original AuthID remained cooled with POST count unchanged; separately created account received only fresh response ID/body, two POSTs/two results and slots0")
}

func TestKiloNativeTransportCancelledStreamThenCleanRequest(t *testing.T) {
	var calls atomic.Int32
	var mu sync.Mutex
	var peers []string
	upstreamCancelled := make(chan struct{})
	f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		mu.Lock()
		peers = append(peers, r.RemoteAddr)
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		if n == 1 {
			_, _ = io.WriteString(w, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp-cancel-wire\",\"status\":\"in_progress\"}}\n\n")
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			close(upstreamCancelled)
			return
		}
		doc := ojson.NewDocString(kiloTerminal)
		_ = doc.Set("response.id", "resp-after-cancel-wire")
		_, _ = fmt.Fprintf(w, "data: %s\n\n", doc.String())
	}, true)
	newKiloWireSOCKS(t, f, "socks5", true, "")
	m2Create(t, f, "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "POST", f.server.URL+"/v1/responses", strings.NewReader(`{"model":"gpt-6.1-sol","input":"cancel-complete-frame","stream":true}`))
	req.Header.Set("Authorization", "Bearer synthetic-kilo-key")
	req.Header.Set("Content-Type", "application/json")
	response, err := f.server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(response.Body)
	frame := ""
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		frame += line
		if line == "\n" {
			break
		}
	}
	if !strings.Contains(frame, "response.created") {
		t.Fatalf("complete downstream frame missing=%s", frame)
	}
	cancel()
	_ = response.Body.Close()
	m3bAwait(t, upstreamCancelled)
	f.assertReleased(t, 1)
	body := kiloWireInference(t, f, "clean-after-cancel")
	f.assertReleased(t, 2)
	mu.Lock()
	defer mu.Unlock()
	if len(peers) != 2 || peers[0] == peers[1] || f.posts.Load() != 2 || f.connects.Load() != 2 || ojson.GetBytes(body, "id").String() != "resp-after-cancel-wire" || strings.Contains(string(body), "resp-cancel-wire") {
		t.Fatalf("cancel isolation peers=%v POST%d CONNECT%d body=%s", peers, f.posts.Load(), f.connects.Load(), body)
	}
	if f.hook.snapshot()[0].AuthID != f.hook.snapshot()[1].AuthID || f.hook.snapshot()[0].Success || !f.hook.snapshot()[1].Success {
		t.Fatalf("cancel results=%+v", f.hook.snapshot())
	}
	t.Log("complete downstream frame observed before cancellation; target context closed; same AuthID/key independent request used fresh connection with no residual bytes, POST2/results2/slots0")
}

func TestKiloNativeTransportClosedIdleConnectionRedials(t *testing.T) {
	var calls atomic.Int32
	var mu sync.Mutex
	var peers []string
	success := kiloWireSuccess(t, &calls)
	f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		peers = append(peers, r.RemoteAddr)
		mu.Unlock()
		success(w, r)
	}, true)
	wire := newKiloWireHTTP(t, f, nil)
	m2Create(t, f, "")
	kiloWireInference(t, f, "before-idle-close")
	f.assertReleased(t, 1)
	wire.mu.Lock()
	connections := append([]net.Conn(nil), wire.connections...)
	wire.mu.Unlock()
	if len(connections) != 2 {
		t.Fatalf("health/inference connections=%d", len(connections))
	}
	// Close only the completed inference's idle peer; this is the injected
	// server-side failure, not cleanup used to claim cancellation success.
	_ = connections[1].Close()
	kiloWireInference(t, f, "after-idle-close")
	f.assertReleased(t, 2)
	mu.Lock()
	defer mu.Unlock()
	if len(peers) != 2 || peers[0] == peers[1] || f.posts.Load() != 2 || f.connects.Load() != 2 {
		t.Fatalf("idle reconnection peers=%v POST%d CONNECT%d", peers, f.posts.Load(), f.connects.Load())
	}
	markers := map[string]int{}
	for i := 0; i < 2; i++ {
		raw := string(<-f.requests)
		for _, label := range []string{"before-idle-close", "after-idle-close"} {
			if strings.Contains(raw, label) {
				markers[label]++
			}
		}
	}
	if markers["before-idle-close"] != 1 || markers["after-idle-close"] != 1 {
		t.Fatalf("business marker counts=%v", markers)
	}
	t.Log("server closed idle peer after complete first response; independent request redialed once, each business marker received once; POST2/CONNECT2/result2/slots0")
}
