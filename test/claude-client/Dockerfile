FROM node:20-slim

RUN apt-get update && apt-get install -y curl && rm -rf /var/lib/apt/lists/*

# Install Claude Code CLI
RUN npm install -g @anthropic-ai/claude-code

# Copy settings with gateway endpoint
RUN mkdir -p /root/.claude
COPY settings.json /root/.claude/settings.json

WORKDIR /workspace

ENTRYPOINT ["claude"]
