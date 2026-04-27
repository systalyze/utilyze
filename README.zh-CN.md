# Utilyze
​
Utilyze 是一款 GPU 利用率分析工具。它关注的不是 GPU 是否处于活跃状态，而是 GPU 的算力有多少真正用于有效计算。

![utlz in action](./assets/utlz.png)

常见的 GPU 利用率工具，例如 `nvidia-smi` 和 `nvtop`，通常只能反映 GPU 是否处于忙碌状态，而不能准确说明硬件资源被实际使用了多少。即使一个任务只使用了很小一部分实际算力，这些工具也可能显示接近 100% 的利用率。

Utilyze 是一款由 [Systalyze](https://systalyze.com) 开发的全新工具。Utilyze通过直接读取 GPU 底层性能计数器，从而测量硬件资源到底实际用了多少；同时，Utilyze还会结合当前运行的模型和硬件配置去推测在现有条件下利用率的上限值。想了解更多的实现细节，可以参考[我们的博客文章](https://systalyze.com/utilyze)。

**其他语言版本：** [English](./README.md)

## 环境要求

- Linux amd64（arm64 支持仍处于实验阶段）
- NVIDIA Ampere 或更新架构的 GPU，例如 A100、H100、H200、B200、RTX 3000 系列及更新型号
- CUDA Toolkit 11.0+
- `sudo` 或 `CAP_SYS_ADMIN` 权限；也可以运行在 privileged container 中

## 安装

```bash
curl -sSfL https://systalyze.com/utilyze/install.sh | sh
```

根据宿主机配置，Utilyze 可能需要 root 权限才能访问 GPU 性能计数器。安装脚本会提示您输入密码，用于完成系统级安装。

如果系统中没有 CUPTI 12+，`utlz` 会在第一次运行时提示您从 PyPI 安装最新版本。

## 快速开始
```bash
# 监控所有 GPU
sudo utlz

# 只监控指定 GPU
sudo utlz --devices 0,2

# 查看每张 GPU 上发现的推理服务
sudo utlz --endpoints
```

由于 NVIDIA Perf SDK 的限制，同一张 GPU 同一时间只能被一个utlz实例监控。

## SOL 上限估算

Utilyze 会自动发现正在运行的推理服务，并识别每张 GPU 上加载的模型。结合模型和 GPU 配置后，Utilyze 会估算这个服务在当前硬件上理论上还能达到的 SOL 上限。

这个上限不是简单套用 GPU 的峰值算力，而是根据当前模型和硬件配置算出来的，因此更接近实际情况。它可以帮助您判断：当前服务是已经接近硬件极限，还是还有明显优化空间。

目前 Utilyze 只支持 vLLM。后续会支持更多推理后端，例如 SGLang。模型和硬件的支持范围也会持续扩展。目前支持节点内的部分模型，以及 H100-80G 和 A100-80G GPU，最多支持 8 张卡。

为了完成这项估算，Utilyze 会匿名发送 GPU 配置信息到 Systalyze 的服务器。如果不希望发送这些信息，可以设置：

```bash
export UTLZ_DISABLE_METRICS=1
```

## 如何免sudo运行？

默认情况下，NVIDIA 只允许管理员用户访问 GPU 性能计数器。如果您希望普通用户也能直接运行 `utlz`，则需要在宿主机上关闭这个限制，然后重启：

```bash
echo 'options nvidia NVreg_RestrictProfilingToAdminUsers=0' | sudo tee /etc/modprobe.d/nvidia-profiling.conf
sudo reboot
```

重启后，`utlz` 就可以无需sudo运行。

如果 `utlz` 启动时提示缺少相关权限，您也可以通过下面的环境变量关闭这条警告：

```bash
export UTLZ_DISABLE_PROFILING_WARNING=1
```

## 参数选项

下面这些命令行参数大多也可以通过环境变量设置：

- `--endpoints`：显示每张 GPU 上发现的推理服务
- `--devices` / `UTLZ_DEVICES`：指定要监控的 GPU，多个设备编号用逗号分隔
- `--log` / `UTLZ_LOG`：将日志写入指定文件；默认不写日志
- `--log-level` / `UTLZ_LOG_LEVEL`：设置日志级别；默认是 `INFO`，可选 `DEBUG`、`WARN`、`ERROR`
- `--version`：显示版本号

以下选项只能通过环境变量设置：

- `UTLZ_HIGH_CONTRAST`：启用高对比度模式；默认开启
- `UTLZ_DISABLE_PROFILING_WARNING`：关闭启动时的 GPU 性能计数器权限警告
- `UTLZ_BACKEND_URL`：设置 Systalyze roofline/SOL 指标 API 的后端地址；默认是 `https://api.systalyze.com/v1/utilyze`
- `UTLZ_DISABLE_METRICS`：关闭负载检测和 Systalyze roofline/SOL 指标 API 调用

## 从源码构建

从源码构建需要准备：

- Go 1.25+，用于构建 CLI
- Docker，用于构建兼容性更好的原生库
- CUDA Toolkit；默认使用 13.1，也可以通过 `CUDA_VERSION` 指定

```bash
# 构建原生库和 CLI
make all

# 使用 Docker 构建并打包原生库
make dist-tarball-docker

# 只构建 CLI
make utlz
```

目前我们还在测试 ARM64 环境下的构建支持，使用的是 CUDA 的 `sbsa-linux` target。
