from __future__ import annotations

import base64
import hashlib
import hmac
import json
import time
import urllib.parse
import urllib.request
from dataclasses import dataclass
from pathlib import Path

from sub2api_monitor.config import DingTalkConfig


@dataclass(slots=True)
class StateSnapshot:
    daily_reports: dict[str, str]
    total_alerts: dict[str, str]
    key_alerts: dict[str, str]


class MonitorStateStore:
    def __init__(
        self,
        path: Path,
        dedupe_daily_summary: bool = True,
        dedupe_threshold_alert: bool = True,
    ) -> None:
        self.path = path
        self.dedupe_daily_summary = dedupe_daily_summary
        self.dedupe_threshold_alert = dedupe_threshold_alert
        self.snapshot = self._load()

    def should_send(self, report, force_notify: bool) -> bool:
        if force_notify:
            return True
        summary_needed = report.send_daily_summary and (
            not self.dedupe_daily_summary or report.dedupe_key not in self.snapshot.daily_reports
        )
        total_needed = bool(report.total_metrics) and (
            not self.dedupe_threshold_alert or report.dedupe_key not in self.snapshot.total_alerts
        )
        keys_needed = bool(report.key_alerts) and (
            not self.dedupe_threshold_alert
            or any(f"{report.dedupe_key}:{item.key_id}" not in self.snapshot.key_alerts for item in report.key_alerts)
        )
        return summary_needed or total_needed or keys_needed

    def mark_sent(self, report) -> None:
        now = time.strftime("%Y-%m-%dT%H:%M:%S")
        if report.send_daily_summary and self.dedupe_daily_summary:
            self.snapshot.daily_reports[report.dedupe_key] = now
        if report.total_metrics and self.dedupe_threshold_alert:
            self.snapshot.total_alerts[report.dedupe_key] = now
        if self.dedupe_threshold_alert:
            for item in report.key_alerts:
                self.snapshot.key_alerts[f"{report.dedupe_key}:{item.key_id}"] = now
        self._save()

    def _load(self) -> StateSnapshot:
        if not self.path.exists():
            return StateSnapshot({}, {}, {})
        try:
            data = json.loads(self.path.read_text(encoding="utf-8"))
        except json.JSONDecodeError:
            return StateSnapshot({}, {}, {})
        return StateSnapshot(
            daily_reports=dict(data.get("daily_reports", {})),
            total_alerts=dict(data.get("total_alerts", {})),
            key_alerts=dict(data.get("key_alerts", {})),
        )

    def _save(self) -> None:
        self.path.parent.mkdir(parents=True, exist_ok=True)
        payload = {
            "daily_reports": self.snapshot.daily_reports,
            "total_alerts": self.snapshot.total_alerts,
            "key_alerts": self.snapshot.key_alerts,
        }
        self.path.write_text(json.dumps(payload, ensure_ascii=False, indent=2), encoding="utf-8")


class DingTalkNotifier:
    def __init__(self, config: DingTalkConfig, timeout_seconds: int) -> None:
        self.config = config
        self.timeout_seconds = timeout_seconds

    def send_markdown(self, title: str, text: str) -> None:
        webhook = self._signed_webhook()
        payload = {
            "msgtype": "markdown",
            "markdown": {"title": title, "text": text},
            "at": {"atMobiles": self.config.at_mobiles, "isAtAll": self.config.at_all},
        }
        request = urllib.request.Request(
            url=webhook,
            data=json.dumps(payload).encode("utf-8"),
            headers={"Content-Type": "application/json"},
            method="POST",
        )
        with urllib.request.urlopen(request, timeout=self.timeout_seconds) as response:
            raw = response.read().decode("utf-8")
        data = json.loads(raw)
        if int(data.get("errcode", -1)) != 0:
            raise RuntimeError(f"钉钉消息发送失败: {raw}")

    def _signed_webhook(self) -> str:
        if not self.config.secret:
            return self.config.webhook
        timestamp = str(int(time.time() * 1000))
        string_to_sign = f"{timestamp}\n{self.config.secret}"
        digest = hmac.new(
            self.config.secret.encode("utf-8"),
            string_to_sign.encode("utf-8"),
            digestmod=hashlib.sha256,
        ).digest()
        sign = urllib.parse.quote_plus(base64.b64encode(digest))
        separator = "&" if "?" in self.config.webhook else "?"
        return f"{self.config.webhook}{separator}timestamp={timestamp}&sign={sign}"
