import json
import urllib.error
import urllib.parse
import urllib.request
from http import HTTPStatus
from typing import Any, Dict, List, Optional

from affiliate_admin import connect_db, load_database_config
from common import AppError
from sub2api_auth import Sub2APIAuth


class UserVerifier:
    def __init__(self, config: Dict[str, Any]):
        self.app_config = config
        self.config = config.get("sub2api", config)
        self.auth = Sub2APIAuth(self.config)
        self.db_config = load_database_config(config)

    def exists(self, email: str) -> bool:
        return self.find_user(email) is not None

    def find_user(self, email: str) -> Optional[Dict[str, Any]]:
        email = email.strip().lower()
        mode = self.config.get("verify_mode", "admin_users_api")
        if mode == "disabled":
            return {"id": 0, "email": email}
        if mode == "database":
            return self.find_user_from_database(email)
        if mode != "admin_users_api":
            raise AppError(HTTPStatus.INTERNAL_SERVER_ERROR, f"不支持的校验模式：{mode}")

        base_url = self.config.get("base_url", "").rstrip("/")
        if not base_url:
            raise AppError(HTTPStatus.INTERNAL_SERVER_ERROR, "未配置 sub2api.base_url")

        payload = self.fetch_users(base_url, email)
        for item in extract_user_items(payload):
            if str(item.get("email", "")).strip().lower() == email:
                return item
        return None

    def fetch_users(self, base_url: str, email: str) -> Any:
        query = urllib.parse.urlencode({"page": 1, "page_size": 1, "search": email})
        url = f"{base_url}/admin/users?{query}"
        for attempt in range(2):
            try:
                return self.open_users(url)
            except urllib.error.HTTPError as err:
                if err.code in (401, 403) and attempt == 0:
                    self.auth.refresh_after_auth_error()
                    continue
                if err.code in (401, 403):
                    raise AppError(HTTPStatus.BAD_GATEWAY, "sub2api 管理员校验鉴权失败") from err
                raise AppError(HTTPStatus.BAD_GATEWAY, f"sub2api 校验失败：HTTP {err.code}") from err
            except AppError:
                raise
            except Exception as err:
                raise AppError(HTTPStatus.BAD_GATEWAY, f"sub2api 校验失败：{err}") from err
        raise AppError(HTTPStatus.BAD_GATEWAY, "sub2api 校验失败")

    def open_users(self, url: str, force_refresh: bool = False) -> Any:
        req = urllib.request.Request(url, headers=self.auth.headers(force_refresh=force_refresh))
        with urllib.request.urlopen(req, timeout=float(self.config.get("timeout_seconds", 8))) as resp:
            return json.loads(resp.read().decode("utf-8"))

    def find_user_from_database(self, email: str) -> Optional[Dict[str, Any]]:
        with connect_db(self.db_config) as conn:
            rows = conn.execute(
                """
                SELECT id,
                       email,
                       COALESCE(username, '') AS username,
                       status,
                       deleted_at,
                       total_recharged
                FROM users
                WHERE lower(email) = lower(%s)
                ORDER BY id
                """,
                (email,),
            ).fetchall()
        if len(rows) != 1:
            return None
        row = dict(rows[0])
        if row.get("deleted_at"):
            return None
        return row


def extract_user_items(payload: Any) -> List[Dict[str, Any]]:
    if isinstance(payload, list):
        return [x for x in payload if isinstance(x, dict)]
    if not isinstance(payload, dict):
        return []

    candidates = [payload.get("data"), payload.get("items"), payload.get("users")]
    if isinstance(payload.get("data"), dict):
        data = payload["data"]
        candidates.extend([data.get("items"), data.get("list"), data.get("users"), data.get("data")])

    for candidate in candidates:
        if isinstance(candidate, list):
            return [x for x in candidate if isinstance(x, dict)]
    return []
