package nvml

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	ErrNvmlNotInitialized = errors.New("NVML not initialized")
)

var initOnce sync.Once
var initErr error

const (
	maxNVLinkCount = 18
)

type BandwidthSnapshot struct {
	DeviceID    int
	Timestamp   time.Time
	PCIeTxBps   *float64
	PCIeRxBps   *float64
	NVLinkTxBps *float64
	NVLinkRxBps *float64
}

type UtilizationSnapshot struct {
	DeviceID   int
	Timestamp  time.Time
	GPUUtilPct *float64
}

type deviceState struct {
	mu              sync.Mutex
	handle          nvmlDeviceHandle
	id              int
	activeNVLinkIDs []uint32
	prevNVLinkTxKiB uint64
	prevNVLinkRxKiB uint64
	prevNVLinkTime  time.Time
	initOnce        sync.Once
	initErr         error
}

type Client struct {
	devices map[int]*deviceState
	mu      sync.Mutex
}

func Init() (*Client, error) {
	if err := load(); err != nil {
		return nil, err
	}

	initOnce.Do(func() {
		ret := nvmlInit_v2()
		if ret != NVML_SUCCESS {
			initErr = fmt.Errorf("failed to initialize NVML: %d", ret)
		}
	})
	if initErr != nil {
		return nil, initErr
	}

	return &Client{devices: make(map[int]*deviceState)}, nil
}

func (n *Client) GetDeviceCount() (int, error) {
	if initErr != nil {
		return -1, ErrNvmlNotInitialized
	}

	var count uint32
	ret := nvmlDeviceGetCount_v2(&count)
	if ret != NVML_SUCCESS {
		return -1, fmt.Errorf("failed to get device count: %d", ret)
	}
	return int(count), nil
}

func (n *Client) device(index int) (*deviceState, error) {
	if initErr != nil {
		return nil, ErrNvmlNotInitialized
	}
	if index < 0 {
		return nil, fmt.Errorf("invalid device index: %d", index)
	}

	n.mu.Lock()
	device, ok := n.devices[index]
	if !ok {
		device = &deviceState{}
		n.devices[index] = device
	}
	n.mu.Unlock()

	device.initOnce.Do(func() {
		device.initErr = device.init(index)
	})
	if device.initErr != nil {
		return nil, device.initErr
	}
	return device, nil
}

func (d *deviceState) init(index int) error {
	d.id = index
	if ret := nvmlDeviceGetHandleByIndex_v2(uint32(index), &d.handle); ret != NVML_SUCCESS {
		return fmt.Errorf("failed to get device handle: %d", ret)
	}

	d.activeNVLinkIDs = make([]uint32, 0, maxNVLinkCount)
	for link := uint32(0); link < maxNVLinkCount; link++ {
		var isActive uint32
		if ret := nvmlDeviceGetNvLinkState(d.handle, link, &isActive); ret == NVML_SUCCESS && isActive == NVML_FEATURE_ENABLED {
			d.activeNVLinkIDs = append(d.activeNVLinkIDs, link)
		}
	}
	return nil
}

func (d *deviceState) pollBandwidth(now time.Time) (BandwidthSnapshot, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	snapshot := BandwidthSnapshot{
		DeviceID:  d.id,
		Timestamp: now,
	}

	var pcieTxKBps uint32
	var pcieRxKBps uint32
	pcieSuccess := true
	if ret := nvmlDeviceGetPcieThroughput(d.handle, NVML_PCIE_UTIL_TX_BYTES, &pcieTxKBps); ret != NVML_SUCCESS {
		pcieSuccess = false
	}
	if ret := nvmlDeviceGetPcieThroughput(d.handle, NVML_PCIE_UTIL_RX_BYTES, &pcieRxKBps); ret != NVML_SUCCESS {
		pcieSuccess = false
	}

	if pcieSuccess {
		txBps := float64(pcieTxKBps) * 1000
		rxBps := float64(pcieRxKBps) * 1000
		snapshot.PCIeTxBps = &txBps
		snapshot.PCIeRxBps = &rxBps
	}

	if len(d.activeNVLinkIDs) == 0 {
		return snapshot, nil
	}

	fields := make([]nvmlFieldValue, len(d.activeNVLinkIDs)*2)
	for i, link := range d.activeNVLinkIDs {
		fields[i*2].FieldId = NVML_FI_DEV_NVLINK_THROUGHPUT_DATA_TX
		fields[i*2].ScopeId = link
		fields[i*2+1].FieldId = NVML_FI_DEV_NVLINK_THROUGHPUT_DATA_RX
		fields[i*2+1].ScopeId = link
	}

	if ret := nvmlDeviceGetFieldValues(d.handle, len(fields), fields); ret != NVML_SUCCESS {
		return snapshot, nil
	}

	var totalTXKiB uint64
	var totalRXKiB uint64
	for _, field := range fields {
		if field.NvmlReturn != NVML_SUCCESS {
			continue
		}
		switch field.FieldId {
		case NVML_FI_DEV_NVLINK_THROUGHPUT_DATA_TX:
			totalTXKiB += field.Value.UllVal()
		case NVML_FI_DEV_NVLINK_THROUGHPUT_DATA_RX:
			totalRXKiB += field.Value.UllVal()
		}
	}

	if !d.prevNVLinkTime.IsZero() {
		dtSec := now.Sub(d.prevNVLinkTime).Seconds()
		if dtSec > 0 {
			var deltaTXKiB uint64
			var deltaRXKiB uint64
			if totalTXKiB >= d.prevNVLinkTxKiB {
				deltaTXKiB = totalTXKiB - d.prevNVLinkTxKiB
			}
			if totalRXKiB >= d.prevNVLinkRxKiB {
				deltaRXKiB = totalRXKiB - d.prevNVLinkRxKiB
			}
			txBps := float64(deltaTXKiB) * 1024 / dtSec
			rxBps := float64(deltaRXKiB) * 1024 / dtSec
			snapshot.NVLinkTxBps = &txBps
			snapshot.NVLinkRxBps = &rxBps
		}
	}

	d.prevNVLinkTxKiB = totalTXKiB
	d.prevNVLinkRxKiB = totalRXKiB
	d.prevNVLinkTime = now
	return snapshot, nil
}

func (n *Client) GetDeviceUUID(deviceID int) (string, error) {
	device, err := n.device(deviceID)
	if err != nil {
		return "", err
	}
	buf := make([]byte, nvmlDeviceUUIDBufferSize)
	if ret := nvmlDeviceGetUUID(device.handle, &buf[0], nvmlDeviceUUIDBufferSize); ret != NVML_SUCCESS {
		return "", fmt.Errorf("nvmlDeviceGetUUID(%d): %d", deviceID, ret)
	}
	for i, b := range buf {
		if b == 0 {
			return string(buf[:i]), nil
		}
	}
	return string(buf), nil
}

func (n *Client) GetDeviceName(deviceID int) (string, error) {
	device, err := n.device(deviceID)
	if err != nil {
		return "", err
	}
	buf := make([]byte, nvmlDeviceNameBufferSize)
	if ret := nvmlDeviceGetName(device.handle, &buf[0], nvmlDeviceNameBufferSize); ret != NVML_SUCCESS {
		return "", fmt.Errorf("nvmlDeviceGetName(%d): %d", deviceID, ret)
	}
	for i, b := range buf {
		if b == 0 {
			return string(buf[:i]), nil
		}
	}
	return string(buf), nil
}

func (n *Client) PollBandwidth(deviceID int, now time.Time) (BandwidthSnapshot, error) {
	device, err := n.device(deviceID)
	if err != nil {
		return BandwidthSnapshot{}, err
	}
	return device.pollBandwidth(now)
}

func (n *Client) PollUtilization(deviceID int, now time.Time) (UtilizationSnapshot, error) {
	if nvmlDeviceGetUtilizationRates == nil {
		return UtilizationSnapshot{}, errors.New("nvmlDeviceGetUtilizationRates not available")
	}
	device, err := n.device(deviceID)
	if err != nil {
		return UtilizationSnapshot{}, err
	}

	var utilization nvmlUtilization
	if ret := nvmlDeviceGetUtilizationRates(device.handle, &utilization); ret != NVML_SUCCESS {
		return UtilizationSnapshot{}, fmt.Errorf("nvmlDeviceGetUtilizationRates(%d): %d", deviceID, ret)
	}
	gpuUtilPct := float64(utilization.GPU)
	return UtilizationSnapshot{
		DeviceID:   deviceID,
		Timestamp:  now,
		GPUUtilPct: &gpuUtilPct,
	}, nil
}

type ProcessInfo struct {
	PID                int
	UsedGpuMemoryBytes uint64
	GpuInstanceID      uint32
	ComputeInstanceID  uint32
}

func (n *Client) GetComputeProcesses(deviceID int) ([]ProcessInfo, error) {
	if initErr != nil {
		return nil, ErrNvmlNotInitialized
	}
	if nvmlDeviceGetComputeRunningProcesses == nil {
		return nil, errors.New("nvmlDeviceGetComputeRunningProcesses not available (driver may be too old)")
	}
	device, err := n.device(deviceID)
	if err != nil {
		return nil, err
	}

	// probe call with count=0 to get required size for variable-length array
	var count uint32
	ret := nvmlDeviceGetComputeRunningProcesses(device.handle, &count, nil)
	switch ret {
	case NVML_SUCCESS:
		if count == 0 {
			return nil, nil
		}
	case NVML_ERROR_INSUFFICIENT_SIZE:
		// expected, `count` now holds the required size
	default:
		return nil, fmt.Errorf("nvmlDeviceGetComputeRunningProcesses(%d) probe: %d", deviceID, ret)
	}

	// pad by a handful of entries in case a new process races in between the probe and the fill
	bufSize := count + 4
	buf := make([]nvmlProcessInfo, bufSize)
	count = bufSize
	ret = nvmlDeviceGetComputeRunningProcesses(device.handle, &count, buf)
	if ret != NVML_SUCCESS {
		return nil, fmt.Errorf("nvmlDeviceGetComputeRunningProcesses(%d) fill: %d", deviceID, ret)
	}

	out := make([]ProcessInfo, count)
	for i := 0; i < int(count); i++ {
		out[i] = ProcessInfo{
			PID:                int(buf[i].Pid),
			UsedGpuMemoryBytes: buf[i].UsedGpuMemory,
			GpuInstanceID:      buf[i].GpuInstanceId,
			ComputeInstanceID:  buf[i].ComputeInstanceId,
		}
	}
	return out, nil
}
