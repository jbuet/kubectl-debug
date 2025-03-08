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

The tool supports three main use cases:

### 1. Create a standalone debug pod

```bash
kubectl-debug -it --image jbuet/debug:latest
```

This creates a new debug pod in the default namespace. You can specify a different namespace with `-n`.

### 2. Create a copy of an existing pod with debug container

```bash
kubectl-debug -p <target-pod> --copy -it --image jbuet/debug:latest
```

This creates a new pod that:
- Is a copy of the target pod
- Includes your debug container
- Shares process namespace
- Inherits labels from the target pod
- Uses the specified debug image

### 3. Add a debug container to an existing pod

```bash
kubectl-debug -p <target-pod> -it --image jbuet/debug:latest
```

This adds an ephemeral container to the target pod that:
- Shares process namespace with the target pod
- Uses the specified debug image
- Inherits security context from the target pod

### Flags

- `-n, --namespace`: Namespace for the debug pod (default: "default")
- `-p, --pod`: Name of the target pod (optional)
- `--image`: Debug container image (default: "jbuet/debug:latest")
- `-i, --stdin`: Keep stdin open even if not attached
- `-t, --tty`: Allocate a TTY for the container
- `--rm`: Remove the debug pod after the session ends
- `--copy`: Create a copy of the target pod instead of adding a container
- `--profile`: Security profile to use (general, restricted, baseline, privileged)
- `--memory-limit`: Memory limit for the debug container (default: "128Mi")
- `--cpu-request`: CPU request for the debug container (default: "100m")
- `--memory-request`: Memory request for the debug container (default: "128Mi")

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