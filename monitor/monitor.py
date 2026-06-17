#!/usr/bin/env python
# -*- coding: utf-8 -*-

from __future__ import print_function

import argparse
import base64
import codecs
import datetime
import hashlib
import hmac
import json
import os
import re
import ssl
import sys
import time
import urllib
import urllib2


DEFAULT_BASE_URL = "http://192.168.30.96:10888"
DEFAULT_APIKEY_FILE = "apikey.txt"
DEFAULT_TIMEOUT_SECONDS = 30
DEFAULT_TIMEZONE = "Asia/Shanghai"
DEFAULT_DINGTALK_KEYWORD = "Token额度"


class ApiKeyItem(object):
    def __init__(self, name, api_key):
        self.name = name
        self.api_key = api_key


class UsageRow(object):
    def __init__(self, name, api_key, status, actual_cost, requests, total_tokens, error=""):
        self.name = name
        self.api_key = api_key
        self.status = status
        self.actual_cost = actual_cost
        self.requests = requests
        self.total_tokens = total_tokens
        self.error = error


def parse_args():
    parser = argparse.ArgumentParser(
        description="读取同目录 apikey 清单，查询 Sub2API 今日使用额度，并可发送钉钉通知。"
    )
    parser.add_argument("--base-url", default=os.getenv("SUB2API_BASE_URL", DEFAULT_BASE_URL), help="Sub2API 根地址")
    parser.add_argument("--apikey-file", default=os.getenv("APIKEY_FILE", DEFAULT_APIKEY_FILE), help="API Key 清单文件")
    parser.add_argument("--timezone", default=os.getenv("TZ_NAME", DEFAULT_TIMEZONE), help="统计日期时区")
    parser.add_argument("--timeout", type=int, default=int(os.getenv("TIMEOUT_SECONDS", DEFAULT_TIMEOUT_SECONDS)), help="请求超时秒数")
    parser.add_argument("--insecure", action="store_true", help="跳过 HTTPS 证书校验")
    parser.add_argument("--dry-run", action="store_true", help="只打印结果，不发送钉钉")
    parser.add_argument("--dingtalk-webhook", default=os.getenv("DINGTALK_WEBHOOK", ""), help="钉钉机器人 webhook")
    parser.add_argument("--dingtalk-secret", default=os.getenv("DINGTALK_SECRET", ""), help="钉钉机器人加签 secret")
    parser.add_argument("--keyword", default=os.getenv("DINGTALK_KEYWORD", DEFAULT_DINGTALK_KEYWORD), help="钉钉关键词")
    return parser.parse_args()


def main():
    args = parse_args()
    script_dir = os.path.dirname(os.path.abspath(__file__))
    apikey_path = resolve_apikey_path(script_dir, args.apikey_file)
    target_date = get_today(args.timezone)

    items = read_apikeys(apikey_path)
    if not items:
        print_text(u"API Key 清单为空: {0}".format(to_text(apikey_path)), sys.stderr)
        return 2

    rows = []
    for item in items:
        rows.append(query_usage(item, args.base_url, target_date, args.timeout, not args.insecure))
    rows.sort(key=lambda row: row.actual_cost, reverse=True)

    print_text(format_console_report(target_date, rows))

    if args.dry_run:
        return 0
    if not args.dingtalk_webhook:
        print_text(u"未配置钉钉 webhook，已跳过通知。可设置 DINGTALK_WEBHOOK 或传 --dingtalk-webhook。")
        return 0

    send_dingtalk_markdown(
        webhook=args.dingtalk_webhook,
        secret=args.dingtalk_secret,
        title=u"Sub2API 今日额度表 {0}".format(target_date),
        text=format_dingtalk_markdown(target_date, rows, args.keyword),
        timeout_seconds=args.timeout,
    )
    print_text(u"钉钉通知已发送。")
    return 0


def print_text(text, stream=sys.stdout):
    if isinstance(text, unicode):
        data = text.encode("utf-8")
    else:
        data = text
    stream.write(data + "\n")


def to_text(value):
    if isinstance(value, unicode):
        return value
    if isinstance(value, str):
        return value.decode("utf-8", "replace")
    if isinstance(value, BaseException):
        parts = []
        for item in value.args:
            try:
                parts.append(to_text(item))
            except Exception:
                parts.append(to_text(repr(item)))
        if parts:
            return u" ".join(parts)
    try:
        return unicode(value)
    except UnicodeDecodeError:
        return unicode(repr(value), "utf-8", "replace")
    except Exception:
        return unicode(repr(value), "utf-8", "replace")


def resolve_apikey_path(base_dir, raw_path):
    path = raw_path
    if not os.path.isabs(path):
        path = os.path.join(base_dir, path)
    if os.path.exists(path):
        return path
    if ":" in raw_path or "\\" in raw_path:
        fallback = os.path.join(base_dir, os.path.basename(raw_path.replace("\\", "/")))
        if os.path.exists(fallback):
            return fallback
    alt_name = None
    if os.path.basename(path) == "apikey.txt":
        alt_name = "apikeys.txt"
    elif os.path.basename(path) == "apikeys.txt":
        alt_name = "apikey.txt"
    if alt_name:
        alt_path = os.path.join(os.path.dirname(path), alt_name)
        if os.path.exists(alt_path):
            return alt_path
    return path


def read_apikeys(path):
    if not os.path.exists(path):
        raise IOError(u"API Key 清单不存在: {0}".format(to_text(path)))

    items = []
    fh = codecs.open(path, "r", "utf-8-sig")
    try:
        for line_no, raw in enumerate(fh, start=1):
            line = raw.strip()
            if not line or line.startswith(u"#"):
                continue
            name, api_key = parse_apikey_line(line)
            if not api_key:
                raise ValueError(u"{0}:{1} 格式错误，预期为: 名称,sk-xxx 或 名称<TAB>sk-xxx".format(path, line_no))
            if not name:
                name = mask_api_key(api_key)
            items.append(ApiKeyItem(name, api_key))
    finally:
        fh.close()
    return items


def parse_apikey_line(line):
    if u"," in line:
        parts = [item.strip() for item in line.split(u",", 1)]
    else:
        parts = [item.strip() for item in re.split(r"\s+", line, maxsplit=1) if item.strip()]

    if len(parts) == 1 and parts[0].startswith(u"sk-"):
        return mask_api_key(parts[0]), parts[0]
    if len(parts) >= 2:
        return parts[0], parts[1]
    return u"", u""


def query_usage(item, base_url, target_date, timeout_seconds, verify_ssl):
    params = urllib.urlencode({"start_date": target_date, "end_date": target_date})
    url = "{0}/v1/usage?{1}".format(base_url.rstrip("/"), params)
    request = urllib2.Request(
        url=url,
        headers={
            "Authorization": "Bearer {0}".format(item.api_key),
            "Content-Type": "application/json",
        },
    )

    try:
        response = open_url(request, timeout_seconds, verify_ssl)
        try:
            payload = json.loads(response.read().decode("utf-8"))
        finally:
            response.close()
    except urllib2.HTTPError as exc:
        return build_error_row(item, parse_http_error(exc))
    except Exception as exc:
        return build_error_row(item, to_text(exc))

    usage_today = get_dict(get_dict(payload, u"usage"), u"today")
    return UsageRow(
        name=item.name,
        api_key=item.api_key,
        status=to_text(payload.get(u"status", u"unknown")),
        actual_cost=float(usage_today.get(u"actual_cost", 0.0)),
        requests=int(usage_today.get(u"requests", 0)),
        total_tokens=int(usage_today.get(u"total_tokens", 0)),
    )


def open_url(request, timeout_seconds, verify_ssl):
    url = request.get_full_url().lower()
    if url.startswith("https://") and not verify_ssl:
        try:
            context = ssl._create_unverified_context()
            handler = urllib2.HTTPSHandler(context=context)
            opener = urllib2.build_opener(handler)
            return opener.open(request, timeout=timeout_seconds)
        except Exception:
            pass
    return urllib2.urlopen(request, timeout=timeout_seconds)


def get_dict(payload, key):
    value = payload.get(key)
    return value if isinstance(value, dict) else {}


def build_error_row(item, message):
    return UsageRow(item.name, item.api_key, "error", 0.0, 0, 0, message)


def parse_http_error(exc):
    raw = exc.read().decode("utf-8", "replace")
    try:
        data = json.loads(raw)
    except ValueError:
        return raw or "HTTP {0}".format(exc.code)

    if isinstance(data, dict):
        error_field = data.get("error")
        if isinstance(error_field, dict) and error_field.get("message"):
            return to_text(error_field.get("message"))
        if data.get("message"):
            return to_text(data.get("message"))
    return raw or "HTTP {0}".format(exc.code)


def format_console_report(target_date, rows):
    success_count = sum(1 for row in rows if not row.error)
    failed_count = len(rows) - success_count
    total_cost = sum(row.actual_cost for row in rows)
    lines = [
        u"Sub2API 今日额度表 - {0}".format(target_date),
        u"成功 {0} | 失败 {1} | 总额度 ${2:.4f}".format(success_count, failed_count, total_cost),
        u"",
        u"名称\t今日额度(USD)\t请求数\t总Token\t状态",
    ]
    for row in rows:
        amount = u"{0:.4f}".format(row.actual_cost) if not row.error else u"-"
        status = to_text(row.status) if not row.error else u"error: {0}".format(to_text(row.error))
        lines.append(u"{0}\t{1}\t{2}\t{3}\t{4}".format(to_text(row.name), amount, row.requests, row.total_tokens, status))
    return u"\n".join(lines)


def format_dingtalk_markdown(target_date, rows, keyword):
    success_count = sum(1 for row in rows if not row.error)
    failed_count = len(rows) - success_count
    total_cost = sum(row.actual_cost for row in rows)
    lines = []
    if to_text(keyword).strip():
        lines.append(to_text(keyword).strip())
    lines.extend(
        [
            u"### Sub2API 今日额度表 {0}".format(target_date),
            u"- 成功: {0}".format(success_count),
            u"- 失败: {0}".format(failed_count),
            u"- 总额度: ${0:.4f}".format(total_cost),
            u"",
            u"| 名称 | 今日额度(USD) | 请求数 | 总Token | 状态 |",
            u"| --- | ---: | ---: | ---: | --- |",
        ]
    )
    for row in rows:
        amount = u"{0:.4f}".format(row.actual_cost) if not row.error else u"-"
        status = to_text(row.status) if not row.error else u"error: {0}".format(to_text(row.error))
        lines.append(u"| {0} | {1} | {2} | {3} | {4} |".format(
            escape_markdown_table(to_text(row.name)),
            amount,
            row.requests,
            row.total_tokens,
            escape_markdown_table(status),
        ))
    return u"\n".join(lines)


def send_dingtalk_markdown(webhook, secret, title, text, timeout_seconds):
    payload = {
        "msgtype": "markdown",
        "markdown": {"title": title, "text": text},
        "at": {"atMobiles": [], "isAtAll": False},
    }
    data = json.dumps(payload).encode("utf-8")
    request = urllib2.Request(
        url=signed_webhook(webhook, secret),
        data=data,
        headers={"Content-Type": "application/json"},
    )
    response = urllib2.urlopen(request, timeout=timeout_seconds)
    try:
        raw = response.read().decode("utf-8")
    finally:
        response.close()
    result = json.loads(raw)
    if int(result.get("errcode", -1)) != 0:
        raise RuntimeError(u"钉钉消息发送失败: {0}".format(to_text(raw)))


def signed_webhook(webhook, secret):
    if not secret:
        return webhook
    timestamp = str(int(time.time() * 1000))
    string_to_sign = "{0}\n{1}".format(timestamp, secret)
    digest = hmac.new(secret.encode("utf-8"), string_to_sign.encode("utf-8"), hashlib.sha256).digest()
    sign = urllib.quote_plus(base64.b64encode(digest))
    separator = "&" if "?" in webhook else "?"
    return "{0}{1}timestamp={2}&sign={3}".format(webhook, separator, timestamp, sign)


def mask_api_key(api_key):
    value = to_text(api_key).strip()
    if len(value) <= 12:
        return value or u"unknown_key"
    return u"{0}...{1}".format(value[:8], value[-4:])


def escape_markdown_table(value):
    return to_text(value).replace(u"|", u"\\|").replace(u"\n", u" ")


def parse_timezone_offset_minutes(timezone_name):
    value = to_text(timezone_name).strip()
    if not value:
        return None
    normalized = value.lower()
    presets = {
        "asia/shanghai": 8 * 60,
        "asia/chongqing": 8 * 60,
        "prc": 8 * 60,
        "cst": 8 * 60,
        "cst8": 8 * 60,
        "utc": 0,
        "gmt": 0,
        "z": 0,
    }
    if normalized in presets:
        return presets[normalized]
    if normalized.startswith("utc") or normalized.startswith("gmt"):
        normalized = normalized[3:]
    if normalized and normalized[0] in "+-":
        sign = 1 if normalized[0] == "+" else -1
        raw = normalized[1:]
        if ":" in raw:
            parts = raw.split(":", 1)
            hours = parts[0]
            minutes = parts[1]
        else:
            hours = raw
            minutes = "0"
        if hours.isdigit() and minutes.isdigit():
            return sign * (int(hours) * 60 + int(minutes))
    return None


def get_today(timezone_name):
    offset_minutes = parse_timezone_offset_minutes(timezone_name)
    if offset_minutes is not None:
        delta = datetime.timedelta(minutes=offset_minutes)
        return (datetime.datetime.utcnow() + delta).date().isoformat()
    return datetime.datetime.now().date().isoformat()


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except KeyboardInterrupt:
        print_text("已中断", sys.stderr)
        raise SystemExit(130)
    except Exception as exc:
        print_text(u"执行失败: {0}".format(to_text(exc)), sys.stderr)
        raise SystemExit(1)
