# kubectl-debug

A Kubernetes debugging tool that creates secure debug pods with non-root privileges.

## Features

- Creates debug pods with secure defaults
- Non-root execution
- Resource limits
- Security context configuration
- Easy to use CLI interface
- Process namespace sharing with target pods
- Label inheritance from target pods

## Installation

### Using Go

```bash
go install github.com/jbuet/kubectl-debug@latest
```

### Using Binary Releases

Download the latest release from the [releases page](https://github.com/jbuet/kubectl-debug/releases).

## Usage

The tool supports two main use cases:

### 1. Create a standalone debug pod

```bash
kubectl-debug -i <debug-image>
```

This creates a new debug pod in the default namespace. You can specify a different namespace with `-n`.

### 2. Create a debug pod targeting an existing pod

```bash
kubectl-debug -p <target-pod> -i <debug-image>
```

This creates a debug pod that:
- Shares process namespace with the target pod
- Inherits labels from the target pod
- Uses the specified debug image

### Flags

- `-n, --namespace`: Namespace for the debug pod (default: "default")
- `-p, --pod`: Name of the target pod (optional)
- `-i, --image`: Debug container image (required)
- `-a, --attach`: Automatically attach to the debug pod after creation
- `-r, --remove`: Remove the debug pod after the debug session (requires --attach)

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