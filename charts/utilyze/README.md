# utilyze Helm chart

This chart runs one Utilyze collector on each NVIDIA GPU node as a DaemonSet.
The collectors observe GPUs without reserving them, so GPU workloads can still
schedule normally.

## Requirements

- A working GPU setup: driver, container toolkit, and device plugin. The GPU
  Operator provides all three.
- The operator's dcgm-exporter disabled (`--set dcgmExporter.enabled=false`).
  Its profiling metrics hold the same GPU sampler Utilyze needs.
- A namespace at the `privileged` Pod Security level. The pods use `hostPID`
  and `SYS_ADMIN`.
- The image on your nodes or in a registry (`image.repository`, default
  `utilyze:dev` — see [docs/docker.md](../../docs/docker.md)).
- On OpenShift: `--set openshift.createSCC=true` (needs cluster-admin).

## Install

### GPU Operator cluster

The defaults are designed for GPU Operator clusters:

```sh
helm install utilyze ./charts/utilyze
```

### Toolkit-only cluster

For a cluster with the driver and nvidia-container-toolkit on the host, but no
operator or NFD:

```sh
helm install utilyze ./charts/utilyze --set nodeSelector=null
```

k3s v1.36 and newer automatically create the `nvidia` RuntimeClass when they
detect the toolkit.

On older k3s or plain containerd, an `nvidia` RuntimeClass object may not
exist. Create one with `handler: nvidia`. If the nvidia runtime is the
containerd default, use `--set runtimeClassName=""` instead.

### Airgapped cluster

Install from an internal mirror and disable metrics reporting:

```sh
helm install utilyze ./charts/utilyze \
  --set global.imageRegistry=registry.internal:5000 \
  --set metrics.enabled=false
```

## Verify the installation

Check the pods and their startup logs:

```sh
kubectl get pods -l app.kubernetes.io/name=utilyze -o wide
kubectl logs -l app.kubernetes.io/name=utilyze --tail=20
```

To view that node's live metrics from your workstation, keep the port-forward
running and connect with:

```sh
utlz --connect ws://127.0.0.1:8079/live
```

## Troubleshooting

### DCGM owns the sampler

The sampler is exclusive. In both directions, the first process to acquire it
keeps it:

- If Utilyze holds the sampler, the operator's dcgm-exporter GPU collector
  fails unrecoverably with "third-party Profiling module returned an
  unrecoverable error." It then silently serves **no GPU metrics until its pod
  restarts**.
- If DCGM holds the sampler, Utilyze pods fail their startup probe and are
  restarted repeatedly.
- Disable the operator's dcgm-exporter or remove its `DCGM_FI_PROF_*` fields.
  The Utilyze pods then recover automatically on their next restart.

### A rollout causes one extra restart

`maxSurge: 0` prevents two *running* pods on one node. However, the DaemonSet
controller creates the replacement as soon as the old pod is marked for
deletion. It does not wait for the old process to exit fully.

The replacement can start while the old process still holds the sampler. It
then fails its startup probe and is restarted; the restart acquires the
sampler cleanly. One extra restart during a rollout is normal.

*Permanent* contention, such as DCGM profiling, causes an indefinite
CrashLoopBackOff with rising restart counts. Fix the contention, not the pod.

## Reference

### Known limitations

- Per-workload attribution through vLLM model discovery currently assumes that
  the inference server is reachable on node-local `127.0.0.1`. This is true
  for `hostNetwork` inference pods and bare-metal servers. It is not true for
  pods with their own network namespace. Raw SOL/NVML metrics work in either
  case.
- Only one sampler can use each GPU. Do not run two releases on the same nodes.
  Do not run Nsight Compute or DCGM profiling alongside Utilyze.
- The Service load-balances across nodes. Use port-forward to reach a specific
  node's collector.
