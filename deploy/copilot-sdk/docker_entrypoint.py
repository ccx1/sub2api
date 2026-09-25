"""Container entrypoint: server.py startup checks, bound to the container network.

server.py listens on 127.0.0.1 for same-host systemd deployments; inside Docker the
Sub2API container reaches the sidecar over the compose network instead. Requests
still require the sidecar bearer key; do not publish the port on the host.
"""
import os

import uvicorn

import server

if __name__ == "__main__":
    os.umask(0o077)
    uvicorn.run(server.load_app(),
                host=os.environ.get("COPILOT_SIDECAR_HOST", "0.0.0.0"),
                port=int(os.environ.get("COPILOT_SIDECAR_PORT", "18765")),
                access_log=False)
