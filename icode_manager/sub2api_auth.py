import json
import time
import urllib.error
import urllib.request
from http import HTTPStatus
from typing import Any, Dict

from common import AppError


class Sub2APIAuth:
    def __init__(self, config: Dict[str, Any]):
        self.config = config
        self.base_url = str(config.get("base_url", "")).rstrip("/")
        self.auth = config.get("auth") or {}
        self.mode = self.resolve_mode()
        self.access_token = self.auth.get("access_token") or config.get("admin_token", "")
        self.refresh_token = self.auth.get("refresh_token", "")
        self.expires_at = 0.0

    def resolve_mode(self) -> str:
        if self.config.get("admin_api_key"):
            return "api_key"
        if self.config.get("admin_token"):
            return "token"
        return self.auth.get("mode", "token")

    def headers(self, force_refresh: bool = False) -> Dict[str, str]:
        headers = {"Accept": "application/json"}
        if self.mode == "api_key":
            key = self.auth.get("api_key") or self.config.get("admin_api_key")
            if not key:
                raise AppError(HTTPStatus.INTERNAL_SERVER_ERROR, "未配置 sub2api admin api key")
            headers["x-api-key"] = key
            return headers

        token = self.get_access_token(force_refresh)
        headers["Authorization"] = f"Bearer {token}"
        return headers

    def refresh_after_auth_error(self) -> bool:
        if self.mode == "api_key":
            return False
        self.access_token = ""
        self.get_access_token(force_refresh=True)
        return True

    def get_access_token(self, force_refresh: bool = False) -> str:
        if not self.base_url:
            raise AppError(HTTPStatus.INTERNAL_SERVER_ERROR, "未配置 sub2api.base_url")

        if self.mode == "login":
            self.ensure_login_token(force_refresh)
        elif self.mode == "token":
            self.ensure_static_token(force_refresh)
        else:
            raise AppError(HTTPStatus.INTERNAL_SERVER_ERROR, f"不支持的 sub2api 鉴权模式：{self.mode}")

        if not self.access_token:
            raise AppError(HTTPStatus.INTERNAL_SERVER_ERROR, "未取得 sub2api access token")
        return self.access_token

    def ensure_static_token(self, force_refresh: bool) -> None:
        if force_refresh and self.refresh_token:
            self.refresh()
            return
        if not self.access_token:
            raise AppError(HTTPStatus.INTERNAL_SERVER_ERROR, "token 模式需要配置 access_token")
        if self.should_refresh() and self.refresh_token:
            self.refresh()

    def ensure_login_token(self, force_refresh: bool) -> None:
        if force_refresh:
            if self.refresh_token and self.try_refresh():
                return
            self.login()
            return
        if self.access_token and not self.should_refresh():
            return
        if self.refresh_token and self.try_refresh():
            return
        self.login()

    def should_refresh(self) -> bool:
        return bool(self.expires_at and time.time() >= self.expires_at - 30)

    def try_refresh(self) -> bool:
        try:
            self.refresh()
            return True
        except AppError:
            return False

    def login(self) -> None:
        email = self.auth.get("email", "")
        password = self.auth.get("password", "")
        if not email or not password:
            raise AppError(HTTPStatus.INTERNAL_SERVER_ERROR, "login 模式需要配置 email 和 password")

        payload = {"email": email, "password": password}
        if self.auth.get("turnstile_token"):
            payload["turnstile_token"] = self.auth["turnstile_token"]

        data = unwrap_response(self.post_json("/auth/login", payload))
        if data.get("requires_2fa"):
            data = self.complete_2fa(data)
        self.apply_token_payload(data)

    def complete_2fa(self, login_payload: Dict[str, Any]) -> Dict[str, Any]:
        totp_code = self.auth.get("totp_code", "")
        temp_token = login_payload.get("temp_token", "")
        if not totp_code:
            raise AppError(HTTPStatus.BAD_GATEWAY, "sub2api 管理员启用了 2FA，需要配置当前 totp_code")
        return unwrap_response(self.post_json("/auth/login/2fa", {"temp_token": temp_token, "totp_code": totp_code}))

    def refresh(self) -> None:
        if not self.refresh_token:
            raise AppError(HTTPStatus.UNAUTHORIZED, "refresh token 为空")
        data = unwrap_response(self.post_json("/auth/refresh", {"refresh_token": self.refresh_token}))
        self.apply_token_payload(data)

    def apply_token_payload(self, data: Dict[str, Any]) -> None:
        token = data.get("access_token", "")
        if not token:
            raise AppError(HTTPStatus.BAD_GATEWAY, "sub2api 响应缺少 access_token")
        self.access_token = token
        self.refresh_token = data.get("refresh_token") or self.refresh_token
        expires_in = int(data.get("expires_in") or 0)
        self.expires_at = time.time() + expires_in if expires_in > 0 else 0.0

    def post_json(self, path: str, payload: Dict[str, Any]) -> Any:
        body = json.dumps(payload).encode("utf-8")
        req = urllib.request.Request(
            self.base_url + path,
            data=body,
            headers={"Accept": "application/json", "Content-Type": "application/json"},
            method="POST",
        )
        try:
            with urllib.request.urlopen(req, timeout=float(self.config.get("timeout_seconds", 8))) as resp:
                return json.loads(resp.read().decode("utf-8"))
        except urllib.error.HTTPError as err:
            raise AppError(HTTPStatus.BAD_GATEWAY, f"sub2api 鉴权失败：HTTP {err.code}") from err
        except Exception as err:
            raise AppError(HTTPStatus.BAD_GATEWAY, f"sub2api 鉴权失败：{err}") from err


def unwrap_response(payload: Any) -> Dict[str, Any]:
    if isinstance(payload, dict) and isinstance(payload.get("data"), dict):
        return payload["data"]
    if isinstance(payload, dict):
        return payload
    raise AppError(HTTPStatus.BAD_GATEWAY, "sub2api 响应格式不正确")
