package tschart

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
)

const maxStoredValues = 8192

var (
	defaultBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("8"))
	defaultAxisStyle = lipgloss.NewStyle().Bold(true)

	axisAnsiKey  = "_axis"
	panelAnsiKey = "_panel"
)

type sample struct {
	t     time.Time
	value float64
}

type series struct {
	style   lipgloss.Style
	samples []sample
}

type Model struct {
	XTicks         int
	XTickFormatter TickFormatter

	YRange         [2]float64
	YTicks         int
	YTickFormatter TickFormatter
	AutoScale      bool

	BorderStyle lipgloss.Style
	AxisStyle   lipgloss.Style
	PanelStyle  lipgloss.Style

	DetailMode bool

	width, height    int
	canvasW, canvasH int

	xaxis  bool
	xticks []float64

	yaxis        bool
	yticks       []float64
	renderYRange [2]float64

	Resolution time.Duration
	series     map[string]*series
	styleANSI  map[string][2]string // lookup table for ANSI codes of series styles to prevent lipgloss.Render calls

	grids              map[string]*brailleGrid
	lineGrids          map[string]*lineGrid
	thresholds         []ThresholdLine
	thresholdGrids     []*brailleGrid
	thresholdLineGrids []*lineGrid

	chartCache        string
	yaxisCache        string
	xaxisCache        string
	seriesRenderCache []string // cached series in order to prevent stale orderings
	viewCache         string
	viewDirty         bool
}

func (m *Model) View() string {
	if m.viewDirty {
		m.buildView()
		m.viewDirty = false
	}
	return m.viewCache
}

func (m *Model) buildView() {
	view := m.BorderStyle.Render(m.chartCache)
	if m.yaxis {
		view = lipgloss.JoinHorizontal(lipgloss.Top, m.yaxisCache, view)
	}
	if m.xaxis {
		view = lipgloss.JoinVertical(lipgloss.Left, view, m.xaxisCache)
	}
	m.viewCache = m.PanelStyle.Render(view)
}

func (m *Model) calcTicks() {
	m.xticks = make([]float64, m.XTicks)
	for i := range m.XTicks {
		m.xticks[i] = float64(i) / float64(m.XTicks-1)
	}
	m.yticks = make([]float64, m.YTicks)
	for i := range m.YTicks {
		m.yticks[i] = float64(i) / float64(m.YTicks-1)
	}
}

func (m *Model) Push(seriesName string, t time.Time, value float64) {
	s := m.series[seriesName]
	if s == nil {
		s = &series{}
		m.series[seriesName] = s
	}

	if n := len(s.samples); n > 0 {
		last := s.samples[n-1]
		if t.Before(last.t) {
			return
		}
		if t.Equal(last.t) {
			s.samples[n-1].value = value
			return
		}
	}

	s.samples = append(s.samples, sample{t: t, value: value})
	trimSamples(s)
}

func trimSamples(s *series) {
	if len(s.samples) <= maxStoredValues {
		return
	}
	n := maxStoredValues / 2
	copy(s.samples, s.samples[len(s.samples)-n:])
	s.samples = s.samples[:n]
}

func (m *Model) resample(s *series, windowStart, windowEnd time.Time, graphW int) []float64 {
	if graphW <= 0 || len(s.samples) == 0 {
		return nil
	}
	windowNs := windowEnd.Sub(windowStart).Nanoseconds()
	if windowNs <= 0 {
		return nil
	}

	result := make([]float64, graphW)
	holdVal := 0.0
	si := 0

	for si < len(s.samples) && !s.samples[si].t.After(windowStart) {
		holdVal = s.samples[si].value
		si++
	}

	for col := range graphW {
		colEnd := windowStart.Add(time.Duration(windowNs * int64(col+1) / int64(graphW)))
		for si < len(s.samples) && !s.samples[si].t.After(colEnd) {
			holdVal = s.samples[si].value
			si++
		}
		result[col] = holdVal
	}

	return result
}

func (m *Model) applyOuterSize(width, height int) {
	m.width = max(width, 1)
	m.height = max(height, 1)
	m.canvasW = max(m.width-m.BorderStyle.GetHorizontalFrameSize(), 0)
	inner := height
	if m.xaxis {
		inner -= m.XAxisHeight()
	}
	m.canvasH = max(inner-m.BorderStyle.GetVerticalFrameSize(), 0)
}

func (m *Model) Resize(width, height int) {
	m.applyOuterSize(width, height)
	m.buildGrids()
	m.buildThresholdGrids()
	m.yaxisCache = m.renderYAxis()
	m.xaxisCache = m.renderXAxis()
	m.chartCache = m.renderChart(m.seriesRenderCache)
	m.viewDirty = true
}

func (m *Model) buildGrids() {
	if !m.DetailMode {
		m.grids = nil
		m.lineGrids = make(map[string]*lineGrid)
		for name := range m.series {
			g := newLineGrid(m.canvasW, m.canvasH)
			m.lineGrids[name] = &g
		}
		return
	}
	m.lineGrids = nil
	m.grids = make(map[string]*brailleGrid)
	for name := range m.series {
		g := newBrailleGrid(m.canvasW, m.canvasH)
		m.grids[name] = &g
	}
}

// styleToANSI extracts the ANSI escape prefix and suffix that lipgloss wraps around text
func styleToANSI(s lipgloss.Style) [2]string {
	const marker = "\U000F0000" // private-use char, won't appear in ANSI codes
	r := s.Render(marker)
	i := strings.Index(r, marker)
	if i < 0 {
		return [2]string{"", ""}
	}
	return [2]string{r[:i], r[i+len(marker):]}
}

func (m *Model) buildStyleANSITable() {
	m.styleANSI = make(map[string][2]string)

	m.styleANSI[axisAnsiKey] = styleToANSI(m.AxisStyle)
	m.styleANSI[panelAnsiKey] = styleToANSI(m.PanelStyle)
	for name, s := range m.series {
		m.styleANSI[name] = styleToANSI(s.style)
	}
}

func (m *Model) tickRow(pct float64) int {
	if m.canvasH <= 1 {
		return 0
	}
	if !m.DetailMode {
		return int(math.Round((1 - pct) * float64(m.canvasH-1)))
	}
	graphH := m.canvasH * 4
	graphY := int(math.Round((1 - pct) * float64(graphH-1)))
	return graphY / 4
}

func (m *Model) ensureBrailleGrid(seriesName string) {
	if m.grids == nil {
		m.grids = make(map[string]*brailleGrid)
	}
	if m.grids[seriesName] == nil {
		g := newBrailleGrid(m.canvasW, m.canvasH)
		m.grids[seriesName] = &g
	}
}

func (m *Model) ensureLineGrid(seriesName string) {
	if m.lineGrids == nil {
		m.lineGrids = make(map[string]*lineGrid)
	}
	if m.lineGrids[seriesName] == nil {
		g := newLineGrid(m.canvasW, m.canvasH)
		m.lineGrids[seriesName] = &g
	}
}

// Draw renders the chart for the given series names and current time.
// The first name in seriesNames is the topmost layer (first non-empty cell wins).
func (m *Model) Draw(seriesNames []string, now time.Time) {
	m.seriesRenderCache = slices.Clone(seriesNames)
	if m.Resolution > 0 {
		now = now.Truncate(m.Resolution)
	}
	windowStart := now.Add(-m.TimeRange())

	yRange := m.yRangeForDraw(seriesNames, windowStart, now)
	if yRange != m.renderYRange {
		m.renderYRange = yRange
		m.yaxisCache = m.renderYAxis()
		m.xaxisCache = m.renderXAxis()
		m.buildThresholdGrids()
		m.viewDirty = true
	}

	if !m.DetailMode {
		for _, g := range m.lineGrids {
			g.clear()
		}
		graphW := m.canvasW
		for _, name := range seriesNames {
			s := m.series[name]
			if s == nil || len(s.samples) == 0 {
				continue
			}
			if values := m.resample(s, windowStart, now, graphW); values != nil {
				m.ensureLineGrid(name)
				m.lineGrids[name].drawValues(values, yRange[0], yRange[1])
			}
		}
		m.chartCache = m.renderChart(seriesNames)
		return
	}

	for _, g := range m.grids {
		g.clear()
	}

	graphW := m.canvasW * 2
	for _, name := range seriesNames {
		s := m.series[name]
		if s == nil || len(s.samples) == 0 {
			continue
		}
		if values := m.resample(s, windowStart, now, graphW); values != nil {
			m.ensureBrailleGrid(name)
			m.grids[name].drawValues(values, yRange[0], yRange[1])
		}
	}
	m.chartCache = m.renderChart(seriesNames)
}

func (m *Model) Invalidate() {
	m.viewDirty = true
}

func (m *Model) renderChart(seriesNames []string) string {
	if m.canvasW <= 0 || m.canvasH <= 0 {
		return ""
	}
	if !m.DetailMode {
		return m.renderChartPlain(seriesNames)
	}
	return m.renderChartBraille(seriesNames)
}

func (m *Model) renderChartBraille(seriesNames []string) string {
	var buf strings.Builder
	buf.Grow(m.canvasW * m.canvasH * 6)

	for y := range m.canvasH {
		if y > 0 {
			buf.WriteByte('\n')
		}
		prevKey := ""
		for x := range m.canvasW {
			idx := y*m.canvasW + x

			var seriesBits uint8
			for _, g := range m.grids {
				seriesBits |= g.bits[idx]
			}
			var thresholdBits uint8
			for _, tg := range m.thresholdGrids {
				if tg != nil {
					thresholdBits |= tg.bits[idx]
				}
			}

			var combined uint8
			if seriesBits != 0 {
				combined = seriesBits
			} else {
				combined = thresholdBits
			}

			hasSeriesStyle := false
			styleKey := panelAnsiKey
			for _, name := range seriesNames {
				if m.grids[name] == nil {
					continue
				}
				if m.grids[name].bits[idx] != 0 {
					styleKey = name
					hasSeriesStyle = true
					break
				}
			}

			if !hasSeriesStyle && thresholdBits != 0 {
				for i, tg := range m.thresholdGrids {
					if tg != nil && tg.bits[idx] != 0 {
						styleKey = fmt.Sprintf("_threshold_%d", i)
						break
					}
				}
			}

			if styleKey != prevKey {
				if prevKey != "" {
					buf.WriteString(m.styleANSI[prevKey][1])
				}
				buf.WriteString(m.styleANSI[styleKey][0])
				prevKey = styleKey
			}

			if combined == 0 {
				buf.WriteByte(' ')
			} else if seriesBits == 0 && thresholdBits != 0 {
				buf.WriteRune('─')
			} else {
				buf.WriteRune(rune(0x2800) + rune(combined))
			}
		}
		if prevKey != "" {
			buf.WriteString(m.styleANSI[prevKey][1])
		}
	}
	return buf.String()
}

func (m *Model) renderChartPlain(seriesNames []string) string {
	var buf strings.Builder
	buf.Grow(m.canvasW * m.canvasH * 6)

	for y := range m.canvasH {
		if y > 0 {
			buf.WriteByte('\n')
		}
		prevKey := ""
		for x := range m.canvasW {
			var glyph rune
			styleKey := panelAnsiKey

			for _, name := range seriesNames {
				g := m.lineGrids[name]
				if g == nil {
					continue
				}
				if r := g.runeAt(x, y); r != 0 {
					glyph = r
					styleKey = name
					break
				}
			}

			if glyph == 0 {
				for i, tg := range m.thresholdLineGrids {
					if tg == nil {
						continue
					}
					if r := tg.runeAt(x, y); r != 0 {
						glyph = r
						styleKey = fmt.Sprintf("_threshold_%d", i)
						break
					}
				}
			}

			if styleKey != prevKey {
				if prevKey != "" {
					buf.WriteString(m.styleANSI[prevKey][1])
				}
				buf.WriteString(m.styleANSI[styleKey][0])
				prevKey = styleKey
			}

			if glyph == 0 {
				buf.WriteByte(' ')
			} else {
				buf.WriteRune(glyph)
			}
		}
		if prevKey != "" {
			buf.WriteString(m.styleANSI[prevKey][1])
		}
	}
	return buf.String()
}

func (m *Model) EnableAxes(xaxis, yaxis bool) {
	if m.xaxis == xaxis && m.yaxis == yaxis {
		return
	}
	m.xaxis = xaxis
	m.yaxis = yaxis
	m.yaxisCache = m.renderYAxis()
	m.xaxisCache = m.renderXAxis()
	m.viewDirty = true
}

func (m *Model) SetSeriesStyle(seriesName string, style lipgloss.Style) {
	s := m.series[seriesName]
	if s == nil {
		s = &series{}
		m.series[seriesName] = s
	}
	s.style = style
	m.styleANSI[seriesName] = styleToANSI(style)
}

// ThresholdLine represents a horizontal dashed line at a fixed Y value
type ThresholdLine struct {
	Value float64
	Style lipgloss.Style
}

// SetThresholds configures horizontal dashed ceiling lines on the chart
func (m *Model) SetThresholds(thresholds []ThresholdLine) {
	m.thresholds = thresholds
	for i, threshold := range thresholds {
		key := fmt.Sprintf("_threshold_%d", i)
		m.styleANSI[key] = styleToANSI(threshold.Style)
	}
	m.buildThresholdGrids()
	m.viewDirty = true
}

func (m *Model) SetDetailMode(enabled bool) {
	if m.DetailMode == enabled {
		return
	}
	m.DetailMode = enabled
	m.grids = nil
	m.lineGrids = nil
	m.buildThresholdGrids()
	m.viewDirty = true
}

func (m *Model) buildThresholdGrids() {
	if !m.DetailMode {
		m.thresholdGrids = nil
		m.thresholdLineGrids = make([]*lineGrid, len(m.thresholds))
		if m.canvasW <= 0 || m.canvasH <= 0 {
			return
		}
		for i, threshold := range m.thresholds {
			grid := newLineGrid(m.canvasW, m.canvasH)
			m.thresholdLineGrids[i] = &grid

			yRange := m.renderYRange
			span := yRange[1] - yRange[0]
			if span <= 0 {
				continue
			}
			normalized := clamp01((threshold.Value - yRange[0]) / span)
			row := int(math.Round(clamp01(1-normalized) * float64(grid.canvasH-1)))
			grid.fillRow(row)
		}
		return
	}

	m.thresholdLineGrids = nil
	m.thresholdGrids = make([]*brailleGrid, len(m.thresholds))
	if m.canvasW <= 0 || m.canvasH <= 0 {
		return
	}
	for i, threshold := range m.thresholds {
		grid := newBrailleGrid(m.canvasW, m.canvasH)
		m.thresholdGrids[i] = &grid

		yRange := m.renderYRange
		span := yRange[1] - yRange[0]
		if span <= 0 {
			continue
		}
		normalized := clamp01((threshold.Value - yRange[0]) / span)
		graphY := int(math.Floor(clamp01(1-normalized) * float64(grid.graphH-1)))
		if graphY < 0 {
			graphY = 0
		}

		for graphX := 0; graphX < grid.graphW; graphX++ {
			grid.setDot(graphX, graphY)
		}
	}
}

func (m *Model) TimeRange() time.Duration {
	if m.canvasW <= 0 || m.Resolution <= 0 {
		return 0
	}
	return time.Duration(m.canvasW) * m.Resolution
}
func (m *Model) XAxisHeight() int { return 1 }

func niceCeiling(value float64) float64 {
	if value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return 1
	}
	magnitude := math.Pow(10, math.Floor(math.Log10(value)))
	switch fraction := value / magnitude; {
	case fraction <= 1:
		return magnitude
	case fraction <= 2:
		return 2 * magnitude
	case fraction <= 5:
		return 5 * magnitude
	default:
		return 10 * magnitude
	}
}

func (m *Model) yRangeForDraw(seriesNames []string, windowStart, windowEnd time.Time) [2]float64 {
	if !m.AutoScale {
		return m.YRange
	}

	minY := m.YRange[0]
	maxValue := math.Inf(-1)
	found := false

	for _, name := range seriesNames {
		s := m.series[name]
		if s == nil || len(s.samples) == 0 {
			continue
		}
		var lastPreWindow float64
		hasPreWindow := false
		for _, sp := range s.samples {
			if sp.t.After(windowEnd) {
				break
			}
			v := sp.value
			if math.IsNaN(v) || math.IsInf(v, 0) {
				continue
			}
			if sp.t.Before(windowStart) {
				lastPreWindow = v
				hasPreWindow = true
				continue
			}
			maxValue = max(maxValue, v)
			found = true
		}
		if hasPreWindow {
			maxValue = max(maxValue, lastPreWindow)
			found = true
		}
	}

	if !found {
		return m.renderYRange
	}

	span := maxValue - minY
	if span <= 0 {
		return m.renderYRange
	}

	return [2]float64{minY, minY + niceCeiling(span*1.1)}
}

func (m *Model) YAxisWidth() int {
	w := 0
	n := len(m.yticks)
	for i, pct := range m.yticks {
		v := pct*(m.renderYRange[1]-m.renderYRange[0]) + m.renderYRange[0]
		if lw := lipgloss.Width(m.YTickFormatter(m, v, i, n)); lw > w {
			w = lw
		}
	}
	return w
}

func (m *Model) renderYAxis() string {
	if !m.yaxis {
		return ""
	}

	axisW := m.YAxisWidth()
	axisH := m.canvasH + m.BorderStyle.GetVerticalFrameSize()
	if axisW <= 0 || axisH <= 0 || m.canvasH <= 0 {
		return ""
	}

	cell := lipgloss.NewStyle().Inherit(m.AxisStyle).Width(axisW).Align(lipgloss.Right)
	lines := make([]string, axisH)
	for i := range lines {
		lines[i] = cell.Render("")
	}

	n := len(m.yticks)
	for i, pct := range m.yticks {
		row := m.tickRow(pct)
		if row < 0 || row >= m.canvasH {
			continue
		}
		y := row + 1 // +1 for top border line
		v := pct*(m.renderYRange[1]-m.renderYRange[0]) + m.renderYRange[0]
		lines[y] = cell.Render(m.YTickFormatter(m, v, i, n))
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (m *Model) renderXAxis() string {
	prefixW := 0
	if m.yaxis {
		prefixW = m.YAxisWidth()
	}
	axisW := m.BorderStyle.GetHorizontalFrameSize() + m.canvasW
	if axisW <= 0 || m.canvasW <= 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("└")
	cursor := 0
	n := len(m.xticks)
	for i, pct := range m.xticks {
		label := m.XTickFormatter(m, pct, i, n)
		labelW := lipgloss.Width(label)

		x := int(pct * float64(m.canvasW))
		if i == n-1 {
			x = m.canvasW - labelW
		}
		if x < 0 || x < cursor || x+labelW > m.canvasW {
			continue
		}
		b.WriteString(strings.Repeat("─", x-cursor))
		b.WriteString(label)
		cursor = x + labelW
	}
	b.WriteString(strings.Repeat("─", m.canvasW-cursor))
	mid := b.String()

	edge := max(0, axisW-m.canvasW)
	left, right := edge/2, edge-edge/2
	return lipgloss.JoinHorizontal(lipgloss.Left,
		m.AxisStyle.Width(prefixW+left-1).Render(""),
		m.AxisStyle.Render(mid),
		m.AxisStyle.Width(right).Render(""))
}

func (m *Model) Reset() {
	m.series = make(map[string]*series)
	m.viewDirty = true
}

func New(w, h int, opts ...Option) *Model {
	m := &Model{
		YRange: [2]float64{0, 100},
		YTicks: 5,
		XTicks: 5,
		XTickFormatter: func(_ *Model, v float64, _, _ int) string {
			return fmt.Sprintf("%4.1f", v)
		},
		YTickFormatter: func(_ *Model, v float64, _, _ int) string {
			return fmt.Sprintf("%4.1f", v)
		},
		BorderStyle: defaultBorderStyle,
		AxisStyle:   defaultAxisStyle,
		PanelStyle:  lipgloss.NewStyle(),
		Resolution:  200 * time.Millisecond,

		series: make(map[string]*series),
		width:  w,
		height: h,
	}
	for _, opt := range opts {
		opt(m)
	}
	m.renderYRange = m.YRange
	m.applyOuterSize(w, h)
	m.calcTicks()
	m.buildStyleANSITable()
	return m
}
