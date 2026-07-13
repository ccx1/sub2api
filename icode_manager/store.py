import contextlib
import datetime as dt
import json
import sqlite3
from http import HTTPStatus
from pathlib import Path
from typing import Any, Dict, List, Optional, Tuple

from common import AppError, hash_email, parse_time, utc_now


DEFAULT_MESSAGE_TEMPLATE = "欢迎您，尊贵的{vip等级}用户，下面是您本次的兑换码。"
ANY_TIER = object()
THRESHOLD_BASIS_TOTAL_RECHARGE = "total_recharge"
THRESHOLD_BASIS_RECENT_RECHARGE = "recent_recharge"
THRESHOLD_BASIS_CURRENT_BALANCE = "current_balance"
THRESHOLD_BASIS_VALUES = {
    THRESHOLD_BASIS_TOTAL_RECHARGE,
    THRESHOLD_BASIS_RECENT_RECHARGE,
    THRESHOLD_BASIS_CURRENT_BALANCE,
}


class ClosingConnection(sqlite3.Connection):
    def __exit__(self, exc_type: Any, exc_value: Any, traceback: Any) -> bool:
        result = super().__exit__(exc_type, exc_value, traceback)
        self.close()
        return result


class Store:
    def __init__(self, db_path: Path):
        self.db_path = db_path
        self.db_path.parent.mkdir(parents=True, exist_ok=True)
        self.init_db()

    def connect(self) -> sqlite3.Connection:
        conn = sqlite3.connect(
            str(self.db_path),
            timeout=15,
            isolation_level=None,
            factory=ClosingConnection,
        )
        conn.row_factory = sqlite3.Row
        conn.execute("PRAGMA foreign_keys = ON")
        conn.execute("PRAGMA journal_mode = WAL")
        conn.execute("PRAGMA busy_timeout = 8000")
        return conn

    def init_db(self) -> None:
        with self.connect() as conn:
            conn.executescript(
                """
                CREATE TABLE IF NOT EXISTS activities (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    slug TEXT NOT NULL UNIQUE,
                    name TEXT NOT NULL,
                    description TEXT NOT NULL DEFAULT '',
                    use_url TEXT NOT NULL DEFAULT '',
                    packet_type TEXT NOT NULL DEFAULT 'ordinary',
                    message_template TEXT NOT NULL DEFAULT '',
                    starts_at TEXT,
                    ends_at TEXT,
                    status TEXT NOT NULL DEFAULT 'draft',
                    created_at TEXT NOT NULL,
                    updated_at TEXT NOT NULL
                );

                CREATE TABLE IF NOT EXISTS activity_tiers (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    activity_id INTEGER NOT NULL REFERENCES activities(id) ON DELETE CASCADE,
                    tier_key TEXT NOT NULL,
                    name TEXT NOT NULL,
                    threshold_amount REAL NOT NULL DEFAULT 0,
                    threshold_basis TEXT NOT NULL DEFAULT 'total_recharge',
                    usage_days INTEGER NOT NULL DEFAULT 0,
                    animations TEXT NOT NULL DEFAULT '[]',
                    sort_order INTEGER NOT NULL DEFAULT 0,
                    is_active INTEGER NOT NULL DEFAULT 1,
                    created_at TEXT NOT NULL,
                    updated_at TEXT NOT NULL,
                    UNIQUE(activity_id, tier_key)
                );

                CREATE TABLE IF NOT EXISTS codes (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    activity_id INTEGER NOT NULL REFERENCES activities(id) ON DELETE CASCADE,
                    tier_id INTEGER,
                    code TEXT NOT NULL,
                    status TEXT NOT NULL DEFAULT 'available',
                    claimed_by_email TEXT,
                    claimed_at TEXT,
                    created_at TEXT NOT NULL,
                    UNIQUE(activity_id, code)
                );

                CREATE TABLE IF NOT EXISTS claims (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    activity_id INTEGER NOT NULL REFERENCES activities(id) ON DELETE CASCADE,
                    code_id INTEGER NOT NULL UNIQUE REFERENCES codes(id) ON DELETE RESTRICT,
                    tier_id INTEGER,
                    email TEXT NOT NULL,
                    email_hash TEXT NOT NULL,
                    claimed_at TEXT NOT NULL,
                    user_exists_checked_at TEXT NOT NULL,
                    UNIQUE(activity_id, email_hash)
                );

                CREATE INDEX IF NOT EXISTS idx_codes_activity_status ON codes(activity_id, status, id);
                CREATE INDEX IF NOT EXISTS idx_claims_activity_email ON claims(activity_id, email_hash);
                """
            )
            columns = {row["name"] for row in conn.execute("PRAGMA table_info(activities)").fetchall()}
            if "use_url" not in columns:
                conn.execute("ALTER TABLE activities ADD COLUMN use_url TEXT NOT NULL DEFAULT ''")
            if "packet_type" not in columns:
                conn.execute("ALTER TABLE activities ADD COLUMN packet_type TEXT NOT NULL DEFAULT 'ordinary'")
            if "message_template" not in columns:
                conn.execute("ALTER TABLE activities ADD COLUMN message_template TEXT NOT NULL DEFAULT ''")
            code_columns = {row["name"] for row in conn.execute("PRAGMA table_info(codes)").fetchall()}
            if "tier_id" not in code_columns:
                conn.execute("ALTER TABLE codes ADD COLUMN tier_id INTEGER")
            claim_columns = {row["name"] for row in conn.execute("PRAGMA table_info(claims)").fetchall()}
            if "tier_id" not in claim_columns:
                conn.execute("ALTER TABLE claims ADD COLUMN tier_id INTEGER")
            tier_columns = {row["name"] for row in conn.execute("PRAGMA table_info(activity_tiers)").fetchall()}
            if "threshold_basis" not in tier_columns:
                conn.execute(
                    "ALTER TABLE activity_tiers ADD COLUMN threshold_basis TEXT NOT NULL DEFAULT 'total_recharge'"
                )
            if "is_active" not in tier_columns:
                conn.execute("ALTER TABLE activity_tiers ADD COLUMN is_active INTEGER NOT NULL DEFAULT 1")
            if "usage_days" not in tier_columns:
                conn.execute("ALTER TABLE activity_tiers ADD COLUMN usage_days INTEGER NOT NULL DEFAULT 0")
            conn.execute(
                "CREATE INDEX IF NOT EXISTS idx_codes_activity_tier_status ON codes(activity_id, tier_id, status, id)"
            )
            conn.execute(
                "CREATE INDEX IF NOT EXISTS idx_activity_tiers_activity ON activity_tiers(activity_id, sort_order, id)"
            )

    def upsert_activity(
        self,
        slug: str,
        name: str,
        description: str,
        use_url: str,
        packet_type: str = "ordinary",
        message_template: str = "",
        starts_at: Optional[str] = None,
        ends_at: Optional[str] = None,
        status: str = "draft",
    ) -> None:
        now = utc_now()
        template = (message_template or DEFAULT_MESSAGE_TEMPLATE) if packet_type == "tier" else ""
        with self.connect() as conn:
            cur = conn.execute(
                """
                UPDATE activities
                SET name=?, description=?, use_url=?, packet_type=?, message_template=?,
                    starts_at=?, ends_at=?, status=?, updated_at=?
                WHERE slug=?
                """,
                (name, description, use_url, packet_type, template, starts_at, ends_at, status, now, slug),
            )
            if cur.rowcount == 0:
                conn.execute(
                    """
                    INSERT INTO activities(
                        slug, name, description, use_url, packet_type, message_template,
                        starts_at, ends_at, status, created_at, updated_at
                    )
                    VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
                    """,
                    (slug, name, description, use_url, packet_type, template, starts_at, ends_at, status, now, now),
                )

    def upsert_tiers(self, slug: str, tiers: List[Dict[str, Any]]) -> None:
        now = utc_now()
        with self.connect() as conn:
            activity = self.get_activity(slug, conn)
            seen_keys = set()
            for index, tier in enumerate(tiers):
                tier_key = str(tier.get("tier_key") or "").strip()
                if not tier_key:
                    continue
                seen_keys.add(tier_key)
                name = str(tier.get("name") or tier_key).strip()
                threshold_amount = float(tier.get("threshold_amount") or 0)
                threshold_basis = normalize_threshold_basis(tier.get("threshold_basis"))
                usage_days = max(0, int(tier.get("usage_days") or 0))
                animations = json.dumps(clean_animation_list(tier.get("animations")), ensure_ascii=False)
                sort_order = int(tier.get("sort_order") if tier.get("sort_order") is not None else index)
                cur = conn.execute(
                    """
                    UPDATE activity_tiers
                    SET name=?, threshold_amount=?, threshold_basis=?, usage_days=?, animations=?,
                        sort_order=?, is_active=1, updated_at=?
                    WHERE activity_id=? AND tier_key=?
                    """,
                    (
                        name,
                        threshold_amount,
                        threshold_basis,
                        usage_days,
                        animations,
                        sort_order,
                        now,
                        activity["id"],
                        tier_key,
                    ),
                )
                if cur.rowcount == 0:
                    conn.execute(
                        """
                        INSERT INTO activity_tiers(
                            activity_id, tier_key, name, threshold_amount, threshold_basis, usage_days, animations,
                            sort_order, is_active, created_at, updated_at
                        )
                        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
                        """,
                        (
                            activity["id"],
                            tier_key,
                            name,
                            threshold_amount,
                            threshold_basis,
                            usage_days,
                            animations,
                            sort_order,
                            1,
                            now,
                            now,
                        ),
                    )

            existing = conn.execute(
                "SELECT id, tier_key FROM activity_tiers WHERE activity_id=?",
                (activity["id"],),
            ).fetchall()
            for row in existing:
                if row["tier_key"] in seen_keys:
                    continue
                claimed = conn.execute(
                    "SELECT COUNT(*) FROM claims WHERE activity_id=? AND tier_id=?",
                    (activity["id"], row["id"]),
                ).fetchone()[0]
                if claimed > 0:
                    conn.execute(
                        """
                        UPDATE activity_tiers SET is_active=0, updated_at=?
                        WHERE id=?
                        """,
                        (now, row["id"]),
                    )
                    conn.execute(
                        "DELETE FROM codes WHERE activity_id=? AND tier_id=? AND status='available'",
                        (activity["id"], row["id"]),
                    )
                    continue
                conn.execute(
                    "DELETE FROM codes WHERE activity_id=? AND tier_id=? AND status='available'",
                    (activity["id"], row["id"]),
                )
                conn.execute(
                    "UPDATE codes SET tier_id=NULL WHERE activity_id=? AND tier_id=?",
                    (activity["id"], row["id"]),
                )
                conn.execute("DELETE FROM activity_tiers WHERE id=?", (row["id"],))

    def import_codes(self, slug: str, codes: List[str], tier_key: Optional[str] = None) -> int:
        now = utc_now()
        with self.connect() as conn:
            activity = self.get_activity(slug, conn)
            tier_id = None
            if tier_key:
                tier = get_tier_by_key(conn, activity["id"], tier_key)
                if not tier:
                    raise AppError(HTTPStatus.NOT_FOUND, "红包档次不存在")
                tier_id = tier["id"]
            elif activity["packet_type"] == "tier":
                raise AppError(HTTPStatus.BAD_REQUEST, "档次红包导入兑换码时必须选择红包档次")
            count = 0
            for code in codes:
                try:
                    conn.execute(
                        "INSERT INTO codes(activity_id, tier_id, code, created_at) VALUES (?, ?, ?, ?)",
                        (activity["id"], tier_id, code, now),
                    )
                    count += 1
                except sqlite3.IntegrityError:
                    continue
            return count

    def close_activity(self, slug: str) -> None:
        with self.connect() as conn:
            cur = conn.execute(
                "UPDATE activities SET status='closed', updated_at=? WHERE slug=?",
                (utc_now(), slug),
            )
            if cur.rowcount == 0:
                raise AppError(HTTPStatus.NOT_FOUND, "活动不存在")

    def list_activities(self, include_private: bool = False) -> List[Dict[str, Any]]:
        with self.connect() as conn:
            rows = conn.execute(
                """
                SELECT * FROM activities
                ORDER BY updated_at DESC, id DESC
                """
            ).fetchall()
            return [self.activity_summary(row["slug"], include_private=include_private) for row in rows]

    def get_activity(self, slug: str, conn: Optional[sqlite3.Connection] = None) -> sqlite3.Row:
        def query(c: sqlite3.Connection) -> sqlite3.Row:
            row = c.execute("SELECT * FROM activities WHERE slug=?", (slug,)).fetchone()
            if not row:
                raise AppError(HTTPStatus.NOT_FOUND, "活动不存在")
            return row

        if conn is not None:
            return query(conn)
        with self.connect() as new_conn:
            return query(new_conn)

    def activity_summary(self, slug: str, include_private: bool = False) -> Dict[str, Any]:
        with self.connect() as conn:
            activity = self.get_activity(slug, conn)
            remaining = activity_remaining(conn, activity)
            claimed = conn.execute(
                "SELECT COUNT(*) FROM claims WHERE activity_id=?",
                (activity["id"],),
            ).fetchone()[0]
            claimable, status_text = activity_claim_state(activity, remaining)
            summary = {
                "id": activity["id"],
                "slug": activity["slug"],
                "name": activity["name"],
                "description": activity["description"],
                "use_url": activity["use_url"],
                "packet_type": activity["packet_type"],
                "starts_at": activity["starts_at"],
                "ends_at": activity["ends_at"],
                "status": activity["status"],
                "status_text": status_text,
                "claimable": claimable,
                "remaining_count": remaining,
                "claimed_count": claimed,
                "tiers": list_tiers(conn, activity["id"], include_private=include_private),
            }
            if include_private and activity["packet_type"] == "tier":
                summary["message_template"] = activity["message_template"] or DEFAULT_MESSAGE_TEMPLATE
            return summary

    def claim_code(self, slug: str, email: str) -> Tuple[Dict[str, Any], bool]:
        email_hash = hash_email(email)
        now = utc_now()
        with self.connect() as conn:
            conn.execute("BEGIN IMMEDIATE")
            try:
                claim, already_claimed = self._claim_code_locked(conn, slug, email, email_hash, now)
                conn.execute("COMMIT")
                return claim, already_claimed
            except Exception:
                with contextlib.suppress(sqlite3.Error):
                    conn.execute("ROLLBACK")
                raise

    def claim_tier_code(
        self,
        slug: str,
        email: str,
        effective_recharge: float,
        current_balance: float = 0.0,
        usage_status: Optional[Dict[int, bool]] = None,
        recharge_by_usage_days: Optional[Dict[int, float]] = None,
    ) -> Tuple[Dict[str, Any], bool]:
        email_hash = hash_email(email)
        now = utc_now()
        with self.connect() as conn:
            conn.execute("BEGIN IMMEDIATE")
            try:
                claim, already_claimed = self._claim_tier_code_locked(
                    conn,
                    slug,
                    email,
                    email_hash,
                    now,
                    effective_recharge,
                    current_balance,
                    usage_status or {},
                    recharge_by_usage_days or {},
                )
                conn.execute("COMMIT")
                return claim, already_claimed
            except Exception:
                with contextlib.suppress(sqlite3.Error):
                    conn.execute("ROLLBACK")
                raise

    def _claim_code_locked(
        self,
        conn: sqlite3.Connection,
        slug: str,
        email: str,
        email_hash: str,
        now: str,
    ) -> Tuple[Dict[str, Any], bool]:
        activity = self.get_activity(slug, conn)
        if activity["packet_type"] != "ordinary":
            raise AppError(HTTPStatus.BAD_REQUEST, "该活动不是普通红包")
        remaining = count_remaining(conn, activity["id"], None)
        claimable, status_text = activity_claim_state(activity, remaining)
        if not claimable:
            raise AppError(HTTPStatus.CONFLICT, status_text)

        existing = find_claim(conn, activity["id"], email_hash)
        if existing:
            return claim_from_row(existing, activity), True

        code = find_available_code(conn, activity["id"], None)
        if not code:
            raise AppError(HTTPStatus.CONFLICT, "兑换码已领完")

        conn.execute(
            """
            INSERT INTO claims(activity_id, code_id, email, email_hash, claimed_at, user_exists_checked_at)
            VALUES (?, ?, ?, ?, ?, ?)
            """,
            (activity["id"], code["id"], email, email_hash, now, now),
        )
        conn.execute(
            """
            UPDATE codes SET status='claimed', claimed_by_email=?, claimed_at=?
            WHERE id=? AND status='available'
            """,
            (email, now, code["id"]),
        )
        return claim_payload(code["code"], now, activity), False

    def _claim_tier_code_locked(
        self,
        conn: sqlite3.Connection,
        slug: str,
        email: str,
        email_hash: str,
        now: str,
        effective_recharge: float,
        current_balance: float,
        usage_status: Dict[int, bool],
        recharge_by_usage_days: Dict[int, float],
    ) -> Tuple[Dict[str, Any], bool]:
        activity = self.get_activity(slug, conn)
        if activity["packet_type"] != "tier":
            raise AppError(HTTPStatus.BAD_REQUEST, "该活动不是档次红包")
        remaining = activity_remaining(conn, activity)
        claimable, status_text = activity_claim_state(activity, remaining)
        if not claimable:
            raise AppError(HTTPStatus.CONFLICT, status_text)

        existing = find_claim(conn, activity["id"], email_hash)
        if existing:
            return claim_from_row(existing, activity), True

        tiers = list_tiers(conn, activity["id"], include_private=True)
        eligible = [
            tier for tier in tiers
            if tier_requirements_met(tier, effective_recharge, current_balance, usage_status, recharge_by_usage_days)
        ]
        eligible.sort(
            key=lambda tier: (
                float(tier.get("threshold_amount") or 0),
                int(tier.get("sort_order") or 0),
            ),
            reverse=True,
        )
        if not eligible:
            raise AppError(HTTPStatus.FORBIDDEN, "暂未匹配到可领取档次，请确认充值金额和使用记录后再试")

        selected = first_available_tier(conn, activity["id"], eligible)
        if not selected:
            raise AppError(HTTPStatus.CONFLICT, "eligible tier codes are exhausted")
        if count_remaining(conn, activity["id"], int(selected["id"])) <= 0:
            raise AppError(HTTPStatus.CONFLICT, "当前可领取档次兑换码已领完")

        code = find_available_code(conn, activity["id"], int(selected["id"]))
        if not code:
            raise AppError(HTTPStatus.CONFLICT, "当前可领取档次兑换码已领完")

        conn.execute(
            """
            INSERT INTO claims(activity_id, code_id, tier_id, email, email_hash, claimed_at, user_exists_checked_at)
            VALUES (?, ?, ?, ?, ?, ?, ?)
            """,
            (activity["id"], code["id"], selected["id"], email, email_hash, now, now),
        )
        conn.execute(
            """
            UPDATE codes SET status='claimed', claimed_by_email=?, claimed_at=?
            WHERE id=? AND status='available'
            """,
            (email, now, code["id"]),
        )
        return claim_payload(code["code"], now, activity, selected), False

    def list_claims(self, slug: str, email: str) -> List[Dict[str, Any]]:
        email_hash = hash_email(email)
        with self.connect() as conn:
            activity = self.get_activity(slug, conn)
            rows = conn.execute(
                """
                SELECT c.code, cl.claimed_at, cl.tier_id,
                       t.tier_key, t.name AS tier_name, t.animations AS tier_animations
                FROM claims cl
                JOIN codes c ON c.id = cl.code_id
                LEFT JOIN activity_tiers t ON t.id = cl.tier_id
                WHERE cl.activity_id=? AND cl.email_hash=?
                ORDER BY cl.claimed_at DESC
                """,
                (activity["id"], email_hash),
            ).fetchall()
            return [claim_from_row(row, activity) for row in rows]

    def search_claims(self, email: str, limit: int = 200) -> List[Dict[str, Any]]:
        email_hash = hash_email(email)
        with self.connect() as conn:
            rows = conn.execute(
                """
                SELECT a.slug AS activity_id,
                       a.name AS activity_name,
                       a.packet_type,
                       a.status AS activity_status,
                       c.code,
                       cl.email,
                       cl.claimed_at,
                       cl.tier_id,
                       t.tier_key,
                       t.name AS tier_name
                FROM claims cl
                JOIN activities a ON a.id = cl.activity_id
                JOIN codes c ON c.id = cl.code_id
                LEFT JOIN activity_tiers t ON t.id = cl.tier_id
                WHERE cl.email_hash=?
                ORDER BY cl.claimed_at DESC, cl.id DESC
                LIMIT ?
                """,
                (email_hash, max(1, min(int(limit), 500))),
            ).fetchall()
            return [claim_history_row(row) for row in rows]

    def all_code_strings(self) -> set:
        with self.connect() as conn:
            rows = conn.execute("SELECT code FROM codes").fetchall()
            return {str(row["code"]) for row in rows}


def count_remaining(conn: sqlite3.Connection, activity_id: int, tier_id: Any = ANY_TIER) -> int:
    sql = "SELECT COUNT(*) FROM codes WHERE activity_id=? AND status='available'"
    params: List[Any] = [activity_id]
    if tier_id is None:
        sql += " AND tier_id IS NULL"
    elif tier_id is not ANY_TIER:
        sql += " AND tier_id=?"
        params.append(tier_id)
    return conn.execute(sql, params).fetchone()[0]


def activity_remaining(conn: sqlite3.Connection, activity: sqlite3.Row) -> int:
    if activity["packet_type"] == "ordinary":
        return count_remaining(conn, activity["id"], None)
    return conn.execute(
        """
        SELECT COUNT(*)
        FROM codes c
        JOIN activity_tiers t ON t.id = c.tier_id
        WHERE c.activity_id=? AND c.status='available' AND t.is_active=1
        """,
        (activity["id"],),
    ).fetchone()[0]


def find_claim(conn: sqlite3.Connection, activity_id: int, email_hash: str) -> Optional[sqlite3.Row]:
    return conn.execute(
        """
        SELECT c.code, cl.claimed_at, cl.tier_id,
               t.tier_key, t.name AS tier_name, t.animations AS tier_animations
        FROM claims cl
        JOIN codes c ON c.id = cl.code_id
        LEFT JOIN activity_tiers t ON t.id = cl.tier_id
        WHERE cl.activity_id=? AND cl.email_hash=?
        """,
        (activity_id, email_hash),
    ).fetchone()


def find_available_code(conn: sqlite3.Connection, activity_id: int, tier_id: Any = ANY_TIER) -> Optional[sqlite3.Row]:
    sql = """
        SELECT id, code FROM codes
        WHERE activity_id=? AND status='available'
    """
    params: List[Any] = [activity_id]
    if tier_id is None:
        sql += " AND tier_id IS NULL"
    elif tier_id is not ANY_TIER:
        sql += " AND tier_id=?"
        params.append(tier_id)
    sql += " ORDER BY RANDOM() LIMIT 1"
    return conn.execute(sql, params).fetchone()


def first_available_tier(
    conn: sqlite3.Connection,
    activity_id: int,
    tiers: List[Dict[str, Any]],
) -> Optional[Dict[str, Any]]:
    for tier in tiers:
        if count_remaining(conn, activity_id, int(tier["id"])) > 0:
            return tier
    return None


def get_tier_by_key(conn: sqlite3.Connection, activity_id: int, tier_key: str) -> Optional[sqlite3.Row]:
    return conn.execute(
        """
        SELECT * FROM activity_tiers
        WHERE activity_id=? AND tier_key=? AND is_active=1
        """,
        (activity_id, tier_key),
    ).fetchone()


def list_tiers(conn: sqlite3.Connection, activity_id: int, include_private: bool = False) -> List[Dict[str, Any]]:
    rows = conn.execute(
        """
        SELECT * FROM activity_tiers
        WHERE activity_id=? AND is_active=1
        ORDER BY sort_order ASC, id ASC
        """,
        (activity_id,),
    ).fetchall()
    return [tier_to_dict(conn, row, include_private=include_private) for row in rows]


def tier_to_dict(conn: sqlite3.Connection, row: sqlite3.Row, include_private: bool = False) -> Dict[str, Any]:
    tier = {
        "id": row["id"],
        "tier_key": row["tier_key"],
        "name": row["name"],
        "animations": decode_animations(row["animations"]),
        "sort_order": row["sort_order"],
        "remaining_count": count_remaining(conn, row["activity_id"], row["id"]),
        "claimed_count": conn.execute(
            "SELECT COUNT(*) FROM claims WHERE activity_id=? AND tier_id=?",
            (row["activity_id"], row["id"]),
        ).fetchone()[0],
    }
    if include_private:
        tier["threshold_amount"] = float(row["threshold_amount"] or 0)
        tier["threshold_basis"] = normalize_threshold_basis(row["threshold_basis"])
        tier["usage_days"] = int(row["usage_days"] or 0)
    return tier


def normalize_threshold_basis(value: Any) -> str:
    basis = str(value or "").strip()
    if basis in THRESHOLD_BASIS_VALUES:
        return basis
    return THRESHOLD_BASIS_TOTAL_RECHARGE


def tier_requirements_met(
    tier: Dict[str, Any],
    effective_recharge: float,
    current_balance: float,
    usage_status: Dict[int, bool],
    recharge_by_usage_days: Dict[int, float],
) -> bool:
    usage_days = int(tier.get("usage_days") or 0)
    threshold = float(tier.get("threshold_amount") or 0)
    basis = normalize_threshold_basis(tier.get("threshold_basis"))
    if basis == THRESHOLD_BASIS_CURRENT_BALANCE:
        basis_amount = current_balance
    elif basis == THRESHOLD_BASIS_RECENT_RECHARGE:
        if usage_days <= 0:
            return False
        basis_amount = float(recharge_by_usage_days.get(usage_days) or 0)
    else:
        basis_amount = effective_recharge
    if threshold > max(0.0, float(basis_amount or 0)):
        return False
    if usage_days <= 0:
        return True
    return bool(usage_status.get(usage_days))


def claim_from_row(row: sqlite3.Row, activity: sqlite3.Row) -> Dict[str, Any]:
    tier = None
    if row["tier_id"]:
        tier = {
            "id": row["tier_id"],
            "tier_key": row["tier_key"] or "",
            "name": row["tier_name"] or "会员",
            "animations": decode_animations(row["tier_animations"]),
        }
    return claim_payload(row["code"], row["claimed_at"], activity, tier)


def claim_history_row(row: sqlite3.Row) -> Dict[str, Any]:
    return {
        "activity_id": row["activity_id"],
        "activity_name": row["activity_name"],
        "packet_type": row["packet_type"],
        "activity_status": row["activity_status"],
        "code": row["code"],
        "email": row["email"],
        "claimed_at": row["claimed_at"],
        "tier": {
            "id": row["tier_id"],
            "tier_key": row["tier_key"] or "",
            "name": row["tier_name"] or "",
        } if row["tier_id"] else None,
    }


def claim_payload(
    code: str,
    claimed_at: str,
    activity: sqlite3.Row,
    tier: Optional[Dict[str, Any]] = None,
) -> Dict[str, Any]:
    animations = clean_animation_list(tier.get("animations") if tier else [])
    message = render_message(activity["message_template"], code, tier) if tier else "领取成功。"
    payload: Dict[str, Any] = {
        "code": code,
        "claimed_at": claimed_at,
        "message": message,
        "animations": animations,
    }
    if tier:
        payload["tier"] = {
            "tier_key": tier.get("tier_key") or "",
            "name": tier.get("name") or "会员",
            "animations": animations,
        }
    return payload


def render_message(template: str, code: str, tier: Optional[Dict[str, Any]]) -> str:
    tier_name = str((tier or {}).get("name") or "会员")
    text = template or DEFAULT_MESSAGE_TEMPLATE
    replacements = {
        "{vip等级}": tier_name,
        "{vip_level}": tier_name,
        "{vipLevel}": tier_name,
        "{level}": tier_name,
        "{tier}": tier_name,
        "{code}": code,
    }
    for key, value in replacements.items():
        text = text.replace(key, value)
    return text


def decode_animations(value: Any) -> List[str]:
    if isinstance(value, list):
        return clean_animation_list(value)
    if not value:
        return []
    try:
        parsed = json.loads(str(value))
    except (TypeError, ValueError):
        return []
    return clean_animation_list(parsed)


def clean_animation_list(value: Any) -> List[str]:
    if not isinstance(value, list):
        return []
    seen = set()
    out = []
    for item in value:
        key = str(item or "").strip()
        if not key or key in seen:
            continue
        seen.add(key)
        out.append(key)
    return out


def activity_claim_state(activity: sqlite3.Row, remaining: int) -> Tuple[bool, str]:
    now = dt.datetime.now(dt.timezone.utc)
    if activity["status"] == "closed":
        return False, "活动已结束"
    if activity["status"] != "open":
        return False, "活动未开放"
    try:
        starts_at = parse_time(activity["starts_at"])
        ends_at = parse_time(activity["ends_at"])
    except ValueError:
        return False, "活动时间配置不正确"
    if starts_at and now < starts_at:
        return False, "活动未开始"
    if ends_at and now > ends_at:
        return False, "活动已结束"
    if remaining <= 0:
        return False, "兑换码已领完"
    return True, "进行中"
