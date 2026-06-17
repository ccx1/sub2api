from __future__ import annotations

from dataclasses import dataclass, field
from datetime import date, datetime, timedelta
from pathlib import Path
from zoneinfo import ZoneInfo

from sub2api_monitor.client import Sub2ApiClient, Sub2ApiError
from sub2api_monitor.config import AppConfig, AuthConfig


@dataclass(slots=True)
class UsageRow:
    key_id: int
    key_name: str
    status: str
    total_requests: int
    total_tokens: int
    total_actual_cost: float
    input_tokens: int
    output_tokens: int
    cache_tokens: int


@dataclass(slots=True)
class DailyAggregate:
    date_str: str
    total_requests: int = 0
    total_tokens: int = 0
    total_actual_cost: float = 0.0


@dataclass(slots=True)
class KeyAlert:
    key_id: int
    key_name: str
    metrics: list[str] = field(default_factory=list)


@dataclass(slots=True)
class MonitorReport:
    target_date: str
    window_dates: list[str]
    matched_key_count: int
    rows: list[UsageRow]
    daily_aggregates: list[DailyAggregate]
    total_metrics: list[str]
    key_alerts: list[KeyAlert]
    send_daily_summary: bool
    dedupe_key: str
    summary_lines: list[str] = field(default_factory=list)
    model_stats: list[dict] = field(default_factory=list)


@dataclass(slots=True)
class NotificationPayload:
    title: str
    body: str


@dataclass(slots=True)
class BatchUsageRow:
    name: str
    api_key: str
    status: str
    actual_cost: float
    requests: int
    total_tokens: int
    error: str = ""


@dataclass(slots=True)
class BatchReport:
    target_date: str
    rows: list[BatchUsageRow]
    success_count: int
    failed_count: int
    total_actual_cost: float
    dedupe_key: str
    send_daily_summary: bool = True
    total_metrics: list[str] = field(default_factory=list)
    key_alerts: list[KeyAlert] = field(default_factory=list)


def build_usage_report(
    client: Sub2ApiClient,
    config: AppConfig,
    override_date: str | None,
    override_history_days: int | None,
) -> MonitorReport:
    if config.auth.mode == "api_key":
        return build_api_key_mode_report(client, config, override_date)

    keys = client.list_api_keys()
    target_date = resolve_target_date(config, override_date)
    history_days = max(1, override_history_days or config.report.history_days)
    window_dates = build_window_dates(target_date, history_days)
    daily_map = {value: DailyAggregate(date_str=value) for value in window_dates}

    rows: list[UsageRow] = []
    for current_date in window_dates:
        current_rows = collect_rows(client, keys, current_date)
        if current_date == target_date:
            rows = current_rows
        for row in current_rows:
            aggregate = daily_map[current_date]
            aggregate.total_requests += row.total_requests
            aggregate.total_tokens += row.total_tokens
            aggregate.total_actual_cost += row.total_actual_cost

    rows.sort(key=lambda row: row.total_actual_cost, reverse=True)
    total_metrics, key_alerts = evaluate_thresholds(config, rows)
    total_today = daily_map[target_date]
    send_summary = not (config.report.quiet_if_no_usage and total_today.total_requests == 0)

    return MonitorReport(
        target_date=target_date,
        window_dates=window_dates,
        matched_key_count=len(keys),
        rows=rows,
        daily_aggregates=[daily_map[item] for item in window_dates],
        total_metrics=total_metrics,
        key_alerts=key_alerts,
        send_daily_summary=send_summary,
        dedupe_key=target_date,
    )


def build_api_key_mode_report(
    client: Sub2ApiClient,
    config: AppConfig,
    override_date: str | None,
) -> MonitorReport:
    target_date = resolve_target_date(config, override_date)
    today = datetime.now(ZoneInfo(config.sub2api.timezone)).date().isoformat()
    if target_date != today:
        raise ValueError("api_key 模式只能查询当天汇总；历史日报需要 token 模式")

    payload = client.get_public_api_key_usage(target_date, target_date)
    usage_today = payload.get("usage", {}).get("today", {})
    quota = payload.get("quota", {}) if isinstance(payload.get("quota"), dict) else {}
    alias = config.auth.api_key_alias or "current_api_key"

    row = UsageRow(
        key_id=0,
        key_name=alias,
        status=str(payload.get("status", "active")),
        total_requests=int(usage_today.get("requests", 0)),
        total_tokens=int(usage_today.get("total_tokens", 0)),
        total_actual_cost=float(usage_today.get("actual_cost", 0.0)),
        input_tokens=int(usage_today.get("input_tokens", 0)),
        output_tokens=int(usage_today.get("output_tokens", 0)),
        cache_tokens=int(usage_today.get("cache_read_tokens", 0)) + int(usage_today.get("cache_creation_tokens", 0)),
    )
    total_metrics, key_alerts = evaluate_thresholds(config, [row])

    summary_lines = [
        f"模式: {payload.get('mode', '-')}",
        f"状态: {payload.get('status', '-')}",
    ]
    if quota:
        summary_lines.append(
            f"额度: ${float(quota.get('used', 0.0)):.4f} / ${float(quota.get('limit', 0.0)):.4f}"
        )
        summary_lines.append(f"剩余额度: ${float(quota.get('remaining', 0.0)):.4f}")

    return MonitorReport(
        target_date=target_date,
        window_dates=[target_date],
        matched_key_count=1,
        rows=[row],
        daily_aggregates=[
            DailyAggregate(
                date_str=target_date,
                total_requests=row.total_requests,
                total_tokens=row.total_tokens,
                total_actual_cost=row.total_actual_cost,
            )
        ],
        total_metrics=total_metrics,
        key_alerts=key_alerts,
        send_daily_summary=not (config.report.quiet_if_no_usage and row.total_requests == 0),
        dedupe_key=target_date,
        summary_lines=summary_lines,
        model_stats=list(payload.get("model_stats", [])) if isinstance(payload.get("model_stats"), list) else [],
    )


def build_batch_report_from_file(txt_path: Path, config: AppConfig, override_date: str | None) -> BatchReport:
    target_date = resolve_target_date(config, override_date)
    today = datetime.now(ZoneInfo(config.sub2api.timezone)).date().isoformat()
    if target_date != today:
        raise ValueError("批量 api_key 表格模式只能查询当天汇总")

    lines = txt_path.read_text(encoding="utf-8").splitlines()
    rows: list[BatchUsageRow] = []
    total_actual_cost = 0.0
    success_count = 0
    failed_count = 0

    for raw in lines:
        line = raw.strip()
        if not line:
            continue
        parts = [item.strip() for item in line.split("\t") if item.strip()]
        if len(parts) < 2:
            parts = [item.strip() for item in line.split() if item.strip()]
        if len(parts) < 2:
            rows.append(BatchUsageRow(name=line, api_key="", status="parse_error", actual_cost=0.0, requests=0, total_tokens=0, error="格式错误"))
            failed_count += 1
            continue

        name, api_key = parts[0], parts[1]
        auth = AuthConfig(mode="api_key", api_key=api_key, api_key_alias=name, session_path=config.auth.session_path)
        client = Sub2ApiClient(config.sub2api, auth, tokens=_empty_tokens())
        try:
            payload = client.get_public_api_key_usage(target_date, target_date)
            today_usage = payload.get("usage", {}).get("today", {})
            actual_cost = float(today_usage.get("actual_cost", 0.0))
            requests = int(today_usage.get("requests", 0))
            total_tokens = int(today_usage.get("total_tokens", 0))
            status = str(payload.get("status", "active"))
            rows.append(
                BatchUsageRow(
                    name=name,
                    api_key=api_key,
                    status=status,
                    actual_cost=actual_cost,
                    requests=requests,
                    total_tokens=total_tokens,
                )
            )
            total_actual_cost += actual_cost
            success_count += 1
        except Sub2ApiError as exc:
            rows.append(
                BatchUsageRow(
                    name=name,
                    api_key=api_key,
                    status="error",
                    actual_cost=0.0,
                    requests=0,
                    total_tokens=0,
                    error=str(exc),
                )
            )
            failed_count += 1

    rows.sort(key=lambda row: row.actual_cost, reverse=True)
    return BatchReport(
        target_date=target_date,
        rows=rows,
        success_count=success_count,
        failed_count=failed_count,
        total_actual_cost=total_actual_cost,
        dedupe_key=f"batch:{target_date}:{txt_path.resolve()}",
    )


def _empty_tokens():
    from sub2api_monitor.config import SessionTokens

    return SessionTokens()


def collect_rows(client: Sub2ApiClient, keys: list[dict], date_str: str) -> list[UsageRow]:
    result: list[UsageRow] = []
    for key in keys:
        stats = client.get_usage_stats(int(key["id"]), date_str)
        result.append(
            UsageRow(
                key_id=int(key["id"]),
                key_name=str(key.get("name", "")),
                status=str(key.get("status", "")),
                total_requests=int(stats.get("total_requests", 0)),
                total_tokens=int(stats.get("total_tokens", 0)),
                total_actual_cost=float(stats.get("total_actual_cost", 0.0)),
                input_tokens=int(stats.get("total_input_tokens", 0)),
                output_tokens=int(stats.get("total_output_tokens", 0)),
                cache_tokens=int(stats.get("total_cache_tokens", 0)),
            )
        )
    return result


def resolve_target_date(config: AppConfig, override_date: str | None) -> str:
    if override_date:
        return override_date
    now = datetime.now(ZoneInfo(config.sub2api.timezone)).date()
    return (now + timedelta(days=config.report.day_offset)).isoformat()


def build_window_dates(target_date: str, history_days: int) -> list[str]:
    last = date.fromisoformat(target_date)
    first = last - timedelta(days=history_days - 1)
    return [(first + timedelta(days=index)).isoformat() for index in range(history_days)]


def evaluate_thresholds(config: AppConfig, rows: list[UsageRow]) -> tuple[list[str], list[KeyAlert]]:
    totals = summarize_rows(rows)
    total_metrics = build_metric_alerts(
        actual_cost=totals.total_actual_cost,
        requests=totals.total_requests,
        tokens=totals.total_tokens,
        cost_limit=config.thresholds.total_actual_cost,
        request_limit=config.thresholds.total_requests,
        token_limit=config.thresholds.total_tokens,
    )
    key_alerts: list[KeyAlert] = []
    for row in rows:
        metrics = build_metric_alerts(
            actual_cost=row.total_actual_cost,
            requests=row.total_requests,
            tokens=row.total_tokens,
            cost_limit=config.thresholds.per_key_actual_cost,
            request_limit=config.thresholds.per_key_requests,
            token_limit=config.thresholds.per_key_total_tokens,
        )
        if metrics:
            key_alerts.append(KeyAlert(key_id=row.key_id, key_name=row.key_name, metrics=metrics))
    return total_metrics, key_alerts


def summarize_rows(rows: list[UsageRow]) -> DailyAggregate:
    summary = DailyAggregate(date_str="")
    for row in rows:
        summary.total_requests += row.total_requests
        summary.total_tokens += row.total_tokens
        summary.total_actual_cost += row.total_actual_cost
    return summary


def build_metric_alerts(
    actual_cost: float,
    requests: int,
    tokens: int,
    cost_limit: float | None,
    request_limit: int | None,
    token_limit: int | None,
) -> list[str]:
    metrics: list[str] = []
    if cost_limit is not None and actual_cost >= cost_limit:
        metrics.append(f"实际扣费 ${actual_cost:.4f} >= ${cost_limit:.4f}")
    if request_limit is not None and requests >= request_limit:
        metrics.append(f"请求数 {requests} >= {request_limit}")
    if token_limit is not None and tokens >= token_limit:
        metrics.append(f"总 Token {tokens} >= {token_limit}")
    return metrics


def format_console_report(report: MonitorReport) -> str:
    lines = [f"Sub2API API Key 使用监控 - {report.target_date}"]
    totals = summarize_rows(report.rows)
    lines.append(f"总请求: {totals.total_requests} | 总 Token: {totals.total_tokens} | 实际扣费: ${totals.total_actual_cost:.4f}")
    for row in report.rows:
        lines.append(f"- {row.key_name}: ${row.total_actual_cost:.4f}")
    return "\n".join(lines)


def format_notification(report: MonitorReport, keyword: str = "", max_keys_in_message: int = 20) -> NotificationPayload:
    totals = summarize_rows(report.rows)
    title = f"Sub2API API Key 日报 {report.target_date}"
    lines = [f"### {title}"]
    if keyword.strip():
        lines.insert(0, keyword.strip())
    lines.append(f"- 总请求: {totals.total_requests}")
    lines.append(f"- 总 Token: {totals.total_tokens}")
    lines.append(f"- 实际扣费: ${totals.total_actual_cost:.4f}")
    lines.append("")
    lines.append("#### 明细")
    for row in report.rows[:max_keys_in_message]:
        lines.append(f"- `{row.key_name}` | ${row.total_actual_cost:.4f}")
    return NotificationPayload(title=title, body="\n".join(lines))


def format_key_listing(keys: list[dict]) -> str:
    lines = ["可见 API Key 列表:"]
    for item in keys:
        lines.append(f"- #{item.get('id')} | {item.get('name')} | status={item.get('status')}")
    return "\n".join(lines)


def format_batch_console_report(report: BatchReport) -> str:
    lines = [f"Sub2API 今日额度表 - {report.target_date}"]
    lines.append(f"成功 {report.success_count} | 失败 {report.failed_count} | 总额度 ${report.total_actual_cost:.4f}")
    lines.append("")
    lines.append("姓名\t今日额度(USD)\t状态")
    for row in report.rows:
        amount = f"{row.actual_cost:.4f}" if not row.error else "-"
        status = row.status if not row.error else f"error: {row.error}"
        lines.append(f"{row.name}\t{amount}\t{status}")
    return "\n".join(lines)


def format_batch_notification(report: BatchReport, keyword: str = "") -> NotificationPayload:
    title = f"Sub2API 今日额度表 {report.target_date}"
    lines = [f"### {title}"]
    if keyword.strip():
        lines.insert(0, keyword.strip())
    lines.append(f"- 成功: {report.success_count}")
    lines.append(f"- 失败: {report.failed_count}")
    lines.append(f"- 总额度: ${report.total_actual_cost:.4f}")
    lines.append("")
    lines.append("| 姓名 | 今日额度(USD) | 状态 |")
    lines.append("| --- | ---: | --- |")
    for row in report.rows:
        amount = f"{row.actual_cost:.4f}" if not row.error else "-"
        status = row.status if not row.error else f"error: {row.error}"
        lines.append(f"| {row.name} | {amount} | {status} |")
    return NotificationPayload(title=title, body="\n".join(lines))
