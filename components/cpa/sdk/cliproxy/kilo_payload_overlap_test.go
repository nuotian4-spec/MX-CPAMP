package cliproxy

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MoeForge/moego/base64"
	"github.com/MoeForge/moego/ojson"
	log "github.com/sirupsen/logrus"
)

func TestKiloPayloadPerformanceOverlap(t *testing.T) {
	mode := os.Getenv("KILO_PAYLOAD_OVERLAP_MODE")
	if mode == "" {
		t.Skip("overlap evidence uses an explicit baseline/candidate run")
	}
	dir := os.Getenv("KILO_PAYLOAD_EVIDENCE")
	imageBody := func(name, mime string) []byte {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		doc := ojson.NewDocString(`{"model":"gpt-6.1-sol","input":[{"role":"user","content":[{"type":"input_image","image_url":""}]}],"stream":true}`)
		_ = doc.Set("input.0.content.0.image_url", "data:image/"+mime+";base64,"+base64.StdEncoding.EncodeToString(raw))
		return doc.Bytes()
	}
	largeBody := imageBody("small-text-original.png", "png")
	equivalentBody := imageBody("small-text-reduced.jpg", "jpeg")
	smallBody := []byte(`{"model":"gpt-6.1-sol","input":"ordinary payload performance fixture","stream":true}`)
	logger := log.StandardLogger()
	oldOut, oldLevel := logger.Out, logger.Level
	hook := &kiloPayloadPerfHook{}
	oldHooks := logger.ReplaceHooks(make(log.LevelHooks))
	logger.AddHook(hook)
	logger.SetOutput(io.Discard)
	logger.SetLevel(log.DebugLevel)
	defer func() { logger.ReplaceHooks(oldHooks); logger.SetOutput(oldOut); logger.SetLevel(oldLevel) }()
	percentile := func(values []float64, p int) float64 {
		copy := slices.Clone(values)
		slices.Sort(copy)
		if len(copy) == 0 {
			return 0
		}
		return copy[(len(copy)*p+99)/100-1]
	}
	var results []map[string]any
	var solo []map[string]any
	for round := 1; round <= 3; round++ {
		t.Run(fmt.Sprintf("round=%d", round), func(t *testing.T) {
			f := newKiloNativeFixture(t, kiloPayloadSuccessStream)
			next := f.cfg.CloneForRuntime()
			field := reflect.ValueOf(&next.Kilo).Elem().FieldByName("ImageReductionEnabled")
			if field.IsValid() {
				field.SetBool(true)
			}
			f.manager.SetConfig(next)
			f.executor.UpdateConfig(next)
			drainDone, drainStopped := make(chan struct{}), make(chan struct{})
			var peakWire atomic.Int64
			go func() {
				defer close(drainStopped)
				for {
					select {
					case body := <-f.requests:
						for old := peakWire.Load(); int64(len(body)) > old; old = peakWire.Load() {
							if peakWire.CompareAndSwap(old, int64(len(body))) {
								break
							}
						}
					case <-drainDone:
						return
					}
				}
			}()
			defer func() { close(drainDone); <-drainStopped }()
			request := func(body []byte) (float64, float64, error) {
				start := time.Now()
				req, _ := http.NewRequest("POST", f.server.URL+"/v1/responses", bytes.NewReader(body))
				req.Header.Set("Authorization", "Bearer synthetic-kilo-key")
				req.Header.Set("Content-Type", "application/json")
				resp, err := f.server.Client().Do(req)
				if err != nil {
					return 0, 0, err
				}
				defer resp.Body.Close()
				var collected bytes.Buffer
				var first float64
				block := make([]byte, 4096)
				for {
					n, readErr := resp.Body.Read(block)
					collected.Write(block[:n])
					if first == 0 && bytes.Contains(collected.Bytes(), []byte("kilo-ok")) {
						first = float64(time.Since(start).Microseconds()) / 1000
					}
					if readErr == io.EOF {
						break
					}
					if readErr != nil {
						return first, 0, readErr
					}
				}
				if resp.StatusCode != 200 || !bytes.Contains(collected.Bytes(), []byte("response.completed")) {
					return first, 0, fmt.Errorf("HTTP=%d response=%s", resp.StatusCode, collected.String())
				}
				return first, float64(time.Since(start).Microseconds()) / 1000, nil
			}
			for i := 0; i < 6; i++ {
				if _, _, err := request(smallBody); err != nil {
					t.Fatal(err)
				}
			}
			runtime.GC()
			var before runtime.MemStats
			runtime.ReadMemStats(&before)
			var peakHeap atomic.Uint64
			var peakEgress atomic.Int64
			sampleDone, sampleStopped := make(chan struct{}), make(chan struct{})
			go func() {
				defer close(sampleStopped)
				ticker := time.NewTicker(10 * time.Millisecond)
				defer ticker.Stop()
				for {
					select {
					case <-ticker.C:
						var m runtime.MemStats
						runtime.ReadMemStats(&m)
						for old := peakHeap.Load(); m.HeapAlloc > old; old = peakHeap.Load() {
							if peakHeap.CompareAndSwap(old, m.HeapAlloc) {
								break
							}
						}
						var active int64
						for _, proxy := range f.executor.KiloProxySnapshot().Proxies {
							active += int64(proxy.Active)
						}
						for old := peakEgress.Load(); active > old; old = peakEgress.Load() {
							if peakEgress.CompareAndSwap(old, active) {
								break
							}
						}
					case <-sampleDone:
						return
					}
				}
			}()
			hook.mu.Lock()
			prepStart := len(hook.durations)
			hook.mu.Unlock()
			var workers sync.WaitGroup
			stopLarge := make(chan struct{})
			ready := make(chan struct{}, 2)
			errors := make(chan error, 2)
			var activeLarge atomic.Int32
			var bigMu sync.Mutex
			var largeFirst, largeTerminal []float64
			start := time.Now()
			for i := 0; i < 2; i++ {
				workers.Add(1)
				go func() {
					defer workers.Done()
					activeLarge.Add(1)
					defer activeLarge.Add(-1)
					ready <- struct{}{}
					for {
						first, end, err := request(largeBody)
						if err != nil {
							errors <- err
							return
						}
						bigMu.Lock()
						largeFirst = append(largeFirst, first)
						largeTerminal = append(largeTerminal, end)
						bigMu.Unlock()
						select {
						case <-stopLarge:
							return
						default:
						}
					}
				}()
			}
			<-ready
			<-ready
			var firstTimes, terminalTimes []float64
			var normalWindows [][2]int64
			overlapped := 0
			pace := time.NewTicker(3 * time.Millisecond)
			for i := 0; i < 200; i++ {
				<-pace.C
				if activeLarge.Load() > 0 {
					overlapped++
				}
				normalStart := time.Now().UnixNano()
				first, end, err := request(smallBody)
				normalWindows = append(normalWindows, [2]int64{normalStart, time.Now().UnixNano()})
				if err != nil {
					t.Error(err)
					break
				}
				firstTimes = append(firstTimes, first)
				terminalTimes = append(terminalTimes, end)
			}
			pace.Stop()
			close(stopLarge)
			workers.Wait()
			close(sampleDone)
			<-sampleStopped
			close(errors)
			for err := range errors {
				t.Error(err)
			}
			var after runtime.MemStats
			runtime.ReadMemStats(&after)
			hook.mu.Lock()
			preparation := slices.Clone(hook.durations[prepStart:])
			intervals := slices.Clone(hook.intervals[prepStart:])
			hook.mu.Unlock()
			prepareOverlap := 0
			for _, normal := range normalWindows {
				for _, prep := range intervals {
					if normal[0] < prep[1] && prep[0] < normal[1] {
						prepareOverlap++
						break
					}
				}
			}
			row := map[string]any{"mode": mode, "round": round, "normal_count": len(firstTimes), "normal_first_ms": firstTimes, "normal_terminal_ms": terminalTimes, "normal_first_p50_ms": percentile(firstTimes, 50), "normal_first_p95_ms": percentile(firstTimes, 95), "normal_terminal_p50_ms": percentile(terminalTimes, 50), "normal_terminal_p95_ms": percentile(terminalTimes, 95), "large_count": len(largeFirst), "large_first_ms": largeFirst, "large_terminal_ms": largeTerminal, "normal_overlapping_large": overlapped, "normal_overlapping_prepare": prepareOverlap, "prepare_ms": preparation, "peak_heap_bytes": peakHeap.Load(), "total_alloc_bytes": after.TotalAlloc - before.TotalAlloc, "gc_count": after.NumGC - before.NumGC, "gc_pause_ns": after.PauseTotalNs - before.PauseTotalNs, "peak_active_egress": peakEgress.Load(), "posts": f.posts.Load(), "connects": f.connects.Load(), "peak_wire_bytes": peakWire.Load(), "elapsed_ms": time.Since(start).Milliseconds()}
			results = append(results, row)
			t.Logf("mode=%s round=%d normal=%d overlap=%d prepOverlap=%d large=%d firstP95=%.3f terminalP95=%.3f heap=%d gc=%d pause=%.3fms", mode, round, len(firstTimes), overlapped, prepareOverlap, len(largeFirst), percentile(firstTimes, 95), percentile(terminalTimes, 95), peakHeap.Load(), after.NumGC-before.NumGC, float64(after.PauseTotalNs-before.PauseTotalNs)/1e6)
			if len(firstTimes) != 200 || overlapped != 200 {
				t.Fatal("ordinary count or concurrent large overlap is below 200")
			}
			for _, item := range []struct {
				name string
				body []byte
			}{{"original", largeBody}, {"equivalent-preshrunk", equivalentBody}} {
				first, end, err := request(item.body)
				if err != nil {
					t.Fatal(err)
				}
				solo = append(solo, map[string]any{"round": round, "input": item.name, "bytes": len(item.body), "first_ms": first, "terminal_ms": end})
			}
		})
	}
	doc := ojson.NewDocString("{}")
	_ = doc.Set("rounds", results)
	_ = doc.Set("successful_large_solo", solo)
	if err := os.WriteFile(filepath.Join(dir, "performance-overlap-"+mode+".json"), doc.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
}
