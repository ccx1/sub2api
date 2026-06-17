# -*- coding: utf-8 -*-
from __future__ import print_function

import io
import json
import os
import re

ALLOWED_AUTH_MODES = set(["token", "admin_api_key", "login"])
ALLOWED_REDEEM_TYPES = set(["balance", "concurrency", "subscription", "invitation"])


def load_json_config(config_path):
    path = os.path.abspath(os.path.expanduser(config_path))
    if not os.path.exists(path):
        raise IOError("config file not found: %s" % path)

    with io.open(path, "r", encoding="utf-8") as handle:
        config = json.load(handle)

    if not isinstance(config, dict):
        raise ValueError("config root must be a JSON object")

    return config, os.path.dirname(path)


def validate_sub2api_config(config):
    sub2api = _require_dict(config, "sub2api")
    _require_string(sub2api, "api_base_url")

    auth = _require_dict(sub2api, "auth")
    mode = _require_string(auth, "mode")
    if mode not in ALLOWED_AUTH_MODES:
        raise ValueError("sub2api.auth.mode must be one of: %s" % sorted(ALLOWED_AUTH_MODES))

    if mode == "token" and not _stringify(auth.get("token", "")).strip():
        raise ValueError("sub2api.auth.token is required when auth.mode=token")
    if mode == "admin_api_key" and not _stringify(auth.get("admin_api_key", "")).strip():
        raise ValueError("sub2api.auth.admin_api_key is required when auth.mode=admin_api_key")
    if mode == "login":
        login = _require_dict(auth, "login")
        _require_string(login, "email")
        _require_string(login, "password")

    products = _require_dict(config, "products")
    if not products:
        raise ValueError("products must contain at least one item")

    for product_key, product in products.items():
        _validate_sub2api_product(product_key, product)


def validate_lianjia_config(config, product_key=None):
    lianjia = _require_dict(config, "lianjia")
    _require_string(lianjia, "endpoint")

    auth = _require_dict(lianjia, "auth")
    mode = _require_string(auth, "mode")
    if mode != "cookie":
        raise ValueError("lianjia.auth.mode currently only supports cookie")
    _require_string(auth, "cookie")

    products = _require_dict(config, "products")
    if not products:
        raise ValueError("products must contain at least one item")

    target_keys = [product_key] if product_key else list(products.keys())
    for current_key in target_keys:
        product = get_product(config, current_key)
        _validate_lianjia_product(current_key, product)


def get_product(config, product_key):
    products = config["products"]
    if product_key not in products:
        raise KeyError("product config not found: %s" % product_key)
    product = products[product_key]
    if not isinstance(product, dict):
        raise ValueError("products.%s must be an object" % product_key)
    return product


def resolve_goods_ids(config, product_key):
    product = get_product(config, product_key)
    listing = _require_dict(product, "listing")
    raw_goods_ids = listing.get("goods_ids")
    if raw_goods_ids is None and "goods_id" in listing:
        raw_goods_ids = [listing["goods_id"]]

    if not isinstance(raw_goods_ids, list) or not raw_goods_ids:
        if listing.get("group_id") not in ("", None):
            raise ValueError(
                "products.%s.listing is missing goods_id. "
                "You configured group_id, but chain upload needs goods_id. "
                "Use listing.goods_id for one target product, or listing.goods_ids for multiple targets."
                % product_key
            )
        raise ValueError(
            "products.%s.listing.goods_id is required. "
            "Use listing.goods_id for one target product, or listing.goods_ids for multiple targets."
            % product_key
        )

    goods_ids = []
    seen = set()
    for index, raw_goods_id in enumerate(raw_goods_ids):
        goods_id = _coerce_positive_int(raw_goods_id, "products.%s.listing.goods_ids[%s]" % (product_key, index))
        if goods_id in seen:
            continue
        seen.add(goods_id)
        goods_ids.append(goods_id)

    if not goods_ids:
        raise ValueError("products.%s.listing.goods_ids resolved to empty" % product_key)
    return goods_ids


def resolve_output_dir(config, config_dir):
    output = config.get("output", {})
    raw_directory = _stringify(output.get("directory", "outputs")).strip() or "outputs"
    if os.path.isabs(raw_directory):
        directory = raw_directory
    else:
        directory = os.path.abspath(os.path.join(config_dir, raw_directory))
    if not os.path.isdir(directory):
        os.makedirs(directory)
    return directory


def output_prefix(product_key):
    safe_key = re.sub(r"[^A-Za-z0-9._-]+", "_", _stringify(product_key)).strip("._-")
    return safe_key or "product"


def ensure_product_group_alignment(sub2api_config, lianjia_config, product_key):
    sub2api_product = get_product(sub2api_config, product_key)
    lianjia_product = get_product(lianjia_config, product_key)

    sub2api_group_id = _optional_positive_int(
        sub2api_product.get("redeem", {}).get("group_id"),
        "sub2api.products.%s.redeem.group_id" % product_key,
    )
    lianjia_group_id = _optional_positive_int(
        lianjia_product.get("listing", {}).get("group_id"),
        "lianjia.products.%s.listing.group_id" % product_key,
    )

    if lianjia_group_id is None:
        return sub2api_group_id
    if sub2api_group_id is None:
        raise ValueError(
            "product %s has lianjia listing.group_id, but sub2api redeem.group_id is missing" % product_key
        )
    if sub2api_group_id != lianjia_group_id:
        raise ValueError(
            "product %s group_id mismatch: sub2api=%s, lianjia=%s"
            % (product_key, sub2api_group_id, lianjia_group_id)
        )
    return sub2api_group_id


def _validate_sub2api_product(product_key, product):
    redeem = _require_dict(product, "redeem")
    redeem_type = _require_string(redeem, "type")
    if redeem_type not in ALLOWED_REDEEM_TYPES:
        raise ValueError("products.%s.redeem.type is invalid" % product_key)
    if "value" not in redeem:
        raise ValueError("products.%s.redeem.value is required" % product_key)
    _coerce_positive_int(redeem.get("count"), "products.%s.redeem.count" % product_key)

    if redeem.get("group_id") not in ("", None):
        _coerce_positive_int(redeem["group_id"], "products.%s.redeem.group_id" % product_key)


def _validate_lianjia_product(product_key, product):
    listing = _require_dict(product, "listing")
    resolve_goods_ids({"products": {product_key: product}}, product_key)
    if listing.get("group_id") not in ("", None):
        _coerce_positive_int(listing["group_id"], "products.%s.listing.group_id" % product_key)


def _require_dict(container, key):
    value = container.get(key)
    if not isinstance(value, dict):
        raise ValueError("%s must be an object" % key)
    return value


def _require_string(container, key):
    value = _stringify(container.get(key, "")).strip()
    if not value:
        raise ValueError("%s is required" % key)
    return value


def _coerce_positive_int(value, field_name):
    try:
        parsed = int(value)
    except (TypeError, ValueError):
        raise ValueError("%s must be a positive integer" % field_name)
    if parsed <= 0:
        raise ValueError("%s must be a positive integer" % field_name)
    return parsed


def _optional_positive_int(value, field_name):
    if value in ("", None):
        return None
    return _coerce_positive_int(value, field_name)


def _stringify(value):
    if value is None:
        return ""
    try:
        unicode_type = unicode  # noqa: F821
    except NameError:
        unicode_type = str

    if isinstance(value, unicode_type):
        try:
            return value.encode("utf-8")
        except AttributeError:
            return value
    return str(value)
