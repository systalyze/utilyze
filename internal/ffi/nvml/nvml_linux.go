//go:build linux

package nvml

import (
	"errors"
	"fmt"
	"sync"

	"github.com/ebitengine/purego"
)

// NOTE: release versions of NVML should likely always have a SONAME of "libnvidia-ml.so.1"
// but I have seen some development versions/container images without the major version
var candidatePaths = []string{"libnvidia-ml.so.1", "libnvidia-ml.so"}

var loadOnce sync.Once
var loadErr error

func load() error {
	loadOnce.Do(func() {
		var lib uintptr
		for _, path := range candidatePaths {
			var openErr error
			lib, openErr = purego.Dlopen(path, purego.RTLD_NOW)
			if openErr == nil {
				break
			}
			loadErr = errors.Join(loadErr, openErr)
		}
		if lib == 0 {
			return
		}

		defer func() {
			if err := recover(); err != nil {
				closeErr := purego.Dlclose(lib)
				loadErr = errors.Join(fmt.Errorf("failed to load symbols: %v", err), closeErr)
			}
		}()
		loadSymbols(lib)
	})
	return loadErr
}

func loadSymbols(lib uintptr) {
	purego.RegisterLibFunc(&nvmlInit_v2, lib, sym_nvmlInit_v2)
	purego.RegisterLibFunc(&nvmlShutdown, lib, sym_nvmlShutdown)
	purego.RegisterLibFunc(&nvmlDeviceGetCount_v2, lib, sym_nvmlDeviceGetCount_v2)
	purego.RegisterLibFunc(&nvmlDeviceGetHandleByIndex_v2, lib, sym_nvmlDeviceGetHandleByIndex_v2)
	purego.RegisterLibFunc(&nvmlDeviceGetPcieThroughput, lib, sym_nvmlDeviceGetPcieThroughput)
	if _, err := purego.Dlsym(lib, sym_nvmlDeviceGetUtilizationRates); err == nil {
		purego.RegisterLibFunc(&nvmlDeviceGetUtilizationRates, lib, sym_nvmlDeviceGetUtilizationRates)
	}
	purego.RegisterLibFunc(&nvmlDeviceGetNvLinkState, lib, sym_nvmlDeviceGetNvLinkState)
	purego.RegisterLibFunc(&nvmlDeviceGetFieldValues, lib, sym_nvmlDeviceGetFieldValues)
	purego.RegisterLibFunc(&nvmlDeviceGetUUID, lib, sym_nvmlDeviceGetUUID)
	purego.RegisterLibFunc(&nvmlDeviceGetName, lib, sym_nvmlDeviceGetName)

	if _, err := purego.Dlsym(lib, sym_nvmlDeviceGetComputeRunningProcesses_v3); err == nil {
		purego.RegisterLibFunc(&nvmlDeviceGetComputeRunningProcesses, lib,
			sym_nvmlDeviceGetComputeRunningProcesses_v3)
	} else if _, err := purego.Dlsym(lib, sym_nvmlDeviceGetComputeRunningProcesses_v2); err == nil {
		purego.RegisterLibFunc(&nvmlDeviceGetComputeRunningProcesses, lib,
			sym_nvmlDeviceGetComputeRunningProcesses_v2)
	}
}
