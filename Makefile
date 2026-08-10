CXX = g++
CXX_FLAGS = -std=c++17 -O2 -fPIC -Wall -Wno-missing-field-initializers -ffunction-sections -fdata-sections \
	-Wl,-s -Wl,--gc-sections -Wl,--exclude-libs,ALL
DEP_FLAGS = -MMD -MP

PLATFORM ?= $(shell uname -m)
OS ?= $(shell echo $(shell uname -s) | tr '[:upper:]' '[:lower:]')

VERSION := $(shell cat VERSION 2>/dev/null || echo 0.0.0)
VERSION_MAJOR := $(firstword $(subst ., ,$(VERSION)))
LIB_NAME := libutlz_sampler
SONAME := $(LIB_NAME).so.$(VERSION_MAJOR)
TARGET := dist/$(LIB_NAME).so.$(VERSION)
EMBEDDED_SAMPLER := internal/ffi/sampler/embedded/libutlz_sampler.so.$(VERSION_MAJOR)
GOOS ?= $(shell go env GOOS 2>/dev/null || echo $(OS))
GOARCH ?= $(shell go env GOARCH 2>/dev/null)
REPO ?= systalyze/utilyze

ifeq ($(PLATFORM), x86_64)
    CUDA_TARGET ?= x86_64-linux
else ifeq ($(PLATFORM), aarch64)
    CUDA_TARGET ?= sbsa-linux
endif

ifeq ($(PLATFORM), x86_64)
    ARCH = amd64
else ifeq ($(PLATFORM), aarch64)
    ARCH = arm64
endif

CUDA_VERSION ?= 13.1
CUDA_PKG_SUFFIX ?= $(subst .,-,$(CUDA_VERSION))
CUDA_INCLUDE ?= /usr/local/cuda-$(CUDA_VERSION)/targets/$(CUDA_TARGET)/include
CUDA_LIB ?= /usr/local/cuda-$(CUDA_VERSION)/targets/$(CUDA_TARGET)/lib
CUDA_CUPTI_LIB ?= /usr/local/cuda-$(CUDA_VERSION)/extras/CUPTI/lib64
CUDA_RUNTIME_LIB_PATH := $(CUDA_LIB):$(CUDA_CUPTI_LIB)
DOCKER ?= docker
DOCKERFILE ?= docker/Dockerfile
DOCKER_PLATFORM ?= linux/$(ARCH)

NATIVE_INCLUDE = native/include
NATIVE_SRC = native/src
NVPW_REDIST = native/third_party/nsight-perf-sdk/redist
NVPW_INCLUDE = $(NVPW_REDIST)/include
NVPW_UTIL_INCLUDE = $(NVPW_REDIST)/NvPerfUtility/include

SOURCES = \
	$(NATIVE_SRC)/sampler_common.cpp \
	$(NATIVE_SRC)/sampler_engine.cpp \
	$(NATIVE_SRC)/sampler_state.cpp \
	$(NATIVE_SRC)/sampler_multi.cpp \
	$(NATIVE_SRC)/sampler_api.cpp

OBJ_DIR = build
OBJECTS = $(SOURCES:$(NATIVE_SRC)/%.cpp=$(OBJ_DIR)/%.o)
DEPS = $(OBJECTS:.o=.d)

TEST_NATIVE_DIR = tests/native
TEST_NATIVE_BIN_DIR = $(OBJ_DIR)/tests/native
TEST_INCLUDES = $(INCLUDES) -I$(TEST_NATIVE_DIR)
NATIVE_TEST_BIN = $(TEST_NATIVE_BIN_DIR)/native_tests
NATIVE_TEST_SOURCES = \
	$(TEST_NATIVE_DIR)/native_tests.cpp \
	$(TEST_NATIVE_DIR)/stubs_sampler_engine.cpp \
	$(NATIVE_SRC)/sampler_common.cpp \
	$(NATIVE_SRC)/sampler_state.cpp \
	$(NATIVE_SRC)/sampler_multi.cpp \
	$(NATIVE_SRC)/sampler_api.cpp
SMOKE_TEST_BIN = $(TEST_NATIVE_BIN_DIR)/smoke_utlz_sampler
FULL_SMOKE_TEST_BIN = $(TEST_NATIVE_BIN_DIR)/smoke_utlz_sampler_full_rotation
FULL_SMOKE_BURN_BIN = $(TEST_NATIVE_BIN_DIR)/cuda_burn

INCLUDES = -I$(NVPW_INCLUDE) -I$(NVPW_UTIL_INCLUDE) -I$(CUDA_INCLUDE) -I$(NATIVE_INCLUDE)
LIBS = -L$(CUDA_LIB) -L$(CUDA_CUPTI_LIB) -L$(CUDA_LIB)/stubs -lnvperf_host -lnvperf_target -lcuda -ldl -lpthread

.PHONY: all clean check-native-platform native test-native-unit test-native-smoke test-native-smoke-full dist-tarball dist-tarball-docker utlz

all: native
	$(MAKE) utlz

check-native-platform:
	@if [ "$(OS)" != "linux" ]; then \
		echo "Unsupported OS for native build: $(OS)"; \
		exit 1; \
	fi
	@case "$(PLATFORM)" in \
		x86_64|aarch64) ;; \
		*) echo "Unsupported platform for native build: $(PLATFORM)"; exit 1 ;; \
	esac

native: $(TARGET)

$(TARGET): $(OBJECTS) | check-native-platform
	@mkdir -p $(dir $@)
	$(CXX) $(CXX_FLAGS) -shared -Wl,-soname,$(SONAME) -static-libstdc++ -static-libgcc -Wl,--as-needed -o $@ $(OBJECTS) $(LIBS)
	ln -sf $(LIB_NAME).so.$(VERSION) dist/$(SONAME)
	ln -sf $(SONAME) dist/$(LIB_NAME).so

$(OBJ_DIR)/%.o: $(NATIVE_SRC)/%.cpp | check-native-platform
	@mkdir -p $(OBJ_DIR)
	$(CXX) $(CXX_FLAGS) $(DEP_FLAGS) $(INCLUDES) -c $< -o $@

$(NATIVE_TEST_BIN): $(NATIVE_TEST_SOURCES) | check-native-platform
	@mkdir -p $(TEST_NATIVE_BIN_DIR)
	$(CXX) $(CXX_FLAGS) $(TEST_INCLUDES) -o $@ $(NATIVE_TEST_SOURCES) $(LIBS)

$(SMOKE_TEST_BIN): $(TEST_NATIVE_DIR)/smoke_utlz_sampler.cpp $(TARGET) | check-native-platform
	@mkdir -p $(TEST_NATIVE_BIN_DIR)
	$(CXX) $(CXX_FLAGS) $(TEST_INCLUDES) -o $@ $(TEST_NATIVE_DIR)/smoke_utlz_sampler.cpp $(TARGET) -Wl,-rpath,$(CURDIR)/dist $(LIBS)

$(FULL_SMOKE_TEST_BIN): $(TEST_NATIVE_DIR)/smoke_utlz_sampler_full_rotation.cpp $(TARGET) | check-native-platform
	@mkdir -p $(TEST_NATIVE_BIN_DIR)
	$(CXX) $(CXX_FLAGS) $(TEST_INCLUDES) -o $@ $(TEST_NATIVE_DIR)/smoke_utlz_sampler_full_rotation.cpp $(TARGET) -Wl,-rpath,$(CURDIR)/dist $(LIBS)

$(FULL_SMOKE_BURN_BIN): $(TEST_NATIVE_DIR)/cuda_burn.cu | check-native-platform
	@mkdir -p $(TEST_NATIVE_BIN_DIR)
	nvcc -O3 -o $@ $(TEST_NATIVE_DIR)/cuda_burn.cu

test-native-unit: $(NATIVE_TEST_BIN)
	./$(NATIVE_TEST_BIN)

test-native-smoke: $(SMOKE_TEST_BIN)
	@if ps -eo comm,args | rg -q '(^|/)(nsys|ncu|nvprof)( |$$)|\bnv-hostengine\b'; then \
		echo "native smoke skipped: profiler processes currently active on this shared machine"; \
	else \
		set +e; \
		sudo -n env LD_LIBRARY_PATH="$(CURDIR)/dist:$(CUDA_RUNTIME_LIB_PATH):$$LD_LIBRARY_PATH" ./$(SMOKE_TEST_BIN); \
		status=$$?; \
		if [ $$status -eq 77 ]; then \
			echo "native smoke skipped: PM resource unavailable"; \
		elif [ $$status -ne 0 ]; then \
			exit $$status; \
		fi; \
	fi

test-native-smoke-full: $(FULL_SMOKE_TEST_BIN) $(FULL_SMOKE_BURN_BIN)
	@device="$${UTLZ_TEST_DEVICE:-0}"; \
	if ps -eo comm,args | rg -q '(^|/)(nsys|ncu|nvprof)( |$$)|\bnv-hostengine\b'; then \
		echo "native full-rotation smoke skipped: profiler processes currently active on this shared machine"; \
	elif ! command -v nvcc >/dev/null 2>&1; then \
		echo "native full-rotation smoke skipped: nvcc not available"; \
	else \
		set +e; \
		./$(FULL_SMOKE_BURN_BIN) $$device 12 >/tmp/utlz-sampler-full-smoke-burn.log 2>&1 & \
		burn_pid=$$!; \
		sleep 1; \
		sudo -n env UTLZ_PERF_TRIGGER_MODE=cpu UTLZ_PERF_TRIGGERS_PER_PASS=2 UTLZ_PERF_TRIGGER_SPACING_MS=120 UTLZ_PERF_PUBLISH_WARMUP_ROTATIONS=1 LD_LIBRARY_PATH="$(CURDIR)/dist:$(CUDA_RUNTIME_LIB_PATH):$$LD_LIBRARY_PATH" ./$(FULL_SMOKE_TEST_BIN) $$device; \
		status=$$?; \
		kill $$burn_pid >/dev/null 2>&1 || true; \
		wait $$burn_pid >/dev/null 2>&1 || true; \
		if [ $$status -eq 77 ]; then \
			echo "native full-rotation smoke skipped: PM resource unavailable"; \
		elif [ $$status -ne 0 ]; then \
			exit $$status; \
		fi; \
	fi

dist-tarball: $(TARGET) | check-native-platform
	$(eval PKG_DIR := dist/$(LIB_NAME)-$(VERSION)-$(OS)-$(ARCH))
	@rm -rf $(PKG_DIR)
	@mkdir -p $(PKG_DIR)
	cp $(TARGET) $(PKG_DIR)/
	cd $(PKG_DIR) && ln -sf $(LIB_NAME).so.$(VERSION) $(SONAME)
	cd $(PKG_DIR) && ln -sf $(SONAME) $(LIB_NAME).so
	cp native/include/utlz_sampler.h $(PKG_DIR)/
	cd dist && tar czf $(LIB_NAME)-$(VERSION)-$(OS)-$(ARCH).tar.gz $(LIB_NAME)-$(VERSION)-$(OS)-$(ARCH)/
	rm -rf $(PKG_DIR)
	@echo "Packaged: dist/$(LIB_NAME)-$(VERSION)-$(OS)-$(ARCH).tar.gz"

dist-tarball-docker: | check-native-platform
	@mkdir -p dist
	$(DOCKER) buildx build \
		--platform $(DOCKER_PLATFORM) \
		--file $(DOCKERFILE) \
		--target export \
		--build-arg CUDA_VERSION=$(CUDA_VERSION) \
		--build-arg CUDA_PKG_SUFFIX=$(CUDA_PKG_SUFFIX) \
		--output type=local,dest=$(CURDIR)/dist \
		.
	@echo "Packaged: dist/$(LIB_NAME)-$(VERSION)-$(OS)-$(ARCH).tar.gz"

-include $(DEPS)

utlz:
	@mkdir -p dist $(dir $(EMBEDDED_SAMPLER))
	@if [ -f "$(TARGET)" ]; then \
		cp "$(TARGET)" "$(EMBEDDED_SAMPLER)"; \
	else \
		echo "warning: $(TARGET) not found; skipping native sampler embed.  Run 'make native' to build the native sampler if you are on linux amd64/arm64."; \
	fi
	go build \
		-trimpath \
		-ldflags="-s -w -X github.com/systalyze/utilyze/internal/version.VERSION=$(VERSION) -X github.com/systalyze/utilyze/internal/ffi/sampler.SHA256SUM=$$(if [ -f "$(EMBEDDED_SAMPLER)" ]; then sha256sum "$(EMBEDDED_SAMPLER)" | cut -d' ' -f1; fi) -X github.com/systalyze/utilyze/internal/version.REPO=$(REPO)" \
		-o dist/utlz-$(GOOS)-$(GOARCH) \
		cmd/main.go

clean:
	rm -f dist/$(LIB_NAME).so* dist/$(LIB_NAME)-*.tar.gz dist/sha256sums.txt
	rm -rf $(OBJ_DIR)
