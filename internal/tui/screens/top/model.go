package top

import (
	"fmt"
	"time"

	"github.com/systalyze/utilyze/internal/format"
	"github.com/systalyze/utilyze/internal/theme"
	"github.com/systalyze/utilyze/internal/tui/components/spinner"
	"github.com/systalyze/utilyze/internal/tui/components/tschart"

	tea "charm.land/bubbletea/v2"
)

type percentValue struct {
	Value float64
	Valid bool
}

func (v percentValue) String() string {
	if !v.Valid {
		return "--.-%"
	}
	return fmt.Sprintf("%4.1f%%", v.Value)
}

func newPercentValue(value float64) percentValue {
	return percentValue{Value: value, Valid: true}
}

type model struct {
	solChartTimeRange       time.Duration
	bandwidthChartTimeRange time.Duration
	drawInterval            time.Duration
	resolution              time.Duration

	initialized bool

	width  int
	height int

	spinner spinner.Model

	err error

	enabledSeries []string

	deviceIDs            []int
	deviceIndexMap       map[int]int // physical device ID → chart index
	solCharts            []*tschart.Model
	online               []bool
	memoryLastValues     []percentValue
	computeLastValues    []percentValue
	grActiveLastValues   []percentValue
	smActivityLastValues []percentValue

	bandwidthChart  *tschart.Model
	nvlinkLastValue float64
	pcieLastValue   float64

	paused        bool
	pausedAt      time.Time
	quitting      bool
	showBandwidth bool

	gpuCeilings map[int]GpuCeiling

	dark        bool
	metricsMode metricsMode
	detailMode  bool
	styles      theme.Styles
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, tea.RequestBackgroundColor)
}

type drawMsg struct{}

func (m model) tick() tea.Cmd {
	return tea.Tick(m.drawInterval, func(t time.Time) tea.Msg {
		return drawMsg{}
	})
}

func (m model) ready() bool {
	return m.initialized && len(m.solCharts) > 0 && m.bandwidthChart != nil
}

func (m *model) initCharts(deviceIDs []int) {
	numDevices := len(deviceIDs)
	m.deviceIDs = deviceIDs
	m.deviceIndexMap = make(map[int]int, numDevices)
	for i, id := range deviceIDs {
		m.deviceIndexMap[id] = i
	}
	m.solCharts = make([]*tschart.Model, numDevices)
	now := time.Now()
	for i := 0; i < numDevices; i++ {
		opts := []tschart.Option{
			tschart.WithResolution(m.resolution),
			tschart.WithXTicks(6),
			tschart.WithXTickFormatter(m.formatTimeSince),
			tschart.WithYRange(0, 100),
			tschart.WithYTicks(5),
			tschart.WithYTickFormatter(func(chart *tschart.Model, v float64, index int, n int) string {
				return fmt.Sprintf("%2d", index*100/(n-1))
			}),
			tschart.WithStyles(m.styles.ChartBorder, m.styles.ChartAxis, m.styles.ChartPanel),
			tschart.WithDetailMode(m.detailMode),
		}
		m.solCharts[i] = tschart.New(m.width, m.height, opts...)
		m.applyUtilizationSeriesStyles(m.solCharts[i])
	}

	bwOpts := []tschart.Option{
		tschart.WithAutoScale(),
		tschart.WithResolution(m.resolution),
		tschart.WithXTicks(3),
		tschart.WithXTickFormatter(m.formatTimeSince),
		tschart.WithYRange(0, 0.5e9),
		tschart.WithYTicks(3),
		tschart.WithYTickFormatter(func(chart *tschart.Model, v float64, index int, n int) string {
			return format.SI(v, bytesSIWidth)
		}),
		tschart.WithStyles(m.styles.ChartBorder, m.styles.ChartAxis, m.styles.ChartPanel),
		tschart.WithDetailMode(m.detailMode),
	}
	m.bandwidthChart = tschart.New(m.width, m.height, bwOpts...)
	m.bandwidthChart.EnableAxes(true, true)
	m.bandwidthChart.SetSeriesStyle(nvlinkSeries, m.styles.NVLink.Inherit(m.styles.ChartPanel))
	m.bandwidthChart.SetSeriesStyle(pcieSeries, m.styles.PCIe.Inherit(m.styles.ChartPanel))
	m.bandwidthChart.Push(nvlinkSeries, now, 0)
	m.bandwidthChart.Push(pcieSeries, now, 0)

	m.computeLastValues = make([]percentValue, numDevices)
	m.memoryLastValues = make([]percentValue, numDevices)
	m.grActiveLastValues = make([]percentValue, numDevices)
	m.smActivityLastValues = make([]percentValue, numDevices)
	m.online = make([]bool, numDevices)
}

func (m model) draw() {
	now := time.Now()
	if m.paused && !m.pausedAt.IsZero() {
		now = m.pausedAt
	}
	mode := m.metricsMode.def()
	for _, chart := range m.solCharts {
		chart.Draw(mode.seriesNames(m), now)
		chart.Invalidate()
	}
	m.bandwidthChart.Draw(m.enabledSeries, now)
	m.bandwidthChart.Invalidate()
}

func (m *model) applyCeilingThresholds() {
	for i, chart := range m.solCharts {
		if chart == nil || i >= len(m.deviceIDs) {
			continue
		}
		var thresholds []tschart.ThresholdLine
		if m.metricsMode == MetricsModeSOL {
			if g, ok := m.gpuCeilings[m.deviceIDs[i]]; ok && g.ComputeSolCeiling != nil {
				thresholds = append(thresholds, tschart.ThresholdLine{
					Value: *g.ComputeSolCeiling,
					Style: m.styles.ComputeCeiling.Inherit(m.styles.ChartPanel),
				})
			}
		}
		chart.SetThresholds(thresholds)
	}
}

func (m *model) resetCharts() {
	for _, chart := range m.solCharts {
		chart.Reset()
	}
	m.bandwidthChart.Reset()
	m.applyTheme()
}

func (m model) applyUtilizationSeriesStyles(chart *tschart.Model) {
	if chart == nil {
		return
	}
	for _, series := range allMetricsSeries() {
		chart.SetSeriesStyle(series.name, series.styleFor(m.styles).Inherit(m.styles.ChartPanel))
	}
}

func (m *model) applyTheme() {
	m.styles = theme.NewStyles(m.dark)
	m.spinner = spinner.New(&m.styles.Spinner)

	for _, chart := range m.solCharts {
		if chart == nil {
			continue
		}
		chart.SetDetailMode(m.detailMode)
		chart.SetStyles(m.styles.ChartBorder, m.styles.ChartAxis, m.styles.ChartPanel)
		m.applyUtilizationSeriesStyles(chart)
	}

	if m.bandwidthChart != nil {
		m.bandwidthChart.SetDetailMode(m.detailMode)
		m.bandwidthChart.SetStyles(m.styles.ChartBorder, m.styles.ChartAxis, m.styles.ChartPanel)
		m.bandwidthChart.SetSeriesStyle(nvlinkSeries, m.styles.NVLink.Inherit(m.styles.ChartPanel))
		m.bandwidthChart.SetSeriesStyle(pcieSeries, m.styles.PCIe.Inherit(m.styles.ChartPanel))
		m.bandwidthChart.Invalidate()
	}

	m.applyCeilingThresholds()
}

func (m *model) applyDetailMode() {
	for _, chart := range m.solCharts {
		if chart == nil {
			continue
		}
		chart.SetDetailMode(m.detailMode)
	}
	if m.bandwidthChart != nil {
		m.bandwidthChart.SetDetailMode(m.detailMode)
	}
}

func New(w int, h int, opts ...Option) model {
	m := model{
		width:         max(w, 1),
		height:        max(h, 1),
		drawInterval:  100 * time.Millisecond,
		resolution:    200 * time.Millisecond,
		showBandwidth: true,
		dark:          true,

		enabledSeries: append(allMetricsSeriesNames(), nvlinkSeries, pcieSeries),
	}
	for _, opt := range opts {
		opt(&m)
	}
	m.styles = theme.NewStyles(m.dark)
	m.spinner = spinner.New(&m.styles.Spinner)
	return m
}
