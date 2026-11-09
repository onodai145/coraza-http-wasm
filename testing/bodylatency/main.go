// bodylatency is a serial diagnostic probe, not a load acceptance test.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	corazaconf "github.com/corazawaf/coraza-coreruleset/coraza/v3"
	crs "github.com/corazawaf/coraza-coreruleset/crs/v4"
	"github.com/corazawaf/coraza/v3"
	"github.com/corazawaf/coraza/v3/debuglog"
	corazaexp "github.com/corazawaf/coraza/v3/experimental"
	corazahttp "github.com/corazawaf/coraza/v3/http"
	hostapi "github.com/http-wasm/http-wasm-host-go/api"
	"github.com/http-wasm/http-wasm-host-go/handler"
	wasmhttp "github.com/http-wasm/http-wasm-host-go/handler/nethttp"
	"github.com/jcchavezs/mergefs"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental"
)

type call struct {
	name     string
	key      string
	start    time.Time
	children time.Duration
}
type timing struct {
	Name        string  `json:"function"`
	Calls       int     `json:"calls"`
	InclusiveMS float64 `json:"inclusive_ms"`
	ExclusiveMS float64 `json:"exclusive_ms"`
}
type profiler struct {
	active   bool
	stack    []call
	times    map[string]*timing
	selected []string
}

func (p *profiler) NewFunctionListener(def api.FunctionDefinition) experimental.FunctionListener {
	n := def.Name()
	// Keep callbacks at operation boundaries; never instrument per-byte loops.
	selected := (strings.Contains(n, "/internal/operators.") && strings.HasSuffix(n, ".Evaluate")) ||
		(strings.Contains(n, "/internal/transformations.") && !strings.Contains(n, "Inplace") && !strings.Contains(n, "inplace")) ||
		((strings.Contains(n, "regexp.Regexp)") || strings.Contains(n, "binaryregexp.Regexp)")) && strings.HasSuffix(n, ".doExecute"))
	if !selected || strings.Contains(n, "asyncify") || strings.Contains(n, "$") {
		return nil
	}
	p.selected = append(p.selected, n)
	return p
}
func (p *profiler) Before(_ context.Context, _ api.Module, def api.FunctionDefinition, _ []uint64, _ experimental.StackIterator) {
	if p.active {
		key := def.Name()
		p.stack = append(p.stack, call{name: def.Name(), key: key, start: time.Now()})
	}
}
func (p *profiler) After(_ context.Context, _ api.Module, def api.FunctionDefinition, _ []uint64) {
	p.finish(def.Name())
}
func (p *profiler) finish(name string) {
	if !p.active {
		return
	}
	if len(p.stack) == 0 {
		panic("unbalanced diagnostic listener")
	}
	c := p.stack[len(p.stack)-1]
	p.stack = p.stack[:len(p.stack)-1]
	if c.name != name {
		panic("mismatched diagnostic listener")
	}
	elapsed := time.Since(c.start)
	if len(p.stack) > 0 {
		p.stack[len(p.stack)-1].children += elapsed
	}
	t := p.times[c.key]
	if t == nil {
		t = &timing{Name: c.key}
		p.times[c.key] = t
	}
	t.Calls++
	t.InclusiveMS += float64(elapsed) / float64(time.Millisecond)
	t.ExclusiveMS += float64(elapsed-c.children) / float64(time.Millisecond)
}

// Rule timing uses Coraza's public debug events; no guest struct layout assumptions.
type ruleLogger struct{ p *profiler }

func (l ruleLogger) IsEnabled(hostapi.LogLevel) bool { return true }
func (l ruleLogger) Log(_ context.Context, _ hostapi.LogLevel, message string) {
	if !l.p.active {
		return
	}
	begin := strings.HasPrefix(message, "Evaluating rule ")
	end := strings.HasPrefix(message, "Finished rule evaluation ")
	if !begin && !end {
		return
	}
	key := ""
	for _, field := range strings.Fields(message) {
		if strings.HasPrefix(field, "rule_id=") {
			key = field
			break
		}
		if strings.HasPrefix(field, "rule_ref=") {
			key = field
		}
	}
	if key == "" {
		panic("rule timing event without identity")
	}
	if begin {
		l.p.stack = append(l.p.stack, call{name: key, key: key, start: time.Now()})
		return
	}
	l.p.finish(key)
}
func (p *profiler) Abort(_ context.Context, _ api.Module, _ api.FunctionDefinition, err error) {
	if p.active {
		panic(err)
	}
}
func fail(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func main() { fail(run()) }
func run() error {
	wasmPath := flag.String("wasm", "build/coraza-http-wasm.wasm", "Existing candidate WASM")
	native := flag.Bool("native", false, "Use native Coraza instead of HTTP WASM")
	sizes := flag.String("sizes", "1024,16384,65536,262144,1048576", "Comma-separated total form body sizes, at most 1 MiB")
	repeat := flag.Int("repeat", 1, "Serial measurements per size, at most 5")
	rulesMode := flag.String("rules", "full", "full or parser (diagnostic isolation only)")
	prefilter := flag.String("prefilter", "Off", "Off or experimental On (diagnostic only)")
	ruleTimes := flag.Bool("rule-times", false, "Use DEBUG start/end rule events; timings include logging overhead")
	interruptible := flag.Bool("interruptible", false, "Enable wazero context interruption checks (changes execution overhead)")
	listen := flag.Bool("functions", false, "Instrument selected guest operation boundaries; timings include callback overhead")
	flag.Parse()
	if *repeat < 1 || *repeat > 5 || (*rulesMode != "full" && *rulesMode != "parser") || (*prefilter != "On" && *prefilter != "Off") || (*native && *listen) || (*ruleTimes && *listen) {
		return fmt.Errorf("invalid diagnostic options")
	}
	var bodySizes []int
	for _, s := range strings.Split(*sizes, ",") {
		n, err := strconv.Atoi(s)
		if err != nil || n < 2 || n > 1024*1024 {
			return fmt.Errorf("invalid bounded body size %q", s)
		}
		bodySizes = append(bodySizes, n)
	}
	directives := "Include @coraza.conf-recommended\nSecRuleEngine On\nSecAuditEngine Off\nSecRequestBodyLimit 13107200\nSecRequestBodyInMemoryLimit 13107200\nSecRxPreFilter " + *prefilter + "\n"
	if *rulesMode == "full" {
		directives += "Include @crs-setup.conf.example\nInclude @owasp_crs/**.conf\n"
	}
	if *ruleTimes {
		directives += "SecDebugLogLevel 4\n"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	p := &profiler{times: map[string]*timing{}}
	if *listen {
		ctx = experimental.WithFunctionListenerFactory(ctx, p)
	}
	// Echo bytes to verify body preservation, with no JSON response inspection.
	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		if _, err := io.Copy(w, r.Body); err != nil {
			panic(err)
		}
	})
	var wrapped http.Handler
	wasmSHA := ""
	if *native {
		cfg := coraza.NewWAFConfig().WithRootFS(mergefs.Merge(crs.FS, corazaconf.FS)).WithDirectives(directives)
		if *ruleTimes {
			cfg = cfg.WithDebugLogger(debuglog.DefaultWithPrinterFactory(func(io.Writer) debuglog.Printer {
				return func(_ debuglog.Level, message, fields string) {
					ruleLogger{p}.Log(ctx, hostapi.LogLevelDebug, message+" "+fields)
				}
			}))
		}
		waf, err := coraza.NewWAF(cfg)
		if err != nil {
			return err
		}
		if closer, ok := waf.(corazaexp.WAFCloser); ok {
			defer closer.Close()
		}
		wrapped = corazahttp.WrapHandler(waf, backend)
	} else {
		guest, err := os.ReadFile(*wasmPath)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(guest)
		wasmSHA = hex.EncodeToString(sum[:])
		config, err := json.Marshal(map[string]any{"directives": []string{directives}})
		if err != nil {
			return err
		}
		mw, err := wasmhttp.NewMiddleware(ctx, guest, handler.GuestConfig(config), handler.Logger(ruleLogger{p}), handler.Runtime(func(c context.Context) (wazero.Runtime, error) {
			return wazero.NewRuntimeWithConfig(c, wazero.NewRuntimeConfigCompiler().WithCloseOnContextDone(*interruptible)), nil
		}))
		if err != nil {
			return err
		}
		defer mw.Close(ctx)
		wrapped = mw.NewHandler(ctx, backend)
	}
	type result struct {
		Size      int     `json:"body_bytes"`
		Iteration int     `json:"iteration"`
		ElapsedMS float64 `json:"elapsed_ms"`
		Status    int     `json:"status"`
		BodySHA   string  `json:"echo_sha256"`
	}
	var measurements []result
	// One small warmup outside measurement/function statistics.
	send := func(body string) (*httptest.ResponseRecorder, time.Duration) {
		r := httptest.NewRequest("POST", "http://example.com/anything", strings.NewReader(body))
		r.Header.Set("User-Agent", "Mozilla/5.0")
		r.Header.Set("Accept", "application/json,text/html;q=0.9,*/*;q=0.8")
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		start := time.Now()
		wrapped.ServeHTTP(w, r.WithContext(ctx))
		return w, time.Since(start)
	}
	warm, _ := send("q=a")
	if warm.Code != 200 || warm.Body.String() != "q=a" {
		return fmt.Errorf("warmup status/body mismatch: %d", warm.Code)
	}
	for _, size := range bodySizes {
		body := "q=" + strings.Repeat("a", size-2)
		for i := 0; i < *repeat; i++ {
			p.active = *listen || *ruleTimes
			w, elapsed := send(body)
			p.active = false
			if w.Code != 200 || w.Body.String() != body {
				return fmt.Errorf("size=%d status/body mismatch: %d", size, w.Code)
			}
			sum := sha256.Sum256(w.Body.Bytes())
			measurements = append(measurements, result{size, i, float64(elapsed) / float64(time.Millisecond), w.Code, hex.EncodeToString(sum[:])})
			fmt.Fprintf(os.Stderr, "size=%d iteration=%d elapsed=%s\n", size, i, elapsed)
		}
	}
	if len(p.stack) != 0 {
		return fmt.Errorf("listener stack did not drain")
	}
	var timings []*timing
	for _, t := range p.times {
		timings = append(timings, t)
	}
	sort.Slice(timings, func(i, j int) bool { return timings[i].ExclusiveMS > timings[j].ExclusiveMS })
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"native": *native, "rules": *rulesMode, "prefilter": *prefilter, "function_callbacks": *listen, "rule_debug_timing": *ruleTimes, "interruptible": *interruptible, "selected_functions": p.selected, "wasm_sha256": wasmSHA, "results": measurements, "functions": timings, "note": "Serial in-process diagnostic; not standard Traefik load acceptance. Nested inclusive times must not be summed."})
}
