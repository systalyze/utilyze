package top

import (
	"github.com/systalyze/utilyze/internal/theme"

	"charm.land/lipgloss/v2"
)

const (
	computeSOLSeries = "compute"
	memorySOLSeries  = "memory"
	grActiveSeries   = "gr_active"
	smActivitySeries = "sm_activity"
)

type metricsMode int

const (
	MetricsModeSOL        metricsMode = iota
	MetricsModeGRActive   metricsMode = iota
	MetricsModeSMActivity metricsMode = iota
)

type metricsSeriesDef struct {
	name        string
	legendLabel string
	style       func(theme.Styles) lipgloss.Style
	value       func(model, int) percentValue
}

type metricsModeDef struct {
	mode    metricsMode
	label   string
	series  []metricsSeriesDef
	hotkeys []hotkeyItem
}

var metricsModes = []metricsModeDef{
	{
		mode:  MetricsModeSOL,
		label: "Utilyze SOL",
		series: []metricsSeriesDef{
			{
				name:        computeSOLSeries,
				legendLabel: "Compute SOL%",
				style:       func(styles theme.Styles) lipgloss.Style { return styles.Compute },
				value: func(m model, deviceIdx int) percentValue {
					return valueAt(m.computeLastValues, deviceIdx)
				},
			},
			{
				name:        memorySOLSeries,
				legendLabel: "Memory SOL%",
				style:       func(styles theme.Styles) lipgloss.Style { return styles.Memory },
				value: func(m model, deviceIdx int) percentValue {
					return valueAt(m.memoryLastValues, deviceIdx)
				},
			},
		},
		hotkeys: []hotkeyItem{
			{key: keyCompute, label: "comp", series: computeSOLSeries},
			{key: keyMemory, label: "mem", series: memorySOLSeries},
		},
	},
	{
		mode:  MetricsModeGRActive,
		label: "GPU Busy (nvtop)",
		series: []metricsSeriesDef{
			{
				name:        grActiveSeries,
				legendLabel: "GPU%",
				style:       func(styles theme.Styles) lipgloss.Style { return styles.PCIe },
				value: func(m model, deviceIdx int) percentValue {
					return valueAt(m.grActiveLastValues, deviceIdx)
				},
			},
		},
		hotkeys: []hotkeyItem{
			{key: keyGrActive, label: "nvtop", series: grActiveSeries},
		},
	},
	{
		mode:  MetricsModeSMActivity,
		label: "SM Activity (DCGM)",
		series: []metricsSeriesDef{
			{
				name:        smActivitySeries,
				legendLabel: "SM%",
				style:       func(styles theme.Styles) lipgloss.Style { return styles.SMActivity },
				value: func(m model, deviceIdx int) percentValue {
					return valueAt(m.smActivityLastValues, deviceIdx)
				},
			},
		},
		hotkeys: []hotkeyItem{
			{key: keySmActivity, label: "dcgm", series: smActivitySeries},
		},
	},
}

func (m metricsMode) def() metricsModeDef {
	for _, def := range metricsModes {
		if def.mode == m {
			return def
		}
	}
	return metricsModes[0]
}

func (m metricsMode) next() metricsMode {
	for i, def := range metricsModes {
		if def.mode == m {
			return metricsModes[(i+1)%len(metricsModes)].mode
		}
	}
	return metricsModes[0].mode
}

func (d metricsModeDef) seriesNames(m model) []string {
	names := make([]string, 0, len(d.series))
	for _, series := range d.series {
		if m.seriesEnabled(series.name) {
			names = append(names, series.name)
		}
	}
	return names
}

func (d metricsModeDef) handleHotkey(m *model, key string) bool {
	for _, hotkey := range d.hotkeys {
		if hotkey.key != key {
			continue
		}
		m.toggleSeries(hotkey.series)
		return true
	}
	return false
}

func allMetricsSeries() []metricsSeriesDef {
	var series []metricsSeriesDef
	for _, mode := range metricsModes {
		series = append(series, mode.series...)
	}
	return series
}

func allMetricsSeriesNames() []string {
	series := allMetricsSeries()
	names := make([]string, 0, len(series))
	for _, s := range series {
		names = append(names, s.name)
	}
	return names
}

func (s metricsSeriesDef) styleFor(styles theme.Styles) lipgloss.Style {
	if s.style == nil {
		return styles.Header
	}
	return s.style(styles)
}

func (s metricsSeriesDef) valueFor(m model, deviceIdx int) percentValue {
	if s.value == nil {
		return percentValue{}
	}
	return s.value(m, deviceIdx)
}

func valueAt(values []percentValue, index int) percentValue {
	if index < 0 || index >= len(values) {
		return percentValue{}
	}
	return values[index]
}
