# syntax=docker/dockerfile:1.7
# A few notes here:
# - builds on rockylinux:8 for glibc 2.28+ compat
# - uses gcc-12 for better C++17 support than rockylinux:8 default gcc-8
# - NVIDIA provides 64-bit desktop ARM support under sbsa not aarch64/arm64
#
# Targets:
# - export (native library tarball, used by CI):
#     docker buildx build --target export --output type=local,dest=./out \
#       -f docker/Dockerfile .
# - runtime (runnable utlz image, the default):
#     docker build -f docker/Dockerfile -t utilyze:dev .
#     docker run -d --gpus all --cap-add SYS_ADMIN --pid host --network host \
#       utilyze:dev
#   See docs/docker.md for what each run flag is needed for.

ARG CUDA_VERSION=13.1
ARG CUDA_PKG_SUFFIX=13-1
ARG GCC_TOOLSET=gcc-toolset-12

FROM rockylinux:8 AS builder

ARG TARGETARCH
ARG CUDA_VERSION
ARG CUDA_PKG_SUFFIX
ARG GCC_TOOLSET

ENV PATH=/opt/rh/${GCC_TOOLSET}/root/usr/bin:/usr/local/cuda-${CUDA_VERSION}/bin:${PATH} \
    LD_LIBRARY_PATH=/opt/rh/${GCC_TOOLSET}/root/usr/lib64

RUN set -eux; \
    case "$TARGETARCH" in \
      amd64) nv_arch=x86_64 ;; \
      arm64) nv_arch=sbsa ;; \
      *) echo "unsupported TARGETARCH: $TARGETARCH" >&2; exit 1 ;; \
    esac; \
    dnf -y install 'dnf-command(config-manager)'; \
    dnf config-manager --add-repo "https://developer.download.nvidia.com/compute/cuda/repos/rhel8/${nv_arch}/cuda-rhel8.repo"; \
    dnf -y install \
      "${GCC_TOOLSET}-gcc" \
      "${GCC_TOOLSET}-gcc-c++" \
      "${GCC_TOOLSET}-binutils" \
      gzip \
      make \
      tar \
      "cuda-driver-devel-${CUDA_PKG_SUFFIX}" \
      "cuda-cudart-devel-${CUDA_PKG_SUFFIX}" \
      "cuda-cupti-${CUDA_PKG_SUFFIX}"; \
    dnf clean all; \
    rm -rf /var/cache/dnf

WORKDIR /src
COPY . .

RUN set -eux; \
    make clean; \
    make -j"$(nproc)" \
      CUDA_VERSION="${CUDA_VERSION}" \
      dist-tarball

FROM scratch AS export
COPY --from=builder /src/dist/libutlz_sampler-*.tar.gz /

# make utlz embeds the native sampler; CGO_ENABLED=0 keeps the binary static
# so it runs on the el8 runtime base.
FROM golang:1.25-bookworm AS go-builder

ENV CGO_ENABLED=0

WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
COPY --from=builder /src/dist/ dist/

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    set -eux; \
    make utlz; \
    install -m 0755 dist/utlz-linux-* /usr/local/bin/utlz; \
    /usr/local/bin/utlz --version

# Stage the two PerfWorks libs (dereferenced) so the runtime image doesn't
# drag all of CUDA along.
FROM builder AS runtime-libs
RUN set -eux; \
    mkdir -p /staging/lib; \
    for lib in libnvperf_host.so libnvperf_target.so; do \
        for src in /usr/local/cuda-*/extras/CUPTI/lib64/"$lib" \
                   /usr/local/cuda-*/targets/*/lib/"$lib"; do \
            if [ -e "$src" ]; then cp -L "$src" /staging/lib/; break; fi; \
        done; \
        test -e "/staging/lib/$lib"; \
    done

# el8 base matches the glibc 2.28 build target. Driver libs are injected at
# run time by the NVIDIA Container Toolkit, never baked in. Drivers: R580+.
FROM rockylinux:8-minimal AS runtime

RUN microdnf -y install libstdc++ ca-certificates && microdnf clean all && \
    mkdir -p /var/lib/utilyze && chmod 1777 /var/lib/utilyze

COPY --from=go-builder /usr/local/bin/utlz /usr/local/bin/utlz
COPY --from=runtime-libs /staging/lib/ /opt/utilyze/lib/

# No NVIDIA_VISIBLE_DEVICES: baking it conflicts with CDI injection (see
# docs/docker.md). NVIDIA_DRIVER_CAPABILITIES only shapes the legacy hook.
ENV HOME=/var/lib/utilyze \
    LD_LIBRARY_PATH=/opt/utilyze/lib \
    NVIDIA_DRIVER_CAPABILITIES=compute,utility \
    UTLZ_SERVICE_MODE=server \
    UTLZ_HOST=0.0.0.0 \
    UTLZ_DISABLE_UPDATE_CHECK=1

EXPOSE 8079

ENTRYPOINT ["/usr/local/bin/utlz"]
