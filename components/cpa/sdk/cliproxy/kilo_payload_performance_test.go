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

type kiloPayloadPerfHook struct {
	mu        sync.Mutex
	durations []int64
	intervals [][2]int64
}

func (*kiloPayloadPerfHook) Levels() []log.Level { return []log.Level{log.DebugLevel} }
func (h *kiloPayloadPerfHook) Fire(entry *log.Entry) error {
	if entry.Data["stage"] == "kilo_image_prepare" {
		h.mu.Lock()
		if value, ok := entry.Data["duration_ms"].(int64); ok {
			h.durations = append(h.durations, value)
			h.intervals = append(h.intervals, [2]int64{entry.Time.Add(-time.Duration(value) * time.Millisecond).UnixNano(), entry.Time.UnixNano()})
		}
		h.mu.Unlock()
	}
	return nil
}

func TestKiloPayloadPerformance(t *testing.T) {
	mode := os.Getenv("KILO_PAYLOAD_PERF_MODE")
	if mode == "" {
		t.Skip("performance evidence uses explicit baseline/candidate runs")
	}
	dir := os.Getenv("KILO_PAYLOAD_EVIDENCE")
	raw, err := os.ReadFile(filepath.Join(dir, "small-text-original.png"))
	if err != nil {
		t.Fatal(err)
	}
	imageURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(raw)
	large := ojson.NewDocString(`{"model":"gpt-6.1-sol","input":[{"role":"user","content":[{"type":"input_image","image_url":""}]}],"stream":true}`)
	_ = large.Set("input.0.content.0.image_url", imageURL)
	largeBody := large.Bytes()
	smallBody := []byte(`{"model":"gpt-6.1-sol","input":"ordinary payload performance fixture","stream":true}`)
	logger := log.StandardLogger()
	oldOut, oldLevel := logger.Out, logger.Level
	hook := &kiloPayloadPerfHook{}
	oldHooks := logger.ReplaceHooks(make(log.LevelHooks))
	logger.AddHook(hook)
	logger.SetOutput(io.Discard)
	logger.SetLevel(log.DebugLevel)
	defer func() { logger.ReplaceHooks(oldHooks); logger.SetOutput(oldOut); logger.SetLevel(oldLevel) }()
	var results []map[string]any
	for _, mixed := range []bool{false, true} {
		for round := 1; round <= 3; round++ {
			t.Run(fmt.Sprintf("mixed=%t/round=%d", mixed, round), func(t *testing.T) {
				f := newKiloNativeFixture(t, kiloPayloadSuccessStream)
				next := f.cfg.CloneForRuntime()
				field := reflect.ValueOf(&next.Kilo).Elem().FieldByName("ImageReductionEnabled")
				if field.IsValid() {
					field.SetBool(true)
				}
				f.manager.SetConfig(next)
				f.executor.UpdateConfig(next)
				drainDone := make(chan struct{})
				drainStopped := make(chan struct{})
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
				var peakHeap atomic.Uint64
				var peakActive atomic.Int64
				sampleDone := make(chan struct{})
				sampleStopped := make(chan struct{})
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
							for old := peakActive.Load(); active > old; old = peakActive.Load() {
								if peakActive.CompareAndSwap(old, active) {
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
				var bigWG sync.WaitGroup
				bigErrors := make(chan error, 2)
				var bigFirst, bigTerminal [2]float64
				start := time.Now()
				if mixed {
					for i := 0; i < 2; i++ {
						bigWG.Add(1)
						go func(index int) {
							defer bigWG.Done()
							first, end, err := request(largeBody)
							bigFirst[index] = first
							bigTerminal[index] = end
							if err != nil {
								bigErrors <- err
							}
						}(i)
					}
				}
				firstTimes := make([]float64, 0, 200)
				terminalTimes := make([]float64, 0, 200)
				pace := time.NewTicker(3 * time.Millisecond)
				for i := 0; i < 200; i++ {
					<-pace.C
					first, end, err := request(smallBody)
					if err != nil {
						t.Error(err)
						break
					}
					firstTimes = append(firstTimes, first)
					terminalTimes = append(terminalTimes, end)
				}
				pace.Stop()
				bigWG.Wait()
				close(sampleDone)
				<-sampleStopped
				close(bigErrors)
				for err := range bigErrors {
					t.Error(err)
				}
				hook.mu.Lock()
				preparation := append([]int64(nil), hook.durations[prepStart:]...)
				hook.mu.Unlock()
				p95 := func(values []float64) float64 {
					sorted := append([]float64(nil), values...)
					slices.Sort(sorted)
					if len(sorted) == 0 {
						return 0
					}
					return sorted[(len(sorted)*95+99)/100-1]
				}
				row := map[string]any{"mode": mode, "mixed": mixed, "round": round, "normal_count": len(firstTimes), "normal_first_p95_ms": p95(firstTimes), "normal_terminal_p95_ms": p95(terminalTimes), "normal_first_ms": firstTimes, "normal_terminal_ms": terminalTimes, "large_first_ms": bigFirst[:], "large_terminal_ms": bigTerminal[:], "prepare_ms": preparation, "peak_heap_bytes": peakHeap.Load(), "peak_active_egress": peakActive.Load(), "connects": f.connects.Load(), "posts": f.posts.Load(), "peak_wire_bytes": peakWire.Load(), "elapsed_ms": time.Since(start).Milliseconds()}
				results = append(results, row)
				t.Logf("mode=%s mixed=%t round=%d N=%d firstP95=%.3f terminalP95=%.3f heap=%d active=%d prepare=%v", mode, mixed, round, len(firstTimes), p95(firstTimes), p95(terminalTimes), peakHeap.Load(), peakActive.Load(), preparation)
				if len(firstTimes) != 200 {
					t.Fatal("ordinary sample count is not 200")
				}
			})
		}
	}
	doc := ojson.NewDocString("{}")
	_ = doc.Set("rounds", results)
	if err := os.WriteFile(filepath.Join(dir, "performance-"+mode+".json"), doc.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
}
