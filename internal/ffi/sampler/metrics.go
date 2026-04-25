package sampler

import "slices"

const (
	metricSMPipeTensorCyclesActive   = "sm__pipe_tensor_cycles_active.avg.pct_of_peak_sustained_elapsed"
	metricSMPipeFmaCyclesActive      = "sm__pipe_fma_cycles_active.avg.pct_of_peak_sustained_elapsed"
	metricSMPipeAluCyclesActive      = "sm__pipe_alu_cycles_active.avg.pct_of_peak_sustained_elapsed"
	metricSMInstExecutedPipeLsu      = "sm__inst_executed_pipe_lsu.avg.pct_of_peak_sustained_elapsed"
	metricSMIssueActive              = "sm__issue_active.avg.pct_of_peak_sustained_elapsed"
	metricSMCyclesActive             = "sm__cycles_active.avg.pct_of_peak_sustained_elapsed"
	metricDramThroughput             = "dram__throughput.avg.pct_of_peak_sustained_elapsed"
	metricL1texDataPipeLsuWavefronts = "l1tex__data_pipe_lsu_wavefronts.avg.pct_of_peak_sustained_elapsed"
)

var (
	smSubPipeMetrics = []string{
		metricSMPipeTensorCyclesActive,
		metricSMPipeFmaCyclesActive,
		metricSMPipeAluCyclesActive,
		metricSMInstExecutedPipeLsu,
		metricSMIssueActive,
	}
	memMetrics = []string{
		metricDramThroughput,
		metricL1texDataPipeLsuWavefronts,
	}
	dcgmMetrics = []string{
		metricSMCyclesActive,
	}
	DefaultMetrics = slices.Concat(smSubPipeMetrics, memMetrics, dcgmMetrics)
)
