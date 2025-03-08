# kubectl-debug

A Kubernetes debugging tool that creates secure debug pods with non-root privileges.

## Features

- Creates debug pods with secure defaults
- Non-root execution
- Resource limits
- Security context configuration
- Easy to use CLI interface

## Installation

### Using Go

```bash
go install github.com/jbuet/kubectl-debug@latest
```

### Using Binary Releases

Download the latest release from the [releases page](https://github.com/jbuet/kubectl-debug/releases).

## Usage

```bash
kubectl-debug -n namespace -p pod-name -i debug-image:tag
```

### Flags

- `-n, --namespace`: Namespace of the pod (default: "default")
- `-p, --pod`: Name of the pod to debug
- `-c, --container`: Name of the container to debug
- `-i, --image`: Debug container image
- `-t, --target`: Target pod name if different from original

## Security Features

- Runs as non-root user (UID 1000)
- Drops all capabilities
- Prevents privilege escalation
- Uses seccomp profile
- Resource limits enforced
- Liveness and readiness probes

## Building from Source

```bash
git clone https://github.com/jbuet/kubectl-debug
cd kubectl-debug
go build
```

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

[MIT License](LICENSE)