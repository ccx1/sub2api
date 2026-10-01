#!/usr/bin/env python3
"""焚决：同一条出站先换票，生成时只带这一次换来的 cookie。"""

import argparse
import http.client
import json
import os
import re
import socket
import ssl
import sys
import threading
import time
import urllib.error
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import quote, urlsplit

URL = "https://chatgpt.com/backend-api/codex/responses"
HOST = "chatgpt.com"
COOKIE_NAMES = ("__cflb", "__oailb")
# 个人票常见 292，Team 票常见 332。两种都算合格，312 是降智。
GOOD_LEN = {292, 332}
SEED_GOOD = ("澳大利亚【3x】", "法国【3x】", "荷兰【3x】", "波兰原生【3x】")
TIMEOUT = 20
HOME_NODE = "🇭🇰【亚洲】香港01丨直连"


def tool_declaration():
    # 官方客户端把工具放在 additional_tools 里，再配 Lite 头。
    # 这样铸出来的是工具回合的票，而不是一句闲聊换来的票。
    # 声明只用来对齐信封，提示里要求不要调用它。
    return {
        "type": "additional_tools",
        "role": "developer",
        "tools": [
            {
                "type": "namespace",
                "name": "codex",
                "description": "local tools",
                "tools": [
                    {
                        "type": "function",
                        "name": "noop",
                        "description": "Do nothing.",
                        "strict": False,
                        "parameters": {
                            "type": "object",
                            "properties": {},
                            "additionalProperties": False,
                        },
                    }
                ],
            }
        ],
    }

IDENTITY = {
    "Originator": "codex_cli_rs",
    "User-Agent": "codex_cli_rs/0.155.0",
    "Version": "0.155.0",
    "OpenAI-Beta": "responses_websockets=2026-02-06",
    "X-OpenAI-Internal-Codex-Responses-Lite": "true",
}


def mint_body():
    return {
        "model": "gpt-6-astra",
        "instructions": "Reply with OK. Do not call tools.",
        "input": [
            tool_declaration(),
            {
                "type": "message",
                "role": "user",
                "content": [{"type": "input_text", "text": "Reply with OK. Do not call tools."}],
            },
        ],
        "stream": True,
        "store": False,
        "parallel_tool_calls": False,
        "include": ["reasoning.encrypted_content"],
        "reasoning": {"context": "all_turns"},
    }


def user_body(prompt):
    return {
        "model": "gpt-6-astra",
        "instructions": "Reply to the user. Do not call tools.",
        "input": [
            tool_declaration(),
            {
                "type": "message",
                "role": "user",
                "content": [{"type": "input_text", "text": prompt}],
            },
        ],
        "stream": True,
        "store": False,
        "parallel_tool_calls": False,
        "include": ["reasoning.encrypted_content"],
        "reasoning": {"context": "all_turns"},
    }


def pair_from_set_cookie(lines):
    found = {}
    for line in lines or []:
        nv = line.split(";", 1)[0].strip()
        if "=" not in nv:
            continue
        name, value = nv.split("=", 1)
        name, value = name.strip(), value.strip()
        if name in COOKIE_NAMES and value and "\n" not in value and "\r" not in value:
            found[name] = value
    if any(name not in found for name in COOKIE_NAMES):
        return ""
    return "; ".join(f"{name}={found[name]}" for name in COOKIE_NAMES)


def first_model(raw):
    match = re.search(r'"model"\s*:\s*"([^"]+)"', raw or "")
    return match.group(1) if match else ""


def load_account(path):
    with open(path, encoding="utf-8") as fh:
        doc = json.load(fh)
    if not isinstance(doc, dict):
        raise SystemExit("账号文件最外层必须是 JSON 对象")
    creds = doc.get("credentials") if isinstance(doc.get("credentials"), dict) else {}
    token = doc.get("access_token") or creds.get("access_token") or ""
    account = (
        doc.get("chatgpt_account_id")
        or doc.get("account_id")
        or creds.get("chatgpt_account_id")
        or creds.get("account_id")
        or ""
    )
    token, account = str(token).strip(), str(account).strip()
    if not token or not account:
        raise SystemExit("账号文件里要有 access_token，以及 chatgpt_account_id 或 account_id")
    return token, account


class Tunnel(http.client.HTTPSConnection):
    def __init__(self, proxy, timeout):
        self._proxy = urlsplit(proxy) if proxy else None
        if self._proxy is None:
            super().__init__(HOST, 443, timeout=timeout)
            return
        super().__init__(self._proxy.hostname, self._proxy.port or 80, timeout=timeout)
        self.set_tunnel(HOST, 443)

def send(conn, headers, body):
    data = json.dumps(body, ensure_ascii=False).encode()
    merged = {
        "Content-Type": "application/json",
        "Accept": "text/event-stream",
        "Content-Length": str(len(data)),
    }
    merged.update(headers)
    merged.pop("Cookie", None)
    if headers.get("Cookie"):
        merged["Cookie"] = headers["Cookie"]
    conn.request("POST", "/backend-api/codex/responses", body=data, headers=merged)
    resp = conn.getresponse()
    raw = resp.read(1 << 20).decode("utf-8", "replace")
    return resp.status, raw, list(resp.headers.get_all("Set-Cookie") or []), resp.getheader("X-Codex-Turn-State") or ""


def base_headers(token, account_id):
    headers = dict(IDENTITY)
    headers["Authorization"] = "Bearer " + token
    headers["ChatGPT-Account-ID"] = account_id
    return headers


def blank(error=""):
    item = {
        "mint_status": 0,
        "mint_len": 0,
        "mint_model": "",
        "cookie": False,
        "lite": True,
        "reuse": False,
        "gen_status": 0,
        "gen_model": "",
    }
    if error:
        item["error"] = error
    return item


def mint_on(conn, token, account_id):
    headers = base_headers(token, account_id)
    status, raw, cookies, ticket = send(conn, headers, mint_body())
    ticket = ticket.strip()
    pair = pair_from_set_cookie(cookies)
    return {
        "mint_status": status,
        "mint_len": len(ticket),
        "mint_model": first_model(raw),
        "cookie": bool(pair),
        "lite": headers.get("X-OpenAI-Internal-Codex-Responses-Lite") == "true",
        "reuse": False,
        "gen_status": 0,
        "gen_model": "",
        "ticket": ticket,
        "pair": pair,
    }


def gen_on(conn, token, account_id, prompt, ticket, pair):
    headers = base_headers(token, account_id)
    headers["X-Codex-Turn-State"] = ticket
    headers["Cookie"] = pair
    status, raw, _, _ = send(conn, headers, user_body(prompt))
    return {
        "mint_status": 200,
        "mint_len": len(ticket),
        "mint_model": "gpt-6-astra",
        "cookie": True,
        "lite": headers.get("X-OpenAI-Internal-Codex-Responses-Lite") == "true",
        "reuse": True,
        "gen_status": status,
        "gen_model": first_model(raw),
    }


def reuse_burst(token, account_id, proxy, prompt, rounds, factory=None):
    conn = factory() if factory else Tunnel(proxy, TIMEOUT)
    try:
        minted = mint_on(conn, token, account_id)
        rows = [minted]
        if (
            not 200 <= minted["mint_status"] < 300
            or minted["mint_len"] not in GOOD_LEN
            or minted["mint_model"] != "gpt-6-astra"
            or not minted["pair"]
        ):
            return rows
        for _ in range(rounds):
            item = gen_on(conn, token, account_id, prompt, minted["ticket"], minted["pair"])
            rows.append(item)
            if not full(item):
                break
        return rows
    except (OSError, http.client.HTTPException, socket.timeout) as exc:
        return [blank(exc.__class__.__name__)]
    finally:
        conn.close()


def one_round(conn, token, account_id, prompt):
    status, raw, cookies, ticket = send(conn, base_headers(token, account_id), mint_body())
    mint_model = first_model(raw)
    pair = pair_from_set_cookie(cookies)
    result = {
        "mint_status": status,
        "mint_len": len(ticket.strip()),
        "mint_model": mint_model,
        "cookie": bool(pair),
        "gen_status": 0,
        "gen_model": "",
    }
    ticket = ticket.strip()
    # 312 / luna 的票留着再生成只会再掉一次。换下一个出口。
    if len(ticket) not in GOOD_LEN or mint_model != "gpt-6-astra" or not pair:
        return result
    headers = base_headers(token, account_id)
    headers["X-Codex-Turn-State"] = ticket
    if pair:
        headers["Cookie"] = pair
    gen_status, gen_raw, _, _ = send(conn, headers, user_body(prompt))
    result["gen_status"] = gen_status
    result["gen_model"] = first_model(gen_raw)
    return result


def line_of(i, item):
    return (
        f"{i} mint_status={item['mint_status']} mint_len={item['mint_len']} "
        f"mint_model={item['mint_model'] or '-'} cookie={str(item['cookie']).lower()} "
        f"lite={str(item.get('lite', True)).lower()} reuse={str(item.get('reuse', False)).lower()} "
        f"gen_status={item['gen_status']} gen_model={item['gen_model'] or '-'}"
    )


def run(url_unused, token, account_id, proxy, prompt, rounds, factory=None):
    del url_unused
    rows = reuse_burst(token, account_id, proxy, prompt, rounds, factory=factory)
    hits = sum(1 for item in rows if item.get("reuse") and full(item))
    for i, item in enumerate(rows, 1):
        text = line_of(i, item)
        if item.get("error"):
            text += " err=" + item["error"]
        print(text, flush=True)
    print(f"{hits}/{rounds}", flush=True)
    return hits, rows


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"
    pairs = []
    seen = []

    def log_message(self, fmt, *args):
        return

    def do_POST(self):
        length = int(self.headers.get("Content-Length") or 0)
        raw = self.rfile.read(length)
        try:
            doc = json.loads(raw.decode() or "{}")
        except json.JSONDecodeError:
            doc = {}
        kinds = [item.get("type") for item in doc.get("input") or [] if isinstance(item, dict)]
        cookie = self.headers.get("Cookie") or ""
        ticket = self.headers.get("X-Codex-Turn-State") or ""
        Handler.seen.append({
            "cookie": cookie,
            "ticket": ticket,
            "lite": self.headers.get("X-OpenAI-Internal-Codex-Responses-Lite") or "",
            "tools": "additional_tools" in kinds,
            "parallel": doc.get("parallel_tool_calls"),
        })
        if not ticket:
            pair = Handler.pairs[len([s for s in Handler.seen if not s["ticket"]]) - 1]
            body = json.dumps({"model": "gpt-6-astra"}).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("X-Codex-Turn-State", pair["ticket"])
            self.send_header("Set-Cookie", f"__cflb={pair['cflb']}; Path=/")
            self.send_header("Set-Cookie", f"__oailb={pair['oailb']}; Path=/")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
            return
        body = json.dumps({"model": "gpt-6-astra", "output_text": "OK"}).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


def self_test():
    Handler.pairs = [
        {"ticket": "a" * 292, "cflb": "cA", "oailb": "oA"},
    ]
    Handler.seen = []
    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    host, port = server.server_address
    try:
        rows = reuse_burst("tok", "acct", "", "OK", 3, factory=lambda: http.client.HTTPConnection(host, port, timeout=5))
    finally:
        server.shutdown()
    mints = [item for item in Handler.seen if not item["ticket"]]
    gens = [item for item in Handler.seen if item["ticket"]]
    if len(mints) != 1 or mints[0]["cookie"] or mints[0]["lite"] != "true" or not mints[0]["tools"] or mints[0]["parallel"] is not False:
        raise SystemExit("铸票应该只有一次，带 Lite 和工具声明，不带 cookie")
    if len(gens) != 3:
        raise SystemExit("合格的票应该复用三次，不再重铸")
    if any(item["lite"] != "true" or not item["tools"] or item["parallel"] is not False for item in gens):
        raise SystemExit("复用时 Lite 或工具声明掉了")
    if any(item["ticket"] != "a" * 292 or "__cflb=cA" not in item["cookie"] or "__oailb=oA" not in item["cookie"] for item in gens):
        raise SystemExit("复用没有沿用同一对")
    if sum(1 for item in rows if item.get("reuse") and item.get("gen_model") == "gpt-6-astra") != 3:
        raise SystemExit("自测计数不对")
    print("self-test ok")


def usable(name):
    upper = name.upper()
    if upper in {"DIRECT", "REJECT", "GLOBAL", "PASS", "PROXY"}:
        return False
    if "REJECT" in upper or "故障" in name:
        return False
    return True


def rank(name):
    score = 0
    if "直连" in name:
        score += 5
    if "家宽" in name:
        score += 4
    if "香港" in name:
        score += 3
    if "日本" in name:
        score += 3
    if "台湾" in name:
        score += 3
    if "新加坡" in name:
        score += 2
    if "美国" in name:
        score += 1
    return score


def mihomo_call(api, group, name=None):
    url = api.rstrip("/") + "/proxies/" + quote(group, safe="")
    data = None
    method = "GET"
    headers = {}
    if name is not None:
        data = json.dumps({"name": name}).encode()
        method = "PUT"
        headers["Content-Type"] = "application/json"
    req = urllib.request.Request(url, data=data, method=method, headers=headers)
    with urllib.request.urlopen(req, timeout=5) as resp:
        raw = resp.read()
    if not raw:
        return {}
    return json.loads(raw)


def memory_path():
    return Path.home() / ".subcpa" / "fenjue-nodes.json"


def load_memory():
    try:
        return json.loads(memory_path().read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return {"good": [], "fail": {}}


def save_memory(data):
    path = memory_path()
    path.parent.mkdir(parents=True, exist_ok=True)
    tmp = path.with_suffix(".json.tmp")
    tmp.write_text(json.dumps(data, ensure_ascii=False, indent=2), encoding="utf-8")
    os.replace(tmp, path)


def remember_good(name):
    data = load_memory()
    good = [item for item in data.get("good", []) if item != name]
    good.insert(0, name)
    data["good"] = good[:12]
    data.setdefault("fail", {}).pop(name, None)
    save_memory(data)


def remember_bad(name, seconds):
    data = load_memory()
    data.setdefault("fail", {})[name] = time.time() + seconds
    save_memory(data)


def node_list(api, group):
    doc = mihomo_call(api, group)
    names = [name for name in (doc.get("all") or []) if usable(name)]
    mem = load_memory()
    now = time.time()
    cooling = {name for name, until in (mem.get("fail") or {}).items() if until > now}
    seen = set()
    ordered = []
    for name in mem.get("good") or []:
        if name in names and name not in cooling and name not in seen:
            ordered.append(name)
            seen.add(name)
    for hint in SEED_GOOD:
        for name in names:
            if hint in name and name not in cooling and name not in seen:
                ordered.append(name)
                seen.add(name)
    rest = [name for name in names if name not in seen and name not in cooling]
    rest.sort(key=rank, reverse=True)
    return ordered + rest


def attempt(token, account_id, proxy, prompt):
    conn = Tunnel(proxy, TIMEOUT)
    try:
        return one_round(conn, token, account_id, prompt)
    except (OSError, http.client.HTTPException, socket.timeout) as exc:
        return {
            "mint_status": 0,
            "mint_len": 0,
            "mint_model": "",
            "cookie": False,
            "gen_status": 0,
            "gen_model": "",
            "error": exc.__class__.__name__,
        }
    finally:
        conn.close()


def show(i, item, node=""):
    text = line_of(i, item)
    if node:
        text = node + " " + text
    if item.get("error"):
        text += " err=" + item["error"]
    print(text, flush=True)


def full(item):
    status = item.get("gen_status", 0)
    return 200 <= status < 300 and item.get("gen_model") == "gpt-6-astra"


def rotate(token, account_id, proxy, prompt, rounds, api, group):
    hits = 0
    tried = 0
    try:
        nodes = node_list(api, group)
        if not nodes:
            raise SystemExit("代理组里没有可换的节点")
        for _ in range(2):
            for name in nodes:
                try:
                    mihomo_call(api, group, name)
                except (OSError, urllib.error.URLError):
                    show(tried + 1, {"mint_status": 0, "mint_len": 0, "mint_model": "", "cookie": False, "gen_status": 0, "gen_model": "", "error": "switch"}, name)
                    continue
                tried += 1
                rows = reuse_burst(token, account_id, proxy, prompt, rounds)
                for item in rows:
                    show(tried, item, name)
                gens = [item for item in rows if item.get("reuse")]
                hits = sum(1 for item in gens if full(item))
                mint = rows[0] if rows else {}
                success = len(gens) == rounds and hits == rounds
                if success:
                    remember_good(name)
                    print(f"{hits}/{rounds} {name}", flush=True)
                    return hits
                if mint.get("error") in {"TimeoutError", "SSLEOFError"}:
                    remember_bad(name, 90)
                else:
                    remember_bad(name, 180)
        print(f"0/{tried}", flush=True)
        return 0
    finally:
        try:
            mihomo_call(api, group, HOME_NODE)
        except (OSError, urllib.error.URLError, ValueError):
            pass


def main():
    parser = argparse.ArgumentParser(description="焚决：同一条出站换票并生成")
    parser.add_argument("--account", help="含 access_token，以及 chatgpt_account_id 或 account_id 的 json")
    parser.add_argument("--proxy", default="http://127.0.0.1:17890", help="http 代理，例如 http://127.0.0.1:17890")
    parser.add_argument("--n", type=int, default=5)
    parser.add_argument("--prompt", default="Reply with the single word OK")
    parser.add_argument("--rotate", action="store_true", help="按本地 mihomo 选择器轮换节点，直到出满血")
    parser.add_argument("--api", default="http://127.0.0.1:19090")
    parser.add_argument("--group", default="宝可梦")
    parser.add_argument("--self-test", action="store_true")
    args = parser.parse_args()
    if args.self_test:
        self_test()
        return
    if args.n < 1:
        parser.error("--n 必须是正整数")
    if not args.account:
        raise SystemExit("要 --account，或者 --self-test")
    token, account_id = load_account(args.account)
    if args.rotate:
        rotate(token, account_id, args.proxy, args.prompt, args.n, args.api, args.group)
        return
    run(URL, token, account_id, args.proxy, args.prompt, args.n)


if __name__ == "__main__":
    try:
        main()
    except ssl.SSLError as exc:
        print("tls " + exc.__class__.__name__, file=sys.stderr)
