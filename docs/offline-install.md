# Offline / airgapped installation

These instructions are for machines without internet access. The installer requires internet at install time, and `utlz` will also try to download CUPTI from PyPI on first run if it can't find it locally — both need to be handled manually.

## Kubernetes installation (Helm)

This is the easiest path for an airgapped cluster. Two things cross the
airgap: the image tarball and the chart (both come from the repo).

On an internet-connected machine:

```bash
git clone https://github.com/systalyze/utilyze.git && cd utilyze
docker build -f docker/Dockerfile -t utilyze:dev .
docker save utilyze:dev | gzip > utilyze-image.tar.gz
```

Copy `utilyze-image.tar.gz` and the `charts/utilyze` directory to the
airgapped environment. Load the image into your internal registry:

```bash
docker load -i utilyze-image.tar.gz
docker tag utilyze:dev registry.internal/utilyze:dev
docker push registry.internal/utilyze:dev
```

Install the chart with metrics reporting disabled, so the collectors make no
outbound connections:

```bash
helm install utilyze ./charts/utilyze \
  --set global.imageRegistry=registry.internal \
  --set metrics.enabled=false
```

The chart README covers the overrides for your cluster type, such as
`nodeSelector` on clusters without the GPU Operator.

## Docker installation (single host)

If the airgapped machine runs Docker or Podman, use the container image. It
contains the binary and the CUPTI/PerfWorks libraries, so you only need one
tarball.

On an internet-connected machine, build the image and save it:

```bash
git clone https://github.com/systalyze/utilyze.git && cd utilyze
docker build -f docker/Dockerfile -t utilyze:<version> .
docker save utilyze:<version> | gzip > utilyze-image.tar.gz
```

Copy the tarball to the airgapped machine and load it:

```bash
docker load -i utilyze-image.tar.gz
```

Then follow [docker.md](docker.md) to run the image, or use the airgapped
Compose file with your tag:

```bash
UTILYZE_IMAGE=utilyze:<version> docker compose -f deploy/docker-compose.airgapped.yml up -d
``` Set
`UTLZ_DISABLE_METRICS=1` to disable metrics reporting.

## Bare-metal installation

**You may not need the CUPTI files.** If the airgapped machine already has the CUDA toolkit or the `nvidia-cuda-cupti-cu12` pip package, `utlz` will find CUPTI on its own — skip step 1b and the `cupti-<version>` steps below and just copy the binary.

<details>
<summary>Check whether CUPTI is already present</summary>

Run this on the airgapped machine — it checks the CUDA toolkit, the `nvidia-cuda-cupti-cu12` pip package, and the loader cache. If anything prints, CUPTI is already available:

```bash
ldconfig -p | grep libnvperf_host
ls /usr/local/cuda*/targets/*/lib/libnvperf_host.so \
   /usr/lib/python*/*-packages/nvidia/cuda_cupti/lib/libnvperf_host.so \
   /usr/local/lib/python*/*-packages/nvidia/cuda_cupti/lib/libnvperf_host.so \
   ~/.local/lib/python*/site-packages/nvidia/cuda_cupti/lib/libnvperf_host.so 2>/dev/null
```

</details>

## 1. On an internet-connected machine, gather two files

**a. The `utlz` binary** — from the [latest release](https://github.com/systalyze/utilyze/releases/latest):

- DGX / x86_64: `utlz-linux-amd64`
- Grace / aarch64: `utlz-linux-arm64`

**b. CUPTI 12+** — download the latest `nvidia-cuda-cupti-cu12` wheel from PyPI for your airgapped machine's architecture:

```bash
# DGX / x86_64
pip3 download nvidia-cuda-cupti-cu12 --no-deps --only-binary=:all: \
  --platform manylinux_2_25_x86_64 --python-version 3 -d .

# Grace / aarch64
pip3 download nvidia-cuda-cupti-cu12 --no-deps --only-binary=:all: \
  --platform manylinux_2_25_aarch64 --python-version 3 -d .
```

If `pip` isn't available, you can browse [the file list on PyPI](https://pypi.org/project/nvidia-cuda-cupti-cu12/#files) and download the matching wheel manually.

Extract the two `.so` files you need (replace `<version>` with the version in the wheel filename, e.g. `12.9.79`):

```bash
unzip -j nvidia_cuda_cupti_cu12-<version>-*.whl \
  'nvidia/cuda_cupti/lib/libnvperf_host.so' \
  'nvidia/cuda_cupti/lib/libnvperf_target.so' \
  -d ./cupti
```

Copy `utlz-linux-{amd64,arm64}` and the `cupti/` directory to the airgapped machine.

## 2. On the airgapped machine, install

Pick one of the two options below. The commands use `utlz-linux-amd64`; on Grace / aarch64, substitute `utlz-linux-arm64`.

### Option 1: Run with `sudo` every time (recommended if you have sudo)

```bash
sudo install -m 0755 utlz-linux-amd64 /usr/local/bin/utlz
sudo ln -sf utlz /usr/local/bin/utilyze
sudo mkdir -p /root/.cache/utlz/cupti-<version>
sudo cp cupti/libnvperf_host.so cupti/libnvperf_target.so /root/.cache/utlz/cupti-<version>/
```

Run:

```bash
sudo utlz
```

### Option 2: Run without `sudo` (requires one-time host admin setup)

NVIDIA restricts GPU profiling counters to root by default. A host admin can disable this restriction once, after which any user on the box can run `utlz` without `sudo`.

Host admin step (one time only, requires sudo):

```bash
echo 'options nvidia NVreg_RestrictProfilingToAdminUsers=0' | sudo tee /etc/modprobe.d/nvidia-profiling.conf
# either reboot, or reload the driver (stops all GPU processes):
sudo modprobe -rf nvidia_uvm nvidia_drm nvidia_modeset nvidia && sudo modprobe nvidia
```

End user install (no sudo):

```bash
mkdir -p ~/.local/bin
install -m 0755 utlz-linux-amd64 ~/.local/bin/utlz
ln -sf utlz ~/.local/bin/utilyze
# make sure ~/.local/bin is on your PATH

mkdir -p ~/.cache/utlz/cupti-<version>
cp cupti/libnvperf_host.so cupti/libnvperf_target.so ~/.cache/utlz/cupti-<version>/
```

Run:

```bash
utlz
```

## 3. Verify

```bash
utlz --version
```

Then start it (`sudo utlz` or `utlz` depending on path). You should see the live monitoring TUI. If `utlz` complains it can't find CUPTI, double-check that `libnvperf_host.so` and `libnvperf_target.so` are sitting directly inside the `cupti-<version>/` directory under the cache of the user that's running `utlz` (`/root/.cache/utlz/cupti-<version>/` for `sudo`, `~/.cache/utlz/cupti-<version>/` otherwise).
