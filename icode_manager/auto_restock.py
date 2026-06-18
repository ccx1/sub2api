# -*- coding: utf-8 -*-
from __future__ import print_function

import argparse
import io
import os
import socket
import sys
from datetime import datetime

from config_loader import (
    ensure_product_group_alignment,
    get_product,
    load_json_config,
    output_prefix,
    resolve_goods_ids,
    resolve_output_dir,
    validate_lianjia_config,
    validate_sub2api_config,
)
from listing_client import GoodsCardStorageClient, extract_goods_stock_map
from sub2api_client import Sub2ApiClient
from http_utils import HttpRequestError, request_json

DEFAULT_SUB2API_CONFIG_PATH = os.path.join(os.path.dirname(__file__), "sub2api-config.json")
DEFAULT_LIANJIA_CONFIG_PATH = os.path.join(os.path.dirname(__file__), "lianjia-config.json")


def build_parser():
    parser = argparse.ArgumentParser(description="Auto restock lianjia goods based on stock_count")
    parser.add_argument("--sub2api-config", default=DEFAULT_SUB2API_CONFIG_PATH, help="path to sub2api-config.json")
    parser.add_argument("--lianjia-config", default=DEFAULT_LIANJIA_CONFIG_PATH, help="path to lianjia-config.json")
    parser.add_argument("--product", help="only process one product; omit to process all products")
    parser.add_argument("--stock-threshold", type=int, default=99, help="restock when stock is below this value")
    parser.add_argument("--min-shortage-to-restock", type=int, default=51, help="only restock when threshold-stock is greater than this value minus 1; default means shortage must be > 50")
    parser.add_argument("--restock-count", type=int, help="override restock count; default is threshold minus current stock")
    parser.add_argument("--summary-only", action="store_true", help="only collect and notify stock summary; do not generate or upload")
    parser.add_argument("--dry-run", action="store_true", help="print plan only; do not generate or upload")
    parser.add_argument("--totp-code", help="provide 2FA code at runtime")
    parser.add_argument("--turnstile-token", help="provide Turnstile token at runtime")
    parser.add_argument("--notify-webhook", help="override DingTalk webhook")
    parser.add_argument("--notify-keyword", help="override DingTalk keyword")
    return parser


def main():
    args = build_parser().parse_args()
    sub2api_config, sub2api_config_dir = load_json_config(args.sub2api_config)
    lianjia_config, _ = load_json_config(args.lianjia_config)
    validate_sub2api_config(sub2api_config)
    validate_lianjia_config(lianjia_config, args.product)

    threshold = int(args.stock_threshold)
    if threshold < 0:
        raise SystemExit("--stock-threshold must be >= 0")
    if int(args.min_shortage_to_restock) < 0:
        raise SystemExit("--min-shortage-to-restock must be >= 0")

    notification = resolve_notification_settings(lianjia_config, args)
    product_keys = [args.product] if args.product else list(lianjia_config["products"].keys())
    output_dir = resolve_output_dir(sub2api_config, sub2api_config_dir)
    sub2api_client = Sub2ApiClient(sub2api_config)
    goods_client = GoodsCardStorageClient(lianjia_config)
    any_failed = False
    summary_lines = []
    run_status = "SUCCESS"
    restock_happened = False
    notify_needed = False
    try:
        goods_map = extract_goods_stock_map(goods_client.list_goods())

        for product_key in product_keys:
            product = get_product(lianjia_config, product_key)
            label = _display(product.get("label", "")).strip() or _display(product_key)
            group_id = ensure_product_group_alignment(sub2api_config, lianjia_config, product_key)
            goods_ids = resolve_goods_ids(lianjia_config, product_key)

            stock_rows = []
            min_stock = None
            missing_goods_ids = []
            for goods_id in goods_ids:
                goods_item = goods_map.get(goods_id)
                if not goods_item:
                    missing_goods_ids.append(goods_id)
                    continue
                stock_count = int(goods_item["stock_count"])
                stock_rows.append(goods_item)
                if min_stock is None or stock_count < min_stock:
                    min_stock = stock_count

            _print_line("product: %s (%s)" % (_display(product_key), label))
            _print_line("group_id: %s" % (group_id if group_id is not None else "-"))
            _print_line("goods_ids: %s" % goods_ids)

            if missing_goods_ids:
                any_failed = True
                summary_lines.append(
                    u"%s: missing goods_ids %s" % (_to_unicode(product_key), _to_unicode(missing_goods_ids))
                )
                _print_line("goods_ids not found in goods list: %s" % missing_goods_ids)
                _print_line("")
                continue

            for row in stock_rows:
                _print_line("- goods_id=%s, name=%s, stock_count=%s" % (row["id"], _display(row["name"] or "-"), row["stock_count"]))

            if min_stock is None:
                any_failed = True
                summary_lines.append(u"%s: no usable stock data" % _to_unicode(product_key))
                _print_line("no usable stock data")
                _print_line("")
                continue

            if min_stock >= threshold:
                summary_lines.append(
                    u"%s: stock ok, min=%s >= threshold=%s" % (_to_unicode(product_key), min_stock, threshold)
                )
                _print_line("stock is enough, min_stock=%s >= threshold=%s, skip" % (min_stock, threshold))
                _print_line("")
                continue

            shortage = int(threshold - min_stock)
            summary_lines.append(
                u"%s: shortage=%s (threshold=%s, min_stock=%s)"
                % (_to_unicode(product_key), shortage, threshold, min_stock)
            )

            if args.summary_only:
                _print_line("summary-only mode, shortage=%s, no restock" % shortage)
                _print_line("")
                notify_needed = True
                continue

            if shortage < int(args.min_shortage_to_restock):
                _print_line(
                    "restock skipped because shortage=%s is less than min_shortage_to_restock=%s"
                    % (shortage, args.min_shortage_to_restock)
                )
                _print_line("")
                continue

            if args.restock_count:
                target_count = int(args.restock_count)
                count_source = "cli override"
            else:
                target_count = shortage
                count_source = "threshold gap"

            if target_count <= 0:
                summary_lines.append(u"%s: restock skipped, computed count <= 0" % _to_unicode(product_key))
                _print_line("restock skipped because computed count is <= 0")
                _print_line("")
                continue

            summary_lines.append(
                u"%s: restock triggered, min=%s, threshold=%s, count=%s (%s)"
                % (_to_unicode(product_key), min_stock, threshold, target_count, _to_unicode(count_source))
            )
            _print_line("restock triggered, min_stock=%s < threshold=%s, count=%s" % (min_stock, threshold, target_count))
            _print_line("count_source: %s" % count_source)
            restock_happened = True
            notify_needed = True

            if args.dry_run:
                summary_lines.append(u"%s: dry-run only, no generate/upload" % _to_unicode(product_key))
                _print_line("dry-run mode, no generate/upload executed")
                _print_line("")
                continue

            output_path = None
            try:
                codes, payload, _ = sub2api_client.generate_codes(
                    product_key,
                    count_override=target_count,
                    turnstile_token=args.turnstile_token,
                    totp_code=args.totp_code,
                )
                output_path = build_output_path(output_dir, product_key)
                with io.open(output_path, "w", encoding="utf-8") as handle:
                    handle.write(u"\n".join([_to_unicode(code) for code in codes]) + u"\n")
                _print_line("generated %s codes: %s" % (len(codes), _display(output_path)))
                summary_lines.append(u"%s: generated %s codes" % (_to_unicode(product_key), len(codes)))

                results = goods_client.upload_cards(product_key, codes)
                failed = False
                ok_count = 0
                for item in results:
                    if item["ok"]:
                        ok_count += 1
                        _print_line("[OK] goods_id=%s" % item["goods_id"])
                    else:
                        failed = True
                        any_failed = True
                        _print_line("[FAIL] goods_id=%s: %s" % (item["goods_id"], _display(item["error"])))

                if failed:
                    summary_lines.append(
                        u"%s: upload partial failed, success=%s/%s"
                        % (_to_unicode(product_key), ok_count, len(results))
                    )
                    _print_line("restock failed, keep output file: %s" % _display(output_path))
                else:
                    os.remove(output_path)
                    summary_lines.append(
                        u"%s: upload success %s/%s, output deleted"
                        % (_to_unicode(product_key), ok_count, len(results))
                    )
                    _print_line("restock done, deleted output file: %s" % _display(output_path))
            except Exception as exc:
                any_failed = True
                summary_lines.append(u"%s: exception %s" % (_to_unicode(product_key), _to_unicode(exc)))
                _print_line("restock exception: %s" % _display(exc))

            _print_line("")
    except Exception as exc:
        any_failed = True
        summary_lines.append(u"global exception: %s" % _to_unicode(exc))
        _print_line("global exception: %s" % _display(exc))

    if any_failed:
        run_status = "FAILED"

    notify_failed = False
    if notification and (notify_needed or any_failed):
        try:
            send_dingtalk_notification(notification, args, run_status, summary_lines, restock_happened)
            _print_line("notification sent")
        except Exception as exc:
            notify_failed = True
            _print_line("notification failed: %s" % _display(exc))

    if any_failed or notify_failed:
        raise SystemExit(1)


def build_output_path(output_dir, product_key):
    timestamp = datetime.now().strftime("%Y%m%d_%H%M%S")
    return os.path.join(output_dir, "%s_%s.txt" % (output_prefix(product_key), timestamp))


def _to_unicode(value):
    if value is None:
        return u""
    try:
        unicode_type = unicode  # noqa: F821
        bytes_type = str
    except NameError:
        return str(value)

    if isinstance(value, unicode_type):
        return value
    if isinstance(value, bytes_type):
        try:
            return value.decode("utf-8")
        except Exception:
            return value.decode("utf-8", "ignore")
    try:
        return unicode_type(value)
    except Exception:
        try:
            return unicode_type(str(value), "utf-8", "ignore")
        except Exception:
            return unicode_type(repr(value), "utf-8", "ignore")


def _display(value):
    return _to_unicode(value)


def _print_line(value):
    text = _display(value)
    sys.stdout.write(str(text) + "\n")


def resolve_notification_settings(config, args):
    section = config.get("notification", {})
    webhook = args.notify_webhook or section.get("dingtalk_webhook") or section.get("webhook")
    keyword = args.notify_keyword or section.get("keyword") or u"Token使用"
    if not webhook:
        return None
    return {
        "webhook": webhook,
        "keyword": keyword,
    }


def send_dingtalk_notification(notification, args, run_status, summary_lines, restock_happened):
    timestamp = datetime.now().strftime("%Y-%m-%d %H:%M:%S")
    hostname = socket.gethostname()
    mode_text = u"summary" if args.summary_only else u"restock"
    header = u"%s\n[%s] auto_restock %s\nhost: %s\nmode: %s\ndry_run: %s" % (
        _to_unicode(notification["keyword"]),
        _to_unicode(timestamp),
        _to_unicode(run_status),
        _to_unicode(hostname),
        mode_text,
        _to_unicode(args.dry_run),
    )

    if args.product:
        header += u"\nproduct: %s" % _to_unicode(args.product)
    header += u"\nstock_threshold: %s" % _to_unicode(args.stock_threshold)
    header += u"\nmin_shortage_to_restock: %s" % _to_unicode(args.min_shortage_to_restock)
    header += u"\nrestock_happened: %s" % _to_unicode(restock_happened)
    if args.restock_count:
        header += u"\nrestock_count: %s" % _to_unicode(args.restock_count)

    content_lines = [header]
    if summary_lines:
        content_lines.extend(summary_lines)
    else:
        content_lines.append(u"no summary")

    payload = {
        "msgtype": "text",
        "text": {
            "content": u"\n".join(content_lines),
        },
    }
    response = request_json(
        "POST",
        notification["webhook"],
        headers={"Content-Type": "application/json"},
        payload=payload,
        timeout=30,
    )
    if not isinstance(response, dict):
        raise HttpRequestError("DingTalk response is invalid", payload=response)
    if int(response.get("errcode", 0)) != 0:
        raise HttpRequestError("DingTalk send failed: %s" % response, payload=response)


if __name__ == "__main__":
    main()
