FROM ubuntu:24.04

RUN apt-get update && apt-get install -y curl && rm -rf /var/lib/apt/lists/*

# Install Claude CLI
RUN curl -fsSL https://cli.claude.ai/install.sh | sh

# Create non-root user
RUN useradd -m -u 10000 synapse

# Switch to non-root user
USER synapse

# Set working directory
WORKDIR /mount

# Set entrypoint
ENTRYPOINT ["claude"]