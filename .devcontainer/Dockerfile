FROM mcr.microsoft.com/devcontainers/go:1.25-bookworm

RUN apt-get update && apt-get install -y \
    tmux \
    unzip \
    fontconfig \
    && rm -rf /var/lib/apt/lists/*

# Install JetBrainsMono Nerd Font
RUN mkdir -p /usr/share/fonts/truetype/nerdfonts \
    && curl -fLo /tmp/JetBrainsMono.zip https://github.com/ryanoasis/nerd-fonts/releases/download/v3.3.0/JetBrainsMono.zip \
    && unzip /tmp/JetBrainsMono.zip -d /usr/share/fonts/truetype/nerdfonts \
    && rm /tmp/JetBrainsMono.zip \
    && fc-cache -fv

# Cache Go dependencies in image layer
WORKDIR /workspace
COPY go.mod go.sum ./
RUN go mod download

WORKDIR /home/vscode
