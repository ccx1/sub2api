# -*- coding: utf-8 -*-
from __future__ import print_function

from config_loader import get_product
from http_utils import HttpRequestError, request_json, unwrap_api_envelope


class Sub2ApiClient(object):
    def __init__(self, config):
        sub2api = config["sub2api"]
        self.config = config
        self.api_base_url = str(sub2api["api_base_url"]).rstrip("/")
        self.timeout = int(sub2api.get("timeout_seconds", 30))
        self.auth = sub2api["auth"]
        self._cached_token = None

    def generate_codes(self, product_key, count_override=None, turnstile_token=None, totp_code=None):
        product = get_product(self.config, product_key)
        redeem = product["redeem"]

        payload = {
            "count": int(count_override or redeem["count"]),
            "type": str(redeem["type"]).strip(),
            "value": redeem["value"],
        }

        for field in ("group_id", "validity_days", "expires_in_days"):
            if field in redeem and redeem[field] not in ("", None):
                payload[field] = redeem[field]

        headers = self._admin_headers(turnstile_token=turnstile_token, totp_code=totp_code)
        raw = request_json(
            "POST",
            self.api_base_url + "/admin/redeem-codes/generate",
            headers=headers,
            payload=payload,
            timeout=self.timeout,
        )
        data = unwrap_api_envelope(raw)
        if not isinstance(data, list):
            raise HttpRequestError("generate redeem codes returned unexpected payload", payload=data)

        codes = []
        for item in data:
            if not isinstance(item, dict):
                raise HttpRequestError("redeem code item payload is invalid", payload=item)
            code = str(item.get("code", "")).strip()
            if not code:
                raise HttpRequestError("redeem code item missing code", payload=item)
            codes.append(code)

        return codes, payload, product

    def _admin_headers(self, turnstile_token=None, totp_code=None):
        headers = {
            "Accept": "application/json",
            "Content-Type": "application/json",
        }
        mode = str(self.auth["mode"]).strip()

        if mode == "token":
            headers["Authorization"] = "Bearer %s" % str(self.auth["token"]).strip()
            return headers

        if mode == "admin_api_key":
            headers["x-api-key"] = str(self.auth["admin_api_key"]).strip()
            return headers

        token = self._cached_token or self._login(turnstile_token=turnstile_token, totp_code=totp_code)
        self._cached_token = token
        headers["Authorization"] = "Bearer %s" % token
        return headers

    def _login(self, turnstile_token=None, totp_code=None):
        login = self.auth["login"]
        payload = {
            "email": str(login["email"]).strip(),
            "password": str(login["password"]),
        }

        effective_turnstile = str(turnstile_token or login.get("turnstile_token", "") or "").strip()
        if effective_turnstile:
            payload["turnstile_token"] = effective_turnstile

        raw = request_json(
            "POST",
            self.api_base_url + "/auth/login",
            headers={"Accept": "application/json", "Content-Type": "application/json"},
            payload=payload,
            timeout=self.timeout,
        )
        data = unwrap_api_envelope(raw)
        if not isinstance(data, dict):
            raise HttpRequestError("login returned unexpected payload", payload=data)

        if data.get("requires_2fa"):
            return self._login_with_2fa(data, totp_code=totp_code)

        token = str(data.get("access_token", "")).strip()
        if not token:
            raise HttpRequestError("login succeeded but access_token is missing", payload=data)
        return token

    def _login_with_2fa(self, login_payload, totp_code=None):
        temp_token = str(login_payload.get("temp_token", "")).strip()
        if not temp_token:
            raise HttpRequestError("2FA login missing temp_token", payload=login_payload)

        configured_code = str(self.auth["login"].get("totp_code", "")).strip()
        effective_code = str(totp_code or configured_code or "").strip()
        if not effective_code:
            raise ValueError("2FA login requires totp_code from CLI or config")

        raw = request_json(
            "POST",
            self.api_base_url + "/auth/login/2fa",
            headers={"Accept": "application/json", "Content-Type": "application/json"},
            payload={"temp_token": temp_token, "totp_code": effective_code},
            timeout=self.timeout,
        )
        data = unwrap_api_envelope(raw)
        if not isinstance(data, dict):
            raise HttpRequestError("2FA login returned unexpected payload", payload=data)

        token = str(data.get("access_token", "")).strip()
        if not token:
            raise HttpRequestError("2FA login succeeded but access_token is missing", payload=data)
        return token
