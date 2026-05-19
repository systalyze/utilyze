package export

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func ptrF(v float64) *float64 { return &v }

func sampleTime() time.Time {
	return time.Date(2026, 4, 30, 10, 0, 0, 0, time.UTC)
}

func sampleRow() Row {
	return Row{
		Timestamp:               sampleTime(),
		DeviceID:                0,
		GpuName:                 "H100-80G",
		ComputeSOLPct:           ptrF(45.231),
		MemorySOLPct:            ptrF(32.1),
		AttainableComputeSOLPct: ptrF(78.0),
		SMActivePct:             ptrF(62.3),
		PCIeTxGBps:              ptrF(1.20001),
		PCIeRxGBps:              ptrF(0.8),
		NVLinkTxGBps:            ptrF(12.5),
		NVLinkRxGBps:            ptrF(11.3),
		ModelName:               "meta-llama/Llama-3-70B",
	}
}

func TestParseFormat(t *testing.T) {
	for _, in := range []string{"csv", "json"} {
		if _, err := ParseFormat(in); err != nil {
			t.Errorf("ParseFormat(%q): unexpected error: %v", in, err)
		}
	}
	if _, err := ParseFormat("xml"); err == nil {
		t.Errorf("ParseFormat(\"xml\"): expected error")
	}
}

func TestFormatTimestamp(t *testing.T) {
	got := FormatTimestamp(time.Date(2026, 4, 30, 10, 0, 0, int(123*time.Millisecond), time.UTC))
	want := "2026-04-30T10:00:00.123Z"
	if got != want {
		t.Errorf("FormatTimestamp = %q, want %q", got, want)
	}
}

func TestWriterCSV(t *testing.T) {
	var buf bytes.Buffer
	w, err := NewWriter(&buf, FormatCSV)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if err := w.WriteRows([]Row{sampleRow()}); err != nil {
		t.Fatalf("WriteRows: %v", err)
	}

	out := buf.String()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines (header + 1 row), got %d: %q", len(lines), out)
	}

	wantHeader := "timestamp,device_id,gpu_name,compute_sol_pct,memory_sol_pct,attainable_compute_sol_pct,sm_active_pct,pcie_tx_gbps,pcie_rx_gbps,nvlink_tx_gbps,nvlink_rx_gbps,model_name"
	if lines[0] != wantHeader {
		t.Errorf("header = %q, want %q", lines[0], wantHeader)
	}

	wantRow := "2026-04-30T10:00:00.000Z,0,H100-80G,45.23,32.1,78,62.3,1.2,0.8,12.5,11.3,meta-llama/Llama-3-70B"
	if lines[1] != wantRow {
		t.Errorf("row = %q, want %q", lines[1], wantRow)
	}

	// Header is not repeated on subsequent writes.
	buf.Reset()
	if err := w.WriteRows([]Row{sampleRow()}); err != nil {
		t.Fatalf("WriteRows (2nd): %v", err)
	}
	if strings.Contains(buf.String(), "timestamp,device_id") {
		t.Errorf("header repeated on second WriteRows: %q", buf.String())
	}
}

func TestWriterCSVMissingValues(t *testing.T) {
	var buf bytes.Buffer
	w, _ := NewWriter(&buf, FormatCSV)
	row := Row{Timestamp: sampleTime(), DeviceID: 1, GpuName: "A100-80G"}
	if err := w.WriteRows([]Row{row}); err != nil {
		t.Fatalf("WriteRows: %v", err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	want := "2026-04-30T10:00:00.000Z,1,A100-80G,,,,,,,,,"
	if lines[1] != want {
		t.Errorf("row = %q, want %q", lines[1], want)
	}
}

func TestWriterJSON(t *testing.T) {
	var buf bytes.Buffer
	w, err := NewWriter(&buf, FormatJSON)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if err := w.WriteRows([]Row{sampleRow()}); err != nil {
		t.Fatalf("WriteRows: %v", err)
	}

	out := strings.TrimRight(buf.String(), "\n")
	lines := strings.Split(out, "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 JSON line, got %d: %q", len(lines), out)
	}

	var got map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &got); err != nil {
		t.Fatalf("invalid JSON %q: %v", lines[0], err)
	}

	if got["timestamp"] != "2026-04-30T10:00:00.000Z" {
		t.Errorf("timestamp = %v", got["timestamp"])
	}
	if got["device_id"].(float64) != 0 {
		t.Errorf("device_id = %v", got["device_id"])
	}
	if got["gpu_name"] != "H100-80G" {
		t.Errorf("gpu_name = %v", got["gpu_name"])
	}
	if got["compute_sol_pct"].(float64) != 45.23 {
		t.Errorf("compute_sol_pct = %v, want 45.23 (rounded)", got["compute_sol_pct"])
	}
	if got["model_name"] != "meta-llama/Llama-3-70B" {
		t.Errorf("model_name = %v", got["model_name"])
	}
}

func TestWriterJSONMissingValuesAreNull(t *testing.T) {
	var buf bytes.Buffer
	w, _ := NewWriter(&buf, FormatJSON)
	row := Row{Timestamp: sampleTime(), DeviceID: 2, GpuName: "A100-80G"}
	if err := w.WriteRows([]Row{row}); err != nil {
		t.Fatalf("WriteRows: %v", err)
	}

	line := strings.TrimRight(buf.String(), "\n")
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	for _, key := range []string{
		"compute_sol_pct", "memory_sol_pct", "attainable_compute_sol_pct",
		"sm_active_pct", "pcie_tx_gbps", "pcie_rx_gbps",
		"nvlink_tx_gbps", "nvlink_rx_gbps",
	} {
		if string(raw[key]) != "null" {
			t.Errorf("%s = %s, want null", key, string(raw[key]))
		}
	}
	if _, present := raw["model_name"]; present {
		t.Errorf("model_name should be omitted when empty")
	}
}

func TestWriterSkipCSVHeader(t *testing.T) {
	var buf bytes.Buffer
	w, _ := NewWriter(&buf, FormatCSV)
	w.SkipCSVHeader()
	if err := w.WriteRows([]Row{sampleRow()}); err != nil {
		t.Fatalf("WriteRows: %v", err)
	}
	if strings.Contains(buf.String(), "timestamp,device_id") {
		t.Errorf("header should be skipped, got: %q", buf.String())
	}
}

func TestWriterUnknownFormat(t *testing.T) {
	if _, err := NewWriter(&bytes.Buffer{}, Format("yaml")); err == nil {
		t.Errorf("expected error for unknown format")
	}
}
