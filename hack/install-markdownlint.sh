#!/bin/bash -xe
# Following example of: https://github.com/openshift/enhancements/blob/master/hack/install-markdownlint.sh

# Install Node.js
NODE_VERSION=22.5.1
ARCH=$(uname -m)
case "${ARCH}" in
    x86_64)
        NODE_ARCH="x64"
        ;;
    aarch64)
        NODE_ARCH="arm64"
        ;;
    *)
        echo "Unsupported architecture: ${ARCH}"
        exit 1
        ;;
esac
NODE_ARCHIVE="https://nodejs.org/dist/v${NODE_VERSION}/node-v${NODE_VERSION}-linux-${NODE_ARCH}.tar.xz"
curl -o node.tar.xz $NODE_ARCHIVE
tar -xJf node.tar.xz -C /usr/local --strip-components=1
rm node.tar.xz
dnf clean all

npm install -g markdownlint@v0.25.1 markdownlint-cli2@v0.4.0
