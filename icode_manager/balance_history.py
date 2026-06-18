import json
import math
import urllib.error
import urllib.parse
import urllib.request
from http import HTTPStatus
from typing import Any, Dict, List, Optional, Set, Tuple

from affiliate_admin import connect_db, load_database_config
from common import AppError
from store import Store
from sub2api_auth import Sub2APIAuth


class RechargeCalculator:
    def __init__(self, config: Dict[str, Any], store: Store):
        self.app_config = config
        self.config = config.get("sub2api", config)
        self.store = store
        self.auth = Sub2APIAuth(self.config)
        self.db_config = load_database_config(config)
        self.base_url = str(self.config.get("base_url", "")).rstrip("/")

    def effective_recharge(self, user_id: int) -> Dict[str, Any]:
        if user_id <= 0:
            return {
                "total_recharged": 0.0,
                "local_deduction": 0.0,
                "effective_recharge": 0.0,
                "matched_codes": [],
            }
        if self.config.get("verify_mode") == "database":
            return self.effective_recharge_from_database(user_id)

        histories = [
            self.fetch_history(user_id, "balance"),
            self.fetch_history(user_id, "admin_balance"),
        ]
        items = []
        total_recharged: Optional[float] = None
        for history in histories:
            items.extend(history["items"])
            if total_recharged is None and history["total_recharged"] is not None:
                total_recharged = history["total_recharged"]

        if total_recharged is None:
            total_recharged = sum_positive_recharge_items(items)

        deduction, matched_codes = local_code_deduction(items, self.store.all_code_strings())
        effective = max(0.0, float(total_recharged) - deduction)
        return {
            "total_recharged": float(total_recharged),
            "local_deduction": deduction,
            "effective_recharge": effective,
            "matched_codes": matched_codes,
        }

    def effective_recharge_from_database(self, user_id: int) -> Dict[str, Any]:
        with connect_db(self.db_config) as conn:
            user = conn.execute(
                "SELECT total_recharged FROM users WHERE id = %s",
                (user_id,),
            ).fetchone()
            rows = conn.execute(
                """
                SELECT code, type, value
                FROM redeem_codes
                WHERE used_by = %s
                  AND type IN ('balance', 'admin_balance')
                ORDER BY used_at DESC NULLS LAST, id DESC
                """,
                (user_id,),
            ).fetchall()
        items = [dict(row) for row in rows]
        total_recharged = parse_float(user.get("total_recharged") if user else None, sum_positive_recharge_items(items))
        deduction, matched_codes = local_code_deduction(items, self.store.all_code_strings())
        effective = max(0.0, float(total_recharged) - deduction)
        return {
            "total_recharged": float(total_recharged),
            "local_deduction": deduction,
            "effective_recharge": effective,
            "matched_codes": matched_codes,
        }

    def fetch_history(self, user_id: int, code_type: str) -> Dict[str, Any]:
        if not self.base_url:
            raise AppError(HTTPStatus.INTERNAL_SERVER_ERROR, "未配置 sub2api.base_url")

        page_size = max(1, int(self.config.get("balance_history_page_size") or 100))
        max_pages = max(1, int(self.config.get("balance_history_max_pages") or 50))
        timezone = str(self.config.get("timezone") or "Asia/Shanghai")
        all_items: List[Dict[str, Any]] = []
        total_recharged: Optional[float] = None

        for page in range(1, max_pages + 1):
            query = {
                "page": page,
                "page_size": page_size,
                "timezone": timezone,
                "type": code_type,
            }
            url = "%s/admin/users/%s/balance-history?%s" % (
                self.base_url,
                user_id,
                urllib.parse.urlencode(query),
            )
            payload = unwrap_payload(self.open_json(url))
            if total_recharged is None and "total_recharged" in payload:
                total_recharged = parse_float(payload.get("total_recharged"), 0.0)
            items = extract_items(payload)
            all_items.extend(items)

            pages = parse_int(payload.get("pages"), 0)
            total = parse_int(payload.get("total"), 0)
            if pages <= 0 and total > 0:
                pages = int(math.ceil(float(total) / float(page_size)))
            if pages > 0 and page >= pages:
                break
            if not items:
                break

        return {"items": all_items, "total_recharged": total_recharged}

    def open_json(self, url: str) -> Any:
        for attempt in range(2):
            try:
                req = urllib.request.Request(url, headers=self.auth.headers())
                with urllib.request.urlopen(req, timeout=float(self.config.get("timeout_seconds", 8))) as resp:
                    return json.loads(resp.read().decode("utf-8"))
            except urllib.error.HTTPError as err:
                if err.code in (401, 403) and attempt == 0:
                    self.auth.refresh_after_auth_error()
                    continue
                if err.code in (401, 403):
                    raise AppError(HTTPStatus.BAD_GATEWAY, "sub2api 充值记录鉴权失败") from err
                raise AppError(HTTPStatus.BAD_GATEWAY, "sub2api 充值记录查询失败：HTTP %s" % err.code) from err
            except AppError:
                raise
            except Exception as err:
                raise AppError(HTTPStatus.BAD_GATEWAY, "sub2api 充值记录查询失败：%s" % err) from err
        raise AppError(HTTPStatus.BAD_GATEWAY, "sub2api 充值记录查询失败")


def unwrap_payload(payload: Any) -> Dict[str, Any]:
    if isinstance(payload, dict) and isinstance(payload.get("data"), dict):
        return payload["data"]
    if isinstance(payload, dict):
        return payload
    return {}


def extract_items(payload: Dict[str, Any]) -> List[Dict[str, Any]]:
    candidates = [payload.get("items"), payload.get("list"), payload.get("data")]
    for candidate in candidates:
        if isinstance(candidate, list):
            return [item for item in candidate if isinstance(item, dict)]
    return []


def local_code_deduction(items: List[Dict[str, Any]], local_codes: Set[str]) -> Tuple[float, List[str]]:
    seen: Set[str] = set()
    deduction = 0.0
    matched_codes = []
    for item in items:
        code = str(item.get("code") or "").strip()
        if not code or code in seen or code not in local_codes:
            continue
        value = parse_float(item.get("value"), 0.0)
        if value <= 0 or not is_recharge_type(item.get("type")):
            continue
        seen.add(code)
        matched_codes.append(code)
        deduction += value
    return deduction, matched_codes


def sum_positive_recharge_items(items: List[Dict[str, Any]]) -> float:
    total = 0.0
    seen: Set[str] = set()
    for item in items:
        code = str(item.get("code") or "").strip()
        if code and code in seen:
            continue
        if code:
            seen.add(code)
        value = parse_float(item.get("value"), 0.0)
        if value > 0 and is_recharge_type(item.get("type")):
            total += value
    return total


def is_recharge_type(value: Any) -> bool:
    if value is None:
        return True
    return str(value) in {"balance", "admin_balance"}


def parse_float(value: Any, default: float) -> float:
    try:
        return float(value)
    except (TypeError, ValueError):
        return default


def parse_int(value: Any, default: int) -> int:
    try:
        return int(value)
    except (TypeError, ValueError):
        return default
