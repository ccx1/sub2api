import secrets
from dataclasses import dataclass
from http import HTTPStatus
from pathlib import Path
from typing import Any, Dict, List, Optional

from common import AppError


AFFILIATE_CODE_LENGTH = 12
AFFILIATE_CODE_CHARSET = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"


@dataclass
class DatabaseConfig:
    host: str = "127.0.0.1"
    port: int = 5432
    dbname: str = "sub2api"
    user: str = "postgres"
    password: str = ""
    sslmode: str = "prefer"
    connect_timeout: int = 10
    source: str = "default"


def load_database_config(config: Dict[str, Any]) -> DatabaseConfig:
    source = "redeem_claim.sub2api_database"
    data = dict(config.get("sub2api_database") or {})
    if not data:
        source = "redeem_claim.sub2api.database"
        data = dict((config.get("sub2api") or {}).get("database") or {})
    if not data:
        source = "backend.config.yaml"
        data = load_backend_database_config()
    if not data:
        source = "default"
        data = {}
    return DatabaseConfig(
        host=str(data.get("host") or "127.0.0.1").strip(),
        port=to_int(data.get("port"), 5432),
        dbname=str(data.get("dbname") or data.get("name") or "sub2api").strip(),
        user=str(data.get("user") or "postgres").strip(),
        password=str(data.get("password") or ""),
        sslmode=str(data.get("sslmode") or "prefer").strip(),
        connect_timeout=to_int(data.get("connect_timeout"), 10),
        source=source,
    )


def load_backend_database_config() -> Dict[str, Any]:
    path = Path(__file__).resolve().parents[1] / "backend" / "config.yaml"
    if not path.exists():
        return {}
    data: Dict[str, Any] = {}
    in_database = False
    for raw_line in path.read_text(encoding="utf-8-sig", errors="ignore").splitlines():
        line = raw_line.split("#", 1)[0].rstrip()
        if not line.strip():
            continue
        if not raw_line.startswith(" ") and line.strip() == "database:":
            in_database = True
            continue
        if in_database and raw_line and not raw_line.startswith(" "):
            break
        if not in_database or ":" not in line:
            continue
        key, value = line.strip().split(":", 1)
        data[key.strip()] = parse_yaml_scalar(value.strip())
    return data


def parse_yaml_scalar(value: str) -> Any:
    value = value.strip()
    if len(value) >= 2 and value[0] == value[-1] and value[0] in {"'", '"'}:
        value = value[1:-1]
    if value.isdigit():
        return int(value)
    return value


def connect_db(config: DatabaseConfig):
    try:
        import psycopg
        from psycopg.rows import dict_row
    except ImportError as exc:
        raise AppError(
            HTTPStatus.INTERNAL_SERVER_ERROR,
            "缺少 PostgreSQL 驱动，请安装 psycopg[binary]",
        ) from exc
    return psycopg.connect(
        host=config.host,
        port=config.port,
        dbname=config.dbname,
        user=config.user,
        password=config.password,
        sslmode=config.sslmode,
        connect_timeout=config.connect_timeout,
        row_factory=dict_row,
    )


class AffiliateBindingService:
    def __init__(self, app_config: Dict[str, Any]):
        self.db_config = load_database_config(app_config)

    def preview(self, inviter_email: str, invitee_email: str, allow_rebind: bool) -> Dict[str, Any]:
        result = {
            "ok": False,
            "can_execute": False,
            "status": "blocked",
            "messages": [],
            "plan": [],
            "inviter": None,
            "invitee": None,
            "invitees": [],
        }
        inviter_email = inviter_email.strip()
        invitee_email = invitee_email.strip()
        if not inviter_email or not invitee_email:
            result["messages"].append("邀请人邮箱和被邀请人邮箱都不能为空")
            return result
        if inviter_email.lower() == invitee_email.lower():
            result["messages"].append("邀请人和被邀请人不能是同一个邮箱")
            return result

        with connect_db(self.db_config) as conn:
            inviter = query_one_account(conn, inviter_email)
            invitee = query_one_account(conn, invitee_email)
            result["inviter"] = inviter
            result["invitee"] = invitee

            validation = validate_accounts(inviter, invitee)
            if validation:
                result["messages"].append(validation)
                return result
            result["invitees"] = query_invitees(conn, inviter["id"])

        if not inviter.get("aff_user_id"):
            result["plan"].append("执行时会先为邀请人补齐 user_affiliates 记录")
        if not invitee.get("aff_user_id"):
            result["plan"].append("执行时会先为被邀请人补齐 user_affiliates 记录")

        current_inviter_id = invitee.get("inviter_id")
        if current_inviter_id == inviter["id"]:
            result["ok"] = True
            result["status"] = "noop"
            result["messages"].append("当前已经是这个邀请关系，无需重复执行")
            return result

        if current_inviter_id is not None and not allow_rebind:
            result["messages"].append(
                "被邀请人当前已绑定到 %s，如需改绑请勾选允许改绑"
                % (invitee.get("inviter_email") or current_inviter_id)
            )
            return result

        if current_inviter_id is not None:
            result["plan"].append("执行时会把被邀请人从旧邀请人改绑到新邀请人")
            result["plan"].append("旧邀请人的 aff_count 减 1，新邀请人的 aff_count 加 1")
        else:
            result["plan"].append("执行时会把被邀请人的 inviter_id 设置为邀请人用户 ID")
            result["plan"].append("邀请人的 aff_count 加 1")

        result["ok"] = True
        result["can_execute"] = True
        result["status"] = "ready"
        return result

    def execute(self, inviter_email: str, invitee_email: str, allow_rebind: bool) -> Dict[str, Any]:
        preview = self.preview(inviter_email, invitee_email, allow_rebind)
        if not preview.get("can_execute") and preview.get("status") != "noop":
            raise AppError(HTTPStatus.CONFLICT, "当前邀请关系不能执行，请先检查预览结果")
        return execute_binding(self.db_config, inviter_email, invitee_email, allow_rebind)


def query_one_account(conn, email: str) -> Dict[str, Any]:
    rows = conn.execute(
        """
        SELECT u.id,
               u.email,
               COALESCE(u.username, '') AS username,
               u.status,
               u.deleted_at,
               u.created_at,
               ua.user_id AS aff_user_id,
               ua.aff_code,
               ua.inviter_id,
               inviter.email AS inviter_email,
               ua.aff_count,
               ua.aff_quota,
               ua.aff_history_quota,
               ua.aff_frozen_quota,
               ua.created_at AS affiliate_created_at,
               ua.updated_at AS affiliate_updated_at
        FROM users u
        LEFT JOIN user_affiliates ua ON ua.user_id = u.id
        LEFT JOIN users inviter ON inviter.id = ua.inviter_id
        WHERE lower(u.email) = lower(%s)
        ORDER BY u.id
        """,
        (email,),
    ).fetchall()
    if len(rows) != 1:
        return {"match_count": len(rows), "email": email}
    row = dict(rows[0])
    row["match_count"] = 1
    return row


def query_invitees(conn, inviter_id: int, limit: int = 20) -> List[Dict[str, Any]]:
    return [
        dict(row)
        for row in conn.execute(
            """
            SELECT invitee.id AS invitee_id,
                   invitee.email AS invitee_email,
                   COALESCE(invitee.username, '') AS invitee_username,
                   ua.created_at AS relation_created_at,
                   ua.updated_at AS relation_updated_at
            FROM user_affiliates ua
            JOIN users invitee ON invitee.id = ua.user_id
            WHERE ua.inviter_id = %s
            ORDER BY ua.updated_at DESC
            LIMIT %s
            """,
            (inviter_id, limit),
        ).fetchall()
    ]


def validate_accounts(inviter: Dict[str, Any], invitee: Dict[str, Any]) -> str:
    if inviter.get("match_count") != 1:
        return "邀请人邮箱匹配到 %s 个用户，必须刚好 1 个" % inviter.get("match_count", 0)
    if invitee.get("match_count") != 1:
        return "被邀请人邮箱匹配到 %s 个用户，必须刚好 1 个" % invitee.get("match_count", 0)
    if inviter.get("deleted_at"):
        return "邀请人账号已删除，已阻止执行"
    if invitee.get("deleted_at"):
        return "被邀请人账号已删除，已阻止执行"
    if inviter["id"] == invitee["id"]:
        return "邀请人和被邀请人是同一个用户 ID，已阻止执行"
    return ""


def generate_aff_code(conn) -> str:
    for _ in range(32):
        code = "".join(secrets.choice(AFFILIATE_CODE_CHARSET) for _ in range(AFFILIATE_CODE_LENGTH))
        exists = conn.execute("SELECT 1 FROM user_affiliates WHERE aff_code = %s LIMIT 1", (code,)).fetchone()
        if not exists:
            return code
    raise RuntimeError("连续生成的邀请码都已存在，请重试")


def ensure_affiliate_profile(conn, user_id: int) -> bool:
    exists = conn.execute("SELECT 1 FROM user_affiliates WHERE user_id = %s", (user_id,)).fetchone()
    if exists:
        return False

    for _ in range(32):
        code = generate_aff_code(conn)
        cur = conn.execute(
            """
            INSERT INTO user_affiliates (user_id, aff_code)
            VALUES (%s, %s)
            ON CONFLICT DO NOTHING
            """,
            (user_id, code),
        )
        if cur.rowcount == 1:
            return True
        exists = conn.execute("SELECT 1 FROM user_affiliates WHERE user_id = %s", (user_id,)).fetchone()
        if exists:
            return False
    raise RuntimeError("补齐 user_affiliates 记录失败：邀请码唯一性冲突过多")


def lock_account(conn, email: str) -> Dict[str, Any]:
    rows = conn.execute(
        """
        SELECT id, email, deleted_at
        FROM users
        WHERE lower(email) = lower(%s)
        ORDER BY id
        FOR UPDATE
        """,
        (email,),
    ).fetchall()
    if len(rows) != 1:
        raise RuntimeError("%s 匹配到 %s 个用户，必须刚好 1 个" % (email, len(rows)))
    row = dict(rows[0])
    if row.get("deleted_at"):
        raise RuntimeError("%s 是已删除用户，不能绑定" % email)
    return row


def execute_binding(config: DatabaseConfig, inviter_email: str, invitee_email: str, allow_rebind: bool) -> Dict[str, Any]:
    if inviter_email.strip().lower() == invitee_email.strip().lower():
        raise AppError(HTTPStatus.BAD_REQUEST, "邀请人和被邀请人不能是同一个邮箱")

    result: Dict[str, Any] = {
        "created_profiles": [],
        "old_inviter_id": None,
        "old_inviter_email": None,
        "bind_command": None,
        "increment_command": None,
        "decrement_command": None,
        "noop": False,
    }
    conn = connect_db(config)
    try:
        with conn.transaction():
            inviter = lock_account(conn, inviter_email)
            invitee = lock_account(conn, invitee_email)
            if inviter["id"] == invitee["id"]:
                raise RuntimeError("邀请人和被邀请人是同一个用户 ID")

            if ensure_affiliate_profile(conn, inviter["id"]):
                result["created_profiles"].append({"user_id": inviter["id"], "email": inviter["email"]})
            if ensure_affiliate_profile(conn, invitee["id"]):
                result["created_profiles"].append({"user_id": invitee["id"], "email": invitee["email"]})

            locked = conn.execute(
                """
                SELECT ua.user_id,
                       ua.inviter_id,
                       inviter.email AS inviter_email
                FROM user_affiliates ua
                LEFT JOIN users inviter ON inviter.id = ua.inviter_id
                WHERE ua.user_id IN (%s, %s)
                ORDER BY ua.user_id
                FOR UPDATE OF ua
                """,
                (inviter["id"], invitee["id"]),
            ).fetchall()
            by_user_id = {row["user_id"]: dict(row) for row in locked}
            invitee_aff = by_user_id.get(invitee["id"])
            if not invitee_aff:
                raise RuntimeError("被邀请人的 user_affiliates 记录不存在")

            current_inviter_id = invitee_aff.get("inviter_id")
            result["old_inviter_id"] = current_inviter_id
            result["old_inviter_email"] = invitee_aff.get("inviter_email")

            if current_inviter_id == inviter["id"]:
                result["noop"] = True
                return result
            if current_inviter_id is not None and not allow_rebind:
                raise RuntimeError("被邀请人已绑定到 %s，未勾选允许改绑" % (invitee_aff.get("inviter_email") or current_inviter_id))

            if current_inviter_id is not None:
                dec = conn.execute(
                    """
                    UPDATE user_affiliates
                    SET aff_count = GREATEST(aff_count - 1, 0),
                        updated_at = NOW()
                    WHERE user_id = %s
                    """,
                    (current_inviter_id,),
                )
                result["decrement_command"] = "UPDATE %s" % dec.rowcount

            bind = conn.execute(
                """
                UPDATE user_affiliates
                SET inviter_id = %s,
                    updated_at = NOW()
                WHERE user_id = %s
                """,
                (inviter["id"], invitee["id"]),
            )
            if bind.rowcount != 1:
                raise RuntimeError("绑定更新影响了 %s 行，已回滚" % bind.rowcount)
            result["bind_command"] = "UPDATE %s" % bind.rowcount

            inc = conn.execute(
                """
                UPDATE user_affiliates
                SET aff_count = aff_count + 1,
                    updated_at = NOW()
                WHERE user_id = %s
                """,
                (inviter["id"],),
            )
            if inc.rowcount != 1:
                raise RuntimeError("邀请人数更新影响了 %s 行，已回滚" % inc.rowcount)
            result["increment_command"] = "UPDATE %s" % inc.rowcount

        after = conn.execute(
            """
            SELECT invitee.id AS invitee_id,
                   invitee.email AS invitee_email,
                   ua.inviter_id,
                   inviter.email AS inviter_email,
                   inviter_aff.aff_count AS inviter_aff_count,
                   ua.updated_at AS relation_updated_at
            FROM users invitee
            JOIN user_affiliates ua ON ua.user_id = invitee.id
            LEFT JOIN users inviter ON inviter.id = ua.inviter_id
            LEFT JOIN user_affiliates inviter_aff ON inviter_aff.user_id = ua.inviter_id
            WHERE lower(invitee.email) = lower(%s)
            ORDER BY invitee.id
            LIMIT 1
            """,
            (invitee_email,),
        ).fetchone()
        result["after"] = dict(after) if after else None
        return result
    finally:
        conn.close()


def to_int(value: Any, default: int) -> int:
    try:
        return int(str(value).strip())
    except (TypeError, ValueError):
        return default
