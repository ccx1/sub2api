# -*- coding: utf-8 -*-
from __future__ import print_function

import argparse
import io
import os
from datetime import datetime

from config_loader import get_product, load_json_config, output_prefix, resolve_output_dir, validate_sub2api_config
from sub2api_client import Sub2ApiClient

DEFAULT_SUB2API_CONFIG_PATH = os.path.join(os.path.dirname(__file__), "sub2api-config.json")


def build_parser():
    parser = argparse.ArgumentParser(description="Generate redeem codes from sub2api admin API")
    parser.add_argument("--sub2api-config", default=DEFAULT_SUB2API_CONFIG_PATH, help="path to sub2api-config.json")
    parser.add_argument("--product", help="product key, for example balance_50")
    parser.add_argument("--count", type=int, help="override count from config")
    parser.add_argument("--output", help="custom output txt path")
    parser.add_argument("--totp-code", help="provide 2FA code at runtime")
    parser.add_argument("--turnstile-token", help="provide Turnstile token at runtime")
    parser.add_argument("--list-products", action="store_true", help="only list configured product keys")
    return parser


def main():
    args = build_parser().parse_args()
    config, config_dir = load_json_config(args.sub2api_config)
    validate_sub2api_config(config)

    if args.list_products:
        print("available products:")
        for product_key, product in config["products"].items():
            label = _display(product.get("label", "")).strip() or "-"
            redeem = product["redeem"]
            group_id = redeem.get("group_id")
            print(
                "- %s: label=%s, type=%s, value=%s, count=%s, group_id=%s"
                % (
                    _display(product_key),
                    label,
                    redeem["type"],
                    redeem["value"],
                    redeem["count"],
                    group_id if group_id not in ("", None) else "-",
                )
            )
        return

    client = Sub2ApiClient(config)
    product_keys = [args.product] if args.product else list(config["products"].keys())

    if args.output and len(product_keys) != 1:
        raise SystemExit("--output can only be used with a single product")

    for product_key in product_keys:
        product = get_product(config, product_key)
        codes, payload, _ = client.generate_codes(
            product_key,
            count_override=args.count,
            turnstile_token=args.turnstile_token,
            totp_code=args.totp_code,
        )

        output_path = resolve_output_path(config, config_dir, product_key, args.output)
        with io.open(output_path, "w", encoding="utf-8") as handle:
            handle.write(u"\n".join([_to_unicode(code) for code in codes]) + u"\n")

        label = _display(product.get("label", "")).strip() or _display(product_key)
        print("generated: %s" % label)
        print("product: %s" % _display(product_key))
        print("count: %s" % len(codes))
        print("type: %s" % payload["type"])
        print("group_id: %s" % payload.get("group_id", "-"))
        print("value: %s" % payload["value"])
        print("output: %s" % output_path)
        print("")


def resolve_output_path(config, config_dir, product_key, custom_output):
    if custom_output:
        output_path = os.path.abspath(os.path.expanduser(custom_output))
        parent = os.path.dirname(output_path)
        if parent and not os.path.isdir(parent):
            os.makedirs(parent)
        return output_path

    timestamp = datetime.now().strftime("%Y%m%d_%H%M%S")
    output_dir = resolve_output_dir(config, config_dir)
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
    text = _to_unicode(value)
    try:
        return text.encode("utf-8")
    except AttributeError:
        return text
    except UnicodeDecodeError:
        return str(value)


if __name__ == "__main__":
    main()
