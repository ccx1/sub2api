#!/usr/bin/env python3
"""Convert Codex/OpenAI OAuth JSON files into sub2api account import JSON."""

from __future__ import annotations

import argparse
import base64
import glob
import hashlib
import json
import sys
from datetime import datetime, timezone
from pathlib import Path
from typing import Any


OPENAI_CLIENT_ID = "app_EMoamEEZ73f0CkXaXp7hrann"
REQUIRED_TOKEN_KEYS = ("access_token", "refresh_token", "id_token")


class ConversionError(Exception):
    """Raised when an input file cannot be converted safely."""


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Convert one or more Codex/OpenAI token JSON files into sub2api import JSON.",
    )
    parser.add_argument(
        "inputs",
        nargs="+",
        help="Input JSON files, directories, or glob patterns. Directories are scanned for *.json.",
    )
    parser.add_argument(
        "-o",
        "--output",
        required=True,
        help="Output JSON file path.",
    )
    parser.add_argument(
        "--accounts-only",
        action="store_true",
        help="Output only the accounts array for the frontend batch-create importer.",
    )
    parser.add_argument(
        "--concurrency",
        type=int,
        default=3,
        help="Account concurrency value. Default: 3.",
    )
    parser.add_argument(
        "--priority",
        type=int,
        default=50,
        help="Account priority value. Default: 50.",
    )
    parser.add_argument(
        "--name-prefix",
        default="",
        help="Optional account name prefix.",
    )
    parser.add_argument(
        "--notes",
        default=None,
        help="Optional notes value applied to every generated account.",
    )
    parser.add_argument(
        "--dedupe",
        choices=("email", "file", "none"),
        default="email",
        help="Deduplicate inputs by email, file path, or not at all. Default: email.",
    )
    parser.add_argument(
        "--pretty",
        action=argparse.BooleanOptionalAction,
        default=True,
        help="Pretty-print output JSON. Default: true.",
    )
    return parser.parse_args()


def expand_inputs(patterns: list[str]) -> list[Path]:
    files: list[Path] = []
    seen: set[Path] = set()
    for pattern in patterns:
        matches = glob.glob(pattern, recursive=True)
        if not matches:
            matches = [pattern]

        for match in matches:
            path = Path(match).expanduser()
            if path.is_dir():
                candidates = sorted(path.glob("*.json"))
            else:
                candidates = [path]

            for candidate in candidates:
                resolved = candidate.resolve()
                if resolved in seen:
                    continue
                seen.add(resolved)
                files.append(resolved)

    return files


def load_json(path: Path) -> dict[str, Any]:
    try:
        with path.open("r", encoding="utf-8-sig") as handle:
            data = json.load(handle)
    except json.JSONDecodeError as exc:
        raise ConversionError(f"{path}: invalid JSON: {exc}") from exc
    except OSError as exc:
        raise ConversionError(f"{path}: cannot read file: {exc}") from exc

    if not isinstance(data, dict):
        raise ConversionError(f"{path}: top-level JSON must be an object")
    return data


def first_string(data: dict[str, Any], *paths: tuple[str, ...]) -> str:
    for keys in paths:
        current: Any = data
        for key in keys:
            if not isinstance(current, dict):
                current = None
                break
            current = current.get(key)
        if isinstance(current, str) and current.strip():
            return current.strip()
    return ""


def decode_jwt_payload(token: str) -> dict[str, Any]:
    parts = token.split(".")
    if len(parts) != 3:
        return {}

    payload = parts[1]
    padding = "=" * (-len(payload) % 4)
    try:
        raw = base64.urlsafe_b64decode((payload + padding).encode("ascii"))
        decoded = json.loads(raw.decode("utf-8"))
    except (ValueError, UnicodeDecodeError):
        return {}
    return decoded if isinstance(decoded, dict) else {}


def enrich_from_jwt(credentials: dict[str, Any], token: str) -> None:
    claims = decode_jwt_payload(token)
    if not claims:
        return

    set_if_missing(credentials, "email", string_value(claims.get("email")))

    exp = claims.get("exp")
    if "expires_at" not in credentials and isinstance(exp, (int, float)) and exp > 0:
        credentials["expires_at"] = datetime.fromtimestamp(exp, tz=timezone.utc).isoformat().replace("+00:00", "Z")

    auth = claims.get("https://api.openai.com/auth")
    if not isinstance(auth, dict):
        set_if_missing(credentials, "chatgpt_user_id", string_value(claims.get("sub")))
        return

    set_if_missing(credentials, "chatgpt_account_id", string_value(auth.get("chatgpt_account_id")))
    set_if_missing(credentials, "chatgpt_user_id", string_value(auth.get("chatgpt_user_id")))
    set_if_missing(credentials, "chatgpt_user_id", string_value(auth.get("user_id")))
    set_if_missing(credentials, "plan_type", string_value(auth.get("chatgpt_plan_type")))
    set_if_missing(credentials, "organization_id", string_value(auth.get("poid")))

    organizations = auth.get("organizations")
    if isinstance(organizations, list) and "organization_id" not in credentials:
        default_org = first_org_id(organizations, default_only=True)
        fallback_org = first_org_id(organizations, default_only=False)
        set_if_missing(credentials, "organization_id", default_org or fallback_org)

    set_if_missing(credentials, "chatgpt_user_id", string_value(claims.get("sub")))


def first_org_id(organizations: list[Any], default_only: bool) -> str:
    for item in organizations:
        if not isinstance(item, dict):
            continue
        if default_only and item.get("is_default") is not True:
            continue
        org_id = string_value(item.get("id"))
        if org_id:
            return org_id
    return ""


def string_value(value: Any) -> str:
    return value.strip() if isinstance(value, str) else ""


def set_if_missing(target: dict[str, Any], key: str, value: str) -> None:
    if value and not string_value(target.get(key)):
        target[key] = value


def token_fingerprint(token: str) -> str:
    return hashlib.sha256(token.encode("utf-8")).hexdigest()


def build_account(data: dict[str, Any], path: Path, args: argparse.Namespace) -> dict[str, Any]:
    credentials = {
        "access_token": first_string(data, ("tokens", "access_token"), ("tokens", "accessToken"), ("access_token",), ("accessToken",), ("token",)),
        "refresh_token": first_string(data, ("tokens", "refresh_token"), ("tokens", "refreshToken"), ("refresh_token",), ("refreshToken",)),
        "id_token": first_string(data, ("tokens", "id_token"), ("tokens", "idToken"), ("id_token",), ("idToken",)),
        "client_id": OPENAI_CLIENT_ID,
    }

    missing = [key for key in REQUIRED_TOKEN_KEYS if not credentials[key]]
    if missing:
        raise ConversionError(f"{path}: missing required token field(s): {', '.join(missing)}")

    email = first_string(data, ("email",), ("user", "email"))
    if email:
        credentials["email"] = email

    enrich_from_jwt(credentials, credentials["id_token"])
    enrich_from_jwt(credentials, credentials["access_token"])

    account_name = build_account_name(path, credentials, data, args.name_prefix)
    extra = {
        "import_source": "codex_json",
        "import_file": path.name,
        "imported_at": now_rfc3339(),
        "access_token_sha256": token_fingerprint(credentials["access_token"]),
    }
    copy_optional_extra(data, extra, "token_source")
    copy_optional_extra(data, extra, "saved_at")
    copy_optional_extra(data, extra, "type", target_key="source_type")

    account: dict[str, Any] = {
        "name": account_name,
        "platform": "openai",
        "type": "oauth",
        "credentials": credentials,
        "extra": extra,
        "concurrency": args.concurrency,
        "priority": args.priority,
    }
    if args.notes is not None:
        account["notes"] = args.notes
    return account


def build_account_name(path: Path, credentials: dict[str, Any], data: dict[str, Any], prefix: str) -> str:
    candidates = [
        first_string(data, ("name",), ("user", "name")),
        string_value(credentials.get("email")),
        string_value(credentials.get("chatgpt_account_id")),
        path.stem,
    ]
    base = next((item for item in candidates if item), f"codex-account-{path.stem}")
    prefix = prefix.strip()
    return f"{prefix}{base}" if prefix else base


def copy_optional_extra(data: dict[str, Any], extra: dict[str, Any], key: str, target_key: str | None = None) -> None:
    value = data.get(key)
    if isinstance(value, str) and value.strip():
        extra[target_key or key] = value.strip()


def now_rfc3339() -> str:
    return datetime.now(timezone.utc).isoformat(timespec="seconds").replace("+00:00", "Z")


def dedupe_accounts(accounts: list[dict[str, Any]], mode: str) -> list[dict[str, Any]]:
    if mode == "none":
        return accounts

    seen: set[str] = set()
    output: list[dict[str, Any]] = []
    for account in accounts:
        credentials = account.get("credentials", {})
        extra = account.get("extra", {})
        if mode == "email" and isinstance(credentials, dict):
            key = string_value(credentials.get("email")).lower()
        elif mode == "file" and isinstance(extra, dict):
            key = string_value(extra.get("import_file")).lower()
        else:
            key = ""

        if key and key in seen:
            continue
        if key:
            seen.add(key)
        output.append(account)
    return output


def write_output(path: Path, payload: Any, pretty: bool) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    indent = 2 if pretty else None
    try:
        with path.open("w", encoding="utf-8", newline="\n") as handle:
            json.dump(payload, handle, ensure_ascii=False, indent=indent)
            handle.write("\n")
    except OSError as exc:
        raise ConversionError(f"{path}: cannot write output: {exc}") from exc


def main() -> int:
    args = parse_args()
    if args.concurrency < 0:
        print("ERROR: --concurrency must be >= 0", file=sys.stderr)
        return 2
    if args.priority < 0:
        print("ERROR: --priority must be >= 0", file=sys.stderr)
        return 2

    files = expand_inputs(args.inputs)
    if not files:
        print("ERROR: no input JSON files found", file=sys.stderr)
        return 2

    accounts: list[dict[str, Any]] = []
    errors: list[str] = []
    for path in files:
        try:
            account = build_account(load_json(path), path, args)
        except ConversionError as exc:
            errors.append(str(exc))
            continue
        accounts.append(account)

    accounts = dedupe_accounts(accounts, args.dedupe)
    if errors:
        for error in errors:
            print(f"ERROR: {error}", file=sys.stderr)
        return 1
    if not accounts:
        print("ERROR: no accounts converted", file=sys.stderr)
        return 1

    payload: Any
    if args.accounts_only:
        payload = accounts
    else:
        payload = {
            "type": "sub2api-data",
            "version": 1,
            "exported_at": now_rfc3339(),
            "proxies": [],
            "accounts": accounts,
        }

    output_path = Path(args.output).expanduser()
    write_output(output_path, payload, args.pretty)
    print(f"Converted {len(accounts)} account(s) from {len(files)} file(s) -> {output_path}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
