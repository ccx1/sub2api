# -*- coding: utf-8 -*-
from __future__ import print_function

import glob
import io
import os

from config_loader import get_product, output_prefix, resolve_goods_ids
from http_utils import HttpRequestError, request_json


class GoodsCardStorageClient(object):
    def __init__(self, config):
        self.config = config
        self.lianjia = config["lianjia"]
        self.endpoint = str(self.lianjia["endpoint"]).strip()
        self.timeout = int(self.lianjia.get("timeout_seconds", 30))
        self.auth = self.lianjia["auth"]

    def upload_cards(self, product_key, card_lines):
        product = get_product(self.config, product_key)
        listing = product["listing"]
        goods_ids = resolve_goods_ids(self.config, product_key)
        content = "\n".join(card_lines)

        results = []
        for goods_id in goods_ids:
            payload = {
                "goods_id": goods_id,
                "content": content,
                "first": int(listing.get("first", self.lianjia.get("first", 0))),
                "remove_repeat": int(listing.get("remove_repeat", self.lianjia.get("remove_repeat", 0))),
            }
            try:
                response = request_json(
                    "POST",
                    self.endpoint,
                    headers=self._headers(goods_id),
                    payload=payload,
                    timeout=self.timeout,
                )
                results.append(
                    {
                        "goods_id": goods_id,
                        "ok": True,
                        "payload": payload,
                        "response": response,
                    }
                )
            except HttpRequestError as exc:
                results.append(
                    {
                        "goods_id": goods_id,
                        "ok": False,
                        "payload": payload,
                        "error": str(exc),
                        "status": exc.status,
                        "response": exc.payload,
                    }
                )

        return results

    def list_goods(self, current=1, page_size=100, goods_type="card", status=999, name="", is_proxy="0"):
        payload = {
            "current": current,
            "pageSize": page_size,
            "goods_type": goods_type,
            "status": status,
            "name": name,
            "is_proxy": is_proxy,
        }
        referer = str(
            self.lianjia.get(
                "goods_list_referer",
                "%s/merchant/goods/list?is_proxy=%s" % (self.lianjia.get("origin", "https://pay.ldxp.cn"), is_proxy),
            )
        )

        response = request_json(
            "POST",
            str(self.lianjia.get("goods_list_endpoint", "https://pay.ldxp.cn/merchantApi/Goods/list")).strip(),
            headers=self._headers(None, referer=referer),
            payload=payload,
            timeout=self.timeout,
        )
        if not isinstance(response, dict):
            raise HttpRequestError("goods list returned unexpected payload", payload=response)
        return response

    def _headers(self, goods_id, referer=None):
        headers = {
            "Accept": "application/json, text/plain, */*",
            "Accept-Language": str(self.lianjia.get("accept_language", "zh-CN,zh;q=0.9")),
            "Content-Type": "application/json",
            "Origin": str(self.lianjia.get("origin", "https://pay.ldxp.cn")),
            "Referer": referer
            or str(
                self.lianjia.get(
                    "referer_template",
                    "https://pay.ldxp.cn/merchant/goods/goods_card_storage_add?goods_id={goods_id}",
                )
            ).format(goods_id=goods_id),
            "User-Agent": str(
                self.lianjia.get(
                    "user_agent",
                    "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 "
                    "(KHTML, like Gecko) Chrome/123.0.0.0 Safari/537.36 Edg/123.0.0.0",
                )
            ),
        }

        merchant_token = str(self.lianjia.get("merchant_token", "")).strip()
        if merchant_token:
            headers["merchant-token"] = merchant_token

        if str(self.auth.get("mode", "")).strip() == "cookie":
            headers["Cookie"] = str(self.auth.get("cookie", "")).strip()

        extra_headers = self.lianjia.get("extra_headers", {})
        if isinstance(extra_headers, dict):
            for key, value in extra_headers.items():
                headers[str(key)] = str(value)
        return headers


def read_card_lines(file_path):
    with io.open(file_path, "r", encoding="utf-8-sig") as handle:
        raw = handle.read()
    lines = [line.strip() for line in raw.replace("\r\n", "\n").split("\n") if line.strip()]
    if not lines:
        raise ValueError("card text file is empty: %s" % file_path)
    return lines


def find_latest_output(output_dir, product_key):
    pattern = os.path.join(output_dir, "%s_*.txt" % output_prefix(product_key))
    matches = sorted(glob.glob(pattern), key=os.path.getmtime, reverse=True)
    if not matches:
        raise IOError("no output txt found for product %s" % product_key)
    return matches[0]


def extract_goods_stock_map(payload):
    if not isinstance(payload, dict):
        raise ValueError("goods list payload must be an object")

    data = payload.get("data")
    if not isinstance(data, dict):
        raise ValueError("goods list payload is missing data")

    raw_list = data.get("list")
    if not isinstance(raw_list, list):
        raise ValueError("goods list payload is missing data.list")

    goods_map = {}
    for item in raw_list:
        if not isinstance(item, dict):
            continue
        try:
            goods_id = int(item.get("id"))
        except (TypeError, ValueError):
            continue

        stock_count = 0
        extend = item.get("extend")
        if isinstance(extend, dict):
            try:
                stock_count = int(extend.get("stock_count", 0))
            except (TypeError, ValueError):
                stock_count = 0

        goods_map[goods_id] = {
            "id": goods_id,
            "name": _to_unicode(item.get("name", "")).strip(),
            "price": item.get("price"),
            "status": item.get("status"),
            "stock_count": stock_count,
            "raw": item,
        }
    return goods_map


def _to_unicode(value):
    if value is None:
        return u""
    try:
        return unicode(value)  # noqa: F821
    except NameError:
        return str(value)
    except UnicodeDecodeError:
        return unicode(str(value), "utf-8", "ignore")  # noqa: F821
