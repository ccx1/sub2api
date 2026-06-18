import datetime as dt
import hashlib
import json
import re
from http import HTTPStatus
from pathlib import Path
from typing import Any, Dict, List, Optional


ROOT = Path(__file__).resolve().parent
EMAIL_RE = re.compile(r"^[^@\s]+@[^@\s]+\.[^@\s]+$")
TIME_RE = re.compile(
    r"^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})(?::(\d{2}))?(Z|[+-]\d{2}:\d{2})?$"
)


class AppError(Exception):
    def __init__(self, status: int, message: str):
        super().__init__(message)
        self.status = status
        self.message = message


def utc_now() -> str:
    return dt.datetime.now(dt.timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")


def parse_time(value: Optional[str]) -> Optional[dt.datetime]:
    if not value:
        return None
    match = TIME_RE.match(value)
    if not match:
        raise ValueError("invalid ISO datetime")

    year, month, day, hour, minute, second, zone = match.groups()
    parsed = dt.datetime(
        int(year),
        int(month),
        int(day),
        int(hour),
        int(minute),
        int(second or "0"),
    )
    if not zone:
        return parsed.replace(tzinfo=dt.timezone.utc)
    if zone == "Z":
        return parsed.replace(tzinfo=dt.timezone.utc)

    sign = 1 if zone[0] == "+" else -1
    offset_hour = int(zone[1:3])
    offset_minute = int(zone[4:6])
    offset = dt.timedelta(hours=offset_hour, minutes=offset_minute) * sign
    return parsed.replace(tzinfo=dt.timezone(offset))


def normalize_email(email: str) -> str:
    normalized = (email or "").strip().lower()
    if not EMAIL_RE.match(normalized):
        raise AppError(HTTPStatus.BAD_REQUEST, "邮箱格式不正确")
    return normalized


def hash_email(email: str) -> str:
    return hashlib.sha256(email.encode("utf-8")).hexdigest()


def load_config(path: Path) -> Dict[str, Any]:
    if not path.exists():
        raise SystemExit(f"配置文件不存在：{path}")
    with path.open("r", encoding="utf-8-sig") as f:
        data = json.load(f)
    return data


def db_path_from_config(config: Dict[str, Any], config_path: Path) -> Path:
    raw = config.get("database", {}).get("path", "redeem_claim.db")
    path = Path(raw)
    if not path.is_absolute():
        path = config_path.parent / path
    return path


def read_codes(path: Path) -> List[str]:
    seen = set()
    out = []
    for line in path.read_text(encoding="utf-8-sig").splitlines():
        code = line.strip()
        if not code or code.startswith("#") or code in seen:
            continue
        seen.add(code)
        out.append(code)
    return out
