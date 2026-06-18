import importlib.util
import json
import sys
import threading
import time
from copy import deepcopy
from datetime import datetime
from http import HTTPStatus
from pathlib import Path
from typing import Any, Dict, List, Optional

from common import AppError


ROOT = Path(__file__).resolve().parent
PRODUCT_TOOLS_ROOT = ROOT
SUB2API_CONFIG_PATH = PRODUCT_TOOLS_ROOT / "sub2api-config.json"
LIANJIA_CONFIG_PATH = PRODUCT_TOOLS_ROOT / "lianjia-config.json"
PRODUCT_LISTING_CONFIG_PATH = ROOT / "product_listing_config.json"


def _load_product_module(name: str, filename: str) -> Any:
    path = PRODUCT_TOOLS_ROOT / filename
    if str(PRODUCT_TOOLS_ROOT) not in sys.path:
        sys.path.insert(0, str(PRODUCT_TOOLS_ROOT))
    spec = importlib.util.spec_from_file_location(f"product_tools_{name}", path)
    if not spec or not spec.loader:
        raise RuntimeError(f"无法加载商品上架模块：{filename}")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


config_loader = _load_product_module("config_loader", "config_loader.py")
listing_client_module = _load_product_module("listing_client", "listing_client.py")
sub2api_client_module = _load_product_module("sub2api_client", "sub2api_client.py")


DEFAULT_RUNTIME_CONFIG = {
    "scheduler": {
        "enabled": False,
        "interval_minutes": 30,
        "stock_threshold": 99,
        "min_shortage_to_restock": 51,
        "restock_count": None,
        "product_key": "",
        "summary_only": False,
        "dry_run": False,
    }
}


class ProductListingService:
    def __init__(self) -> None:
        PRODUCT_LISTING_CONFIG_PATH.parent.mkdir(parents=True, exist_ok=True)

    def status(self) -> Dict[str, Any]:
        sub2api_config = self.load_sub2api_config()
        lianjia_config = self.load_lianjia_config()
        runtime = self.load_runtime_config()
        return {
            "products": self.list_products(sub2api_config, lianjia_config),
            "lianjia": self.mask_lianjia_config(lianjia_config),
            "scheduler": runtime["scheduler"],
            "paths": {
                "sub2api_config": str(SUB2API_CONFIG_PATH),
                "lianjia_config": str(LIANJIA_CONFIG_PATH),
                "runtime_config": str(PRODUCT_LISTING_CONFIG_PATH),
            },
        }

    def update_lianjia_auth(self, cookie: str, merchant_token: str) -> Dict[str, Any]:
        config = self.load_lianjia_config()
        lianjia = config.setdefault("lianjia", {})
        auth = lianjia.setdefault("auth", {})
        auth["mode"] = "cookie"
        if str(cookie or "").strip():
            auth["cookie"] = str(cookie).strip()
        if str(merchant_token or "").strip():
            lianjia["merchant_token"] = str(merchant_token).strip()
        self.save_json(LIANJIA_CONFIG_PATH, config)
        return self.mask_lianjia_config(config)

    def upsert_product(self, payload: Dict[str, Any]) -> Dict[str, Any]:
        product_key = self.clean_product_key(payload.get("product_key") or payload.get("key") or "")
        label = str(payload.get("label") or product_key).strip()
        redeem_type = str(payload.get("redeem_type") or "").strip()
        if redeem_type not in config_loader.ALLOWED_REDEEM_TYPES:
            raise AppError(HTTPStatus.BAD_REQUEST, "兑换码类型不正确")
        redeem_count = self.positive_int(payload.get("redeem_count"), "生成数量")
        goods_ids = self.parse_goods_ids(payload.get("goods_ids") or payload.get("goods_id"))
        redeem: Dict[str, Any] = {
            "count": redeem_count,
            "type": redeem_type,
            "value": self.parse_value(payload.get("redeem_value")),
        }
        group_id = self.optional_positive_int(payload.get("group_id"), "分组 ID")
        if group_id is not None:
            redeem["group_id"] = group_id
        validity_days = self.optional_positive_int(payload.get("validity_days"), "有效期天数")
        if validity_days is not None:
            redeem["validity_days"] = validity_days
        expires_in_days = self.optional_positive_int(payload.get("expires_in_days"), "过期天数")
        if expires_in_days is not None:
            redeem["expires_in_days"] = expires_in_days

        sub2api_config = self.load_sub2api_config()
        lianjia_config = self.load_lianjia_config()
        sub2api_config.setdefault("products", {})[product_key] = {"label": label, "redeem": redeem}
        listing: Dict[str, Any] = {}
        if group_id is not None:
            listing["group_id"] = group_id
        if len(goods_ids) == 1:
            listing["goods_id"] = goods_ids[0]
        else:
            listing["goods_ids"] = goods_ids
        lianjia_config.setdefault("products", {})[product_key] = {"label": label, "listing": listing}
        config_loader.validate_sub2api_config(sub2api_config)
        config_loader.validate_lianjia_config(lianjia_config, product_key)
        config_loader.ensure_product_group_alignment(sub2api_config, lianjia_config, product_key)
        self.save_json(SUB2API_CONFIG_PATH, sub2api_config)
        self.save_json(LIANJIA_CONFIG_PATH, lianjia_config)
        return {
            "key": product_key,
            "label": label,
            "redeem": redeem,
            "listing": listing,
        }

    def update_scheduler(self, payload: Dict[str, Any]) -> Dict[str, Any]:
        runtime = self.load_runtime_config()
        scheduler = runtime["scheduler"]
        for key in (
            "enabled",
            "interval_minutes",
            "stock_threshold",
            "min_shortage_to_restock",
            "restock_count",
            "product_key",
            "summary_only",
            "dry_run",
        ):
            if key in payload:
                scheduler[key] = payload[key]
        scheduler["enabled"] = bool(scheduler.get("enabled"))
        scheduler["summary_only"] = bool(scheduler.get("summary_only"))
        scheduler["dry_run"] = bool(scheduler.get("dry_run"))
        scheduler["interval_minutes"] = max(1, int(scheduler.get("interval_minutes") or 30))
        scheduler["stock_threshold"] = max(0, int(scheduler.get("stock_threshold") or 0))
        scheduler["min_shortage_to_restock"] = max(0, int(scheduler.get("min_shortage_to_restock") or 0))
        restock_count = scheduler.get("restock_count")
        scheduler["restock_count"] = None if restock_count in ("", None) else max(1, int(restock_count))
        scheduler["product_key"] = str(scheduler.get("product_key") or "").strip()
        self.save_json(PRODUCT_LISTING_CONFIG_PATH, runtime)
        return scheduler

    def generate_and_upload(
        self,
        product_key: str,
        count: Optional[int] = None,
        upload: bool = True,
    ) -> Dict[str, Any]:
        product_key = self.clean_product_key(product_key)
        sub2api_config = self.load_sub2api_config()
        lianjia_config = self.load_lianjia_config()
        self.validate_product_configs(sub2api_config, lianjia_config, product_key)
        output_dir = Path(config_loader.resolve_output_dir(sub2api_config, str(SUB2API_CONFIG_PATH.parent)))
        sub2api_client = sub2api_client_module.Sub2ApiClient(sub2api_config)
        codes, payload, product = sub2api_client.generate_codes(
            product_key,
            count_override=count,
        )
        output_path = self.write_codes(output_dir, product_key, codes)
        upload_results: List[Dict[str, Any]] = []
        if upload:
            goods_client = listing_client_module.GoodsCardStorageClient(lianjia_config)
            upload_results = goods_client.upload_cards(product_key, codes)
            if upload_results and all(item.get("ok") for item in upload_results):
                output_path.unlink(missing_ok=True)
        return {
            "product_key": product_key,
            "label": product.get("label") or product_key,
            "generated_count": len(codes),
            "generate_payload": payload,
            "output_path": str(output_path),
            "output_kept": output_path.exists(),
            "upload_results": self.sanitize_upload_results(upload_results),
        }

    def restock_once(self, options: Dict[str, Any]) -> Dict[str, Any]:
        sub2api_config = self.load_sub2api_config()
        lianjia_config = self.load_lianjia_config()
        product_filter = str(options.get("product_key") or "").strip()
        product_keys = [product_filter] if product_filter else list(lianjia_config.get("products", {}).keys())
        threshold = max(0, int(options.get("stock_threshold") or 99))
        min_shortage = max(0, int(options.get("min_shortage_to_restock") or 51))
        restock_count = options.get("restock_count")
        restock_count = None if restock_count in ("", None) else max(1, int(restock_count))
        dry_run = bool(options.get("dry_run"))
        summary_only = bool(options.get("summary_only"))

        config_loader.validate_sub2api_config(sub2api_config)
        config_loader.validate_lianjia_config(lianjia_config, product_filter or None)
        goods_client = listing_client_module.GoodsCardStorageClient(lianjia_config)
        goods_map = listing_client_module.extract_goods_stock_map(goods_client.list_goods())
        rows = []
        for product_key in product_keys:
            rows.append(
                self.process_restock_product(
                    sub2api_config,
                    lianjia_config,
                    goods_map,
                    product_key,
                    threshold,
                    min_shortage,
                    restock_count,
                    dry_run,
                    summary_only,
                )
            )
        return {
            "ran_at": datetime.now().isoformat(timespec="seconds"),
            "threshold": threshold,
            "min_shortage_to_restock": min_shortage,
            "dry_run": dry_run,
            "summary_only": summary_only,
            "products": rows,
        }

    def process_restock_product(
        self,
        sub2api_config: Dict[str, Any],
        lianjia_config: Dict[str, Any],
        goods_map: Dict[int, Dict[str, Any]],
        product_key: str,
        threshold: int,
        min_shortage: int,
        restock_count: Optional[int],
        dry_run: bool,
        summary_only: bool,
    ) -> Dict[str, Any]:
        product = config_loader.get_product(lianjia_config, product_key)
        group_id = config_loader.ensure_product_group_alignment(sub2api_config, lianjia_config, product_key)
        goods_ids = config_loader.resolve_goods_ids(lianjia_config, product_key)
        stocks = []
        missing = []
        for goods_id in goods_ids:
            item = goods_map.get(goods_id)
            if not item:
                missing.append(goods_id)
            else:
                stocks.append({"goods_id": goods_id, "name": item.get("name"), "stock_count": int(item["stock_count"])})
        row = {
            "product_key": product_key,
            "label": product.get("label") or product_key,
            "group_id": group_id,
            "goods_ids": goods_ids,
            "stocks": stocks,
            "missing_goods_ids": missing,
            "status": "checked",
        }
        if missing or not stocks:
            row["status"] = "failed"
            row["message"] = "商品库存数据缺失"
            return row
        min_stock = min(item["stock_count"] for item in stocks)
        shortage = threshold - min_stock
        row.update({"min_stock": min_stock, "shortage": max(0, shortage)})
        if min_stock >= threshold:
            row["status"] = "skipped"
            row["message"] = "库存充足"
            return row
        if summary_only:
            row["status"] = "summary"
            row["message"] = "仅汇总，不补货"
            return row
        if shortage < min_shortage:
            row["status"] = "skipped"
            row["message"] = "缺口未达到补货触发值"
            return row
        count = restock_count or shortage
        row["target_count"] = count
        if dry_run:
            row["status"] = "planned"
            row["message"] = "演练模式，未生成或上架"
            return row
        result = self.generate_and_upload(product_key, count=count, upload=True)
        row["status"] = "restocked"
        row["result"] = result
        return row

    def list_products(self, sub2api_config: Dict[str, Any], lianjia_config: Dict[str, Any]) -> List[Dict[str, Any]]:
        keys = sorted(set(sub2api_config.get("products", {}).keys()) | set(lianjia_config.get("products", {}).keys()))
        products = []
        for key in keys:
            sub_product = sub2api_config.get("products", {}).get(key, {})
            listing_product = lianjia_config.get("products", {}).get(key, {})
            redeem = sub_product.get("redeem", {}) if isinstance(sub_product, dict) else {}
            listing = listing_product.get("listing", {}) if isinstance(listing_product, dict) else {}
            goods_ids = []
            if isinstance(listing, dict):
                raw_goods_ids = listing.get("goods_ids")
                if raw_goods_ids is None and listing.get("goods_id") not in ("", None):
                    raw_goods_ids = [listing.get("goods_id")]
                if isinstance(raw_goods_ids, list):
                    goods_ids = raw_goods_ids
            products.append(
                {
                    "key": key,
                    "label": sub_product.get("label") or listing_product.get("label") or key,
                    "redeem_type": redeem.get("type"),
                    "redeem_value": redeem.get("value"),
                    "redeem_count": redeem.get("count"),
                    "group_id": redeem.get("group_id") or listing.get("group_id"),
                    "validity_days": redeem.get("validity_days"),
                    "expires_in_days": redeem.get("expires_in_days"),
                    "goods_ids": goods_ids,
                    "has_sub2api_config": bool(sub_product),
                    "has_lianjia_config": bool(listing_product),
                }
            )
        return products

    def load_sub2api_config(self) -> Dict[str, Any]:
        return self.load_json(SUB2API_CONFIG_PATH)

    def load_lianjia_config(self) -> Dict[str, Any]:
        return self.load_json(LIANJIA_CONFIG_PATH)

    def load_runtime_config(self) -> Dict[str, Any]:
        if not PRODUCT_LISTING_CONFIG_PATH.exists():
            return deepcopy(DEFAULT_RUNTIME_CONFIG)
        data = self.load_json(PRODUCT_LISTING_CONFIG_PATH)
        merged = deepcopy(DEFAULT_RUNTIME_CONFIG)
        merged["scheduler"].update(data.get("scheduler", {}) if isinstance(data.get("scheduler"), dict) else {})
        return merged

    def load_json(self, path: Path) -> Dict[str, Any]:
        if not path.exists():
            raise AppError(HTTPStatus.NOT_FOUND, f"配置文件不存在：{path}")
        with path.open("r", encoding="utf-8-sig") as handle:
            data = json.load(handle)
        if not isinstance(data, dict):
            raise AppError(HTTPStatus.BAD_REQUEST, f"配置文件必须是 JSON 对象：{path}")
        return data

    def save_json(self, path: Path, data: Dict[str, Any]) -> None:
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(json.dumps(data, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")

    def validate_product_configs(self, sub2api_config: Dict[str, Any], lianjia_config: Dict[str, Any], product_key: str) -> None:
        config_loader.validate_sub2api_config(sub2api_config)
        config_loader.validate_lianjia_config(lianjia_config, product_key)
        config_loader.ensure_product_group_alignment(sub2api_config, lianjia_config, product_key)

    def clean_product_key(self, product_key: str) -> str:
        key = str(product_key or "").strip()
        if not key:
            raise AppError(HTTPStatus.BAD_REQUEST, "请选择商品")
        if len(key) > 80 or not all(ch.isalnum() or ch in "-_." for ch in key):
            raise AppError(HTTPStatus.BAD_REQUEST, "商品标识只能包含字母、数字、短横线、下划线和点")
        return key

    def positive_int(self, value: Any, label: str) -> int:
        try:
            parsed = int(value)
        except (TypeError, ValueError):
            raise AppError(HTTPStatus.BAD_REQUEST, f"{label}必须是正整数")
        if parsed <= 0:
            raise AppError(HTTPStatus.BAD_REQUEST, f"{label}必须是正整数")
        return parsed

    def optional_positive_int(self, value: Any, label: str) -> Optional[int]:
        if value in ("", None):
            return None
        return self.positive_int(value, label)

    def parse_value(self, value: Any) -> Any:
        text = str(value if value is not None else "").strip()
        if not text:
            raise AppError(HTTPStatus.BAD_REQUEST, "兑换码面值不能为空")
        try:
            number = float(text)
        except ValueError:
            return text
        if number <= 0:
            raise AppError(HTTPStatus.BAD_REQUEST, "兑换码面值必须大于 0")
        return int(number) if number.is_integer() else number

    def parse_goods_ids(self, value: Any) -> List[int]:
        if isinstance(value, list):
            parts = value
        else:
            parts = str(value or "").replace("\n", ",").split(",")
        goods_ids = []
        seen = set()
        for part in parts:
            text = str(part).strip()
            if not text:
                continue
            goods_id = self.positive_int(text, "Goods ID")
            if goods_id not in seen:
                seen.add(goods_id)
                goods_ids.append(goods_id)
        if not goods_ids:
            raise AppError(HTTPStatus.BAD_REQUEST, "请至少填写一个 Goods ID")
        return goods_ids

    def write_codes(self, output_dir: Path, product_key: str, codes: List[str]) -> Path:
        output_dir.mkdir(parents=True, exist_ok=True)
        timestamp = datetime.now().strftime("%Y%m%d_%H%M%S")
        output_path = output_dir / f"{config_loader.output_prefix(product_key)}_{timestamp}.txt"
        output_path.write_text("\n".join(str(code) for code in codes) + "\n", encoding="utf-8")
        return output_path

    def mask_lianjia_config(self, config: Dict[str, Any]) -> Dict[str, Any]:
        lianjia = config.get("lianjia", {})
        auth = lianjia.get("auth", {}) if isinstance(lianjia.get("auth"), dict) else {}
        cookie = str(auth.get("cookie") or "")
        merchant_token = str(lianjia.get("merchant_token") or "")
        return {
            "endpoint": lianjia.get("endpoint"),
            "goods_list_endpoint": lianjia.get("goods_list_endpoint"),
            "cookie_configured": bool(cookie.strip()),
            "cookie_preview": self.mask_secret(cookie),
            "merchant_token_configured": bool(merchant_token.strip()),
            "merchant_token_preview": self.mask_secret(merchant_token),
        }

    def mask_secret(self, value: str) -> str:
        value = str(value or "").strip()
        if not value:
            return ""
        if len(value) <= 10:
            return "*" * len(value)
        return f"{value[:4]}...{value[-4:]}"

    def sanitize_upload_results(self, results: List[Dict[str, Any]]) -> List[Dict[str, Any]]:
        clean = []
        for item in results:
            clean.append(
                {
                    "goods_id": item.get("goods_id"),
                    "ok": bool(item.get("ok")),
                    "error": item.get("error"),
                    "status": item.get("status"),
                    "response": item.get("response"),
                }
            )
        return clean


class ProductListingScheduler:
    def __init__(self, service: ProductListingService) -> None:
        self.service = service
        self._stop = threading.Event()
        self._thread: Optional[threading.Thread] = None
        self._lock = threading.Lock()
        self.last_run: Optional[Dict[str, Any]] = None
        self.running = False

    def start(self) -> None:
        if self._thread and self._thread.is_alive():
            return
        self._stop.clear()
        self._thread = threading.Thread(target=self._loop, name="product-listing-scheduler", daemon=True)
        self._thread.start()

    def stop(self) -> None:
        self._stop.set()

    def status(self) -> Dict[str, Any]:
        runtime = self.service.load_runtime_config()["scheduler"]
        return {
            "thread_alive": bool(self._thread and self._thread.is_alive()),
            "running": self.running,
            "last_run": self.last_run,
            "config": runtime,
        }

    def run_once(self, options: Dict[str, Any]) -> Dict[str, Any]:
        with self._lock:
            self.running = True
            try:
                result = self.service.restock_once(options)
                self.last_run = {"ok": True, "result": result}
                return result
            except Exception as exc:
                self.last_run = {"ok": False, "error": str(exc), "ran_at": datetime.now().isoformat(timespec="seconds")}
                raise
            finally:
                self.running = False

    def _loop(self) -> None:
        while not self._stop.is_set():
            config = self.service.load_runtime_config()["scheduler"]
            interval_seconds = max(60, int(config.get("interval_minutes") or 30) * 60)
            if config.get("enabled"):
                try:
                    self.run_once(config)
                except Exception:
                    pass
            self._stop.wait(interval_seconds)
