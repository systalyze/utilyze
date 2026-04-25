package top

import (
	"time"

	"github.com/systalyze/utilyze/internal/metrics"
)

type InitMsg struct {
	DeviceIDs []int
}

type MetricsSnapshotMsg struct {
	Timestamp time.Time
	GPUs      []metrics.GPUSnapshot
}

type ErrorMsg struct {
	Error error
}

type GpuCeiling struct {
	ModelName         *string
	ComputeSolCeiling *float64
}

type RooflineCeilingMsg struct {
	PerGPU map[int]GpuCeiling
}
