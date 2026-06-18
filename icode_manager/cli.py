import argparse
import socketserver
from http.server import HTTPServer
from pathlib import Path
from typing import Any, Dict, Optional

from affiliate_admin import AffiliateBindingService
from balance_history import RechargeCalculator
from common import ROOT, db_path_from_config, load_config, read_codes
from import_settings import ImportSettingsService
from product_listing import ProductListingScheduler, ProductListingService
from publisher import publish_activity
from store import Store
from verifier import UserVerifier
from web import ClaimHandler


class ThreadingHTTPServer(socketserver.ThreadingMixIn, HTTPServer):
    daemon_threads = True


def require_admin(config: Dict[str, Any], token: Optional[str]) -> None:
    expected = config.get("security", {}).get("admin_token", "")
    if expected and token != expected:
        raise SystemExit("admin token 不正确，请用 --admin-token 指定")


def cmd_init(args: argparse.Namespace, config: Dict[str, Any], store: Store) -> None:
    require_admin(config, args.admin_token)
    store.upsert_activity(
        slug=args.slug,
        name=args.name,
        description=args.description or "",
        use_url=args.use_url or "",
        packet_type="ordinary",
        starts_at=args.starts_at,
        ends_at=args.ends_at,
        status=args.status,
    )
    print(f"活动已保存：{args.slug}")


def cmd_import(args: argparse.Namespace, config: Dict[str, Any], store: Store) -> None:
    require_admin(config, args.admin_token)
    codes = read_codes(Path(args.file))
    count = store.import_codes(args.slug, codes)
    print(f"导入完成：新增 {count} 条，输入 {len(codes)} 条")


def cmd_close(args: argparse.Namespace, config: Dict[str, Any], store: Store) -> None:
    require_admin(config, args.admin_token)
    store.close_activity(args.slug)
    print(f"活动已关闭：{args.slug}")


def output_dir_from_config(config: Dict[str, Any]) -> Path:
    publisher = config.get("publisher", {})
    output_dir = Path(publisher.get("output_dir") or ROOT / "public")
    if not output_dir.is_absolute():
        output_dir = ROOT / output_dir
    return output_dir


def cmd_publish(args: argparse.Namespace, config: Dict[str, Any], store: Store) -> None:
    require_admin(config, args.admin_token)
    publisher = config.get("publisher", {})
    link = publish_activity(
        store=store,
        slug=args.slug,
        output_dir=output_dir_from_config(config),
        public_base_url=str(publisher.get("public_base_url") or ""),
        api_base_url=str(publisher.get("api_base_url") or ""),
    )
    print(f"静态页已生成：{link}")


def cmd_serve(args: argparse.Namespace, config: Dict[str, Any], store: Store) -> None:
    ClaimHandler.store = store
    ClaimHandler.config = config
    ClaimHandler.verifier = UserVerifier(config)
    ClaimHandler.recharge_calculator = RechargeCalculator(config, store)
    ClaimHandler.affiliate_service = AffiliateBindingService(config)
    product_listing_service = ProductListingService()
    product_listing_scheduler = ProductListingScheduler(product_listing_service)
    ClaimHandler.product_listing_service = product_listing_service
    ClaimHandler.product_listing_scheduler = product_listing_scheduler
    ClaimHandler.import_settings_service = ImportSettingsService()
    product_listing_scheduler.start()
    host = args.host or config.get("server", {}).get("host", "127.0.0.1")
    port = int(args.port or config.get("server", {}).get("port", 8099))
    server = ThreadingHTTPServer((host, port), ClaimHandler)
    print(f"iCode 兑换码服务已启动：")
    print(f"- 本地管理台：http://{host}:{port}/manager")
    print(f"- 用户页预览：http://{host}:{port}/activity/default")
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        print("\n服务已停止")
    finally:
        product_listing_scheduler.stop()
        server.server_close()


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="sub2api 兑换码活动领取服务")
    parser.add_argument("--config", default=str(ROOT / "config.json"), help="配置文件路径")
    sub = parser.add_subparsers(dest="command")
    add_init_parser(sub)
    add_import_parser(sub)
    add_close_parser(sub)
    add_publish_parser(sub)
    serve = sub.add_parser("serve", help="启动 HTTP 服务")
    serve.add_argument("--host")
    serve.add_argument("--port", type=int)
    return parser


def add_init_parser(sub: argparse._SubParsersAction) -> None:
    init = sub.add_parser("init-activity", help="创建或更新活动")
    init.add_argument("--slug", required=True)
    init.add_argument("--name", required=True)
    init.add_argument("--description", default="")
    init.add_argument("--use-url", default="")
    init.add_argument("--starts-at")
    init.add_argument("--ends-at")
    init.add_argument("--status", choices=["draft", "open", "closed"], default="draft")
    init.add_argument("--admin-token")


def add_import_parser(sub: argparse._SubParsersAction) -> None:
    imp = sub.add_parser("import-codes", help="导入兑换码码池")
    imp.add_argument("--slug", required=True)
    imp.add_argument("--file", required=True)
    imp.add_argument("--admin-token")


def add_close_parser(sub: argparse._SubParsersAction) -> None:
    close = sub.add_parser("close-activity", help="关闭活动")
    close.add_argument("--slug", required=True)
    close.add_argument("--admin-token")


def add_publish_parser(sub: argparse._SubParsersAction) -> None:
    publish = sub.add_parser("publish-activity", help="生成指定活动的静态领取页")
    publish.add_argument("--slug", required=True)
    publish.add_argument("--admin-token")


def main() -> None:
    args = build_parser().parse_args()
    if not args.command:
        raise SystemExit("请指定命令，例如 serve")
    config_path = Path(args.config).resolve()
    config = load_config(config_path)
    store = Store(db_path_from_config(config, config_path))
    commands = {
        "init-activity": cmd_init,
        "import-codes": cmd_import,
        "close-activity": cmd_close,
        "publish-activity": cmd_publish,
        "serve": cmd_serve,
    }
    commands[args.command](args, config, store)
