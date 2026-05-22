# -*- coding: utf-8 -*-
from __future__ import print_function

import argparse
import io
import os
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

DEFAULT_SUB2API_CONFIG_PATH = os.path.join(os.path.dirname(__file__), "sub2api-config.json")
DEFAULT_LIANJIA_CONFIG_PATH = os.path.join(os.path.dirname(__file__), "lianjia-config.json")


def build_parser():
    parser = argparse.ArgumentParser(description="Auto restock lianjia goods based on stock_count")
    parser.add_argument("--sub2api-config", default=DEFAULT_SUB2API_CONFIG_PATH, help="path to sub2api-config.json")
    parser.add_argument("--lianjia-config", default=DEFAULT_LIANJIA_CONFIG_PATH, help="path to lianjia-config.json")
    parser.add_argument("--product", help="only process one product; omit to process all products")
    parser.add_argument("--stock-threshold", type=int, default=99, help="restock when stock is below this value")
    parser.add_argument("--restock-count", type=int, help="override restock count; default is threshold minus current stock")
    parser.add_argument("--dry-run", action="store_true", help="print plan only; do not generate or upload")
    parser.add_argument("--totp-code", help="provide 2FA code at runtime")
    parser.add_argument("--turnstile-token", help="provide Turnstile token at runtime")
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

    product_keys = [args.product] if args.product else list(lianjia_config["products"].keys())
    output_dir = resolve_output_dir(sub2api_config, sub2api_config_dir)
    sub2api_client = Sub2ApiClient(sub2api_config)
    goods_client = GoodsCardStorageClient(lianjia_config)
    goods_map = extract_goods_stock_map(goods_client.list_goods())

    any_failed = False
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

        print("product: %s (%s)" % (_display(product_key), label))
        print("group_id: %s" % (group_id if group_id is not None else "-"))
        print("goods_ids: %s" % goods_ids)

        if missing_goods_ids:
            any_failed = True
            print("goods_ids not found in goods list: %s" % missing_goods_ids)
            print("")
            continue

        for row in stock_rows:
            print("- goods_id=%s, name=%s, stock_count=%s" % (row["id"], row["name"] or "-", row["stock_count"]))

        if min_stock is None:
            any_failed = True
            print("no usable stock data")
            print("")
            continue

        if min_stock >= threshold:
            print("stock is enough, min_stock=%s >= threshold=%s, skip" % (min_stock, threshold))
            print("")
            continue

        if args.restock_count:
            target_count = int(args.restock_count)
            count_source = "cli override"
        else:
            target_count = int(threshold - min_stock)
            count_source = "threshold gap"

        if target_count <= 0:
            print("restock skipped because computed count is <= 0")
            print("")
            continue

        print("restock triggered, min_stock=%s < threshold=%s, count=%s" % (min_stock, threshold, target_count))
        print("count_source: %s" % count_source)

        if args.dry_run:
            print("dry-run mode, no generate/upload executed")
            print("")
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
            print("generated %s codes: %s" % (len(codes), output_path))

            results = goods_client.upload_cards(product_key, codes)
            failed = False
            for item in results:
                if item["ok"]:
                    print("[OK] goods_id=%s" % item["goods_id"])
                else:
                    failed = True
                    any_failed = True
                    print("[FAIL] goods_id=%s: %s" % (item["goods_id"], item["error"]))

            if failed:
                print("restock failed, keep output file: %s" % _display(output_path))
            else:
                os.remove(output_path)
                print("restock done, deleted output file: %s" % _display(output_path))
        except Exception as exc:
            any_failed = True
            print("restock exception: %s" % _display(exc))

        print("")

    if any_failed:
        raise SystemExit(1)


def build_output_path(output_dir, product_key):
    timestamp = datetime.now().strftime("%Y%m%d_%H%M%S")
    return os.path.join(output_dir, "%s_%s.txt" % (output_prefix(product_key), timestamp))


def _to_unicode(value):
    try:
        return unicode(value)  # noqa: F821
    except NameError:
        return str(value)


def _display(value):
    text = _to_unicode(value)
    try:
        return text.encode("utf-8")
    except AttributeError:
        return text
    except UnicodeDecodeError:
        return str(value)


if __name__ == "__main__":
    main()
