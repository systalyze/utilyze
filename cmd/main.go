package main

import (
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
	"syscall"
	"time"

	"github.com/systalyze/utilyze/internal/ffi/cupti"
	"github.com/systalyze/utilyze/internal/ffi/nvml"
	"github.com/systalyze/utilyze/internal/ffi/sampler"
	"github.com/systalyze/utilyze/internal/inference"
	"github.com/systalyze/utilyze/internal/inference/vllm"
	"github.com/systalyze/utilyze/internal/metrics"
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
)

func main() {
	var showVersion bool
	var showEndpoints bool
	var devices string
	var logFile string
	var logLevel string

	flag.BoolVar(&showVersion, "version", false, "print version and exit")
	flag.BoolVar(&showEndpoints, "endpoints", false, "show discovered inference server endpoints per GPU")
	flag.StringVar(&devices, "devices", os.Getenv("UTLZ_DEVICES"), "comma-separated list of device IDs to monitor")
	flag.StringVar(&logFile, "log", os.Getenv("UTLZ_LOG"), "log file to write to")
	flag.StringVar(&logLevel, "log-level", "INFO", "log level for the chat service")

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

	deviceIds, err := parseDeviceIDs(devices)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	_ = version.CheckForUpdates(context.Background(), version.VERSION)

	if err := run(context.Background(), deviceIds); err != nil {
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

func run(ctx context.Context, deviceIds []int) error {
	if err := cupti.EnsureLoaded(); err != nil {
		return err
	}

	showWarning := os.Getenv("UTLZ_DISABLE_PROFILING_WARNING") != "1"
	if hasCaps, _ := sampler.HasProfilingCapabilities(); !hasCaps && showWarning {
		fmt.Fprintln(os.Stderr, "Warning: GPU profiling requires CAP_SYS_ADMIN. You will likely need to run with sudo:")
		fmt.Fprintln(os.Stderr, "  sudo utlz")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "If you've disabled the NVIDIA profiling restriction on the host you can ignore this warning. To do so, run:")
		fmt.Fprintln(os.Stderr, "  echo 'options nvidia NVreg_RestrictProfilingToAdminUsers=0' | sudo tee /etc/modprobe.d/nvidia-profiling.conf")
		fmt.Fprintln(os.Stderr, "Then either reboot, or reload the driver (stops all GPU processes):")
		fmt.Fprintln(os.Stderr, "  sudo modprobe -rf nvidia_uvm nvidia_drm nvidia_modeset nvidia && sudo modprobe nvidia")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "To disable this warning and proceed anyway, set UTLZ_DISABLE_PROFILING_WARNING=1")
		return nil
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	w, h, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		w, h = 80, 24
	}

	screen := top.New(w, h,
		top.WithRefreshInterval(refreshInterval),
		top.WithResolution(resolution),
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
		metricsChan := make(chan metrics.MetricsSnapshot)
		collector, err := metrics.NewCollector(deviceIds, metricsInterval)
		if err != nil {
			p.Send(top.ErrorMsg{Error: err})
			return
		}
		defer collector.Close()

		reporter := newMetricsReporter(collector.NVMLClient(), collector.MonitoredDeviceIDs(), func(perGPU map[int]metrics.GpuCeiling) {
			gpuCeilings := make(map[int]top.GpuCeiling)
			for idx, g := range perGPU {
				gpuCeilings[idx] = top.GpuCeiling{
					ModelName:         g.ModelName,
					ComputeSolCeiling: g.ComputeSolCeiling,
				}
			}
			p.Send(top.RooflineCeilingMsg{PerGPU: gpuCeilings})
		})

		p.Send(top.InitMsg{DeviceIDs: collector.MonitoredDeviceIDs()})

		if reporter != nil {
			go reporter.Start(ctx)
		}

		go collector.Start(ctx, metricsChan)
		for snapshot := range metricsChan {
			if reporter != nil {
				reporter.Observe(snapshot)
			}

			p.Send(top.MetricsSnapshotMsg{
				Timestamp: snapshot.Timestamp,
				GPUs:      snapshot.GPUs,
			})
		}

		if reporter != nil {
			reporter.Stop()
		}
	}()

	_, err = p.Run()
	if errors.Is(err, tea.ErrProgramKilled) {
		return nil
	}
	return err
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
	onCeiling func(perGPU map[int]metrics.GpuCeiling),
) *metrics.Reporter {
	totalGpuCount, err := nvmlClient.GetDeviceCount()
	if err != nil || totalGpuCount <= 0 {
		slog.Warn("metrics: disabled, could not query GPU count", "error", err)
		return nil
	}

	allUUIDs := make([]string, totalGpuCount)
	allNames := make([]string, totalGpuCount)
	gpuIDs := make([]string, totalGpuCount)
	for i := 0; i < totalGpuCount; i++ {
		uuid, _ := nvmlClient.GetDeviceUUID(i)
		allUUIDs[i] = uuid
		allNames[i], _ = nvmlClient.GetDeviceName(i)
		gpuIDs[i] = metrics.GenerateGpuID(uuid)
	}

	return metrics.New(metrics.ReporterConfig{
		HostID:             metrics.GenerateHostID(allUUIDs),
		GpuIDs:             gpuIDs,
		GpuNames:           allNames,
		TotalGpuCount:      totalGpuCount,
		Inference:          newInferenceScanner(nvmlClient, inferenceCacheTTL),
		MonitoredDeviceIDs: monitoredDeviceIDs,
		OnCeiling:          onCeiling,
	})
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
