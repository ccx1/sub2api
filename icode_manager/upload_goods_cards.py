# -*- coding: utf-8 -*-
from __future__ import print_function

import argparse
import os

from config_loader import (
    ensure_product_group_alignment,
    get_product,
    load_json_config,
    resolve_goods_ids,
    resolve_output_dir,
    validate_lianjia_config,
    validate_sub2api_config,
)
from listing_client import GoodsCardStorageClient, find_latest_output, read_card_lines

DEFAULT_SUB2API_CONFIG_PATH = os.path.join(os.path.dirname(__file__), "sub2api-config.json")
DEFAULT_LIANJIA_CONFIG_PATH = os.path.join(os.path.dirname(__file__), "lianjia-config.json")


def build_parser():
    parser = argparse.ArgumentParser(description="Upload local card txt to lianjia goods")
    parser.add_argument("--sub2api-config", default=DEFAULT_SUB2API_CONFIG_PATH, help="path to sub2api-config.json")
    parser.add_argument("--lianjia-config", default=DEFAULT_LIANJIA_CONFIG_PATH, help="path to lianjia-config.json")
    parser.add_argument("--product", help="product key, for example balance_50; omit to process all products")
    parser.add_argument("--input", help="txt path; only valid with a single product")
    return parser


def main():
    args = build_parser().parse_args()
    sub2api_config, sub2api_config_dir = load_json_config(args.sub2api_config)
    lianjia_config, _ = load_json_config(args.lianjia_config)
    validate_sub2api_config(sub2api_config)
    validate_lianjia_config(lianjia_config, args.product)

    product_keys = [args.product] if args.product else list(lianjia_config["products"].keys())
    if args.input and len(product_keys) != 1:
        raise SystemExit("--input can only be used with a single product")

    output_dir = resolve_output_dir(sub2api_config, sub2api_config_dir)
    client = GoodsCardStorageClient(lianjia_config)
    any_failed = False

    for product_key in product_keys:
        group_id = ensure_product_group_alignment(sub2api_config, lianjia_config, product_key)
        product = get_product(lianjia_config, product_key)
        goods_ids = resolve_goods_ids(lianjia_config, product_key)
        input_path = resolve_input_path(args.input, output_dir, product_key)
        card_lines = read_card_lines(input_path)
        results = client.upload_cards(product_key, card_lines)

        print("input: %s" % _display(input_path))
        print("product: %s" % _display(product_key))
        print("group_id: %s" % (group_id if group_id is not None else "-"))
        print("goods_ids: %s" % goods_ids)
        print("card_count: %s" % len(card_lines))
        print("label: %s" % (_display(product.get("label", "")).strip() or "-"))
        print("")

        failed = False
        for item in results:
            goods_id = item["goods_id"]
            if item["ok"]:
                print("[OK] goods_id=%s" % goods_id)
                print(item["response"])
            else:
                failed = True
                any_failed = True
                print("[FAIL] goods_id=%s" % goods_id)
                print(item["error"])
                if item.get("response") is not None:
                    print(item["response"])
            print("")

        success_count = len([item for item in results if item["ok"]])
        print("done: success %s/%s" % (success_count, len(results)))
        print("")

        if not failed:
            os.remove(input_path)
            print("deleted output file: %s" % input_path)
            print("")

        if failed:
            print("product %s has failed uploads" % _display(product_key))
            print("")

    if any_failed:
        raise SystemExit(1)


def resolve_input_path(raw_input, output_dir, product_key):
    if raw_input:
        input_path = os.path.abspath(os.path.expanduser(raw_input))
        if not os.path.exists(input_path):
            raise IOError("card txt not found: %s" % input_path)
        return input_path
    return find_latest_output(output_dir, product_key)


def _display(value):
    if value is None:
        return ""
    if isinstance(value, bytes):
        return value.decode("utf-8", "ignore")
    try:
        return unicode(value)  # noqa: F821
    except NameError:
        return str(value)


if __name__ == "__main__":
    main()
