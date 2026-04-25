package top

import (
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"
)

const (
	keyQuit       = "q"
	keyPause      = "space"
	keyReset      = "r"
	keyCompute    = "c"
	keyMemory     = "m"
	keyNvlink     = "n"
	keyGrActive   = "g"
	keySmActivity = "s"
	keyPcie       = "p"
	keyBandwidth  = "b"
	keyDetail     = "d"
	keyMetricMode = "tab"
)

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch typedMsg := msg.(type) {
	case tea.BackgroundColorMsg:
		m.dark = typedMsg.IsDark()
		m.applyTheme()
		if !m.initialized {
			return m, m.spinner.Tick
		}
		return m, m.beginDraw()
	case tea.InterruptMsg:
		m.quitting = true
		return m, tea.Quit
	case tea.KeyPressMsg:
		switch typedMsg.String() {
		case "ctrl+c", keyQuit:
			m.quitting = true
			return m, tea.Quit
		case keyMetricMode:
			m.metricsMode = m.metricsMode.next()
			m.applyCeilingThresholds()
			m.applyLayout()
			return m, m.beginDraw()
		case keyNvlink:
			if !m.showBandwidth {
				return m, nil
			}
			m.toggleSeries(nvlinkSeries)
			return m, m.beginDraw()
		case keyPcie:
			if !m.showBandwidth {
				return m, nil
			}
			m.toggleSeries(pcieSeries)
			return m, m.beginDraw()
		case keyReset:
			m.resetCharts()
			return m, m.beginDraw()
		case keyPause:
			m.paused = !m.paused
			if m.paused {
				m.pausedAt = time.Now()
			} else {
				m.pausedAt = time.Time{}
			}
			return m, m.beginDraw()
		case keyBandwidth:
			m.showBandwidth = !m.showBandwidth
			m.applyLayout()
			return m, m.beginDraw()
		case keyDetail:
			m.detailMode = !m.detailMode
			m.applyDetailMode()
			return m, m.beginDraw()
		default:
			key := typedMsg.String()
			if m.metricsMode.def().handleHotkey(&m, key) {
				return m, m.beginDraw()
			}
			return m, nil
		}
	case tea.WindowSizeMsg:
		m.width = max(typedMsg.Width, 1)
		m.height = max(typedMsg.Height, 1)
		m.applyLayout()
		return m, m.beginDraw()
	case InitMsg:
		m.initCharts(typedMsg.DeviceIDs)
		m.initialized = true
		m.applyLayout()
		return m, m.beginDraw()
	case drawMsg:
		m.draw()
		if m.paused {
			return m, nil
		}
		return m, m.tick()
	case MetricsSnapshotMsg:
		var pcieBytesPerSecond float64
		var nvlinkBytesPerSecond float64
		hasBandwidth := false

		for _, gpu := range typedMsg.GPUs {
			chartIdx, ok := m.deviceIndexMap[gpu.DeviceID]
			if !ok || chartIdx < 0 || chartIdx >= len(m.solCharts) {
				continue
			}
			chart := m.solCharts[chartIdx]
			if chart == nil {
				continue
			}
			m.online[chartIdx] = true
			if m.paused {
				continue
			}

			if gpu.SOL.Valid {
				chart.Push(computeSOLSeries, typedMsg.Timestamp, gpu.SOL.ComputePct)
				m.computeLastValues[chartIdx] = newPercentValue(gpu.SOL.ComputePct)
				chart.Push(memorySOLSeries, typedMsg.Timestamp, gpu.SOL.MemoryPct)
				m.memoryLastValues[chartIdx] = newPercentValue(gpu.SOL.MemoryPct)
			}
			if gpu.DCGMUtilization.Valid {
				chart.Push(smActivitySeries, typedMsg.Timestamp, gpu.DCGMUtilization.SMActivePct)
				m.smActivityLastValues[chartIdx] = newPercentValue(gpu.DCGMUtilization.SMActivePct)
			}
			if gpu.NVMLUtilization.Valid {
				chart.Push(grActiveSeries, typedMsg.Timestamp, gpu.NVMLUtilization.UtilPct)
				m.grActiveLastValues[chartIdx] = newPercentValue(gpu.NVMLUtilization.UtilPct)
			}
			if gpu.Bandwidth.Valid {
				pcieBytesPerSecond += gpu.Bandwidth.PCIeTxBps + gpu.Bandwidth.PCIeRxBps
				nvlinkBytesPerSecond += gpu.Bandwidth.NVLinkTxBps + gpu.Bandwidth.NVLinkRxBps
				hasBandwidth = true
			}
		}

		if !m.paused && hasBandwidth {
			m.bandwidthChart.Push(pcieSeries, typedMsg.Timestamp, pcieBytesPerSecond)
			m.bandwidthChart.Push(nvlinkSeries, typedMsg.Timestamp, nvlinkBytesPerSecond)
			m.pcieLastValue = pcieBytesPerSecond
			m.nvlinkLastValue = nvlinkBytesPerSecond
		}
		return m, nil
	case RooflineCeilingMsg:
		m.gpuCeilings = typedMsg.PerGPU
		m.applyCeilingThresholds()
		m.applyLayout()
		if m.ready() {
			m.draw()
		}
		return m, nil
	case ErrorMsg:
		m.err = typedMsg.Error
		return m, nil
	default:
		if !m.initialized {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil
	}
}

func (m model) beginDraw() tea.Cmd {
	if !m.ready() {
		return nil
	}

	return func() tea.Msg {
		return drawMsg{}
	}
}

func (m *model) toggleSeries(series string) {
	i := slices.Index(m.enabledSeries, series)
	if i == -1 {
		m.enabledSeries = append(m.enabledSeries, series)
		return
	}
	m.enabledSeries = slices.Delete(m.enabledSeries, i, i+1)
}

func (m model) seriesEnabled(series string) bool {
	return slices.Contains(m.enabledSeries, series)
}
