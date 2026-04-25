package top

import (
	"fmt"
	"strings"
	"time"

	"github.com/systalyze/utilyze/internal/format"
	"github.com/systalyze/utilyze/internal/tui/components/tschart"
	"github.com/systalyze/utilyze/internal/version"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const headerLogo = "⣴⠟⠛⠢⣄⣠⠔⠛⠻⣦\n⠻⣦⣤⠴⠋⠙⠦⣤⣴⠟"

const (
	nvlinkSeries = "nvlink"
	pcieSeries   = "pcie"

	headerSegmentGap = "  "
)

const bytesSIWidth = 5

func clipFrame(width int, content string) string {
	if width <= 0 {
		return content
	}
	lines := strings.Split(content, "\n")
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], width, "")
	}
	return strings.Join(lines, "\n")
}

func (m model) View() tea.View {
	if m.quitting {
		return tea.NewView("")
	}

	if m.err != nil {
		banner := lipgloss.JoinHorizontal(lipgloss.Top, m.styles.Spinner.Render("✗ "), m.err.Error())
		view := tea.NewView(clipFrame(m.width, lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, banner)))
		view.AltScreen = true
		return view
	}

	if !m.initialized {
		banner := lipgloss.JoinHorizontal(lipgloss.Top, m.spinner.View(), " Initializing...")
		view := tea.NewView(clipFrame(m.width, lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, banner)))
		view.AltScreen = true
		return view
	}

	if len(m.solCharts) == 0 {
		banner := m.styles.HeaderLabel.Render("No devices found")
		view := tea.NewView(clipFrame(m.width, lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, banner)))
		view.AltScreen = true
		return view
	}

	l := m.calcLayout(len(m.solCharts), m.width, m.height)

	gridCols := make([]string, 0, l.gridCols)
	maxColHeight := 0
	for c := 0; c < l.gridCols; c++ {
		var parts []string
		colWidth := l.gridCellWidth
		if c == l.gridCols-1 {
			colWidth += l.gridWidthRemainder
		}

		for idx := c; idx < len(m.solCharts); idx += l.gridCols {
			header := m.solChartHeaderView(idx, l.gridPrefixWidth, colWidth, l.twoLine)
			parts = append(parts, header, m.solCharts[idx].View())
		}

		chartWidth := colWidth + l.gridPrefixWidth
		col := m.styles.ChartPanel.Width(chartWidth).Height(1).Render("")
		if len(parts) > 0 {
			col = lipgloss.JoinVertical(lipgloss.Left, parts...)
		}

		gridCols = append(gridCols, col)
		if h := lipgloss.Height(col); h > maxColHeight {
			maxColHeight = h
		}
	}

	for i := range gridCols {
		paddingHeight := maxColHeight - lipgloss.Height(gridCols[i])
		if paddingHeight > 0 {
			colWidth := lipgloss.Width(gridCols[i])
			gridCols[i] = lipgloss.JoinVertical(
				lipgloss.Left,
				gridCols[i],
				m.styles.ChartPanel.Width(colWidth).Height(paddingHeight).Render(""),
			)
		}
	}

	grid := lipgloss.JoinVertical(
		lipgloss.Left,
		m.solHeaderView(),
		lipgloss.JoinHorizontal(lipgloss.Top, gridCols...),
	)
	bodyParts := []string{grid}
	if m.showBandwidth {
		bodyParts = append(bodyParts, lipgloss.JoinVertical(lipgloss.Left, m.bandwidthChartHeaderView(), m.bandwidthChart.View()))
	}
	bodyParts = append(bodyParts, m.hotkeyBarView())
	body := lipgloss.JoinVertical(lipgloss.Left, bodyParts...)
	logo := m.headerLogoView()
	body = lipgloss.NewCompositor(
		lipgloss.NewLayer(body),
		lipgloss.NewLayer(logo).X(max(m.width-lipgloss.Width(logo), 0)).Y(0).Z(1),
	).Render()
	view := tea.NewView(clipFrame(m.width, body))
	view.AltScreen = true
	return view
}

func (m model) headerBar(totalWidth int, inner string) string {
	return m.styles.Header.Width(totalWidth).MaxWidth(totalWidth).Align(lipgloss.Left).Render(inner)
}

func (m model) headerLogoView() string {
	top, bottom, _ := strings.Cut(headerLogo, "\n")
	gap := m.styles.Header.Render(" ")
	line1 := lipgloss.JoinHorizontal(
		lipgloss.Top,
		m.styles.HeaderBold.Render("SYSTALYZE"),
		gap,
		m.styles.HeaderLabel.Render(top),
		gap,
	)
	line2 := lipgloss.JoinHorizontal(
		lipgloss.Top,
		m.styles.HeaderSecondary.Render("utilyze v"+version.VERSION),
		gap,
		m.styles.HeaderLabel.Render(bottom),
		gap,
	)
	width := max(lipgloss.Width(line1), lipgloss.Width(line2))

	return lipgloss.JoinVertical(
		lipgloss.Left,
		m.styles.Header.Width(width).Align(lipgloss.Right).Render(line1),
		m.styles.Header.Width(width).Align(lipgloss.Right).Render(line2),
	)
}

func (m model) solHeaderView() string {
	return lipgloss.JoinVertical(
		lipgloss.Left,
		m.headerBar(m.width, m.solHeaderInner()),
		m.headerBar(m.width, ""),
	)
}

func (m model) solHeaderInner() string {
	name, ceiling, uniform := m.uniformAttribution()
	mode := m.metricsMode.def()

	gap := m.styles.HeaderLabel.Render(" ")
	parts := []string{gap}

	if m.metricsMode != MetricsModeSOL {
		parts = append(parts,
			m.styles.HeaderBold.Render(mode.label),
		)
	}
	for _, series := range mode.series {
		if !m.seriesEnabled(series.name) {
			continue
		}
		if len(parts) > 1 {
			parts = append(parts, gap)
		}
		parts = append(parts,
			series.styleFor(m.styles).Inherit(m.styles.Header).Render("■"),
			m.styles.HeaderLabel.Render(" "+series.legendLabel),
		)
	}
	if m.metricsMode == MetricsModeSOL && uniform && ceiling != nil && m.seriesEnabled(computeSOLSeries) {
		parts = append(parts,
			gap,
			m.styles.ComputeCeiling.Inherit(m.styles.Header).Render("■"),
			m.styles.HeaderLabel.Render(" Attainable Compute SOL% "),
			m.styles.ComputeCeiling.Inherit(m.styles.Header).Render(fmt.Sprintf("[%.0f%%]", *ceiling)),
		)
	}
	if uniform && name != nil {
		parts = append(parts, gap, m.styles.HeaderSecondary.Render(*name))
	}
	return m.headerBar(m.width, lipgloss.JoinHorizontal(lipgloss.Top, parts...))
}

// uniformAttribution returns (modelName, computeCeiling, true) when every
// monitored GPU has the same model name and compute ceiling.
func (m model) uniformAttribution() (*string, *float64, bool) {
	if len(m.deviceIDs) == 0 {
		return nil, nil, false
	}
	var firstName string
	var firstCeil float64
	haveFirst := false
	for _, id := range m.deviceIDs {
		g, ok := m.gpuCeilings[id]
		if !ok || g.ModelName == nil || g.ComputeSolCeiling == nil {
			return nil, nil, false
		}
		if !haveFirst {
			firstName, firstCeil = *g.ModelName, *g.ComputeSolCeiling
			haveFirst = true
			continue
		}
		if firstName != *g.ModelName || firstCeil != *g.ComputeSolCeiling {
			return nil, nil, false
		}
	}
	if !haveFirst {
		return nil, nil, false
	}
	return &firstName, &firstCeil, true
}

func (m model) headerMetricsPart(deviceIdx int) string {
	mode := m.metricsMode.def()
	dot := m.styles.DotOffline
	if m.online[deviceIdx] {
		dot = m.styles.DotOnline
	}

	physicalID := deviceIdx
	if deviceIdx < len(m.deviceIDs) {
		physicalID = m.deviceIDs[deviceIdx]
	}

	parts := []string{
		m.styles.HeaderBold.Render(fmt.Sprintf("GPU %d ", physicalID)),
		dot,
	}

	for _, series := range mode.series {
		if !m.seriesEnabled(series.name) {
			continue
		}
		parts = append(parts,
			series.styleFor(m.styles).Inherit(m.styles.Header).Render(headerSegmentGap+series.valueFor(m, deviceIdx).String()))
	}
	if m.metricsMode == MetricsModeSOL {
		if _, _, uniform := m.uniformAttribution(); !uniform {
			if g, ok := m.gpuCeilings[physicalID]; ok && g.ModelName != nil && g.ComputeSolCeiling != nil {
				parts = append(parts,
					m.styles.ComputeCeiling.Inherit(m.styles.Header).Render(
						fmt.Sprintf(" [%.0f%%]", *g.ComputeSolCeiling)))
			}
		}
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
}

func (m model) headerModelText(deviceIdx int) string {
	physicalID := deviceIdx
	if deviceIdx < len(m.deviceIDs) {
		physicalID = m.deviceIDs[deviceIdx]
	}
	if _, _, uniform := m.uniformAttribution(); uniform {
		return ""
	}
	g, ok := m.gpuCeilings[physicalID]
	if !ok || g.ModelName == nil {
		return ""
	}
	return *g.ModelName
}

func (m model) headerModelPart(deviceIdx, width int, inline bool) string {
	model := m.headerModelText(deviceIdx)
	if model == "" || width <= 0 {
		return ""
	}
	if inline {
		model = headerSegmentGap + model
	}
	return m.styles.HeaderSecondary.Render(ansi.Truncate(model, width, "..."))
}

func (m model) solChartHeaderView(deviceIdx, prefixWidth, width int, twoLine bool) string {
	prefix := m.headerBar(prefixWidth, "")
	metrics := m.headerMetricsPart(deviceIdx)
	if !twoLine {
		model := m.headerModelPart(deviceIdx, max(width-lipgloss.Width(metrics), 0), true)
		return lipgloss.JoinHorizontal(
			lipgloss.Top,
			prefix,
			m.headerBar(width, lipgloss.JoinHorizontal(lipgloss.Top, metrics, model)),
		)
	}
	model := m.headerModelPart(deviceIdx, width, false)
	return lipgloss.JoinVertical(
		lipgloss.Left,
		lipgloss.JoinHorizontal(lipgloss.Top, prefix, m.headerBar(width, metrics)),
		lipgloss.JoinHorizontal(lipgloss.Top, prefix, m.headerBar(width, model)),
	)
}

func (m model) bandwidthChartHeaderView() string {
	return m.headerBar(m.width,
		lipgloss.JoinHorizontal(lipgloss.Top,
			m.styles.HeaderBold.Render(" Bandwidth  "),
			m.styles.PCIe.Inherit(m.styles.Header).Render("■"),
			m.styles.HeaderLabel.Render(fmt.Sprintf(" PCIe %sB/s  ", format.SI(m.pcieLastValue, bytesSIWidth))),
			m.styles.NVLink.Inherit(m.styles.Header).Render("■"),
			m.styles.HeaderLabel.Render(fmt.Sprintf(" NVLink %sB/s", format.SI(m.nvlinkLastValue, bytesSIWidth))),
		))
}

type hotkeyItem struct {
	key     string
	label   string
	series  string
	style   func(model) lipgloss.Style
	enabled func(model) bool
}

func (h hotkeyItem) styleFor(m model, mode metricsModeDef) lipgloss.Style {
	if h.style != nil {
		return h.style(m)
	}
	for _, series := range mode.series {
		if series.name == h.series {
			return series.styleFor(m.styles)
		}
	}
	return m.styles.HeaderBold
}

func (h hotkeyItem) enabledFor(m model) bool {
	if h.enabled != nil {
		return h.enabled(m)
	}
	if h.series == "" {
		return true
	}
	return m.seriesEnabled(h.series)
}

func (m model) hotkeyBarView() string {
	hotkey := func(item hotkeyItem) string {
		style := item.styleFor(m, m.metricsMode.def()).Inherit(m.styles.HeaderBold)
		if !item.enabledFor(m) {
			style = lipgloss.NewStyle().Inherit(m.styles.HeaderBold).Foreground(m.styles.Palette.Subtle)
		}
		return lipgloss.JoinHorizontal(lipgloss.Top,
			style.Render(fmt.Sprintf(" %s ", item.key)),
			m.styles.HeaderSecondary.Render(item.label+" "),
		)
	}

	section := func(items ...hotkeyItem) string {
		parts := make([]string, 0, len(items))
		for _, item := range items {
			parts = append(parts, hotkey(item))
		}
		return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
	}

	toggleLabel := func(enabled bool, showLabel, hideLabel string) string {
		if enabled {
			return hideLabel
		}
		return showLabel
	}

	seriesItems := m.metricsMode.def().hotkeys
	if m.showBandwidth {
		seriesItems = append(seriesItems,
			hotkeyItem{
				key: keyPcie, label: "pcie", series: pcieSeries,
				style: func(m model) lipgloss.Style { return m.styles.PCIe },
			},
			hotkeyItem{
				key: keyNvlink, label: "nvlink", series: nvlinkSeries,
				style: func(m model) lipgloss.Style { return m.styles.NVLink },
			},
		)
	}

	sections := []string{
		section(
			hotkeyItem{key: keyQuit, label: "quit"},
			hotkeyItem{key: keyPause, label: "pause"},
			hotkeyItem{key: keyReset, label: "reset"},
		),
		section(
			hotkeyItem{key: keyMetricMode, label: "cycle mode"},
			hotkeyItem{key: keyDetail, label: toggleLabel(m.detailMode, "show detail", "hide detail")},
			hotkeyItem{key: keyBandwidth, label: toggleLabel(m.showBandwidth, "show bandwidth", "hide bandwidth")},
		),
		section(seriesItems...),
	}

	divider := m.styles.HeaderSecondary.Render(" │ ")
	return m.headerBar(m.width, strings.Join(sections, divider))
}

func (m model) formatTimeSince(chart *tschart.Model, _ float64, index, n int) string {
	if n <= 1 || index == n-1 {
		return "now"
	}
	ago := chart.TimeRange() * time.Duration(n-1-index) / time.Duration(n-1)
	return format.HumanDuration(ago)
}

type layout struct {
	gridPrefixWidth     int
	gridCols            int
	gridCellWidth       int
	gridCellHeight      int
	gridWidthRemainder  int
	gridHeightRemainder int

	bandwidthWidth  int
	bandwidthHeight int

	twoLine bool
}

func (m model) calcLayout(numCharts int, width int, height int) layout {
	if !m.ready() {
		return layout{}
	}

	solAxisWidth := m.solCharts[0].YAxisWidth()
	bwAxisWidth := m.bandwidthChart.YAxisWidth()

	var cols int
	switch {
	case numCharts == 1:
		cols = 1
	case width < 120 || numCharts <= 4:
		cols = 2
	default:
		cols = 4
	}
	rows := (numCharts + cols - 1) / cols

	gridWidth := width - cols*solAxisWidth
	gridCellWidth := max(gridWidth/cols, 1)
	gridWidthRemainder := max(gridWidth-gridCellWidth*cols, 0)

	twoLine := false
	for idx := 0; idx < numCharts; idx++ {
		model := m.headerModelText(idx)
		if model == "" {
			continue
		}
		c := idx % cols
		colWidth := gridCellWidth
		if c == cols-1 {
			colWidth += gridWidthRemainder
		}
		if lipgloss.Width(m.headerMetricsPart(idx))+lipgloss.Width(headerSegmentGap+model) > colWidth {
			twoLine = true
			break
		}
	}

	solChartHeaderLinesPerRow := 1
	if twoLine {
		solChartHeaderLinesPerRow = 2
	}

	const solHeaderLines = 2
	const bandwidthHeaderLines = 1
	const hotkeyLines = 1
	overheadLines := solHeaderLines + solChartHeaderLinesPerRow*rows + hotkeyLines
	if m.showBandwidth {
		overheadLines += bandwidthHeaderLines
	}

	innerHeight := max(height-overheadLines, 1)

	bwHeight := 0
	gridInnerHeight := innerHeight
	if m.showBandwidth {
		bwHeight = max(innerHeight*20/100, 0)
		gridInnerHeight = max(innerHeight-bwHeight, 1)
	}
	gridCellHeight := max(gridInnerHeight/rows, 1)
	gridHeightRemainder := max(gridInnerHeight-gridCellHeight*rows, 0)
	if m.showBandwidth {
		bwHeight += gridHeightRemainder
		gridHeightRemainder = 0
	}

	bwWidth := max(width-bwAxisWidth, 1)

	return layout{
		gridPrefixWidth:     solAxisWidth,
		gridCols:            cols,
		gridCellWidth:       gridCellWidth,
		gridCellHeight:      gridCellHeight,
		gridWidthRemainder:  gridWidthRemainder,
		gridHeightRemainder: gridHeightRemainder,

		bandwidthWidth:  bwWidth,
		bandwidthHeight: bwHeight,

		twoLine: twoLine,
	}
}

func (m *model) applyLayout() {
	if !m.ready() {
		return
	}

	l := m.calcLayout(len(m.solCharts), m.width, m.height)
	for idx, chart := range m.solCharts {
		if chart == nil {
			continue
		}
		isLastChartInColumn := idx+l.gridCols >= len(m.solCharts)
		chart.EnableAxes(isLastChartInColumn, true)
		colWidth := l.gridCellWidth
		isLastColumn := idx%l.gridCols == l.gridCols-1
		if isLastColumn {
			colWidth += l.gridWidthRemainder
		}
		cellHeight := l.gridCellHeight
		if isLastChartInColumn {
			cellHeight += l.gridHeightRemainder
		}
		chart.Resize(colWidth, cellHeight)
	}
	m.bandwidthChart.Resize(l.bandwidthWidth, l.bandwidthHeight)
}
