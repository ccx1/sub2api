# -*- coding: utf-8 -*-
from __future__ import print_function

import json
import subprocess


class HttpRequestError(RuntimeError):
    def __init__(self, message, status=None, payload=None):
        RuntimeError.__init__(self, message)
        self.status = status
        self.payload = payload


def request_json(method, url, headers=None, payload=None, timeout=30):
    marker = "__HTTP_STATUS__:"
    cmd = [
        "curl",
        "-sS",
        "-X",
        str(method).upper(),
        str(url),
        "--connect-timeout",
        str(timeout),
        "--max-time",
        str(timeout),
        "-w",
        "\n" + marker + "%{http_code}",
    ]

    request_headers = dict(headers or {})
    if payload is not None and "Content-Type" not in request_headers:
        request_headers["Content-Type"] = "application/json"

    for key, value in request_headers.items():
        cmd.extend(["-H", "%s: %s" % (str(key), _stringify(value))])

    if payload is not None:
        body = json.dumps(payload, ensure_ascii=False)
        cmd.extend(["--data-binary", _stringify(body)])

    try:
        process = subprocess.Popen(cmd, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        stdout, stderr = process.communicate()
    except OSError as exc:
        raise HttpRequestError("failed to execute curl: %s" % exc)

    raw = _decode_bytes(stdout)
    stderr_text = _decode_bytes(stderr).strip()

    if process.returncode != 0:
        detail = stderr_text or ("curl exited with code %s" % process.returncode)
        raise HttpRequestError("request failed: %s" % detail)

    status, body_text = _split_status(raw, marker)
    parsed = _try_parse_json(body_text)

    if status >= 400:
        detail = _extract_error_message(parsed) or body_text[:300] or "HTTP error"
        raise HttpRequestError("HTTP %s: %s" % (status, detail), status=status, payload=parsed)

    if not body_text.strip():
        return None

    if parsed is None:
        raise HttpRequestError("response is not valid JSON: %s" % body_text[:300])

    return parsed


def unwrap_api_envelope(payload):
    if not isinstance(payload, dict) or "code" not in payload:
        return payload

    if payload.get("code") == 0:
        return payload.get("data")

    detail = _extract_error_message(payload) or "API returned failure"
    raise HttpRequestError(detail, status=_safe_int(payload.get("code")), payload=payload)


def _split_status(raw_text, marker):
    index = raw_text.rfind(marker)
    if index == -1:
        return 0, raw_text
    body_text = raw_text[:index].rstrip("\r\n")
    status_text = raw_text[index + len(marker):].strip()
    try:
        status = int(status_text)
    except (TypeError, ValueError):
        status = 0
    return status, body_text


def _extract_error_message(payload):
    if not isinstance(payload, dict):
        return ""
    for key in ("message", "msg", "reason", "error", "detail"):
        value = _stringify(payload.get(key, "")).strip()
        if value:
            return value
    return ""


def _safe_int(value):
    try:
        return int(value)
    except (TypeError, ValueError):
        return None


def _try_parse_json(raw_text):
    try:
        return json.loads(raw_text)
    except ValueError:
        return None


def _decode_bytes(value):
    if value is None:
        return ""
    try:
        return value.decode("utf-8")
    except AttributeError:
        return value
    except UnicodeDecodeError:
        return value.decode("utf-8", "replace")


def _stringify(value):
    if value is None:
        return ""
    if isinstance(value, bytes):
        return value.decode("utf-8", "ignore")
    try:
        unicode_type = unicode  # noqa: F821
    except NameError:
        return str(value)

    if isinstance(value, unicode_type):
        return value
    return str(value)
