from __future__ import annotations

import json
import logging
import ssl
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass

from sub2api_monitor.config import AuthConfig, SessionTokens, Sub2ApiConfig, save_session_tokens

LOGGER = logging.getLogger(__name__)


class Sub2ApiError(RuntimeError):
    def __init__(self, message: str, status_code: int = 0) -> None:
        super().__init__(message)
        self.status_code = status_code


@dataclass(slots=True)
class ApiEnvelope:
    code: int
    message: str
    data: object


class Sub2ApiClient:
    def __init__(self, sub2api: Sub2ApiConfig, auth: AuthConfig, tokens: SessionTokens) -> None:
        self.sub2api = sub2api
        self.auth = auth
        self.tokens = SessionTokens(
            access_token=tokens.access_token or auth.access_token,
            refresh_token=tokens.refresh_token or auth.refresh_token,
            expires_at=tokens.expires_at,
        )

    def list_api_keys(self) -> list[dict]:
        if self.auth.mode == "api_key":
            alias = self.auth.api_key_alias or self._mask_api_key(self.auth.api_key)
            return [{"id": 0, "name": alias, "status": "active", "quota_used": None}]

        items: list[dict] = []
        page = 1
        while True:
            payload = self.request_json("GET", "/keys", params={"page": page, "page_size": 200})
            data = self._expect_dict(payload)
            batch = list(data.get("items", []))
            items.extend(batch)
            if page >= int(data.get("pages", 1)):
                return items
            page += 1

    def get_usage_stats(self, api_key_id: int, date_str: str) -> dict:
        if self.auth.mode == "api_key":
            raise Sub2ApiError("api_key 模式不支持 /api/v1/usage/stats")
        return self._expect_dict(
            self.request_json(
                "GET",
                "/usage/stats",
                params={
                    "api_key_id": api_key_id,
                    "start_date": date_str,
                    "end_date": date_str,
                    "timezone": self.sub2api.timezone,
                },
            )
        )

    def get_public_api_key_usage(self, start_date: str, end_date: str) -> dict:
        if self.auth.mode != "api_key":
            raise Sub2ApiError("当前不是 api_key 模式")
        return self._expect_dict(
            self.request_public_json(
                "GET",
                "/usage",
                params={"start_date": start_date, "end_date": end_date},
            )
        )

    def request_json(
        self,
        method: str,
        path: str,
        params: dict | None = None,
        payload: dict | None = None,
        allow_retry: bool = True,
    ) -> object:
        if self.auth.mode == "api_key":
            raise Sub2ApiError("api_key 模式不能调用用户态 /api/v1 接口")
        self.ensure_authenticated()
        url = self._build_api_url(path, params)
        body = json.dumps(payload).encode("utf-8") if payload is not None else None
        headers = {"Content-Type": "application/json"}
        if self.tokens.access_token:
            headers["Authorization"] = f"Bearer {self.tokens.access_token}"
        request = urllib.request.Request(url=url, data=body, headers=headers, method=method.upper())
        try:
            with urllib.request.urlopen(
                request,
                timeout=self.sub2api.timeout_seconds,
                context=self._ssl_context(),
            ) as response:
                raw = response.read().decode("utf-8")
        except urllib.error.HTTPError as exc:
            if exc.code == 401 and allow_retry and self.tokens.refresh_token and not path.startswith("/auth/"):
                self.refresh_token()
                return self.request_json(method, path, params, payload, allow_retry=False)
            raise self._parse_http_error(exc, envelope_expected=True) from exc
        envelope = self._parse_envelope(raw)
        if envelope.code != 0:
            raise Sub2ApiError(envelope.message or "sub2api returned error")
        return envelope.data

    def request_public_json(
        self,
        method: str,
        path: str,
        params: dict | None = None,
        payload: dict | None = None,
    ) -> object:
        url = self._build_gateway_url(path, params)
        body = json.dumps(payload).encode("utf-8") if payload is not None else None
        headers = {"Content-Type": "application/json"}
        if self.auth.api_key:
            headers["Authorization"] = f"Bearer {self.auth.api_key}"
        request = urllib.request.Request(url=url, data=body, headers=headers, method=method.upper())
        try:
            with urllib.request.urlopen(
                request,
                timeout=self.sub2api.timeout_seconds,
                context=self._ssl_context(),
            ) as response:
                raw = response.read().decode("utf-8")
        except urllib.error.HTTPError as exc:
            raise self._parse_http_error(exc, envelope_expected=False) from exc
        data = json.loads(raw)
        if not isinstance(data, dict):
            raise Sub2ApiError("服务端返回数据结构异常，预期是对象")
        return data

    def ensure_authenticated(self) -> None:
        if self.auth.mode == "api_key":
            return
        if self.tokens.access_token:
            return
        if self.tokens.refresh_token:
            self.refresh_token()
            return
        if self.auth.mode == "password":
            self.login()
            return
        raise Sub2ApiError("当前没有可用 access token，也无法自动刷新")

    def login(self) -> None:
        payload = {"email": self.auth.email, "password": self.auth.password}
        data = self._expect_dict(self._request_auth("/auth/login", payload))
        if data.get("requires_2fa"):
            raise Sub2ApiError("当前账号启用了 2FA，password 模式无法自动完成，请切换到 token 模式")
        self._update_tokens(
            access_token=str(data.get("access_token", "")).strip(),
            refresh_token=str(data.get("refresh_token", "")).strip(),
            expires_in=data.get("expires_in"),
        )

    def refresh_token(self) -> None:
        if not self.tokens.refresh_token:
            raise Sub2ApiError("没有 refresh_token，无法刷新登录态")
        data = self._expect_dict(
            self._request_auth("/auth/refresh", {"refresh_token": self.tokens.refresh_token})
        )
        self._update_tokens(
            access_token=str(data.get("access_token", "")).strip(),
            refresh_token=str(data.get("refresh_token", "")).strip(),
            expires_in=data.get("expires_in"),
        )

    def _request_auth(self, path: str, payload: dict) -> object:
        url = self._build_api_url(path, None)
        request = urllib.request.Request(
            url=url,
            data=json.dumps(payload).encode("utf-8"),
            headers={"Content-Type": "application/json"},
            method="POST",
        )
        try:
            with urllib.request.urlopen(
                request,
                timeout=self.sub2api.timeout_seconds,
                context=self._ssl_context(),
            ) as response:
                raw = response.read().decode("utf-8")
        except urllib.error.HTTPError as exc:
            raise self._parse_http_error(exc, envelope_expected=True) from exc
        envelope = self._parse_envelope(raw)
        if envelope.code != 0:
            raise Sub2ApiError(envelope.message or "认证失败")
        return envelope.data

    def _update_tokens(self, access_token: str, refresh_token: str, expires_in: object) -> None:
        if not access_token:
            raise Sub2ApiError("服务端没有返回 access_token")
        self.tokens = SessionTokens(
            access_token=access_token,
            refresh_token=refresh_token or self.tokens.refresh_token,
            expires_at=str(expires_in or ""),
        )
        save_session_tokens(self.auth.session_path, self.tokens)
        LOGGER.info("已更新本地会话缓存: %s", self.auth.session_path)

    def _normalized_base_url(self) -> str:
        base = self.sub2api.base_url.rstrip("/")
        if base.endswith("/api/v1"):
            return base[: -len("/api/v1")]
        return base

    def _build_api_url(self, path: str, params: dict | None) -> str:
        base = f"{self._normalized_base_url()}/api/v1{path}"
        if not params:
            return base
        query = urllib.parse.urlencode(params)
        return f"{base}?{query}"

    def _build_gateway_url(self, path: str, params: dict | None) -> str:
        base = f"{self._normalized_base_url()}/v1{path}"
        if not params:
            return base
        query = urllib.parse.urlencode(params)
        return f"{base}?{query}"

    def _ssl_context(self) -> ssl.SSLContext | None:
        if self.sub2api.verify_ssl:
            return None
        context = ssl.create_default_context()
        context.check_hostname = False
        context.verify_mode = ssl.CERT_NONE
        return context

    def _parse_http_error(self, exc: urllib.error.HTTPError, envelope_expected: bool) -> Sub2ApiError:
        raw = exc.read().decode("utf-8", errors="replace")
        try:
            if envelope_expected:
                envelope = self._parse_envelope(raw)
                return Sub2ApiError(envelope.message or raw, status_code=exc.code)
            data = json.loads(raw)
            if isinstance(data, dict):
                error_field = data.get("error")
                if isinstance(error_field, dict) and error_field.get("message"):
                    return Sub2ApiError(str(error_field["message"]), status_code=exc.code)
                if data.get("message"):
                    return Sub2ApiError(str(data["message"]), status_code=exc.code)
            return Sub2ApiError(raw or f"HTTP {exc.code}", status_code=exc.code)
        except Exception:
            return Sub2ApiError(raw or f"HTTP {exc.code}", status_code=exc.code)

    def _parse_envelope(self, raw: str) -> ApiEnvelope:
        data = json.loads(raw)
        if not isinstance(data, dict):
            raise Sub2ApiError("服务端返回的不是 JSON 对象")
        return ApiEnvelope(
            code=int(data.get("code", -1)),
            message=str(data.get("message", "")),
            data=data.get("data"),
        )

    def _expect_dict(self, value: object) -> dict:
        if not isinstance(value, dict):
            raise Sub2ApiError("服务端返回数据结构异常，预期是对象")
        return value

    def _mask_api_key(self, value: str) -> str:
        raw = value.strip()
        if len(raw) <= 12:
            return raw or "current_api_key"
        return f"{raw[:8]}...{raw[-4:]}"
