# Running Utilyze in Docker

## Build

Build the image from the repo:

```sh
git clone https://github.com/systalyze/utilyze.git && cd utilyze
docker build -f docker/Dockerfile -t utilyze:dev .
```

## Run

```sh
docker run -d --name utilyze \
  --gpus all \
  --cap-add SYS_ADMIN \
  --pid host \
  --network host \
  utilyze:dev
```

View the metrics from any machine with `utlz`:

```sh
utlz --connect ws://<host>:8079/live
```

The container runs in `server` mode and listens on `0.0.0.0:8079`.

### Required flags

Each flag enables a different part of monitoring:

| Flag | Purpose | Without it |
|---|---|---|
| `--gpus all` | Tells the NVIDIA Container Toolkit to inject driver libraries and `/dev/nvidia*` | No GPUs are visible |
| `--cap-add SYS_ADMIN` | Allows access to GPU performance counters, which NVIDIA restricts to privileged processes by default | Only NVML metrics are available; no compute/memory SOL |
| `--pid host` | Allows the workload scanner to see host processes behind GPU PIDs | No per-workload attribution |
| `--network host` | Allows the scanner to probe discovered inference servers on `127.0.0.1:<port>`, and makes the metrics service reachable on the host | No model discovery or metrics access |

## Common variations

### Select specific GPUs

Narrow `--gpus all` to `--gpus '"device=2,3"'`, or use
`-e NVIDIA_VISIBLE_DEVICES=2,3`. The `utlz --devices` indices are relative to
the GPUs visible inside the container.

### Kubernetes

A Helm chart with its own guide is at
[charts/utilyze](../charts/utilyze/README.md).

### Use Compose, systemd, or Quadlet

Ready-made service definitions for production hosts are in `deploy/`:

- `docker-compose.yml` (`docker-compose.airgapped.yml` for hosts without
  internet access)
- `systemd/utilyze.service` (Docker under systemd)
- `quadlet/utilyze.container` (Podman under systemd on RHEL-family hosts)

### Run with Podman and CDI

Generate a CDI spec once on the host:

```sh
sudo nvidia-ctk cdi generate --output=/etc/cdi/nvidia.yaml
```

Then run:

```sh
sudo podman run -d --name utilyze \
  --device nvidia.com/gpu=all \
  --cap-add SYS_ADMIN --pid host --network host \
  --security-opt label=disable \
  utilyze:dev
```

On RHEL-family hosts:

- **Run Podman as root for profiling.** Rootless Podman cannot grant real
  `CAP_SYS_ADMIN` over host GPU resources. You can instead set
  `NVreg_RestrictProfilingToAdminUsers=0` on the host and drop the capability.
- **Allow access through SELinux.** SELinux blocks the injected driver
  libraries by default. Use `--security-opt label=disable` as shown above, or
  install the `nvidia-container-toolkit` SELinux policy module.

### Run without `SYS_ADMIN`

You can lift the profiling restriction for the whole host instead of using
`--cap-add SYS_ADMIN`: set `NVreg_RestrictProfilingToAdminUsers=0` in the
NVIDIA driver options. The container then needs no added capabilities.

### Airgapped hosts

Build or download the image tarball on an internet-connected machine, copy it
over, and `docker load` it. See [offline-install.md](offline-install.md).
Set `UTLZ_DISABLE_METRICS=1` to disable metrics reporting.

## Reference

### Image internals

The image contains the `utlz` binary with the native sampler embedded, plus
the PerfWorks libraries (`libnvperf_host.so`, `libnvperf_target.so`) it needs
at run time. Driver-coupled libraries (`libcuda.so.1`, `libnvidia-ml.so.1`)
and `/dev/nvidia*` are not in the image: the NVIDIA Container Toolkit injects
them at run time. The supported driver floor is R580+ (the bundled PerfWorks
libraries are CUDA 13.1-era).

### Sampler exclusivity

Only one profiler can sample a given GPU at a time. Do not run two Utilyze
instances, or Utilyze alongside Nsight Compute or DCGM profiling metrics
(`DCGM_FI_PROF_*`), against the same GPU.
