FROM mcr.microsoft.com/devcontainers/go:1

# Install additional tools if needed
RUN apt-get update && apt-get install -y \
    curl \
    && rm -rf /var/lib/apt/lists/*