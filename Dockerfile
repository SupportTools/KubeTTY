# syntax=docker/dockerfile:1.6

ARG GO_VERSION=1.24.3
ARG NODE_MAJOR=20
ARG KUBECTL_VERSION=v1.30.3
ARG HELM_VERSION=v3.15.2
ARG YQ_VERSION=v4.44.3
ARG KUBETTY_VERSION=dev

###############################
# Build React frontend assets #
###############################
FROM node:${NODE_MAJOR}-bullseye AS ui-builder
WORKDIR /workspace

# Copy both web and server so Vite's output path (../server/cmd/gateway/ui/dist) exists.
COPY web ./web
COPY server ./server

WORKDIR /workspace/web
RUN npm install
RUN npm run build

###############################
# Build Go backend binary     #
###############################
FROM golang:${GO_VERSION}-bullseye AS go-builder
ARG KUBETTY_VERSION=dev
ARG GIT_COMMIT=unknown
WORKDIR /workspace

COPY server/go.mod server/go.sum ./server/
RUN cd server && go mod download

COPY server ./server
# Vite outputs to server/cmd/gateway/ui/dist; both binaries embed ui/dist relative to their cmd dir
COPY --from=ui-builder /workspace/server/cmd/gateway/ui/dist ./server/cmd/gateway/ui/dist
COPY --from=ui-builder /workspace/server/cmd/gateway/ui/dist ./server/cmd/project/ui/dist

WORKDIR /workspace/server

# Build with version info injected via ldflags
RUN BUILD_TIME=$(date -u '+%Y-%m-%dT%H:%M:%SZ') && \
    LDFLAGS="-X main.version=${KUBETTY_VERSION} -X main.gitCommit=${GIT_COMMIT} -X main.buildTime=${BUILD_TIME}" && \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "${LDFLAGS}" -o /workspace/kubetty-gateway ./cmd/gateway && \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "${LDFLAGS}" -o /workspace/kubetty-project ./cmd/project

###############################
# Runtime image with tooling  #
###############################
FROM ubuntu:22.04 AS runtime
ARG NODE_MAJOR
ARG GO_VERSION
ARG KUBECTL_VERSION
ARG HELM_VERSION
ARG YQ_VERSION
ENV PATH=/usr/local/go/bin:/opt/ai/bin:${PATH}
ENV TERM=xterm-256color
# Browser automation environment (Playwright/Puppeteer)
ENV PLAYWRIGHT_BROWSERS_PATH=/opt/ms-playwright
ENV PUPPETEER_SKIP_CHROMIUM_DOWNLOAD=true
WORKDIR /workspace

ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y --no-install-recommends \
    aggregate \
    bash \
    bat \
    build-essential \
    ca-certificates \
    cmake \
    curl \
    dnsutils \
    fd-find \
    fzf \
    gdb \
    gh \
    git \
    git-lfs \
    gnupg2 \
    htop \
    httpie \
    iftop \
    iproute2 \
    ipset \
    iptables \
    iptraf-ng \
    iputils-ping \
    jq \
    less \
    ltrace \
    make \
    man-db \
    mitmproxy \
    mtr-tiny \
    nano \
    net-tools \
    ninja-build \
    nmap \
    nmon \
    openssl \
    openssh-client \
    openssh-server \
    pkg-config \
    postgresql-client \
    procps \
    python3 \
    python3-pip \
    ripgrep \
    rsync \
    screen \
    socat \
    strace \
    stress \
    stress-ng \
    sudo \
    sysstat \
    tcpdump \
    tmux \
    tree \
    unzip \
    valgrind \
    vim \
    wget \
    xz-utils \
    zlib1g \
    zsh \
    # Browser automation dependencies (fonts for proper rendering)
    fonts-liberation \
    fonts-noto-color-emoji \
    && git lfs install --system \
    && rm -rf /var/lib/apt/lists/*

# Install GUI stack components (conditional - only used when GUI_ENABLED=true)
# - supervisor: Process manager for orchestrating GUI components
# - xvfb: X Virtual Framebuffer (headless X server)
# - x11vnc: VNC server exposing Xvfb display
# - xfce4: Lightweight desktop environment
# - dbus-x11: D-Bus for desktop integration
# - firefox: Mozilla Firefox browser
# - imagemagick: For wallpaper generation with project info
RUN apt-get update && apt-get install -y --no-install-recommends \
    supervisor \
    xvfb \
    x11vnc \
    xfce4 \
    xfce4-terminal \
    thunar \
    dbus-x11 \
    fonts-dejavu-core \
    firefox \
    imagemagick \
    && rm -rf /var/lib/apt/lists/*

# Install Google Chrome (stable) for GUI browser access
# Note: Chromium from apt is a snap package which doesn't work in containers
# Chrome is installed to /opt/google/chrome/ but /opt is mounted as a PVC in project pods,
# so we backup Chrome to /usr/local/share/chrome-backup for sync on container start
RUN curl -fsSL https://dl.google.com/linux/direct/google-chrome-stable_current_amd64.deb -o /tmp/chrome.deb \
    && apt-get update \
    && apt-get install -y --no-install-recommends /tmp/chrome.deb \
    && rm -f /tmp/chrome.deb \
    && rm -rf /var/lib/apt/lists/* \
    && mkdir -p /usr/local/share/chrome-backup \
    && cp -a /opt/google /usr/local/share/chrome-backup/

RUN groupadd -g 1000 mmattox && useradd -m -u 1000 -g mmattox -s /bin/bash mmattox \
    && echo 'mmattox:mmattox' | chpasswd \
    && usermod -aG sudo mmattox \
    && echo 'mmattox ALL=(ALL) NOPASSWD:ALL' > /etc/sudoers.d/mmattox \
    && chmod 0440 /etc/sudoers.d/mmattox
WORKDIR /home/mmattox

# Install Node.js runtime (for running React/JS tooling inside the pod if needed).
RUN curl -fsSL https://deb.nodesource.com/setup_${NODE_MAJOR}.x | bash - \
    && apt-get update \
    && apt-get install -y --no-install-recommends nodejs \
    && rm -rf /var/lib/apt/lists/*

# Install Playwright and Puppeteer globally with Chromium only
# Note: Firefox and WebKit removed to reduce image size (~1.7GB savings)
# Browsers installed to /opt/ms-playwright (set via PLAYWRIGHT_BROWSERS_PATH env var)
# Backed up to /opt/ms-playwright-backup because /opt is mounted as PVC in project pods
RUN npm install -g playwright puppeteer \
    && mkdir -p /opt/ms-playwright \
    && npx playwright install --with-deps chromium \
    && chown -R mmattox:mmattox /usr/lib/node_modules/ \
    && chown -R mmattox:mmattox /opt/ms-playwright \
    && cp -a /opt/ms-playwright /opt/ms-playwright-backup \
    && rm -rf /tmp/* /root/.npm

# Install Docker CLI from Debian repos.
RUN apt-get update && apt-get install -y --no-install-recommends docker.io \
    && rm -rf /var/lib/apt/lists/*

# Install Docker Compose V2 plugin and V1 compatibility symlink
RUN mkdir -p /usr/local/lib/docker/cli-plugins \
    && curl -fsSL https://github.com/docker/compose/releases/download/v2.29.1/docker-compose-linux-x86_64 \
       -o /usr/local/lib/docker/cli-plugins/docker-compose \
    && chmod +x /usr/local/lib/docker/cli-plugins/docker-compose \
    && ln -s /usr/local/lib/docker/cli-plugins/docker-compose /usr/local/bin/docker-compose

# Install Docker Buildx plugin
RUN curl -fsSL https://github.com/docker/buildx/releases/download/v0.16.2/buildx-v0.16.2.linux-amd64 \
       -o /usr/local/lib/docker/cli-plugins/docker-buildx \
    && chmod +x /usr/local/lib/docker/cli-plugins/docker-buildx

# Symlink fd command.
RUN ln -s /usr/bin/fdfind /usr/local/bin/fd

# Install Go toolchain for on-cluster development.
RUN curl -fsSL https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz -o /tmp/go.tgz \
    && tar -C /usr/local -xzf /tmp/go.tgz \
    && rm /tmp/go.tgz

# Install Kubernetes/Cloud tooling.
RUN set -eux; \
    curl -fsSL https://github.com/kubernetes-sigs/kustomize/releases/download/kustomize%2Fv5.4.2/kustomize_v5.4.2_linux_amd64.tar.gz -o /tmp/kustomize.tgz; \
    tar -C /tmp -xzf /tmp/kustomize.tgz; \
    mv /tmp/kustomize /usr/local/bin/kustomize; \
    curl -fsSL https://github.com/helmfile/helmfile/releases/download/v0.161.0/helmfile_0.161.0_linux_amd64.tar.gz -o /tmp/helmfile.tgz; \
    tar -C /tmp -xzf /tmp/helmfile.tgz; \
    mv /tmp/helmfile /usr/local/bin/helmfile; \
    curl -fsSL https://github.com/derailed/k9s/releases/download/v0.32.4/k9s_Linux_amd64.tar.gz -o /tmp/k9s.tgz; \
    tar -C /tmp -xzf /tmp/k9s.tgz; \
    mv /tmp/k9s /usr/local/bin/k9s; \
    curl -fsSL https://releases.hashicorp.com/terraform/1.8.5/terraform_1.8.5_linux_amd64.zip -o /tmp/terraform.zip; \
    unzip /tmp/terraform.zip -d /usr/local/bin; \
    curl -fsSL https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip -o /tmp/awscliv2.zip; \
    unzip /tmp/awscliv2.zip -d /tmp; \
    /tmp/aws/install --update; \
    rm -rf /tmp/kustomize.tgz /tmp/helmfile.tgz /tmp/k9s.tgz /tmp/terraform.zip /tmp/awscliv2.zip /tmp/aws

# Install kubectl.
RUN curl -fsSLo /usr/local/bin/kubectl https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/amd64/kubectl \
    && chmod +x /usr/local/bin/kubectl

# Install Helm.
RUN curl -fsSL https://get.helm.sh/helm-${HELM_VERSION}-linux-amd64.tar.gz -o /tmp/helm.tgz \
    && tar -C /tmp -xzf /tmp/helm.tgz \
    && mv /tmp/linux-amd64/helm /usr/local/bin/helm \
    && rm -rf /tmp/helm.tgz /tmp/linux-amd64

# Install yq.
RUN curl -fsSL https://github.com/mikefarah/yq/releases/download/${YQ_VERSION}/yq_linux_amd64 -o /usr/local/bin/yq \
    && chmod +x /usr/local/bin/yq

# Install Kind (Kubernetes in Docker) for spinning up temporary k8s clusters.
RUN curl -fsSL https://kind.sigs.k8s.io/dl/v0.24.0/kind-linux-amd64 -o /usr/local/bin/kind \
    && chmod +x /usr/local/bin/kind

# Install k3d (k3s in Docker) for lightweight temporary k8s clusters.
RUN curl -fsSL https://raw.githubusercontent.com/k3d-io/k3d/main/install.sh | bash

# Install Minikube for local Kubernetes clusters.
RUN curl -fsSL https://storage.googleapis.com/minikube/releases/latest/minikube-linux-amd64 -o /usr/local/bin/minikube \
    && chmod +x /usr/local/bin/minikube

# Install Skaffold for continuous k8s development.
RUN curl -fsSL https://storage.googleapis.com/skaffold/releases/latest/skaffold-linux-amd64 -o /usr/local/bin/skaffold \
    && chmod +x /usr/local/bin/skaffold

# Install Tilt for live k8s development.
RUN curl -fsSL https://raw.githubusercontent.com/tilt-dev/tilt/master/scripts/install.sh | bash

# Install Telepresence for connecting local dev to remote clusters.
RUN curl -fsSL https://app.getambassador.io/download/tel2oss/releases/download/v2.18.0/telepresence-linux-amd64 -o /usr/local/bin/telepresence \
    && chmod +x /usr/local/bin/telepresence

# Install kubectx and kubens for quick context/namespace switching.
RUN curl -fsSL https://github.com/ahmetb/kubectx/releases/download/v0.9.5/kubectx_v0.9.5_linux_x86_64.tar.gz -o /tmp/kubectx.tgz \
    && tar -C /usr/local/bin -xzf /tmp/kubectx.tgz kubectx \
    && curl -fsSL https://github.com/ahmetb/kubectx/releases/download/v0.9.5/kubens_v0.9.5_linux_x86_64.tar.gz -o /tmp/kubens.tgz \
    && tar -C /usr/local/bin -xzf /tmp/kubens.tgz kubens \
    && rm -f /tmp/kubectx.tgz /tmp/kubens.tgz

# Install stern for multi-pod log tailing.
RUN curl -fsSL https://github.com/stern/stern/releases/download/v1.30.0/stern_1.30.0_linux_amd64.tar.gz -o /tmp/stern.tgz \
    && tar -C /usr/local/bin -xzf /tmp/stern.tgz stern \
    && rm -f /tmp/stern.tgz

# Install DevSpace for k8s development workflow.
RUN curl -fsSL https://github.com/devspace-sh/devspace/releases/latest/download/devspace-linux-amd64 -o /usr/local/bin/devspace \
    && chmod +x /usr/local/bin/devspace

# Install ctlptl for declarative local cluster management.
RUN curl -fsSL https://github.com/tilt-dev/ctlptl/releases/download/v0.8.28/ctlptl.0.8.28.linux.x86_64.tar.gz -o /tmp/ctlptl.tgz \
    && tar -C /usr/local/bin -xzf /tmp/ctlptl.tgz ctlptl \
    && rm -f /tmp/ctlptl.tgz

# Install vcluster for virtual clusters inside k8s.
RUN curl -fsSL https://github.com/loft-sh/vcluster/releases/latest/download/vcluster-linux-amd64 -o /usr/local/bin/vcluster \
    && chmod +x /usr/local/bin/vcluster

# Install python-based helpers for LLM tooling (placeholders for Claude/Codex/Gemini CLIs).
RUN pip3 install --no-cache-dir anthropic google-generativeai openai

# Create directory for optional proprietary CLI binaries (installed at runtime).
RUN mkdir -p /opt/ai/bin && chmod 755 /opt/ai/bin

# Note: Ollama removed to reduce image size (~3.4GB savings)
# Users can install Ollama at runtime if needed: curl -fsSL https://ollama.com/install.sh | sh

# Install Claude logging helper.
COPY scripts/claude_with_log.sh /etc/profile.d/claude.sh
RUN chmod +x /etc/profile.d/claude.sh

# Copy compiled server binaries.
COPY --from=go-builder /workspace/kubetty-gateway /usr/local/bin/kubetty-gateway
COPY --from=go-builder /workspace/kubetty-project /usr/local/bin/kubetty-project

# Copy and install entrypoint script for mode selection.
COPY scripts/entrypoint.sh /usr/local/bin/kubetty-entrypoint
RUN chmod 755 /usr/local/bin/kubetty-entrypoint

# Copy GUI stack scripts and configuration.
COPY scripts/start-gui.sh /usr/local/bin/start-gui
COPY scripts/kubetty-gui.sh /etc/profile.d/kubetty-gui.sh
COPY scripts/generate-wallpaper.sh /usr/local/bin/generate-wallpaper
COPY config/supervisor/kubetty.conf /etc/supervisor/conf.d/kubetty.conf
RUN chmod 755 /usr/local/bin/start-gui /usr/local/bin/generate-wallpaper && \
    chmod 644 /etc/profile.d/kubetty-gui.sh && \
    chmod 644 /etc/supervisor/conf.d/kubetty.conf && \
    mkdir -p /var/log/supervisor /var/run/supervisor && \
    chown -R mmattox:mmattox /var/log/supervisor /var/run/supervisor

# Copy KubeTTY assets for wallpaper generation.
RUN mkdir -p /usr/share/kubetty
COPY assets/logo.png /usr/share/kubetty/logo.png
COPY assets/wallpaper.png /usr/share/kubetty/wallpaper.png
RUN chmod 644 /usr/share/kubetty/logo.png /usr/share/kubetty/wallpaper.png

# Copy XFCE desktop configuration (sets custom wallpaper).
COPY config/xfce4 /home/mmattox/.config/xfce4
RUN mkdir -p /home/mmattox/.local/share/backgrounds && \
    chown -R mmattox:mmattox /home/mmattox/.config /home/mmattox/.local

# Default session storage/log directories.
RUN mkdir -p /home/mmattox/claude_logs && chown -R mmattox:mmattox /home/mmattox

# Copy Claude Code settings.
RUN mkdir -p /home/mmattox/.claude
COPY .claude/settings.json /home/mmattox/.claude/settings.json
RUN chown -R mmattox:mmattox /home/mmattox/.claude

# Create bash profile with KubeTTY configuration.
RUN { \
    echo '# Display MOTD (Message of the Day)'; \
    echo 'if [ -f /etc/motd ]; then cat /etc/motd; echo ""; fi'; \
    echo ''; \
    echo '# Custom prompt using KUBETTY_USER and KUBETTY_PROJECT from environment'; \
    echo "export PS1='\\[\\033[01;32m\\]\${KUBETTY_USER:-\$USER}@\${KUBETTY_PROJECT:-kubetty}\\[\\033[00m\\]:\\[\\033[01;34m\\]\\w\\[\\033[00m\\]\\$ '"; \
    echo ''; \
    echo '# bash autocompletion'; \
    echo 'if [ -f /usr/local/share/bash-completion/bash_completion ]; then . /usr/local/share/bash-completion/bash_completion; fi'; \
    echo ''; \
    echo 'alias reload="source $HOME/.bash_profile"'; \
    echo 'alias grep="grep --color=auto"'; \
    echo 'alias ls="ls --color=auto"'; \
    echo 'alias ll="ls -la"'; \
    echo ''; \
    echo 'export KUBE_EDITOR="nano"'; \
    echo 'export GOROOT=/usr/local/go'; \
    echo 'export GOPATH=$HOME/go'; \
    echo 'export PATH=$GOPATH/bin:$GOROOT/bin:$PATH'; \
    } > /home/mmattox/.bash_profile && \
    chown mmattox:mmattox /home/mmattox/.bash_profile

# Copy MOTD (Message of the Day) with ASCII art.
COPY motd /etc/motd
RUN chmod 644 /etc/motd

# Write version info for wallpaper generator
ARG KUBETTY_VERSION
RUN echo "${KUBETTY_VERSION}" > /etc/kubetty-version && chmod 644 /etc/kubetty-version

EXPOSE 8080
USER mmattox
ENTRYPOINT ["/usr/local/bin/kubetty-entrypoint"]
