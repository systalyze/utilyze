// Package export provides a headless CSV/JSON writer for streaming GPU
// efficiency metrics to a file or stdout. It is used by `utlz --export` to
// emit one row per monitored GPU at a configurable interval, similar to
// `dcgmi dmon` or `nvidia-smi --query-gpu=... --format=csv`.
package export

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"sync"
	"time"
)

// Format is the wire format for exported rows.
type Format string

const (
	FormatCSV  Format = "csv"
	FormatJSON Format = "json"
)

// ParseFormat normalizes and validates a format string.
func ParseFormat(s string) (Format, error) {
	switch Format(s) {
	case FormatCSV:
		return FormatCSV, nil
	case FormatJSON:
		return FormatJSON, nil
	default:
		return "", fmt.Errorf("unsupported export format %q (expected %q or %q)", s, FormatCSV, FormatJSON)
	}
}

// Row is a single exported metrics record for one GPU at one point in time.
//
// Optional numeric fields use *float64 so that missing values can be
// distinguished from zero: they render as empty CSV cells and as JSON null.
type Row struct {
	Timestamp               time.Time
	DeviceID                int
	GpuName                 string
	ComputeSOLPct           *float64
	MemorySOLPct            *float64
	AttainableComputeSOLPct *float64
	SMActivePct             *float64
	PCIeTxGBps              *float64
	PCIeRxGBps              *float64
	NVLinkTxGBps            *float64
	NVLinkRxGBps            *float64
	ModelName               string
}

// Header returns the CSV header columns in the order written by Writer.
func Header() []string {
	return []string{
		"timestamp",
		"device_id",
		"gpu_name",
		"compute_sol_pct",
		"memory_sol_pct",
		"attainable_compute_sol_pct",
		"sm_active_pct",
		"pcie_tx_gbps",
		"pcie_rx_gbps",
		"nvlink_tx_gbps",
		"nvlink_rx_gbps",
		"model_name",
	}
}

// FormatTimestamp renders t as an ISO 8601 UTC timestamp with millisecond
// precision and a trailing Z, e.g. "2026-04-30T10:00:00.000Z".
func FormatTimestamp(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}

// Writer streams Row values to an underlying io.Writer in either CSV or JSON
// (one JSON object per line). Writer is safe for concurrent use.
type Writer struct {
	format        Format
	mu            sync.Mutex
	csvw          *csv.Writer
	jsonenc       *json.Encoder
	headerWritten bool
}

// NewWriter constructs a Writer that emits records in the requested format.
func NewWriter(w io.Writer, format Format) (*Writer, error) {
	switch format {
	case FormatCSV:
		return &Writer{format: format, csvw: csv.NewWriter(w)}, nil
	case FormatJSON:
		return &Writer{format: format, jsonenc: json.NewEncoder(w)}, nil
	default:
		return nil, fmt.Errorf("unsupported export format %q", format)
	}
}

// WriteRows emits the given rows. For CSV, the header is written once on the
// first call. For JSON, each row becomes one newline-delimited JSON object.
func (w *Writer) WriteRows(rows []Row) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	switch w.format {
	case FormatCSV:
		if !w.headerWritten {
			if err := w.csvw.Write(Header()); err != nil {
				return err
			}
			w.headerWritten = true
		}
		for _, row := range rows {
			if err := w.csvw.Write(rowToCSV(row)); err != nil {
				return err
			}
		}
		w.csvw.Flush()
		return w.csvw.Error()
	case FormatJSON:
		for _, row := range rows {
			if err := w.jsonenc.Encode(rowToJSON(row)); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported export format %q", w.format)
	}
}

// SkipCSVHeader marks the CSV header as already written, so it will not be
// emitted on the first WriteRows call. This is useful when appending to an
// existing CSV file. It is a no-op for non-CSV formats.
func (w *Writer) SkipCSVHeader() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.format == FormatCSV {
		w.headerWritten = true
	}
}

// Flush flushes any buffered output to the underlying writer.
func (w *Writer) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.csvw != nil {
		w.csvw.Flush()
		return w.csvw.Error()
	}
	return nil
}

func rowToCSV(r Row) []string {
	return []string{
		FormatTimestamp(r.Timestamp),
		strconv.Itoa(r.DeviceID),
		r.GpuName,
		formatFloat(r.ComputeSOLPct, 2),
		formatFloat(r.MemorySOLPct, 2),
		formatFloat(r.AttainableComputeSOLPct, 2),
		formatFloat(r.SMActivePct, 2),
		formatFloat(r.PCIeTxGBps, 4),
		formatFloat(r.PCIeRxGBps, 4),
		formatFloat(r.NVLinkTxGBps, 4),
		formatFloat(r.NVLinkRxGBps, 4),
		r.ModelName,
	}
}

// jsonRow mirrors Row but uses tagged fields and pointer types so that
// missing values render as JSON null and the field order matches Header().
type jsonRow struct {
	Timestamp               string   `json:"timestamp"`
	DeviceID                int      `json:"device_id"`
	GpuName                 string   `json:"gpu_name"`
	ComputeSOLPct           *float64 `json:"compute_sol_pct"`
	MemorySOLPct            *float64 `json:"memory_sol_pct"`
	AttainableComputeSOLPct *float64 `json:"attainable_compute_sol_pct"`
	SMActivePct             *float64 `json:"sm_active_pct"`
	PCIeTxGBps              *float64 `json:"pcie_tx_gbps"`
	PCIeRxGBps              *float64 `json:"pcie_rx_gbps"`
	NVLinkTxGBps            *float64 `json:"nvlink_tx_gbps"`
	NVLinkRxGBps            *float64 `json:"nvlink_rx_gbps"`
	ModelName               string   `json:"model_name,omitempty"`
}

func rowToJSON(r Row) jsonRow {
	return jsonRow{
		Timestamp:               FormatTimestamp(r.Timestamp),
		DeviceID:                r.DeviceID,
		GpuName:                 r.GpuName,
		ComputeSOLPct:           roundPtr(r.ComputeSOLPct, 2),
		MemorySOLPct:            roundPtr(r.MemorySOLPct, 2),
		AttainableComputeSOLPct: roundPtr(r.AttainableComputeSOLPct, 2),
		SMActivePct:             roundPtr(r.SMActivePct, 2),
		PCIeTxGBps:              roundPtr(r.PCIeTxGBps, 4),
		PCIeRxGBps:              roundPtr(r.PCIeRxGBps, 4),
		NVLinkTxGBps:            roundPtr(r.NVLinkTxGBps, 4),
		NVLinkRxGBps:            roundPtr(r.NVLinkRxGBps, 4),
		ModelName:               r.ModelName,
	}
}

func formatFloat(v *float64, prec int) string {
	if v == nil {
		return ""
	}
	return strconv.FormatFloat(roundTo(*v, prec), 'f', -1, 64)
}

func roundPtr(v *float64, prec int) *float64 {
	if v == nil {
		return nil
	}
	r := roundTo(*v, prec)
	return &r
}

func roundTo(v float64, prec int) float64 {
	scale := 1.0
	for i := 0; i < prec; i++ {
		scale *= 10
	}
	if v >= 0 {
		return float64(int64(v*scale+0.5)) / scale
	}
	return float64(int64(v*scale-0.5)) / scale
}
