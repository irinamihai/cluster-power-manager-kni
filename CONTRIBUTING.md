# Contributing to Cluster Power Manager

Thank you for your interest in contributing to the Cluster Power Manager
project! This document provides guidelines and information for contributors.

## Code of Conduct

This project adheres to the [Contributor Covenant Code of Conduct](./CODE_OF_CONDUCT.md).
By participating, you are expected to uphold this code. Please report
unacceptable behavior to the project maintainers.

## How to Contribute

### Reporting Issues

If you find a bug or have a feature request, please open a
[GitHub Issue](https://github.com/cluster-power-manager/cluster-power-manager/issues).
When reporting a bug, include:

* A clear and descriptive title
* Steps to reproduce the issue
* Expected behavior and actual behavior
* Your environment details (Kubernetes version, OS, CPU architecture, etc.)
* Any relevant logs or error messages

### Submitting Pull Requests

1. Fork the repository and create a branch from `main`.
2. Make your changes, following the project's coding style.
3. Add or update tests as appropriate.
4. Ensure all tests pass (see [Development Workflow](#development-workflow)
   below).
5. Submit a pull request with a clear description of the changes.

When submitting a pull request:

* Keep changes focused - one logical change per pull request.
* Write clear, descriptive commit messages.
* Reference any related issues in the PR description.
* Be responsive to review feedback.

### Development Workflow

This project is a Kubernetes Operator built with Go. It uses an image-based
build system and requires a Linux host with Docker or Podman installed.

The following `make` targets are available for development:

```bash
# Run tests
make test

# Run linter
make golangci-lint

# Generate CRDs and code
make generate
make manifests

# Build images
make images

# Build and push images
make build-push-images
```

Refer to the [README](./README.md) for detailed instructions on building
images, setting up prerequisites, and deploying the operator.

### Code Review

All submissions require review before merging. Maintainers will review pull
requests and may request changes. The review process aims to:

* Ensure code quality and consistency
* Verify correctness and test coverage
* Maintain project architecture and design principles

## Getting Help

If you have questions about contributing, feel free to open a
[GitHub Issue](https://github.com/cluster-power-manager/cluster-power-manager/issues)
or start a
[GitHub Discussion](https://github.com/cluster-power-manager/cluster-power-manager/discussions).
