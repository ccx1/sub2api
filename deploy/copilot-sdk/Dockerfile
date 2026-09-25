# Copilot SDK sidecar image: pinned ghcp_proxy checkout + github-copilot-sdk 1.0.14.
# Build context is this directory (deploy/copilot-sdk). The runtime needs glibc,
# so keep a Debian-based image; Alpine/musl is not supported here.
FROM python:3.12-slim-bookworm

ARG GHCP_REPO=https://github.com/Nonary/ghcp_proxy.git
ARG GHCP_REVISION=ad23ce2db3b5212c0355762d981c3877322fb160

RUN apt-get update \
    && apt-get install -y --no-install-recommends git ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# server.py verifies the checkout revision and a clean tracked tree at startup.
RUN git init -q /opt/adapter \
    && git -C /opt/adapter remote add origin "$GHCP_REPO" \
    && git -C /opt/adapter fetch -q --depth 1 origin "$GHCP_REVISION" \
    && git -C /opt/adapter checkout -q --detach FETCH_HEAD \
    && test "$(git -C /opt/adapter rev-parse HEAD)" = "$GHCP_REVISION" \
    && git config --system --add safe.directory /opt/adapter

RUN pip install --no-cache-dir -r /opt/adapter/requirements.txt 'github-copilot-sdk==1.0.14' \
    && python -m compileall -q /opt/adapter

# Bake the Copilot runtime bundle so the first request does not download ~60MB.
ENV COPILOT_CLI_EXTRACT_DIR=/opt/copilot-runtime
RUN python -c "from copilot._cli_download import download_cli; print(download_cli())"

COPY server.py login.py docker_entrypoint.py /app/

RUN useradd --system --uid 10001 --home-dir /data --shell /usr/sbin/nologin copilot \
    && install -d -o copilot -g copilot -m 0700 /data

ENV HOME=/data \
    COPILOT_ADAPTER_DIR=/opt/adapter \
    GHCP_STATE_DIR=/data \
    GHCP_CONFIG_DIR=/data/config \
    GHCP_CACHE_DIR=/data/cache \
    COPILOT_SIDECAR_KEY_FILE=/data/sidecar-key \
    COPILOT_SIGNING_KEY_FILE=/data/signing-key \
    COPILOT_SIDECAR_PORT=18765 \
    COPILOT_ALLOW_DIRECT=0 \
    PYTHONUNBUFFERED=1

USER copilot
WORKDIR /opt/adapter
VOLUME ["/data"]
EXPOSE 18765

HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
    CMD python -c "import os,urllib.request; urllib.request.urlopen('http://127.0.0.1:%s/healthz' % os.environ.get('COPILOT_SIDECAR_PORT', '18765'), timeout=3)"

CMD ["python", "/app/docker_entrypoint.py"]
