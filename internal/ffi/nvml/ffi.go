package nvml

import (
	"encoding/binary"
	"unsafe"
)

const (
	sym_nvmlInit_v2                             = "nvmlInit_v2"
	sym_nvmlShutdown                            = "nvmlShutdown"
	sym_nvmlDeviceGetCount_v2                   = "nvmlDeviceGetCount_v2"
	sym_nvmlDeviceGetHandleByIndex_v2           = "nvmlDeviceGetHandleByIndex_v2"
	sym_nvmlDeviceGetPcieThroughput             = "nvmlDeviceGetPcieThroughput"
	sym_nvmlDeviceGetUtilizationRates           = "nvmlDeviceGetUtilizationRates"
	sym_nvmlDeviceGetNvLinkState                = "nvmlDeviceGetNvLinkState"
	sym_nvmlDeviceGetFieldValues                = "nvmlDeviceGetFieldValues"
	sym_nvmlDeviceGetUUID                       = "nvmlDeviceGetUUID"
	sym_nvmlDeviceGetName                       = "nvmlDeviceGetName"
	sym_nvmlDeviceGetComputeRunningProcesses_v3 = "nvmlDeviceGetComputeRunningProcesses_v3"
	sym_nvmlDeviceGetComputeRunningProcesses_v2 = "nvmlDeviceGetComputeRunningProcesses_v2"
)

const (
	nvmlDeviceUUIDBufferSize = 96
	nvmlDeviceNameBufferSize = 96
)

// ref: https://docs.nvidia.com/deploy/nvml-api/group__nvmlDeviceEnumvs.html#group__nvmlDeviceEnumvs_1g06fa9b5de08c6cc716fbf565e93dd3d0
type nvmlReturn int

const (
	NVML_SUCCESS                 nvmlReturn = 0
	NVML_ERROR_UNINITIALIZED     nvmlReturn = 1
	NVML_ERROR_INVALID_ARGUMENT  nvmlReturn = 2
	NVML_ERROR_NOT_SUPPORTED     nvmlReturn = 3
	NVML_ERROR_NO_PERMISSION     nvmlReturn = 4
	NVML_ERROR_NOT_FOUND         nvmlReturn = 5
	NVML_ERROR_INSUFFICIENT_SIZE nvmlReturn = 7
)

type nvmlDeviceHandle unsafe.Pointer

type nvmlPcieUtilCounterType uint32

const (
	NVML_PCIE_UTIL_TX_BYTES nvmlPcieUtilCounterType = 0
	NVML_PCIE_UTIL_RX_BYTES nvmlPcieUtilCounterType = 1
)

type nvmlUtilization struct {
	GPU    uint32
	Memory uint32
}

const (
	NVML_FEATURE_DISABLED uint32 = 0
	NVML_FEATURE_ENABLED  uint32 = 1
)

// ref: https://docs.nvidia.com/deploy/nvml-api/unionnvmlValue__t.html#unionnvmlValue__t
type nvmlValue [8]byte

func (v nvmlValue) UllVal() uint64 {
	return binary.NativeEndian.Uint64(v[:])
}

type nvmlValueType uint32

const (
	NVML_VALUE_TYPE_DOUBLE nvmlValueType = 0
	NVML_VALUE_TYPE_UI     nvmlValueType = 1
	NVML_VALUE_TYPE_UL     nvmlValueType = 2
	NVML_VALUE_TYPE_ULL    nvmlValueType = 3
	NVML_VALUE_TYPE_SLL    nvmlValueType = 4
	NVML_VALUE_TYPE_SI     nvmlValueType = 5
	NVML_VALUE_TYPE_US     nvmlValueType = 6
)

type nvmlFieldId uint32

const (
	NVML_FI_DEV_NVLINK_THROUGHPUT_DATA_TX nvmlFieldId = 138
	NVML_FI_DEV_NVLINK_THROUGHPUT_DATA_RX nvmlFieldId = 139
)

// ref: https://github.com/NVIDIA/nvidia-settings/blob/bb364318e301b0702ab2a6f6a5e84321ee966e11/src/nvml.h#L2967
// DO NOT trust the NVML API reference guide struct layout, it's always in alphabetical order. Check the header...
type nvmlFieldValue struct {
	FieldId       nvmlFieldId
	ScopeId       uint32
	TimestampUsec int64
	LatencyUsec   int64
	ValueType     nvmlValueType
	NvmlReturn    nvmlReturn
	Value         nvmlValue
}

// ref: https://github.com/NVIDIA/nvidia-settings/blob/bb364318e301b0702ab2a6f6a5e84321ee966e11/src/nvml.h#L312
type nvmlProcessInfo struct {
	Pid               uint32
	_                 uint32 // UsedGpuMemory has ull 8-byte alignment, so we add 4 bytes of padding before
	UsedGpuMemory     uint64
	GpuInstanceId     uint32
	ComputeInstanceId uint32
}

var (
	nvmlInit_v2                   func() nvmlReturn
	nvmlShutdown                  func() nvmlReturn
	nvmlDeviceGetCount_v2         func(outCount *uint32) nvmlReturn
	nvmlDeviceGetHandleByIndex_v2 func(index uint32, device *nvmlDeviceHandle) nvmlReturn
	nvmlDeviceGetPcieThroughput   func(device nvmlDeviceHandle, counter nvmlPcieUtilCounterType, value *uint32) nvmlReturn
	nvmlDeviceGetUtilizationRates func(device nvmlDeviceHandle, utilization *nvmlUtilization) nvmlReturn
	nvmlDeviceGetNvLinkState      func(device nvmlDeviceHandle, link uint32, isActive *uint32) nvmlReturn
	nvmlDeviceGetFieldValues      func(device nvmlDeviceHandle, valuesCount int, values []nvmlFieldValue) nvmlReturn
	nvmlDeviceGetUUID             func(device nvmlDeviceHandle, uuid *byte, length uint32) nvmlReturn
	nvmlDeviceGetName             func(device nvmlDeviceHandle, name *byte, length uint32) nvmlReturn
	// nvmlDeviceGetComputeRunningProcesses is resolved at load time to either
	// _v3 (preferred) or _v2 (older driver fallback). Both accept the same
	// argument layout; v3 populates GpuInstanceId/ComputeInstanceId for MIG.
	nvmlDeviceGetComputeRunningProcesses func(device nvmlDeviceHandle, count *uint32, infos []nvmlProcessInfo) nvmlReturn
)
