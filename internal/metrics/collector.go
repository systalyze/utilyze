package metrics

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"github.com/systalyze/utilyze/internal/ffi/nvml"
	"github.com/systalyze/utilyze/internal/ffi/sampler"
)

type Collector struct {
	nv              *nvml.Client
	sampler         *sampler.Sampler
	metricsInterval time.Duration
	deviceIds       []int
}

func NewCollector(deviceIds []int, metricsInterval time.Duration) (*Collector, error) {
	// Lock this goroutine to a single OS thread during initialization only. NVML init may retain a CUDA
	// primary context that is thread-local; the sampler's BeginSession needs that context current on the
	// same thread. After init, Poll only reads a ring buffer and doesn't need any CUDA context.
	return withLockedOSThread(func() (*Collector, error) {
		nv, err := nvml.Init()
		if err != nil {
			return nil, fmt.Errorf("failed to initialize NVML: %w", err)
		}

		if len(deviceIds) == 0 {
			numDevices, err := nv.GetDeviceCount()
			if err != nil {
				return nil, fmt.Errorf("failed to get device count: %w", err)
			}
			deviceIds = make([]int, numDevices)
			for i := 0; i < numDevices; i++ {
				deviceIds[i] = i
			}
		}

		s, err := sampler.Init(deviceIds, sampler.DefaultMetrics, metricsInterval)
		if err != nil {
			return nil, err
		}
		return &Collector{
			nv:              nv,
			sampler:         s,
			metricsInterval: metricsInterval,
			deviceIds:       s.InitializedDeviceIDs(),
		}, nil
	})
}

type SOLSnapshot struct {
	ComputePct float64
	MemoryPct  float64
	Valid      bool
}

type BandwidthSnapshot struct {
	PCIeTxBps   float64
	PCIeRxBps   float64
	NVLinkTxBps float64
	NVLinkRxBps float64
	Valid       bool
}

type DCGMUtilizationSnapshot struct {
	SMActivePct float64
	Valid       bool
}

type NVMLUtilizationSnapshot struct {
	UtilPct float64
	Valid   bool
}

type GPUSnapshot struct {
	DeviceID        int
	SOL             SOLSnapshot
	Bandwidth       BandwidthSnapshot
	DCGMUtilization DCGMUtilizationSnapshot
	NVMLUtilization NVMLUtilizationSnapshot
}

type MetricsSnapshot struct {
	Timestamp time.Time
	GPUs      []GPUSnapshot
}

func (c *Collector) Start(ctx context.Context, metrics chan MetricsSnapshot) {
	defer close(metrics)
	t := time.NewTicker(c.metricsInterval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			pollTime := time.Now()
			gpus := make([]GPUSnapshot, 0, len(c.deviceIds))
			for _, deviceID := range c.deviceIds {
				gpu := GPUSnapshot{DeviceID: deviceID}
				hasData := false

				snapshot, err := c.sampler.Poll(deviceID)
				if err == nil {
					if snapshot.ComputeSOLPct != nil && snapshot.MemorySOLPct != nil {
						gpu.SOL.ComputePct = *snapshot.ComputeSOLPct
						gpu.SOL.MemoryPct = *snapshot.MemorySOLPct
						gpu.SOL.Valid = true
						hasData = true
					}
					if snapshot.SMActivePct != nil {
						gpu.DCGMUtilization.SMActivePct = *snapshot.SMActivePct
						gpu.DCGMUtilization.Valid = true
						hasData = true
					}
				}

				utilizationSnapshot, err := c.nv.PollUtilization(deviceID, pollTime)
				if err == nil && utilizationSnapshot.GPUUtilPct != nil {
					gpu.NVMLUtilization.UtilPct = *utilizationSnapshot.GPUUtilPct
					gpu.NVMLUtilization.Valid = true
					hasData = true
				}

				bandwidthSnapshot, err := c.nv.PollBandwidth(deviceID, pollTime)
				if err == nil &&
					bandwidthSnapshot.PCIeTxBps != nil &&
					bandwidthSnapshot.PCIeRxBps != nil &&
					bandwidthSnapshot.NVLinkTxBps != nil &&
					bandwidthSnapshot.NVLinkRxBps != nil {
					gpu.Bandwidth.PCIeTxBps = *bandwidthSnapshot.PCIeTxBps
					gpu.Bandwidth.PCIeRxBps = *bandwidthSnapshot.PCIeRxBps
					gpu.Bandwidth.NVLinkTxBps = *bandwidthSnapshot.NVLinkTxBps
					gpu.Bandwidth.NVLinkRxBps = *bandwidthSnapshot.NVLinkRxBps
					gpu.Bandwidth.Valid = true
					hasData = true
				}

				if hasData {
					gpus = append(gpus, gpu)
				}
			}

			if len(gpus) > 0 {
				metrics <- MetricsSnapshot{
					Timestamp: pollTime,
					GPUs:      gpus,
				}
			}
		}
	}
}

func (c *Collector) MonitoredDeviceIDs() []int {
	return c.deviceIds
}

func (c *Collector) NVMLClient() *nvml.Client {
	return c.nv
}

func (c *Collector) Close() error {
	return c.sampler.Close()
}

func withLockedOSThread[T any](fn func() (T, error)) (T, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	return fn()
}
