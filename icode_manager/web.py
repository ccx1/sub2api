import json
import sys
import urllib.parse
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler
from pathlib import Path
from typing import Any, Dict, List, Optional

from affiliate_admin import AffiliateBindingService
from balance_history import RechargeCalculator
from common import ROOT, AppError, normalize_email, parse_time
from import_settings import ImportSettingsService
from product_listing import ProductListingScheduler, ProductListingService
from publisher import publish_activity
from store import DEFAULT_MESSAGE_TEMPLATE
from store import Store
from verifier import UserVerifier


class ClaimHandler(BaseHTTPRequestHandler):
    store: Store
    verifier: UserVerifier
    recharge_calculator: RechargeCalculator
    affiliate_service: AffiliateBindingService
    product_listing_service: ProductListingService
    product_listing_scheduler: ProductListingScheduler
    import_settings_service: ImportSettingsService
    config: Dict[str, Any]

    def log_message(self, fmt: str, *args: Any) -> None:
        sys.stderr.write("[%s] %s\n" % (self.log_date_time_string(), fmt % args))

    def do_OPTIONS(self) -> None:
        self.send_response(HTTPStatus.NO_CONTENT)
        self.add_cors_headers()
        self.end_headers()

    def do_GET(self) -> None:
        self.dispatch("GET")

    def do_POST(self) -> None:
        self.dispatch("POST")

    def dispatch(self, method: str) -> None:
        try:
            parsed = urllib.parse.urlparse(self.path)
            if self.serve_page(method, parsed.path):
                return
            if self.serve_api(method, parsed):
                return
            raise AppError(HTTPStatus.NOT_FOUND, "接口不存在")
        except AppError as err:
            self.json_response({"ok": False, "error": err.message}, err.status)
        except Exception as err:
            self.json_response({"ok": False, "error": f"服务异常：{err}"}, HTTPStatus.INTERNAL_SERVER_ERROR)

    def serve_page(self, method: str, path: str) -> bool:
        if method == "GET" and path in {"/manager", "/manager/"}:
            self.ensure_manager_access()
            self.send_static("manager.html")
            return True
        if method == "GET" and (path == "/" or path.startswith("/activity/")):
            self.send_static("index.html")
            return True
        if method == "GET" and path.startswith("/static/"):
            self.send_static(path[len("/static/") :])
            return True
        return False

    def serve_api(self, method: str, parsed: urllib.parse.ParseResult) -> bool:
        parts = [p for p in parsed.path.split("/") if p]
        if len(parts) >= 2 and parts[:2] == ["api", "manager"]:
            return self.serve_manager_api(method, parts)
        if len(parts) < 3 or parts[:2] != ["api", "activities"]:
            return False

        slug = parts[2]
        if method == "GET" and len(parts) == 3:
            self.json_response({"ok": True, "activity": self.store.activity_summary(slug)})
            return True
        if method == "POST" and len(parts) == 4 and parts[3] == "claim":
            self.handle_claim(slug)
            return True
        if method == "GET" and len(parts) == 4 and parts[3] == "claims":
            self.handle_claims(slug, parsed.query)
            return True
        return False

    def handle_claim(self, slug: str) -> None:
        body = self.read_json()
        email = normalize_email(body.get("email", ""))
        user = self.ensure_user_exists(email)
        activity = self.store.activity_summary(slug, include_private=True)
        if activity.get("packet_type") == "tier":
            recharge = self.recharge_calculator.effective_recharge(int(user.get("id") or 0))
            claim, already_claimed = self.store.claim_tier_code(slug, email, recharge["effective_recharge"])
            claim["recharge"] = {
                "effective_recharge": recharge["effective_recharge"],
                "local_deduction": recharge["local_deduction"],
            }
        else:
            claim, already_claimed = self.store.claim_code(slug, email)
        self.json_response({"ok": True, "claim": claim, "already_claimed": already_claimed})

    def handle_claims(self, slug: str, query: str) -> None:
        params = urllib.parse.parse_qs(query)
        email = normalize_email(params.get("email", [""])[0])
        self.ensure_user_exists(email)
        self.json_response({"ok": True, "claims": self.store.list_claims(slug, email)})

    def serve_manager_api(self, method: str, parts: List[str]) -> bool:
        self.ensure_manager_access(require_token=True)
        if len(parts) >= 3 and parts[2] == "affiliate":
            return self.serve_affiliate_api(method, parts)
        if len(parts) >= 3 and parts[2] == "listing":
            return self.serve_listing_api(method, parts)
        if len(parts) >= 3 and parts[2] == "import-settings":
            return self.serve_import_settings_api(method, parts)
        if len(parts) == 3 and parts[2] == "activities" and method == "GET":
            self.json_response({"ok": True, "activities": self.store.list_activities(include_private=True)})
            return True
        if len(parts) == 3 and parts[2] == "activities" and method == "POST":
            self.handle_manager_save_activity()
            return True
        if len(parts) == 5 and parts[2] == "activities" and parts[4] == "codes" and method == "POST":
            self.handle_manager_import_codes(parts[3])
            return True
        if len(parts) == 5 and parts[2] == "activities" and parts[4] == "close" and method == "POST":
            self.store.close_activity(parts[3])
            self.json_response({"ok": True, "activity": self.store.activity_summary(parts[3])})
            return True
        if len(parts) == 5 and parts[2] == "activities" and parts[4] == "publish" and method == "POST":
            self.handle_manager_publish(parts[3])
            return True
        return False

    def serve_import_settings_api(self, method: str, parts: List[str]) -> bool:
        if len(parts) == 3 and method == "GET":
            self.json_response({"ok": True, "status": self.import_settings_service.status()})
            return True
        if len(parts) == 3 and method == "POST":
            settings = self.import_settings_service.save(self.read_json())
            self.json_response({"ok": True, "settings": settings})
            return True
        return False

    def serve_listing_api(self, method: str, parts: List[str]) -> bool:
        if len(parts) == 4 and parts[3] == "status" and method == "GET":
            status = self.product_listing_service.status()
            status["scheduler_status"] = self.product_listing_scheduler.status()
            self.json_response({"ok": True, "status": status})
            return True
        if len(parts) == 4 and parts[3] == "auth" and method == "POST":
            body = self.read_json()
            lianjia = self.product_listing_service.update_lianjia_auth(
                cookie=str(body.get("cookie") or ""),
                merchant_token=str(body.get("merchant_token") or ""),
            )
            self.json_response({"ok": True, "lianjia": lianjia})
            return True
        if len(parts) == 4 and parts[3] == "products" and method == "POST":
            product = self.product_listing_service.upsert_product(self.read_json())
            status = self.product_listing_service.status()
            self.json_response({"ok": True, "product": product, "status": status})
            return True
        if len(parts) == 4 and parts[3] == "generate-upload" and method == "POST":
            body = self.read_json()
            result = self.product_listing_service.generate_and_upload(
                product_key=str(body.get("product_key") or ""),
                count=positive_int_or_none(body.get("count")),
                upload=bool(body.get("upload", True)),
            )
            self.json_response({"ok": True, "result": result})
            return True
        if len(parts) == 4 and parts[3] == "scheduler" and method == "POST":
            body = self.read_json()
            scheduler = self.product_listing_service.update_scheduler(body)
            self.json_response({"ok": True, "scheduler": scheduler, "scheduler_status": self.product_listing_scheduler.status()})
            return True
        if len(parts) == 4 and parts[3] == "restock-once" and method == "POST":
            body = self.read_json()
            result = self.product_listing_scheduler.run_once(body)
            self.json_response({"ok": True, "result": result, "scheduler_status": self.product_listing_scheduler.status()})
            return True
        return False

    def serve_affiliate_api(self, method: str, parts: List[str]) -> bool:
        if len(parts) == 4 and parts[3] == "preview" and method == "POST":
            body = self.read_json()
            preview = self.affiliate_service.preview(
                inviter_email=normalize_email(body.get("inviter_email", "")),
                invitee_email=normalize_email(body.get("invitee_email", "")),
                allow_rebind=bool(body.get("allow_rebind")),
            )
            self.json_response({"ok": True, "preview": preview})
            return True
        if len(parts) == 4 and parts[3] == "execute" and method == "POST":
            body = self.read_json()
            if not bool(body.get("confirm")):
                raise AppError(HTTPStatus.BAD_REQUEST, "执行前必须确认")
            result = self.affiliate_service.execute(
                inviter_email=normalize_email(body.get("inviter_email", "")),
                invitee_email=normalize_email(body.get("invitee_email", "")),
                allow_rebind=bool(body.get("allow_rebind")),
            )
            self.json_response({"ok": True, "result": result})
            return True
        return False

    def handle_manager_save_activity(self) -> None:
        body = self.read_json()
        slug = clean_slug(body.get("activity_id") or body.get("slug"))
        status = body.get("status") or "draft"
        if status not in {"draft", "open", "closed"}:
            raise AppError(HTTPStatus.BAD_REQUEST, "活动状态不正确")
        starts_at = empty_to_none(body.get("starts_at"))
        ends_at = empty_to_none(body.get("ends_at"))
        validate_time_range(starts_at, ends_at)
        packet_type = str(body.get("packet_type") or "ordinary").strip()
        if packet_type not in {"ordinary", "tier"}:
            raise AppError(HTTPStatus.BAD_REQUEST, "红包类型不正确")
        message_template = ""
        if packet_type == "tier":
            message_template = str(body.get("message_template") or DEFAULT_MESSAGE_TEMPLATE).strip()
        self.store.upsert_activity(
            slug=slug,
            name=clean_required(body.get("name"), "活动标题不能为空"),
            description=str(body.get("description") or "").strip(),
            use_url=clean_use_url(body.get("use_url")),
            packet_type=packet_type,
            message_template=message_template,
            starts_at=starts_at,
            ends_at=ends_at,
            status=status,
        )
        if packet_type == "tier":
            self.store.upsert_tiers(slug, normalize_tiers(body.get("tiers")))
        self.json_response({"ok": True, "activity": self.store.activity_summary(slug, include_private=True)})

    def handle_manager_import_codes(self, slug: str) -> None:
        body = self.read_json()
        codes = unique_codes(str(body.get("codes") or "").splitlines())
        if not codes:
            raise AppError(HTTPStatus.BAD_REQUEST, "请至少填写一个兑换码")
        count = self.store.import_codes(slug, codes, tier_key=empty_to_none(body.get("tier_key")))
        self.json_response({"ok": True, "imported_count": count, "input_count": len(codes)})

    def handle_manager_publish(self, slug: str) -> None:
        publisher = self.config.get("publisher", {})
        output_dir = Path(publisher.get("output_dir") or ROOT / "public")
        if not output_dir.is_absolute():
            output_dir = ROOT / output_dir
        link = publish_activity(
            store=self.store,
            slug=slug,
            output_dir=output_dir,
            public_base_url=str(publisher.get("public_base_url") or ""),
            api_base_url=str(publisher.get("api_base_url") or ""),
        )
        self.json_response({"ok": True, "url": link, "output_dir": str(output_dir)})

    def ensure_manager_access(self, require_token: bool = False) -> None:
        if self.config.get("manager", {}).get("local_only", True) and not self.client_is_loopback():
            raise AppError(HTTPStatus.FORBIDDEN, "管理台仅允许本机访问")
        if require_token:
            expected = self.config.get("security", {}).get("admin_token", "")
            actual = self.headers.get("X-Admin-Token", "")
            if expected and actual != expected:
                raise AppError(HTTPStatus.UNAUTHORIZED, "管理口令不正确")

    def client_is_loopback(self) -> bool:
        host = self.client_address[0] if self.client_address else ""
        return host in {"127.0.0.1", "::1", "localhost"} or host.startswith("127.")

    def ensure_user_exists(self, email: str) -> Dict[str, Any]:
        user = self.verifier.find_user(email)
        if not user:
            raise AppError(HTTPStatus.FORBIDDEN, "该邮箱不是有效的 iCode 用户")
        return user

    def read_json(self) -> Dict[str, Any]:
        length = int(self.headers.get("Content-Length", "0") or "0")
        if length <= 0:
            return {}
        try:
            data = json.loads(self.rfile.read(length).decode("utf-8"))
        except json.JSONDecodeError:
            raise AppError(HTTPStatus.BAD_REQUEST, "请求 JSON 不正确")
        if not isinstance(data, dict):
            raise AppError(HTTPStatus.BAD_REQUEST, "请求体必须是 JSON object")
        return data

    def send_static(self, name: str) -> None:
        static_root = (ROOT / "static").resolve()
        path = (static_root / name.replace("\\", "/").lstrip("/")).resolve()
        if not path.exists() or not path.is_file() or static_root not in path.parents:
            raise AppError(HTTPStatus.NOT_FOUND, "静态文件不存在")
        content = path.read_bytes()
        content_type = static_content_type(path.suffix)
        self.send_response(HTTPStatus.OK)
        self.add_cors_headers()
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(content)))
        self.end_headers()
        self.wfile.write(content)

    def json_response(self, payload: Dict[str, Any], status: int = HTTPStatus.OK) -> None:
        body = json.dumps(payload, ensure_ascii=False).encode("utf-8")
        self.send_response(status)
        self.add_cors_headers()
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def add_cors_headers(self) -> None:
        origins = self.config.get("security", {}).get("allowed_origins") or []
        origin = self.headers.get("Origin", "")
        if "*" in origins:
            self.send_header("Access-Control-Allow-Origin", "*")
        elif origin and origin in origins:
            self.send_header("Access-Control-Allow-Origin", origin)
        self.send_header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
        self.send_header("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Admin-Token")


def static_content_type(suffix: str) -> str:
    if suffix == ".html":
        return "text/html; charset=utf-8"
    if suffix == ".css":
        return "text/css; charset=utf-8"
    if suffix == ".js":
        return "application/javascript; charset=utf-8"
    return "application/octet-stream"


def clean_slug(value: Any) -> str:
    slug = str(value or "").strip()
    if not slug:
        raise AppError(HTTPStatus.BAD_REQUEST, "活动 ID 不能为空")
    if len(slug) > 80 or not all(ch.isalnum() or ch in "-_" for ch in slug):
        raise AppError(HTTPStatus.BAD_REQUEST, "活动 ID 只能包含字母、数字、短横线和下划线")
    return slug


def clean_required(value: Any, message: str) -> str:
    text = str(value or "").strip()
    if not text:
        raise AppError(HTTPStatus.BAD_REQUEST, message)
    return text


def clean_use_url(value: Any) -> str:
    text = str(value or "").strip()
    if not text:
        return ""
    parsed = urllib.parse.urlparse(text)
    if parsed.scheme in {"http", "https"} and parsed.netloc:
        return text
    if text.startswith("/") and not text.startswith("//"):
        return text
    raise AppError(HTTPStatus.BAD_REQUEST, "跳转地址必须是 http(s) 地址或站内路径")


def empty_to_none(value: Any) -> Optional[str]:
    text = str(value or "").strip()
    return text or None


def positive_int_or_none(value: Any) -> Optional[int]:
    if value in ("", None):
        return None
    try:
        parsed = int(value)
    except (TypeError, ValueError):
        raise AppError(HTTPStatus.BAD_REQUEST, "数量必须是正整数")
    if parsed <= 0:
        raise AppError(HTTPStatus.BAD_REQUEST, "数量必须是正整数")
    return parsed


def validate_time_range(starts_at: Optional[str], ends_at: Optional[str]) -> None:
    try:
        start = parse_time(starts_at)
        end = parse_time(ends_at)
    except ValueError:
        raise AppError(HTTPStatus.BAD_REQUEST, "时间格式不正确，请使用 ISO 格式")
    if start and end and start >= end:
        raise AppError(HTTPStatus.BAD_REQUEST, "结束时间必须晚于开始时间")


def unique_codes(lines: List[str]) -> List[str]:
    seen = set()
    out = []
    for line in lines:
        code = line.strip()
        if not code or code in seen:
            continue
        seen.add(code)
        out.append(code)
    return out


def normalize_tiers(value: Any) -> List[Dict[str, Any]]:
    if not isinstance(value, list):
        return []
    out = []
    seen = set()
    for index, item in enumerate(value):
        if not isinstance(item, dict):
            continue
        tier_key = clean_slug(item.get("tier_key"))
        if tier_key in seen:
            raise AppError(HTTPStatus.BAD_REQUEST, f"红包档次重复：{tier_key}")
        seen.add(tier_key)
        try:
            threshold_amount = float(item.get("threshold_amount") or 0)
        except (TypeError, ValueError):
            raise AppError(HTTPStatus.BAD_REQUEST, "红包档次门槛金额不正确")
        if threshold_amount < 0:
            raise AppError(HTTPStatus.BAD_REQUEST, "红包档次门槛金额不能小于 0")
        animations = item.get("animations") if isinstance(item.get("animations"), list) else []
        out.append(
            {
                "tier_key": tier_key,
                "name": clean_required(item.get("name"), "红包档次名称不能为空"),
                "threshold_amount": threshold_amount,
                "animations": [str(x).strip() for x in animations if str(x).strip()],
                "sort_order": int(item.get("sort_order") if item.get("sort_order") is not None else index),
            }
        )
    return out
