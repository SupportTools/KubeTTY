# No `# syntax=` directive on purpose: it makes BuildKit pull the docker/dockerfile frontend from
# Docker Hub before anything else; the frontend built into BuildKit covers every instruction here.
#
# Fetch tiers (cluster-services CLAUDE.md build rule; cluster-services-1494):
#   - base images: the Harbor Docker Hub proxy cache (a cache we run), tag AND digest pinned. The
#     digests are the ones these tags resolved to on 2026-11-19 (KubeTTY#46 build), so the bases
#     are byte-identical to what docker.io served. Off-network, override e.g.
#     --build-arg RUNTIME_IMAGE=ubuntu:22.04.
#   - apt, Ubuntu archive + security (runtime stage): the org fetch-cache /ubuntu/ and
#     /ubuntu-security/ prefixes (cluster-services fetch-cache/, in-cluster name, resolves inside a
#     buildx RUN on the self-hosted-linux runners). Anywhere else it fails fast and falls back to
#     upstream ("apt source: MIRROR FAILED"); APT_FALLBACK=none makes that a build failure (CI).
#     Pass --build-arg APT_MIRROR= to skip the attempt. apt verifies the InRelease signatures
#     either way. The image's own /etc/apt/sources.list is restored after the last apt step, so
#     the shipped image points at the Ubuntu archive exactly as before.
#   - npm (ui-builder `npm ci` and the global playwright/puppeteer install): the org Verdaccio
#     mirror (ns npm-proxy, cluster-services-853/1027), a cache we run. Same fallback rules as apt:
#     NPM_FALLBACK=none in CI, NPM_REGISTRY= to skip. The mirror only counts as used if npm's http
#     log shows fetches from it. ui-builder installs strictly from web/package-lock.json (sha512
#     integrity). The global tools have no lockfile, so they are pinned by version plus
#     --before=${NPM_BEFORE}: every transitive dependency resolves to what the registry held at that
#     instant, which reproduces the tree prod shipped on 2026-11-19 exactly (cluster-services-1496).
#   - Go modules: Athens (GOPROXY=https://goproxy.support.tools, a cache we run). CI passes Athens
#     only; the default falls through to proxy.golang.org off-network. go.sum verifies every module.
#   - Playwright browser zips: the fetch-cache /playwright/ prefix (cdn.playwright.dev),
#     FETCH_FALLBACK=none in CI.
#   - kubectl, helm, yq: the fetch-cache /k8s/, /helm/, /github-releases/ prefixes, sha256 pinned
#     (build/fetch.sh), FETCH_FALLBACK=none in CI.
#   - upstream-pinned (no tier we run serves them yet; follow-up cluster-services-1496 notes): the
#     other release binaries, the Go tarball, the Google Chrome .deb and the NodeSource nodejs .deb
#     (apt, GPG-verified against the vendored build/nodesource.gpg), and pip (PyPI,
#     --require-hashes on build/requirements-ai.txt). Each is pinned to the exact version prod
#     shipped on 2026-11-19 and, except apt/pip which verify their own way, sha256-checked by
#     build/fetch.sh, which prints "source=upstream-pinned" for each one in the build log.
ARG NODE_IMAGE=harbor.support.tools/dockerhub/library/node:20-bullseye@sha256:c0122351f25f04facee976f9db7214789eabadb489f4e4aea9cd00a0d6af77c4
ARG GO_IMAGE=harbor.support.tools/dockerhub/library/golang:1.26.11-bookworm@sha256:dc40a13b61304647ed7e5d581d246b86413dfc29d63c6d018310feb93cd98fc0
ARG RUNTIME_IMAGE=harbor.support.tools/dockerhub/library/ubuntu:22.04@sha256:9abcaffc92a5279fff3a6a41635c0800ea33e528425fb389658806476e7defde

ARG GO_VERSION=1.26.11
ARG NODE_MAJOR=20
ARG KUBECTL_VERSION=v1.30.3
ARG HELM_VERSION=v3.22.1
ARG YQ_VERSION=v4.54.3
ARG KUBETTY_VERSION=dev

###############################
# Build React frontend assets #
###############################
FROM ${NODE_IMAGE} AS ui-builder
WORKDIR /workspace

# Copy both web and server so Vite's output path (../server/cmd/gateway/ui/dist) exists.
COPY web ./web
COPY server ./server

WORKDIR /workspace/web
ARG NPM_REGISTRY=http://verdaccio.npm-proxy.svc.a1-ops-prd.local/
ARG NPM_FALLBACK=upstream
RUN set -eu; \
    used=0; \
    if [ -n "${NPM_REGISTRY}" ] \
       && npm ci --no-audit --fund=false --registry="${NPM_REGISTRY}" --loglevel=http > /tmp/npm.log 2>&1; then \
      hits=$(grep -c "fetch GET 200 ${NPM_REGISTRY}" /tmp/npm.log || true); \
      if [ "${hits}" -gt 0 ]; then used=1; echo "npm source: mirror ${NPM_REGISTRY} (${hits} fetches)"; \
      else echo "npm source: mirror requested but 0 fetches went to it"; fi; \
    fi; \
    if [ "${used}" = 0 ]; then \
      [ -z "${NPM_REGISTRY}" ] || { echo "npm source: MIRROR FAILED (${NPM_REGISTRY})"; tail -20 /tmp/npm.log; }; \
      if [ "${NPM_FALLBACK}" = "none" ]; then echo "NPM_FALLBACK=none: refusing upstream" >&2; exit 1; fi; \
      echo "npm source: upstream"; rm -rf node_modules; npm ci --no-audit --fund=false; \
    fi; \
    rm -f /tmp/npm.log
RUN npm run build

###############################
# Build Go backend binary     #
###############################
FROM ${GO_IMAGE} AS go-builder
ARG KUBETTY_VERSION=dev
ARG GIT_COMMIT=unknown
WORKDIR /workspace

# GOTOOLCHAIN=local: never download a toolchain at build time (the image's Go satisfies go.mod).
ARG GOPROXY=https://goproxy.support.tools|https://proxy.golang.org
ENV GOPROXY=${GOPROXY} \
    GOTOOLCHAIN=local
COPY server/go.mod server/go.sum ./server/
# -x prints every "# get <url>" line, so the build log shows which proxy served each module.
RUN cd server && echo "go module source: GOPROXY=${GOPROXY}" && go mod download -x

COPY server ./server
# Vite outputs to server/cmd/gateway/ui/dist; both binaries embed ui/dist relative to their cmd dir
COPY --from=ui-builder /workspace/server/cmd/gateway/ui/dist ./server/cmd/gateway/ui/dist
COPY --from=ui-builder /workspace/server/cmd/gateway/ui/dist ./server/cmd/project/ui/dist

WORKDIR /workspace/server

# Build with version info injected via ldflags
RUN BUILD_TIME=$(date -u '+%Y-%m-%dT%H:%M:%SZ') && \
    LDFLAGS="-X main.version=${KUBETTY_VERSION} -X main.gitCommit=${GIT_COMMIT} -X main.buildTime=${BUILD_TIME}" && \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "${LDFLAGS}" -o /workspace/kubetty-gateway ./cmd/gateway && \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "${LDFLAGS}" -o /workspace/kubetty-project ./cmd/project && \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /workspace/kubetty-authuser ./cmd/kubetty-authuser

###############################
# Runtime image with tooling  #
###############################
FROM ${RUNTIME_IMAGE} AS runtime
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
# The image's copy of the Chrome .deb: /opt is a PVC in project pods.
ENV PUPPETEER_EXECUTABLE_PATH=/usr/local/share/chrome-backup/google/chrome/chrome
WORKDIR /workspace

ENV DEBIAN_FRONTEND=noninteractive

# Point apt at the org fetch-cache for every apt step in this stage (see the header). The original
# sources.list is kept aside and restored after the last apt step below.
# --error-on=any matters: plain `apt-get update` exits 0 when every mirror fetch fails (it only
# warns), so without it an unreachable mirror is "success" and the install then dies (exit 100).
ARG APT_MIRROR=http://fetch-cache.fetch-cache.svc.a1-ops-prd.local
ARG APT_FALLBACK=upstream
# build/fetch.sh (pinned release binaries) and the Playwright/Puppeteer browser downloads use the
# same fetch-cache; FETCH_MIRROR= skips it, FETCH_FALLBACK=none (CI) refuses upstream.
ARG FETCH_MIRROR=http://fetch-cache.fetch-cache.svc.a1-ops-prd.local
ARG FETCH_FALLBACK=upstream
RUN set -eu; \
    src=/etc/apt/sources.list; \
    cp -p "$src" /etc/apt/sources.list.kubetty-orig; \
    if [ -n "${APT_MIRROR}" ]; then \
      sed -i -e "s#http://archive.ubuntu.com/ubuntu/\?#${APT_MIRROR}/ubuntu/#g" \
             -e "s#http://security.ubuntu.com/ubuntu/\?#${APT_MIRROR}/ubuntu-security/#g" "$src"; \
    fi; \
    if [ -n "${APT_MIRROR}" ] && apt-get -o Acquire::Retries=1 -o Acquire::http::Timeout=10 update --error-on=any; then \
      echo "apt source: mirror ${APT_MIRROR}"; \
    else \
      [ -z "${APT_MIRROR}" ] || echo "apt source: MIRROR FAILED (${APT_MIRROR})"; \
      if [ "${APT_FALLBACK}" = "none" ]; then echo "APT_FALLBACK=none: refusing upstream" >&2; exit 1; fi; \
      cp -p /etc/apt/sources.list.kubetty-orig "$src"; \
      echo "apt source: upstream"; \
    fi; \
    grep -E '^deb ' "$src"; \
    # Build-time only (removed with the sources.list restore below): a cold fetch-cache MISS is as
    # slow as the Canonical archive behind it, which on 2026-11-19 timed out TLS handshakes and gave
    # 504s (cluster-services-1497), so retry each file instead of failing the whole install.
    printf 'Acquire::Retries "5";\nAcquire::http::Timeout "300";\n' > /etc/apt/apt.conf.d/99kubetty-build-retries; \
    rm -rf /var/lib/apt/lists/*

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

# USN-8944-1: ca-certificates 20260816ubuntu0.22.04.1 drops the compromised eMudhra emSign
# roots. Upgrade explicitly so a cached layer from an older build cannot keep the old bundle.
RUN apt-get update && apt-get install -y --only-upgrade --no-install-recommends ca-certificates \
    && dpkg --compare-versions "$(dpkg-query -W -f='${Version}' ca-certificates)" ge 20260816 \
    && rm -rf /var/lib/apt/lists/*

# Install GUI stack components (conditional - only used when GUI_ENABLED=true)
# - supervisor: Process manager for orchestrating GUI components
# - xvfb: X Virtual Framebuffer (headless X server)
# - x11vnc: VNC server exposing Xvfb display
# - xfce4: Lightweight desktop environment
# - dbus-x11: D-Bus for desktop integration
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
    imagemagick \
    && rm -rf /var/lib/apt/lists/*

# Install Google Chrome (stable) for GUI browser access
# Note: Chromium from apt is a snap package which doesn't work in containers
# Chrome is installed to /opt/google/chrome/ but /opt is mounted as a PVC in project pods,
# so we backup Chrome to /usr/local/share/chrome-backup for sync on container start
# Pinned to the version prod shipped on 2026-11-19 (cluster-services-1496); upstream-pinned tier
# (dl.google.com, sha256 checked). Google drops old .debs from the pool after a while, so a bump is
# CHROME_VERSION + CHROME_SHA256 together (sha256sum of the new pool .deb).
# The google-chrome.sources the package's postinst writes is held aside until the last apt step, so
# the later apt-get updates fetch only the Ubuntu fetch-cache, not dl.google.com.
ARG CHROME_VERSION=158.0.8127.36-1
ARG CHROME_SHA256=4696bce00b128f385a870ba2eb6eede61698f22374ea0d8e09af8a1c6543d397
RUN --mount=type=bind,source=build/fetch.sh,target=/tmp/fetch.sh \
    sh /tmp/fetch.sh /tmp/chrome.deb "${CHROME_SHA256}" \
       "https://dl.google.com/linux/chrome/deb/pool/main/g/google-chrome-stable/google-chrome-stable_${CHROME_VERSION}_amd64.deb" \
    && apt-get update \
    && apt-get install -y --no-install-recommends /tmp/chrome.deb \
    && rm -f /tmp/chrome.deb \
    && rm -rf /var/lib/apt/lists/* \
    && mkdir -p /usr/local/share/chrome-backup \
    && cp -a /opt/google /usr/local/share/chrome-backup/ \
    && mkdir -p /etc/apt/sources.list.d.kubetty-held \
    && mv /etc/apt/sources.list.d/google-chrome.sources /etc/apt/sources.list.d.kubetty-held/

RUN groupadd -g 1000 mmattox && useradd -m -u 1000 -g mmattox -s /bin/bash mmattox \
    && echo 'mmattox:mmattox' | chpasswd \
    && usermod -aG sudo mmattox \
    && echo 'mmattox ALL=(ALL) NOPASSWD:ALL' > /etc/sudoers.d/mmattox \
    && chmod 0440 /etc/sudoers.d/mmattox
WORKDIR /home/mmattox

# Install Node.js runtime (for running React/JS tooling inside the pod if needed).
# Does what https://deb.nodesource.com/setup_20.x did, without piping a live script into bash
# (cluster-services-1496): the same pre-requisite packages, the NodeSource keyring (vendored as
# build/nodesource.gpg, byte-identical to `curl .../gpgkey/nodesource-repo.gpg.key | gpg --dearmor`
# on 2026-11-19, key 6F71F525282841EEDAF851B42F59B5F99B1BE0B4), the same sources/preferences files,
# and nodejs pinned to the version prod shipped. Tier: upstream-pinned (deb.nodesource.com; apt
# verifies the InRelease signature against the vendored key and the .deb against its hash).
# The nodesource.sources file is held aside after the install, like google-chrome.sources.
ARG NODEJS_VERSION=20.20.2-1nodesource1
COPY build/nodesource.gpg /usr/share/keyrings/nodesource.gpg
RUN set -eu; \
    chmod 644 /usr/share/keyrings/nodesource.gpg; \
    apt-get update; \
    apt-get install -y apt-transport-https ca-certificates curl gnupg; \
    printf 'Types: deb\nURIs: https://deb.nodesource.com/node_%s.x\nSuites: nodistro\nComponents: main\nArchitectures: %s\nSigned-By: /usr/share/keyrings/nodesource.gpg\n' \
      "${NODE_MAJOR}" "$(dpkg --print-architecture)" > /etc/apt/sources.list.d/nodesource.sources; \
    printf 'Package: nsolid\nPin: origin deb.nodesource.com\nPin-Priority: 600\n' > /etc/apt/preferences.d/nsolid; \
    printf 'Package: nodejs\nPin: origin deb.nodesource.com\nPin-Priority: 600\n' > /etc/apt/preferences.d/nodejs; \
    apt-get update; \
    apt-get install -y --no-install-recommends "nodejs=${NODEJS_VERSION}"; \
    mv /etc/apt/sources.list.d/nodesource.sources /etc/apt/sources.list.d.kubetty-held/; \
    rm -rf /var/lib/apt/lists/*

# Install Playwright and Puppeteer globally with Chromium only
# Note: Firefox and WebKit removed to reduce image size (~1.7GB savings)
# Browsers installed to /opt/ms-playwright (set via PLAYWRIGHT_BROWSERS_PATH env var)
# Backed up to /opt/ms-playwright-backup because /opt is mounted as PVC in project pods
# Fetch tiers (cluster-services-1496): npm from Verdaccio (NPM_FALLBACK as in ui-builder); versions
# pinned and transitive deps frozen with --before (see the header), which reproduces prod's
# 2026-11-19 tree. Playwright's browser zip comes from the fetch-cache /playwright/ prefix via
# PLAYWRIGHT_DOWNLOAD_HOST. Puppeteer downloads no browser and uses PUPPETEER_EXECUTABLE_PATH.
# npm 11 replaces Node 20's bundled npm 10, whose tar 6 has a Critical CVE fixed only in tar 7.
ARG PLAYWRIGHT_VERSION=1.65.1
ARG PUPPETEER_VERSION=24.43.1
ARG NPM_VERSION=11.21.3
ARG NPM_BEFORE=2026-11-19T12:59:00Z
ARG NPM_REGISTRY=http://verdaccio.npm-proxy.svc.a1-ops-prd.local/
ARG NPM_FALLBACK=upstream
RUN set -eu; \
    pkgs="playwright@${PLAYWRIGHT_VERSION} puppeteer@${PUPPETEER_VERSION}"; \
    npm_g() { \
      PUPPETEER_SKIP_DOWNLOAD=true npm install -g --no-audit --fund=false --before="${NPM_BEFORE}" "$@" $pkgs \
        && npm install -g --no-audit --fund=false --before="${NPM_BEFORE}" "$@" "npm@${NPM_VERSION}"; \
    }; \
    used=0; \
    if [ -n "${NPM_REGISTRY}" ] \
       && npm_g --registry="${NPM_REGISTRY}" --loglevel=http > /tmp/npm.log 2>&1; then \
      hits=$(grep -c "fetch GET 200 ${NPM_REGISTRY}" /tmp/npm.log || true); \
      if [ "${hits}" -gt 0 ]; then used=1; echo "npm source: mirror ${NPM_REGISTRY} (${hits} fetches)"; \
      else echo "npm source: mirror requested but 0 fetches went to it"; fi; \
    fi; \
    if [ "${used}" = 0 ]; then \
      [ -z "${NPM_REGISTRY}" ] || { echo "npm source: MIRROR FAILED (${NPM_REGISTRY})"; tail -20 /tmp/npm.log; }; \
      if [ "${NPM_FALLBACK}" = "none" ]; then echo "NPM_FALLBACK=none: refusing upstream" >&2; exit 1; fi; \
      echo "npm source: upstream"; \
      npm_g; \
    fi; \
    npm --version | grep -qx "${NPM_VERSION}"; \
    rm -f /tmp/npm.log; \
    mkdir -p /opt/ms-playwright; \
    if [ -n "${FETCH_MIRROR}" ] \
       && PLAYWRIGHT_DOWNLOAD_HOST="${FETCH_MIRROR}/playwright" npx playwright install --with-deps chromium; then \
      echo "browser source: fetch-cache ${FETCH_MIRROR}/playwright"; \
    else \
      [ -z "${FETCH_MIRROR}" ] || echo "browser source: MIRROR FAILED (${FETCH_MIRROR}/playwright)"; \
      if [ "${FETCH_FALLBACK}" = "none" ]; then echo "FETCH_FALLBACK=none: refusing upstream" >&2; exit 1; fi; \
      rm -rf /opt/ms-playwright/*; \
      echo "browser source: upstream"; npx playwright install --with-deps chromium; \
    fi; \
    chown -R mmattox:mmattox /usr/lib/node_modules/; \
    chown -R mmattox:mmattox /opt/ms-playwright; \
    cp -a /opt/ms-playwright /opt/ms-playwright-backup; \
    rm -rf /tmp/* /root/.npm

# Last apt step: restore the image's stock Ubuntu sources.list (byte-identical to the base image's)
# and drop the build-time retry config, so the shipped image does not depend on the fetch-cache.
# The held NodeSource/Chrome sources go back too, so /etc/apt matches what the image always shipped.
RUN mv /etc/apt/sources.list.kubetty-orig /etc/apt/sources.list \
    && mv /etc/apt/sources.list.d.kubetty-held/* /etc/apt/sources.list.d/ \
    && rmdir /etc/apt/sources.list.d.kubetty-held \
    && rm /etc/apt/apt.conf.d/99kubetty-build-retries \
    && grep -q '^deb http://archive.ubuntu.com/ubuntu/ jammy main' /etc/apt/sources.list

# Release binaries below: build/fetch.sh, sha256 pinned. Each call prints its source tier.
# Docker CLI only: dockerd runs in the privileged DinD sidecar, so this unprivileged container
# needs no daemon, containerd or runc.
RUN --mount=type=bind,source=build/fetch.sh,target=/tmp/fetch.sh sh /tmp/fetch.sh /tmp/docker.tgz 7c1ebadc65c716cdc6d35fc78759ed0e4317debf43c21dd12f066f2006999def \
      https://download.docker.com/linux/static/stable/x86_64/docker-29.10.0.tgz \
    && tar -C /usr/local/bin --strip-components=1 -xzf /tmp/docker.tgz docker/docker \
    && rm -f /tmp/docker.tgz

# Install Docker Compose plugin and V1 compatibility symlink
RUN --mount=type=bind,source=build/fetch.sh,target=/tmp/fetch.sh mkdir -p /usr/local/lib/docker/cli-plugins \
    && sh /tmp/fetch.sh /usr/local/lib/docker/cli-plugins/docker-compose b8840f5008c447f41fce0a57ae3adeab42c89b1eb6e6012524bcabbfb4ffbaf6 \
       https://github.com/docker/compose/releases/download/v5.8.0/docker-compose-linux-x86_64 \
    && chmod +x /usr/local/lib/docker/cli-plugins/docker-compose \
    && ln -s /usr/local/lib/docker/cli-plugins/docker-compose /usr/local/bin/docker-compose

# Install Docker Buildx plugin
RUN --mount=type=bind,source=build/fetch.sh,target=/tmp/fetch.sh sh /tmp/fetch.sh /usr/local/lib/docker/cli-plugins/docker-buildx 5fb31a097304fbf6dd4038649a0decf32c2b9510e4047acd7d68b9d700c46194 \
       https://github.com/docker/buildx/releases/download/v0.39.0/buildx-v0.39.0.linux-amd64 \
    && chmod +x /usr/local/lib/docker/cli-plugins/docker-buildx

# Symlink fd command.
RUN ln -s /usr/bin/fdfind /usr/local/bin/fd

# Install Go toolchain for on-cluster development.
# sha256 = go.dev's published checksum for go1.26.11.linux-amd64.tar.gz (GO_VERSION and this move together).
RUN --mount=type=bind,source=build/fetch.sh,target=/tmp/fetch.sh sh /tmp/fetch.sh /tmp/go.tgz 9849f4e04ee35303714e34fbc3ac8ab44ea3e448a95aa182f91e3b9b79c710ec \
      https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz \
    && tar -C /usr/local -xzf /tmp/go.tgz \
    && rm /tmp/go.tgz

# Install Kubernetes/Cloud tooling.
RUN --mount=type=bind,source=build/fetch.sh,target=/tmp/fetch.sh set -eux; \
    sh /tmp/fetch.sh /tmp/kustomize.tgz cb9e31198d3f63b44848bd0afc4c8efc56e6cfef9c93a4a39a211940e6decd61 https://github.com/kubernetes-sigs/kustomize/releases/download/kustomize%2Fv5.8.3/kustomize_v5.8.3_linux_amd64.tar.gz; \
    tar -C /tmp -xzf /tmp/kustomize.tgz; \
    mv /tmp/kustomize /usr/local/bin/kustomize; \
    sh /tmp/fetch.sh /tmp/helmfile.tgz 1f19ced8adb0cdf2350e94ca598cf64e390d8c21d0d9b4bdc1c9f246d4591c9d https://github.com/helmfile/helmfile/releases/download/v1.8.4/helmfile_1.8.4_linux_amd64.tar.gz; \
    tar -C /tmp -xzf /tmp/helmfile.tgz; \
    mv /tmp/helmfile /usr/local/bin/helmfile; \
    sh /tmp/fetch.sh /tmp/k9s.tgz d62611d9be2c35b782a765e98421500196acbf8be844cd3d9e32d4fa7846da05 https://github.com/derailed/k9s/releases/download/v0.32.4/k9s_Linux_amd64.tar.gz; \
    tar -C /tmp -xzf /tmp/k9s.tgz; \
    mv /tmp/k9s /usr/local/bin/k9s; \
    sh /tmp/fetch.sh /tmp/terraform.zip e6f16b2f0b137ee5729caa8c4e79a3bae4ee261acd198a1fcfa8584a6296605b https://releases.hashicorp.com/terraform/1.17.2/terraform_1.17.2_linux_amd64.zip; \
    unzip /tmp/terraform.zip terraform -d /usr/local/bin; \
    sh /tmp/fetch.sh /tmp/awscliv2.zip f1b67518c009e9deb94152aaba5cc015e80af6b0630a7931d3662df42b5f568e https://awscli.amazonaws.com/awscli-exe-linux-x86_64-2.41.1.zip; \
    unzip /tmp/awscliv2.zip -d /tmp; \
    /tmp/aws/install --update; \
    rm -rf /tmp/kustomize.tgz /tmp/helmfile.tgz /tmp/k9s.tgz /tmp/terraform.zip /tmp/awscliv2.zip /tmp/aws

# Install kubectl.
RUN --mount=type=bind,source=build/fetch.sh,target=/tmp/fetch.sh sh /tmp/fetch.sh /usr/local/bin/kubectl abd83816bd236b266c3643e6c852b446f068fe260f3296af1a25b550854ec7e5 \
      https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/amd64/kubectl k8s/release/${KUBECTL_VERSION}/bin/linux/amd64/kubectl \
    && chmod +x /usr/local/bin/kubectl

# Install Helm.
RUN --mount=type=bind,source=build/fetch.sh,target=/tmp/fetch.sh sh /tmp/fetch.sh /tmp/helm.tgz 896cf63684a42a723d61c980369d6e588330927deb13676ca0d85006dd6da0ea \
      https://get.helm.sh/helm-${HELM_VERSION}-linux-amd64.tar.gz helm/helm-${HELM_VERSION}-linux-amd64.tar.gz \
    && tar -C /tmp -xzf /tmp/helm.tgz \
    && mv /tmp/linux-amd64/helm /usr/local/bin/helm \
    && rm -rf /tmp/helm.tgz /tmp/linux-amd64

# Install yq.
RUN --mount=type=bind,source=build/fetch.sh,target=/tmp/fetch.sh sh /tmp/fetch.sh /usr/local/bin/yq 129534601871538efe0ba4c5e62a34fb1d5f469bcb975c4fddeb467d82974973 \
      https://github.com/mikefarah/yq/releases/download/${YQ_VERSION}/yq_linux_amd64 github-releases/mikefarah/yq/${YQ_VERSION}/yq_linux_amd64 \
    && chmod +x /usr/local/bin/yq

# GitHub CLI release build; Ubuntu 22.04's gh 2.4.0 is built with Go 1.18.
RUN --mount=type=bind,source=build/fetch.sh,target=/tmp/fetch.sh sh /tmp/fetch.sh /tmp/gh.tgz 79e7ab5db738c48777b696e374f8b428f0647debfb424f44d2f426370407fe2f \
      https://github.com/cli/cli/releases/download/v2.105.0/gh_2.105.0_linux_amd64.tar.gz \
    && tar -C /usr/local/bin --strip-components=2 -xzf /tmp/gh.tgz gh_2.105.0_linux_amd64/bin/gh \
    && rm -f /tmp/gh.tgz

# Install Kind (Kubernetes in Docker) for spinning up temporary k8s clusters.
RUN --mount=type=bind,source=build/fetch.sh,target=/tmp/fetch.sh sh /tmp/fetch.sh /usr/local/bin/kind b89aada5a39d620da3fcd16435b7f28d858927dd53f92cbac77686b0588b600d https://kind.sigs.k8s.io/dl/v0.24.0/kind-linux-amd64 \
    && chmod +x /usr/local/bin/kind

# Install k3d (k3s in Docker) for lightweight temporary k8s clusters.
# Was `curl .../k3d-io/k3d/main/install.sh | bash` (installs the latest release binary); prod had v5.9.0.
RUN --mount=type=bind,source=build/fetch.sh,target=/tmp/fetch.sh sh /tmp/fetch.sh /usr/local/bin/k3d 06d8f25bc3a971c4eb29e0ff08429b180402db0f4dec838c9eac427e296800a0 \
      https://github.com/k3d-io/k3d/releases/download/v5.9.0/k3d-linux-amd64 \
    && chmod 755 /usr/local/bin/k3d

# Install Minikube for local Kubernetes clusters.
# Was releases/latest; prod had v1.39.0.
RUN --mount=type=bind,source=build/fetch.sh,target=/tmp/fetch.sh sh /tmp/fetch.sh /usr/local/bin/minikube b738496da01be06bbaf80c688f57ce25acd3849fbb518155f3a88e03ef555aa4 https://storage.googleapis.com/minikube/releases/v1.39.0/minikube-linux-amd64 \
    && chmod +x /usr/local/bin/minikube

# Install Skaffold for continuous k8s development.
# Was releases/latest; prod had v2.25.0.
RUN --mount=type=bind,source=build/fetch.sh,target=/tmp/fetch.sh sh /tmp/fetch.sh /usr/local/bin/skaffold 42b9e2e3246c19b78fcc53dd60cc7ded1da8704293886b909a2a02b9bef34d20 https://storage.googleapis.com/skaffold/releases/v2.25.0/skaffold-linux-amd64 \
    && chmod +x /usr/local/bin/skaffold

# Install Tilt for live k8s development.
# Was `curl .../tilt-dev/tilt/master/scripts/install.sh | bash` (latest release into /usr/local/bin);
# prod had v0.37.9.
RUN --mount=type=bind,source=build/fetch.sh,target=/tmp/fetch.sh sh /tmp/fetch.sh /tmp/tilt.tgz e70bb856b8e1cddb249b1e4231c1b1edca1f78167415effbdc9a1e893b6b9a19 \
      https://github.com/tilt-dev/tilt/releases/download/v0.37.9/tilt.0.37.9.linux.x86_64.tar.gz \
    && tar -C /usr/local/bin -xzf /tmp/tilt.tgz tilt \
    && rm -f /tmp/tilt.tgz

# Install Telepresence for connecting local dev to remote clusters.
RUN --mount=type=bind,source=build/fetch.sh,target=/tmp/fetch.sh sh /tmp/fetch.sh /usr/local/bin/telepresence b34ec6977a4636acf5f6047fcc4b7d04e73c60332f73af957218a6a5d942c4bc https://app.getambassador.io/download/tel2oss/releases/download/v2.18.0/telepresence-linux-amd64 \
    && chmod +x /usr/local/bin/telepresence

# Install kubectx and kubens for quick context/namespace switching.
RUN --mount=type=bind,source=build/fetch.sh,target=/tmp/fetch.sh sh /tmp/fetch.sh /tmp/kubectx.tgz a2247ffd23e79f89abdd0e8173379d7172511f02a3f63c9936d3824e0dd60648 https://github.com/ahmetb/kubectx/releases/download/v0.9.5/kubectx_v0.9.5_linux_x86_64.tar.gz \
    && tar -C /usr/local/bin -xzf /tmp/kubectx.tgz kubectx \
    && sh /tmp/fetch.sh /tmp/kubens.tgz acc1a9c7f6b722fbe5fad25dd0e784a7335d18436b9c414ab996629e82702cba https://github.com/ahmetb/kubectx/releases/download/v0.9.5/kubens_v0.9.5_linux_x86_64.tar.gz \
    && tar -C /usr/local/bin -xzf /tmp/kubens.tgz kubens \
    && rm -f /tmp/kubectx.tgz /tmp/kubens.tgz

# Install stern for multi-pod log tailing.
RUN --mount=type=bind,source=build/fetch.sh,target=/tmp/fetch.sh sh /tmp/fetch.sh /tmp/stern.tgz ea1bf1f1dddf1fd4b9971148582e88c637884ac1592dcba71838a6a42277708b https://github.com/stern/stern/releases/download/v1.30.0/stern_1.30.0_linux_amd64.tar.gz \
    && tar -C /usr/local/bin -xzf /tmp/stern.tgz stern \
    && rm -f /tmp/stern.tgz

# Install DevSpace for k8s development workflow.
# Was releases/latest; prod had v6.3.21.
RUN --mount=type=bind,source=build/fetch.sh,target=/tmp/fetch.sh sh /tmp/fetch.sh /usr/local/bin/devspace 9720a6c87a2465f6d73b4db5a908e15e1971158443b5de9162d94aa5f87f79f0 https://github.com/devspace-sh/devspace/releases/download/v6.3.21/devspace-linux-amd64 \
    && chmod +x /usr/local/bin/devspace

# Install ctlptl for declarative local cluster management.
RUN --mount=type=bind,source=build/fetch.sh,target=/tmp/fetch.sh sh /tmp/fetch.sh /tmp/ctlptl.tgz 98a55d8bfd61ab95b72e2e81d21e540d704065ce90d7972cd4ed8600dd091905 https://github.com/tilt-dev/ctlptl/releases/download/v0.8.28/ctlptl.0.8.28.linux.x86_64.tar.gz \
    && tar -C /usr/local/bin -xzf /tmp/ctlptl.tgz ctlptl \
    && rm -f /tmp/ctlptl.tgz

# Install vcluster for virtual clusters inside k8s.
# Was releases/latest; prod had v0.38.0.
RUN --mount=type=bind,source=build/fetch.sh,target=/tmp/fetch.sh sh /tmp/fetch.sh /usr/local/bin/vcluster a27849d95f6565fb0eaf452f593b17628116201dd054cde6d83354afac6f89e5 https://github.com/loft-sh/vcluster/releases/download/v0.38.0/vcluster-linux-amd64 \
    && chmod +x /usr/local/bin/vcluster

# Install python-based helpers for LLM tooling (placeholders for Claude/Codex/Gemini CLIs).
# certifi >= 2026.10.19 drops the eMudhra emSign roots (USN-8944-1).
# Exact pins of the set prod shipped on 2026-11-19, every file hash-checked (cluster-services-1496).
# Tier: upstream-pinned (PyPI); no PyPI mirror runs in the org yet. To change the set, re-resolve
# the four top-level packages and regenerate build/requirements-ai.txt with all PyPI hashes.
RUN --mount=type=bind,source=build/requirements-ai.txt,target=/tmp/requirements-ai.txt \
    echo "pip source: upstream-pinned https://pypi.org (--require-hashes)" \
    && pip3 install --no-cache-dir --require-hashes -r /tmp/requirements-ai.txt

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
# Account admin CLI (create/list/set-active/scramble-passwords). Run it in the gateway pod, which has the
# CNPG_* environment: kubectl exec deploy/gateway -- kubetty-authuser list (cluster-services-1534).
COPY --from=go-builder /workspace/kubetty-authuser /usr/local/bin/kubetty-authuser

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

# Fleet CA standard (operator decision 2026-11-13, SupportTools cluster-services-1375): NO eMudhra
# emSign roots at all (ECC G3, ECC C3, Root C1, Root G1), in any file of the image. This is the last
# root step, so nothing installed later can bring one back. noemsign-ca.sh is a vendored copy of the
# canonical SupportTools/cluster-services noemsign/noemsign-ca.sh (keep the sha256 identical;
# cluster-services-1438):
#  - strip: deselects every emSign entry in /etc/ca-certificates.conf ("!" lines survive later
#    ca-certificates upgrades), rebuilds the store with --fresh, then deletes every emSign certificate
#    file (including the deselected /usr/share/ca-certificates/mozilla/emSign_*.crt sources) and
#    rewrites every PEM bundle (certifi, gRPC roots.pem, ...) without its emSign certificates;
#  - guard: fails the build if any PEM/DER certificate anywhere on the filesystem still decodes to
#    an emSign/eMudhra subject. Node's compiled-in roots are a binary (WARN only); NODE_OPTIONS below
#    makes Node use the cleaned system store instead.
# No fetch: runs on what is already installed. This one image is both the gateway and the project
# sandbox image, so the guard covers both.
COPY --chmod=0755 noemsign-ca.sh /usr/local/sbin/noemsign-ca.sh
RUN /usr/local/sbin/noemsign-ca.sh strip / && /usr/local/sbin/noemsign-ca.sh guard /
ENV NODE_OPTIONS=--use-openssl-ca

EXPOSE 8080
USER mmattox
ENTRYPOINT ["/usr/local/bin/kubetty-entrypoint"]
