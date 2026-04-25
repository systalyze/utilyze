package metrics

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"math"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/systalyze/utilyze/internal/inference"
)

const (
	DefaultBackendURL = "https://api.systalyze.com/v1/utilyze"
	backendURLEnvVar  = "UTLZ_BACKEND_URL"
	disableEnvVar     = "UTLZ_DISABLE_METRICS"
)

type GpuCeiling struct {
	Index             int
	ModelName         *string
	ComputeSolCeiling *float64
}

type CeilingCallback func(perGPU map[int]GpuCeiling)

type ReporterConfig struct {
	BackendURL         string
	HostID             string
	GpuIDs             []string // indexed by physical device ID
	GpuNames           []string // indexed by physical device ID
	TotalGpuCount      int
	OnCeiling          CeilingCallback
	Inference          inference.Scanner
	MonitoredDeviceIDs []int
}

type Reporter struct {
	config     ReporterConfig
	scanner    inference.Scanner
	mu         sync.Mutex
	windowBuf  []MetricsSnapshot
	inflight   bool
	cancelFunc context.CancelFunc
}

func New(config ReporterConfig) *Reporter {
	if os.Getenv(disableEnvVar) == "1" {
		return nil
	}

	backendURL := config.BackendURL
	if backendURL == "" {
		backendURL = os.Getenv(backendURLEnvVar)
	}
	if backendURL == "" {
		backendURL = DefaultBackendURL
	}
	config.BackendURL = backendURL

	return &Reporter{config: config, scanner: config.Inference}
}

func (r *Reporter) Observe(snapshot MetricsSnapshot) {
	r.mu.Lock()
	r.windowBuf = append(r.windowBuf, snapshot)
	r.mu.Unlock()
}

func (r *Reporter) Start(ctx context.Context) {
	ctx, r.cancelFunc = context.WithCancel(ctx)

	jitterMs := hashToInt(r.config.HostID) % 5000
	select {
	case <-ctx.Done():
		return
	case <-time.After(time.Duration(jitterMs) * time.Millisecond):
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.tick(ctx)
		}
	}
}

func (r *Reporter) Stop() {
	if r.cancelFunc != nil {
		r.cancelFunc()
	}
}

func (r *Reporter) tick(ctx context.Context) {
	r.mu.Lock()
	skip := r.inflight
	window := r.windowBuf
	r.windowBuf = nil
	if !skip {
		r.inflight = true
	}
	r.mu.Unlock()

	if skip {
		return
	}

	defer func() {
		r.mu.Lock()
		r.inflight = false
		r.mu.Unlock()
	}()

	if len(window) == 0 {
		return
	}

	discoveryGPUs := r.config.MonitoredDeviceIDs
	if len(discoveryGPUs) == 0 {
		seen := make(map[int]bool)
		for _, snap := range window {
			for _, gpu := range snap.GPUs {
				if !seen[gpu.DeviceID] {
					seen[gpu.DeviceID] = true
					discoveryGPUs = append(discoveryGPUs, gpu.DeviceID)
				}
			}
		}
	}

	var attributions map[int]inference.Attribution
	if r.scanner != nil {
		var err error
		attributions, err = r.scanner.Scan(ctx, discoveryGPUs)
		if err != nil {
			slog.Debug("metrics: scan error", "err", err)
			return
		}
	}

	type agg struct {
		computeSum, memorySum                          float64
		solCount                                       int
		pcieTxSum, pcieRxSum, nvlinkTxSum, nvlinkRxSum float64
		bwCount                                        int
	}
	byID := make(map[int]*agg)
	for _, snap := range window {
		for _, gpu := range snap.GPUs {
			a := byID[gpu.DeviceID]
			if a == nil {
				a = &agg{}
				byID[gpu.DeviceID] = a
			}
			if gpu.SOL.Valid {
				a.computeSum += gpu.SOL.ComputePct
				a.memorySum += gpu.SOL.MemoryPct
				a.solCount++
			}
			if gpu.Bandwidth.Valid {
				a.pcieTxSum += gpu.Bandwidth.PCIeTxBps
				a.pcieRxSum += gpu.Bandwidth.PCIeRxBps
				a.nvlinkTxSum += gpu.Bandwidth.NVLinkTxBps
				a.nvlinkRxSum += gpu.Bandwidth.NVLinkRxBps
				a.bwCount++
			}
		}
	}

	gpus := make([]MetricsGpu, 0, len(discoveryGPUs))
	for _, deviceID := range discoveryGPUs {
		gpuID := ""
		gpuName := ""
		if deviceID >= 0 && deviceID < len(r.config.GpuIDs) {
			gpuID = r.config.GpuIDs[deviceID]
		}
		if deviceID >= 0 && deviceID < len(r.config.GpuNames) {
			gpuName = r.config.GpuNames[deviceID]
		}

		var computePct, memoryPct, pcieGBs, nvlinkGBs float64
		if a := byID[deviceID]; a != nil {
			if a.solCount > 0 {
				computePct = a.computeSum / float64(a.solCount)
				memoryPct = a.memorySum / float64(a.solCount)
			}
			if a.bwCount > 0 {
				pcieGBs = (a.pcieTxSum + a.pcieRxSum) / float64(a.bwCount) / 1e9
				nvlinkGBs = (a.nvlinkTxSum + a.nvlinkRxSum) / float64(a.bwCount) / 1e9
			}
		}

		var modelName *string
		if att, ok := attributions[deviceID]; ok && att.ModelID != "" {
			m := att.ModelID
			modelName = &m
		}

		gpus = append(gpus, MetricsGpu{
			Index:      deviceID,
			GpuID:      gpuID,
			GpuModel:   gpuName,
			ModelName:  modelName,
			ComputePct: math.Round(computePct*100) / 100,
			MemoryPct:  math.Round(memoryPct*100) / 100,
			PcieGBs:    math.Round(pcieGBs*10000) / 10000,
			NvlinkGBs:  math.Round(nvlinkGBs*10000) / 10000,
		})
	}

	if len(gpus) == 0 {
		return
	}

	payload := MetricsPayload{
		SchemaVersion: 1,
		HostID:        r.config.HostID,
		SampledAtMs:   window[len(window)-1].Timestamp.UnixMilli(),
		Mode:          "native",
		GpuCount:      r.config.TotalGpuCount,
		GPUs:          gpus,
	}

	r.postMetrics(ctx, &payload)
}

func (r *Reporter) postMetrics(ctx context.Context, payload *MetricsPayload) {
	postCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	body, err := json.Marshal(payload)
	if err != nil {
		slog.Debug("metrics: post metrics marshal error", "err", err)
		return
	}

	slog.Debug("metrics: post metrics request", "url", r.config.BackendURL+"/metrics", "body", string(body))
	start := time.Now()
	request, err := http.NewRequestWithContext(postCtx, http.MethodPost, r.config.BackendURL+"/metrics", bytes.NewReader(body))
	if err != nil {
		slog.Debug("metrics: post metrics request error", "err", err)
		return
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := http.DefaultClient.Do(request)
	slog.Debug("metrics: post metrics responded", "duration", time.Since(start))
	if err != nil {
		slog.Debug("metrics: post metrics response error", "err", err)
		return
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		slog.Debug("metrics: post metrics response not ok", "status", response.StatusCode, "body", string(body))
		return
	}

	var metricsResponse MetricsResponse
	if err := json.NewDecoder(response.Body).Decode(&metricsResponse); err != nil {
		slog.Debug("metrics: post metrics response decode error", "err", err)
		return
	}
	slog.Debug("metrics: post metrics response ceilings", "response", metricsResponse)

	if r.config.OnCeiling != nil {
		perGPU := make(map[int]GpuCeiling)
		for _, g := range metricsResponse.GpuCeilings {
			perGPU[g.Index] = GpuCeiling{
				Index:             g.Index,
				ModelName:         g.ModelName,
				ComputeSolCeiling: g.ComputeSolCeiling,
			}
		}
		r.config.OnCeiling(perGPU)
	}
}

func hashToInt(s string) int {
	h := 0
	for _, c := range s {
		h = ((h << 5) - h + int(c))
	}
	if h < 0 {
		h = -h
	}
	return h
}
