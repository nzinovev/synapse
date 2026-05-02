FROM ubuntu:24.04

RUN apt-get update && apt-get install -y wget && rm -rf /var/lib/apt/lists/*

# Create non-root user
RUN useradd -m -u 10000 synapse

# Switch to non-root user
USER synapse

# Set working directory
WORKDIR /mount

# Set entrypoint (assuming 'agent' is the Cursor CLI binary)
ENTRYPOINT ["agent"]