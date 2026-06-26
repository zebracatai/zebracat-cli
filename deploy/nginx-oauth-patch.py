#!/usr/bin/env python3
"""Patch the Zebracat production nginx so the public API + OAuth discovery work.

It adds, to the EXISTING server blocks (only api.zebracat.ai and mcp.zebracat.ai):
  - api.zebracat.ai : /.well-known/ , /api/v1/public/ , /api/public/ , /api/oauth/  -> backend
  - mcp.zebracat.ai : /.well-known/                                                 -> mcpserver

Safe to run: backs up the file, validates with `nginx -t`, auto-rolls back on
failure, and is idempotent (re-running is a no-op).

Usage:
    curl -fsSL https://raw.githubusercontent.com/zebracatai/zebracat-cli/main/deploy/nginx-oauth-patch.py | sudo python3
    # or, to patch a different file / dry-run:
    sudo python3 nginx-oauth-patch.py [/path/to/nginx.conf] [--dry-run]
"""
import re
import shutil
import subprocess
import sys
import time

MARK = "ZEBRACAT-OAUTH-ROUTES"

API = """    # ZEBRACAT-OAUTH-ROUTES (public API + OAuth)
    location ^~ /.well-known/ {
        proxy_pass http://backend;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
    location /api/v1/public/ {
        limit_req zone=perip burst=5;
        proxy_pass http://backend;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        client_max_body_size 2048M;
        proxy_read_timeout 3000; proxy_connect_timeout 3000; proxy_send_timeout 3000;
    }
    location /api/public/ {
        limit_req zone=perip burst=5;
        proxy_pass http://backend;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        client_max_body_size 2048M;
        proxy_read_timeout 3000; proxy_connect_timeout 3000; proxy_send_timeout 3000;
    }
    location /api/oauth/ {
        limit_req zone=perip burst=5;
        proxy_pass http://backend;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
"""

MCP = """    # ZEBRACAT-OAUTH-ROUTES (MCP OAuth discovery)
    location ^~ /.well-known/ {
        proxy_pass http://mcpserver;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
"""

RETURN_404 = re.compile(r"location\s*/\s*\{\s*return\s+404\s*;\s*\}")


def insert(text, anchor, snippet):
    i = text.find(anchor)
    if i < 0:
        sys.exit("ABORT: could not find %r — no changes made." % anchor)
    m = RETURN_404.search(text, i)
    if not m:
        sys.exit("ABORT: no `location / { return 404; }` after %r — no changes made." % anchor)
    return text[: m.start()] + snippet + text[m.start():]


def main():
    args = [a for a in sys.argv[1:] if a != "--dry-run"]
    dry = "--dry-run" in sys.argv[1:]
    path = args[0] if args else "/etc/nginx/sites-enabled/default"

    src = open(path).read()
    if MARK in src:
        print("Already patched — nothing to do.")
        return

    new = insert(src, "server_name api.zebracat.ai;", API)
    new = insert(new, "server_name mcp.zebracat.ai;", MCP)

    if dry:
        sys.stdout.write(new)
        return

    bak = path + ".bak-" + time.strftime("%Y%m%d-%H%M%S")
    shutil.copy(path, bak)
    open(path, "w").write(new)
    print("Patched. Backup at", bak)

    test = subprocess.run(["nginx", "-t"], capture_output=True, text=True)
    sys.stdout.write(test.stderr)
    if test.returncode != 0:
        shutil.copy(bak, path)
        sys.exit("nginx -t FAILED — restored the backup, nothing changed.")

    reload = subprocess.run(["systemctl", "reload", "nginx"], capture_output=True, text=True)
    if reload.returncode != 0:
        sys.stdout.write(reload.stderr)
        sys.exit("reload failed (config is valid; check `systemctl status nginx`).")
    print("✓ nginx updated and reloaded.")


if __name__ == "__main__":
    main()
