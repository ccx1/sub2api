import json
from copy import deepcopy
from http import HTTPStatus
from pathlib import Path
from typing import Any, Dict

from common import AppError, ROOT


IMPORT_SETTINGS_PATH = ROOT / "codex_import_settings.json"

DEFAULT_IMPORT_SETTINGS: Dict[str, Any] = {
    "name_prefix": "",
    "notes": "",
    "concurrency": 3,
    "priority": 50,
    "dedupe": "email",
    "output_mode": "bundle",
}


class ImportSettingsService:
    def status(self) -> Dict[str, Any]:
        return {
            "settings": self.load(),
            "path": str(IMPORT_SETTINGS_PATH),
        }

    def load(self) -> Dict[str, Any]:
        if not IMPORT_SETTINGS_PATH.exists():
            return deepcopy(DEFAULT_IMPORT_SETTINGS)
        try:
            data = json.loads(IMPORT_SETTINGS_PATH.read_text(encoding="utf-8-sig"))
        except json.JSONDecodeError as exc:
            raise AppError(HTTPStatus.BAD_REQUEST, f"导入设置 JSON 不正确：{exc}")
        if not isinstance(data, dict):
            raise AppError(HTTPStatus.BAD_REQUEST, "导入设置必须是 JSON 对象")
        settings = deepcopy(DEFAULT_IMPORT_SETTINGS)
        settings.update({key: data.get(key) for key in settings if key in data})
        return self.normalize(settings)

    def save(self, payload: Dict[str, Any]) -> Dict[str, Any]:
        settings = self.normalize(payload)
        IMPORT_SETTINGS_PATH.write_text(json.dumps(settings, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
        return settings

    def normalize(self, payload: Dict[str, Any]) -> Dict[str, Any]:
        dedupe = str(payload.get("dedupe") or DEFAULT_IMPORT_SETTINGS["dedupe"]).strip()
        output_mode = str(payload.get("output_mode") or payload.get("outputMode") or DEFAULT_IMPORT_SETTINGS["output_mode"]).strip()
        if dedupe not in {"email", "file", "none"}:
            raise AppError(HTTPStatus.BAD_REQUEST, "去重方式不正确")
        if output_mode not in {"bundle", "accounts"}:
            raise AppError(HTTPStatus.BAD_REQUEST, "输出格式不正确")
        return {
            "name_prefix": str(payload.get("name_prefix") or payload.get("namePrefix") or "").strip(),
            "notes": str(payload.get("notes") or ""),
            "concurrency": self.non_negative_int(payload.get("concurrency"), "并发数"),
            "priority": self.non_negative_int(payload.get("priority"), "优先级"),
            "dedupe": dedupe,
            "output_mode": output_mode,
        }

    def non_negative_int(self, value: Any, label: str) -> int:
        if value in ("", None):
            value = 0
        try:
            parsed = int(value)
        except (TypeError, ValueError):
            raise AppError(HTTPStatus.BAD_REQUEST, f"{label}必须是不小于 0 的整数")
        if parsed < 0:
            raise AppError(HTTPStatus.BAD_REQUEST, f"{label}必须是不小于 0 的整数")
        return parsed
