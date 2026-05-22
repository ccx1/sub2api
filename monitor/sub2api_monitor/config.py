from __future__ import annotations

import json
import tomllib
from dataclasses import dataclass, field
from pathlib import Path


@dataclass(slots=True)
class Sub2ApiConfig:
    base_url: str
    timezone: str = "Asia/Shanghai"
    timeout_seconds: int = 30
    verify_ssl: bool = True


@dataclass(slots=True)
class AuthConfig:
    mode: str
    api_key: str = ""
    api_key_alias: str = ""
    access_token: str = ""
    refresh_token: str = ""
    email: str = ""
    password: str = ""
    session_path: Path = Path(".runtime/auth_session.json")


@dataclass(slots=True)
class KeyFilterConfig:
    active_only: bool = True
    include_ids: list[int] = field(default_factory=list)
    include_names: list[str] = field(default_factory=list)
    include_name_contains: list[str] = field(default_factory=list)
    exclude_ids: list[int] = field(default_factory=list)
    exclude_names: list[str] = field(default_factory=list)


@dataclass(slots=True)
class ReportConfig:
    day_offset: int = 0
    history_days: int = 1
    sort_by: str = "actual_cost"
    max_keys_in_message: int = 20
    quiet_if_no_usage: bool = False


@dataclass(slots=True)
class ThresholdConfig:
    total_actual_cost: float | None = None
    total_requests: int | None = None
    total_tokens: int | None = None
    per_key_actual_cost: float | None = None
    per_key_requests: int | None = None
    per_key_total_tokens: int | None = None


@dataclass(slots=True)
class DingTalkConfig:
    enabled: bool = False
    webhook: str = ""
    secret: str = ""
    keyword: str = ""
    at_all: bool = False
    at_mobiles: list[str] = field(default_factory=list)


@dataclass(slots=True)
class StateConfig:
    path: Path = Path(".runtime/monitor_state.json")
    dedupe_daily_summary: bool = True
    dedupe_threshold_alert: bool = True


@dataclass(slots=True)
class SessionTokens:
    access_token: str = ""
    refresh_token: str = ""
    expires_at: str = ""


@dataclass(slots=True)
class AppConfig:
    sub2api: Sub2ApiConfig
    auth: AuthConfig
    keys: KeyFilterConfig
    report: ReportConfig
    thresholds: ThresholdConfig
    dingtalk: DingTalkConfig
    state: StateConfig


def _path_from(config_path: Path, raw: str, default_value: str) -> Path:
    value = raw.strip() or default_value
    path = Path(value)
    return path if path.is_absolute() else (config_path.parent / path).resolve()


def _ensure_list(raw: object) -> list[object]:
    if raw is None:
        return []
    if not isinstance(raw, list):
        raise ValueError("配置里的列表字段必须是数组")
    return raw


def _clean_strings(values: list[object]) -> list[str]:
    return [str(value).strip() for value in values if str(value).strip()]


def _clean_ints(values: list[object]) -> list[int]:
    return [int(value) for value in values]


def load_app_config(config_path: Path) -> AppConfig:
    if not config_path.exists():
        raise FileNotFoundError(f"配置文件不存在: {config_path}")

    raw = tomllib.loads(config_path.read_text(encoding="utf-8"))
    sub_raw = raw.get("sub2api", {})
    auth_raw = raw.get("auth", {})
    keys_raw = raw.get("keys", {})
    report_raw = raw.get("report", {})
    threshold_raw = raw.get("thresholds", {})
    dingtalk_raw = raw.get("dingtalk", {})
    state_raw = raw.get("state", {})

    base_url = str(sub_raw.get("base_url", "")).strip().rstrip("/")
    if not base_url.startswith("http"):
        raise ValueError("sub2api.base_url 必须是完整的 http/https 地址")

    mode = str(auth_raw.get("mode", "")).strip().lower()
    if mode not in {"api_key", "token", "password"}:
        raise ValueError("auth.mode 只允许 api_key、token 或 password")
    if mode == "api_key" and not str(auth_raw.get("api_key", "")).strip():
        raise ValueError("api_key 模式需要填写 auth.api_key")
    if mode == "password" and not (
        str(auth_raw.get("email", "")).strip() and str(auth_raw.get("password", "")).strip()
    ):
        raise ValueError("password 模式需要 email 和 password")

    report = ReportConfig(
        day_offset=int(report_raw.get("day_offset", 0)),
        history_days=max(1, int(report_raw.get("history_days", 1))),
        sort_by=str(report_raw.get("sort_by", "actual_cost")).strip() or "actual_cost",
        max_keys_in_message=max(1, int(report_raw.get("max_keys_in_message", 20))),
        quiet_if_no_usage=bool(report_raw.get("quiet_if_no_usage", False)),
    )
    if report.sort_by not in {"actual_cost", "requests", "total_tokens"}:
        raise ValueError("report.sort_by 只允许 actual_cost、requests、total_tokens")

    return AppConfig(
        sub2api=Sub2ApiConfig(
            base_url=base_url,
            timezone=str(sub_raw.get("timezone", "Asia/Shanghai")).strip() or "Asia/Shanghai",
            timeout_seconds=max(5, int(sub_raw.get("timeout_seconds", 30))),
            verify_ssl=bool(sub_raw.get("verify_ssl", True)),
        ),
        auth=AuthConfig(
            mode=mode,
            api_key=str(auth_raw.get("api_key", "")).strip(),
            api_key_alias=str(auth_raw.get("api_key_alias", "")).strip(),
            access_token=str(auth_raw.get("access_token", "")).strip(),
            refresh_token=str(auth_raw.get("refresh_token", "")).strip(),
            email=str(auth_raw.get("email", "")).strip(),
            password=str(auth_raw.get("password", "")).strip(),
            session_path=_path_from(
                config_path,
                str(auth_raw.get("session_path", "")),
                ".runtime/auth_session.json",
            ),
        ),
        keys=KeyFilterConfig(
            active_only=bool(keys_raw.get("active_only", True)),
            include_ids=_clean_ints(_ensure_list(keys_raw.get("include_ids"))),
            include_names=_clean_strings(_ensure_list(keys_raw.get("include_names"))),
            include_name_contains=_clean_strings(_ensure_list(keys_raw.get("include_name_contains"))),
            exclude_ids=_clean_ints(_ensure_list(keys_raw.get("exclude_ids"))),
            exclude_names=_clean_strings(_ensure_list(keys_raw.get("exclude_names"))),
        ),
        report=report,
        thresholds=ThresholdConfig(
            total_actual_cost=_maybe_float(threshold_raw.get("total_actual_cost")),
            total_requests=_maybe_int(threshold_raw.get("total_requests")),
            total_tokens=_maybe_int(threshold_raw.get("total_tokens")),
            per_key_actual_cost=_maybe_float(threshold_raw.get("per_key_actual_cost")),
            per_key_requests=_maybe_int(threshold_raw.get("per_key_requests")),
            per_key_total_tokens=_maybe_int(threshold_raw.get("per_key_total_tokens")),
        ),
        dingtalk=DingTalkConfig(
            enabled=bool(dingtalk_raw.get("enabled", False)),
            webhook=str(dingtalk_raw.get("webhook", "")).strip(),
            secret=str(dingtalk_raw.get("secret", "")).strip(),
            keyword=str(dingtalk_raw.get("keyword", "")).strip(),
            at_all=bool(dingtalk_raw.get("at_all", False)),
            at_mobiles=_clean_strings(_ensure_list(dingtalk_raw.get("at_mobiles"))),
        ),
        state=StateConfig(
            path=_path_from(config_path, str(state_raw.get("path", "")), ".runtime/monitor_state.json"),
            dedupe_daily_summary=bool(state_raw.get("dedupe_daily_summary", True)),
            dedupe_threshold_alert=bool(state_raw.get("dedupe_threshold_alert", True)),
        ),
    )


def _maybe_float(value: object) -> float | None:
    return None if value in (None, "") else float(value)


def _maybe_int(value: object) -> int | None:
    return None if value in (None, "") else int(value)


def load_session_tokens(path: Path) -> SessionTokens:
    if not path.exists():
        return SessionTokens()
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError:
        return SessionTokens()
    return SessionTokens(
        access_token=str(data.get("access_token", "")).strip(),
        refresh_token=str(data.get("refresh_token", "")).strip(),
        expires_at=str(data.get("expires_at", "")).strip(),
    )


def save_session_tokens(path: Path, tokens: SessionTokens) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    payload = {
        "access_token": tokens.access_token,
        "refresh_token": tokens.refresh_token,
        "expires_at": tokens.expires_at,
    }
    path.write_text(json.dumps(payload, ensure_ascii=False, indent=2), encoding="utf-8")
