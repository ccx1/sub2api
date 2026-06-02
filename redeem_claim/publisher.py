import html
import json
import shutil
from pathlib import Path
from typing import Any, Dict

from common import ROOT
from store import Store


def publish_activity(
    store: Store,
    slug: str,
    output_dir: Path,
    public_base_url: str,
    api_base_url: str,
) -> str:
    activity = store.activity_summary(slug)
    activity_dir = output_dir / slug
    static_dir = output_dir / "static"
    activity_dir.mkdir(parents=True, exist_ok=True)
    static_dir.mkdir(parents=True, exist_ok=True)

    shutil.copy2(ROOT / "static" / "styles.css", static_dir / "styles.css")
    html_text = render_activity_html(activity, api_base_url)
    (activity_dir / "index.html").write_text(html_text, encoding="utf-8")

    base = public_base_url.rstrip("/")
    return "{}/{}/".format(base, slug) if base else str((activity_dir / "index.html").resolve())


def render_activity_html(activity: Dict[str, Any], api_base_url: str) -> str:
    template = (ROOT / "static" / "index.html").read_text(encoding="utf-8")
    config = {
        "activityId": activity["slug"],
        "name": activity["name"],
        "description": activity["description"],
        "useUrl": activity.get("use_url", ""),
        "apiBase": api_base_url.rstrip("/"),
    }
    script = "<script>window.REDEEM_CLAIM_STATIC_CONFIG = "
    script += json.dumps(config, ensure_ascii=False)
    script += ";</script>"

    title = html.escape(activity["name"] or "iCode 兑换码领取")
    description = html.escape(activity["description"] or "输入 iCode 账号邮箱领取活动兑换码。")
    return (
        template.replace('href="/static/styles.css"', 'href="../static/styles.css"')
        .replace("<!-- REDEEM_CLAIM_STATIC_CONFIG -->", script)
        .replace("<title>iCode 兑换码领取</title>", f"<title>{title}</title>")
        .replace('<h1 id="title">iCode 兑换码领取</h1>', f'<h1 id="title">{title}</h1>')
        .replace("输入 iCode 账号邮箱领取活动兑换码。", description, 1)
    )
