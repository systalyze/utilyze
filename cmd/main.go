package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/coder/websocket"
	"github.com/systalyze/utilyze/internal/config"
	"github.com/systalyze/utilyze/internal/export"
	"github.com/systalyze/utilyze/internal/ffi/cupti"
	"github.com/systalyze/utilyze/internal/ffi/nvml"
	"github.com/systalyze/utilyze/internal/ffi/sampler"
	"github.com/systalyze/utilyze/internal/inference"
	"github.com/systalyze/utilyze/internal/inference/vllm"
	"github.com/systalyze/utilyze/internal/metrics"
	"github.com/systalyze/utilyze/internal/service"
	"github.com/systalyze/utilyze/internal/tui/screens/top"
	"github.com/systalyze/utilyze/internal/version"

	tea "charm.land/bubbletea/v2"
	"golang.org/x/term"
)

const (
	resolution        = 500 * time.Millisecond
	refreshInterval   = 1000 * time.Millisecond
	metricsInterval   = 250 * time.Millisecond
	inferenceCacheTTL = 30 * time.Second
	vllmProbeTimeout  = 2 * time.Second

	serviceModeEnv = "UTLZ_SERVICE_MODE"
	serviceAddrEnv = "UTLZ_SERVICE_ADDR"

	serviceModeAuto   = "auto"
	serviceModeServer = "server"
	serviceModeClient = "client"
)

type runConfig struct {
	mode        string
	connectAddr string
	listenAddr  string
	config      config.Config
}

type exportConfig struct {
	format   string
	file     string
	interval time.Duration
}

func main() {
	var showVersion bool
	var showEndpoints bool
	var devices string
	var logFile string
	var logLevel string

	var serviceAddr string
	var serviceMode string
	var servicePort string

	var exportFormat string
	var exportFile string
	var exportInterval time.Duration

	flag.BoolVar(&showVersion, "version", false, "print version and exit")
	flag.StringVar(&devices, "devices", os.Getenv("UTLZ_DEVICES"), "comma-separated list of device IDs to monitor")
	flag.BoolVar(&showEndpoints, "endpoints", false, "show discovered inference server endpoints per GPU")

	flag.StringVar(&serviceAddr, "connect", os.Getenv(serviceAddrEnv), "address to connect to for remote metrics over websocket")
	flag.StringVar(&serviceAddr, "c", os.Getenv(serviceAddrEnv), "address to connect to for remote metrics over websocket")
	flag.StringVar(&serviceMode, "mode", defaultServiceMode(), "service mode to run in (auto, server, client)")
	flag.StringVar(&servicePort, "port", "8079", "port to listen on for server mode")

	flag.StringVar(&logFile, "log", os.Getenv("UTLZ_LOG"), "log file to write to")
	flag.StringVar(&logLevel, "log-level", "INFO", "log level for the chat service")

	flag.StringVar(&exportFormat, "export", "", "export metrics in a structured format instead of running the TUI (csv or json)")
	flag.StringVar(&exportFile, "export-file", "", "file to write exported metrics to (default: stdout)")
	flag.DurationVar(&exportInterval, "export-interval", time.Second, "interval between exported rows (e.g. 500ms, 1s)")

	flag.Parse()

	var level slog.Level
	if err := level.UnmarshalText([]byte(logLevel)); err != nil {
		log.Fatalf("failed to parse log level: %v\n", err)
	}
	if logFile != "" {
		var logw io.Writer = io.Discard
		if f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644); err == nil {
			logw = f
		}
		slog.SetDefault(slog.New(slog.NewTextHandler(logw, &slog.HandlerOptions{Level: level})))
	}

	if showVersion {
		fmt.Printf("utilyze v%s\n", version.VERSION)
		os.Exit(0)
	}

	if showEndpoints {
		if err := runShowEndpoints(context.Background()); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	_ = version.CheckForUpdates(context.Background(), version.VERSION)

	deviceIds, err := parseDeviceIDs(devices)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if exportFormat != "" {
		expCfg := exportConfig{format: exportFormat, file: exportFile, interval: exportInterval}
		if err := runExport(context.Background(), deviceIds, expCfg); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if serviceAddr != "" && serviceMode == serviceModeAuto {
		serviceMode = serviceModeClient
	}

	runCfg := runConfig{
		mode:        serviceMode,
		connectAddr: serviceAddress(serviceAddr, servicePort),
		listenAddr:  serviceAddress("", servicePort),
		config:      config.Load(),
	}
	if err := run(context.Background(), deviceIds, runCfg); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func runShowEndpoints(ctx context.Context) error {
	nvmlClient, err := nvml.Init()
	if err != nil {
		return fmt.Errorf("nvml: %w", err)
	}

	count, err := nvmlClient.GetDeviceCount()
	if err != nil {
		return fmt.Errorf("nvml: %w", err)
	}

	gpus := make([]int, count)
	for i := 0; i < count; i++ {
		gpus[i] = i
	}

	scanner := newInferenceScanner(nvmlClient, 0)
	startScan := time.Now()
	atts, err := scanner.Scan(ctx, gpus)
	if err != nil {
		return err
	}
	scanDur := time.Since(startScan)

	fmt.Printf("utlz debug endpoints — scan took %s\n\n", scanDur.Truncate(time.Millisecond))

	if len(atts) == 0 {
		fmt.Println("no attributions discovered (no inference servers found, or /v1/models unreachable)")
		return nil
	}

	sorted := make([]int, 0, len(atts))
	for g := range atts {
		sorted = append(sorted, g)
	}
	sort.Ints(sorted)

	fmt.Printf("%-4s %-10s %-6s %-8s %s\n", "GPU", "sid", "port", "backend", "model")
	for _, g := range sorted {
		a := atts[g]
		fmt.Printf("%-4d %-10d %-6d %-8s %s\n",
			a.GPU, a.SessionID, a.Endpoint.Port, a.Backend, a.ModelID)
	}
	return nil
}

func run(ctx context.Context, deviceIds []int, runCfg runConfig) error {
	mode, err := serviceMode(runCfg.mode)
	if err != nil {
		return err
	}

	switch mode {
	case serviceModeServer:
		return runServer(ctx, deviceIds, runCfg.listenAddr, runCfg.config.ClientID)
	case serviceModeClient:
		return runClient(ctx, runCfg.connectAddr, runCfg.config.ClientID)
	case "", serviceModeAuto:
		if service.ServerAvailable(ctx, runCfg.connectAddr, runCfg.config.ClientID) {
			return runClient(ctx, runCfg.connectAddr, runCfg.config.ClientID)
		}
		return runLocal(ctx, deviceIds, runCfg.listenAddr, runCfg.config.ClientID)
	default:
		return fmt.Errorf("unknown service mode %q", mode)
	}
}

func serviceMode(mode string) (string, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case "", serviceModeAuto, serviceModeServer, serviceModeClient:
		return mode, nil
	default:
		return "", fmt.Errorf("%s must be %q, %q, or %q", serviceModeEnv, serviceModeAuto, serviceModeServer, serviceModeClient)
	}
}

func defaultServiceMode() string {
	if mode := strings.TrimSpace(os.Getenv(serviceModeEnv)); mode != "" {
		return mode
	}
	return serviceModeAuto
}

func serviceAddress(addr string, port string) string {
	if strings.TrimSpace(addr) != "" {
		return addr
	}
	port = strings.TrimSpace(port)
	if port == "" {
		port = service.DefaultPort
	}
	return service.DefaultHost + ":" + port
}

func ensureCanCollectMetrics() (bool, error) {
	if err := cupti.EnsureLoaded(); err != nil {
		return false, err
	}
	if hasCaps, _ := sampler.HasProfilingCapabilities(); hasCaps || os.Getenv("UTLZ_DISABLE_PROFILING_WARNING") == "1" {
		return true, nil
	}

	fmt.Fprintln(os.Stderr, "Warning: GPU profiling requires CAP_SYS_ADMIN. You will likely need to run with sudo:")
	fmt.Fprintln(os.Stderr, "  sudo utlz")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "If you've disabled the NVIDIA profiling restriction on the host you can ignore this warning. To do so, run:")
	fmt.Fprintln(os.Stderr, "  echo 'options nvidia NVreg_RestrictProfilingToAdminUsers=0' | sudo tee /etc/modprobe.d/nvidia-profiling.conf")
	fmt.Fprintln(os.Stderr, "Then either reboot, or reload the driver (stops all GPU processes):")
	fmt.Fprintln(os.Stderr, "  sudo modprobe -rf nvidia_uvm nvidia_drm nvidia_modeset nvidia && sudo modprobe nvidia")
	fmt.Fprintln(os.Stderr, "")
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprintln(os.Stderr, "To proceed anyway in non-interactive environments, set UTLZ_DISABLE_PROFILING_WARNING=1")
		return false, nil
	}

	fmt.Fprintln(os.Stderr, "Press Enter to continue anyway, or Ctrl-C to quit.")
	fmt.Fprintln(os.Stderr, "To skip this prompt in the future, set UTLZ_DISABLE_PROFILING_WARNING=1")
	if _, err := bufio.NewReader(os.Stdin).ReadString('\n'); err != nil {
		return false, fmt.Errorf("failed to read confirmation: %w", err)
	}
	return true, nil
}

func runServer(ctx context.Context, deviceIds []int, addr string, clientID string) error {
	if _, err := ensureCanCollectMetrics(); err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ctx, stopSignals := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()

	collector, err := metrics.NewCollector(deviceIds, metricsInterval)
	if err != nil {
		return err
	}
	defer collector.Close()

	connUrl := service.LiveURL(addr)
	fmt.Fprintf(os.Stderr, "Live metrics URL: %s\n", connUrl)
	fmt.Fprintf(os.Stderr, "You can view metrics from this machine from another machine by running:")
	fmt.Fprintf(os.Stderr, "  utlz --connect %s\n", connUrl)

	svc := service.NewService()
	reporter, err := newMetricsReporter(collector.NVMLClient(), collector.MonitoredDeviceIDs(), clientID, svc.ConnectedClientIDs, func(perGPU map[int]metrics.GpuCeiling) {
		svc.BroadcastCeilings(perGPU)
	})
	if err != nil {
		return err
	}
	if reporter != nil {
		go reporter.Start(ctx)
		defer reporter.Stop()
	}

	go svc.RunCollector(ctx, collector, func(snapshot metrics.MetricsSnapshot) {
		if reporter != nil {
			reporter.Observe(snapshot)
		}
	})

	return svc.Run(ctx, addr)
}

func runLocal(ctx context.Context, deviceIds []int, addr string, clientID string) error {
	if _, err := ensureCanCollectMetrics(); err != nil {
		return err
	}

	svc := service.NewService()
	return runTUI(ctx, "", func(ctx context.Context, p *tea.Program) error {
		collector, err := metrics.NewCollector(deviceIds, metricsInterval)
		if err != nil {
			return err
		}
		defer collector.Close()

		reporter, err := newMetricsReporter(collector.NVMLClient(), collector.MonitoredDeviceIDs(), clientID, svc.ConnectedClientIDs, func(perGPU map[int]metrics.GpuCeiling) {
			svc.BroadcastCeilings(perGPU)
			p.Send(top.RooflineCeilingMsg{PerGPU: convertCeilings(perGPU)})
		})
		if err != nil {
			return err
		}
		if reporter != nil {
			go reporter.Start(ctx)
			defer reporter.Stop()
		}

		go func() {
			if err := svc.Run(ctx, addr); err != nil && ctx.Err() == nil {
				p.Send(top.ErrorMsg{Error: err})
			}
		}()

		p.Send(top.InitMsg{DeviceIDs: collector.MonitoredDeviceIDs()})
		svc.RunCollector(ctx, collector, func(snapshot metrics.MetricsSnapshot) {
			if reporter != nil {
				reporter.Observe(snapshot)
			}
			p.Send(top.MetricsSnapshotMsg{Timestamp: snapshot.Timestamp, GPUs: snapshot.GPUs})
		})
		return nil
	})
}

func runClient(ctx context.Context, addr string, clientID string) error {
	return runTUI(ctx, "", func(ctx context.Context, p *tea.Program) error {
		// when the server abruptly closes the connection, the JSON parse fails with an invalid frame payload data error
		err := service.Stream(ctx, addr, clientID, func(event service.Event) error {
			handleServiceEvent(p, event)
			return nil
		})
		if err != nil && websocket.CloseStatus(err) == websocket.StatusInvalidFramePayloadData {
			return fmt.Errorf("connection closed by server: %w", err)
		}
		return err
	})
}

func runTUI(ctx context.Context, connectionURL string, runReporter func(context.Context, *tea.Program) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	w, h, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		w, h = 80, 24
	}

	screen := top.New(w, h,
		top.WithRefreshInterval(refreshInterval),
		top.WithResolution(resolution),
		top.WithConnectionURL(connectionURL),
	)
	p := tea.NewProgram(screen, tea.WithContext(ctx))

	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
		<-c
		cancel()

		<-c // kill if double interrupt
		p.Kill()
	}()

	go func() {
		err := runReporter(ctx, p)
		if err != nil && ctx.Err() == nil {
			p.Send(top.ErrorMsg{Error: err})
		}
	}()

	_, err = p.Run()
	if errors.Is(err, tea.ErrProgramKilled) {
		return nil
	}
	return err
}

func handleServiceEvent(p *tea.Program, event service.Event) {
	switch event.Type {
	case service.EventInit:
		p.Send(top.InitMsg{DeviceIDs: event.DeviceIDs})
	case service.EventMetrics:
		if event.Snapshot != nil {
			p.Send(top.MetricsSnapshotMsg{Timestamp: event.Snapshot.Timestamp, GPUs: event.Snapshot.GPUs})
		}
	case service.EventCeilings:
		p.Send(top.RooflineCeilingMsg{PerGPU: convertCeilings(event.Ceilings)})
	}
}

func convertCeilings(perGPU map[int]metrics.GpuCeiling) map[int]top.GpuCeiling {
	if len(perGPU) == 0 {
		return nil
	}
	gpuCeilings := make(map[int]top.GpuCeiling, len(perGPU))
	for idx, g := range perGPU {
		gpuCeilings[idx] = top.GpuCeiling{
			ModelName:         g.ModelName,
			ComputeSolCeiling: g.ComputeSolCeiling,
		}
	}
	return gpuCeilings
}

func newInferenceScanner(nvmlClient *nvml.Client, cacheTTL time.Duration) inference.Scanner {
	if nvmlClient == nil {
		return nil
	}

	return inference.New(
		nvmlClient,
		[]inference.Backend{vllm.NewBackend(vllmProbeTimeout)},
		cacheTTL,
	)
}

func newMetricsReporter(
	nvmlClient *nvml.Client,
	monitoredDeviceIDs []int,
	clientID string,
	clientIDs func() []string,
	onCeiling func(perGPU map[int]metrics.GpuCeiling),
) (*metrics.Reporter, error) {
	totalGpuCount, err := nvmlClient.GetDeviceCount()
	if err != nil || totalGpuCount <= 0 {
		return nil, fmt.Errorf("could not query GPU count: %w", err)
	}

	allNames := make([]string, totalGpuCount)
	gpuIDs := make([]string, totalGpuCount)
	for i := 0; i < totalGpuCount; i++ {
		uuid, _ := nvmlClient.GetDeviceUUID(i)
		allNames[i], _ = nvmlClient.GetDeviceName(i)
		gpuIDs[i] = config.GenerateGpuID(uuid)
	}

	return metrics.New(metrics.ReporterConfig{
		ClientID:           clientID,
		ClientIDs:          clientIDs,
		GpuIDs:             gpuIDs,
		GpuNames:           allNames,
		TotalGpuCount:      totalGpuCount,
		Inference:          newInferenceScanner(nvmlClient, inferenceCacheTTL),
		MonitoredDeviceIDs: monitoredDeviceIDs,
		OnCeiling:          onCeiling,
	}), nil
}

func parseDeviceIDs(envValue string) ([]int, error) {
	envValue = strings.TrimSpace(envValue)
	if envValue == "" {
		return nil, nil
	}
	parts := strings.Split(envValue, ",")
	ids := make([]int, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.Atoi(part)
		if err != nil {
			return nil, fmt.Errorf("invalid device ID %q: %w", part, err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// runExport runs the headless metrics export loop. It collects samples in the
// background and emits one row per monitored GPU at exportCfg.interval, in the
// requested CSV/JSON format. Output goes to exportCfg.file when non-empty,
// otherwise to stdout. The loop runs until ctx is cancelled (e.g. SIGINT).
func runExport(ctx context.Context, deviceIds []int, exportCfg exportConfig) error {
	format, err := export.ParseFormat(strings.ToLower(strings.TrimSpace(exportCfg.format)))
	if err != nil {
		return err
	}
	if exportCfg.interval <= 0 {
		return fmt.Errorf("--export-interval must be positive (got %s)", exportCfg.interval)
	}

	if _, err := ensureCanCollectMetrics(); err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ctx, stopSignals := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()

	out := io.Writer(os.Stdout)
	skipHeader := false
	if exportCfg.file != "" {
		f, err := os.OpenFile(exportCfg.file, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return fmt.Errorf("open export file: %w", err)
		}
		defer f.Close()
		// If we're appending to an existing non-empty CSV file, skip the
		// header row so we don't interleave headers with prior data.
		if info, err := f.Stat(); err == nil && info.Size() > 0 {
			skipHeader = true
		}
		out = f
	}

	writer, err := export.NewWriter(out, format)
	if err != nil {
		return err
	}
	if skipHeader {
		writer.SkipCSVHeader()
	}
	defer writer.Flush()

	collector, err := metrics.NewCollector(deviceIds, metricsInterval)
	if err != nil {
		return err
	}
	defer collector.Close()

	nv := collector.NVMLClient()
	monitored := collector.MonitoredDeviceIDs()
	gpuNames := make(map[int]string, len(monitored))
	for _, d := range monitored {
		name, _ := nv.GetDeviceName(d)
		gpuNames[d] = name
	}

	// Latest snapshot per device. The collector publishes a fresh snapshot
	// every metricsInterval (250ms by default); we sample whichever is most
	// recent each time the export ticker fires.
	var latestMu sync.Mutex
	latest := make(map[int]metrics.GPUSnapshot, len(monitored))
	var latestTs time.Time

	// Cache the most recent attainable-compute-SOL ceiling and detected
	// model name received from the Systalyze backend, indexed by device ID.
	var ceilMu sync.RWMutex
	ceilings := map[int]metrics.GpuCeiling{}

	reporter, err := newMetricsReporter(nv, monitored, "", nil, func(perGPU map[int]metrics.GpuCeiling) {
		ceilMu.Lock()
		ceilings = perGPU
		ceilMu.Unlock()
	})
	if err != nil {
		return err
	}
	if reporter != nil {
		go reporter.Start(ctx)
		defer reporter.Stop()
	}

	// Independently scan inference servers so we can populate the model_name
	// column even when the Systalyze backend reporter is disabled (e.g.
	// UTLZ_DISABLE_METRICS=1) or has not yet returned a ceiling response.
	scanner := newInferenceScanner(nv, inferenceCacheTTL)
	var attMu sync.RWMutex
	attributions := map[int]inference.Attribution{}
	if scanner != nil {
		go func() {
			scan := func() {
				scanCtx, cancelScan := context.WithTimeout(ctx, 5*time.Second)
				defer cancelScan()
				if a, err := scanner.Scan(scanCtx, monitored); err == nil {
					attMu.Lock()
					attributions = a
					attMu.Unlock()
				}
			}
			scan()
			t := time.NewTicker(inferenceCacheTTL)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					scan()
				}
			}
		}()
	}

	snapshots := make(chan metrics.MetricsSnapshot)
	go collector.Start(ctx, snapshots)
	go func() {
		for snap := range snapshots {
			if reporter != nil {
				reporter.Observe(snap)
			}
			latestMu.Lock()
			latestTs = snap.Timestamp
			for _, g := range snap.GPUs {
				latest[g.DeviceID] = g
			}
			latestMu.Unlock()
		}
	}()

	ticker := time.NewTicker(exportCfg.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case tick := <-ticker.C:
			rows := buildExportRows(tick, monitored, gpuNames, &latestMu, &latestTs, latest, &ceilMu, ceilings, &attMu, attributions)
			if len(rows) == 0 {
				continue
			}
			if err := writer.WriteRows(rows); err != nil {
				return fmt.Errorf("write export rows: %w", err)
			}
		}
	}
}

// buildExportRows snapshots the latest per-device collector state, ceilings and
// inference attributions under their respective locks and returns one Row per
// monitored device. The returned timestamp falls back to the ticker time when
// no sample has been observed yet.
func buildExportRows(
	tick time.Time,
	monitored []int,
	gpuNames map[int]string,
	latestMu *sync.Mutex,
	latestTs *time.Time,
	latest map[int]metrics.GPUSnapshot,
	ceilMu *sync.RWMutex,
	ceilings map[int]metrics.GpuCeiling,
	attMu *sync.RWMutex,
	attributions map[int]inference.Attribution,
) []export.Row {
	latestMu.Lock()
	ts := *latestTs
	if ts.IsZero() {
		ts = tick
	}
	snaps := make(map[int]metrics.GPUSnapshot, len(latest))
	for k, v := range latest {
		snaps[k] = v
	}
	latestMu.Unlock()

	ceilMu.RLock()
	localCeilings := make(map[int]metrics.GpuCeiling, len(ceilings))
	for k, v := range ceilings {
		localCeilings[k] = v
	}
	ceilMu.RUnlock()

	attMu.RLock()
	localAtts := make(map[int]inference.Attribution, len(attributions))
	for k, v := range attributions {
		localAtts[k] = v
	}
	attMu.RUnlock()

	rows := make([]export.Row, 0, len(monitored))
	for _, d := range monitored {
		row := export.Row{Timestamp: ts, DeviceID: d, GpuName: gpuNames[d]}
		if g, ok := snaps[d]; ok {
			if g.SOL.Valid {
				c := g.SOL.ComputePct
				m := g.SOL.MemoryPct
				row.ComputeSOLPct = &c
				row.MemorySOLPct = &m
			}
			if g.DCGMUtilization.Valid {
				v := g.DCGMUtilization.SMActivePct
				row.SMActivePct = &v
			}
			if g.Bandwidth.Valid {
				ptx := g.Bandwidth.PCIeTxBps / 1e9
				prx := g.Bandwidth.PCIeRxBps / 1e9
				ntx := g.Bandwidth.NVLinkTxBps / 1e9
				nrx := g.Bandwidth.NVLinkRxBps / 1e9
				row.PCIeTxGBps = &ptx
				row.PCIeRxGBps = &prx
				row.NVLinkTxGBps = &ntx
				row.NVLinkRxGBps = &nrx
			}
		}
		if c, ok := localCeilings[d]; ok {
			if c.ComputeSolCeiling != nil {
				v := *c.ComputeSolCeiling
				row.AttainableComputeSOLPct = &v
			}
			if c.ModelName != nil && *c.ModelName != "" {
				row.ModelName = *c.ModelName
			}
		}
		if row.ModelName == "" {
			if a, ok := localAtts[d]; ok && a.ModelID != "" {
				row.ModelName = a.ModelID
			}
		}
		// Skip rows that have no metric data at all (e.g. before the
		// first sampler snapshot has arrived). Otherwise we'd emit a
		// row of nothing but nulls every interval.
		if !rowHasMetrics(row) {
			continue
		}
		rows = append(rows, row)
	}
	return rows
}

// rowHasMetrics reports whether row carries at least one numeric metric.
// Identifying metadata (timestamp, device_id, gpu_name, model_name) doesn't
// count.
func rowHasMetrics(row export.Row) bool {
	return row.ComputeSOLPct != nil ||
		row.MemorySOLPct != nil ||
		row.AttainableComputeSOLPct != nil ||
		row.SMActivePct != nil ||
		row.PCIeTxGBps != nil ||
		row.PCIeRxGBps != nil ||
		row.NVLinkTxGBps != nil ||
		row.NVLinkRxGBps != nil
}
