import contextlib
import datetime as dt
import sqlite3
from http import HTTPStatus
from pathlib import Path
from typing import Any, Dict, List, Optional, Tuple

from common import AppError, hash_email, parse_time, utc_now


class Store:
    def __init__(self, db_path: Path):
        self.db_path = db_path
        self.db_path.parent.mkdir(parents=True, exist_ok=True)
        self.init_db()

    def connect(self) -> sqlite3.Connection:
        conn = sqlite3.connect(str(self.db_path), timeout=15, isolation_level=None)
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
                    starts_at TEXT,
                    ends_at TEXT,
                    status TEXT NOT NULL DEFAULT 'draft',
                    created_at TEXT NOT NULL,
                    updated_at TEXT NOT NULL
                );

                CREATE TABLE IF NOT EXISTS codes (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    activity_id INTEGER NOT NULL REFERENCES activities(id) ON DELETE CASCADE,
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

    def upsert_activity(
        self,
        slug: str,
        name: str,
        description: str,
        use_url: str,
        starts_at: Optional[str],
        ends_at: Optional[str],
        status: str,
    ) -> None:
        now = utc_now()
        with self.connect() as conn:
            cur = conn.execute(
                """
                UPDATE activities
                SET name=?, description=?, use_url=?, starts_at=?, ends_at=?, status=?, updated_at=?
                WHERE slug=?
                """,
                (name, description, use_url, starts_at, ends_at, status, now, slug),
            )
            if cur.rowcount == 0:
                conn.execute(
                    """
                    INSERT INTO activities(slug, name, description, use_url, starts_at, ends_at, status, created_at, updated_at)
                    VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
                    """,
                    (slug, name, description, use_url, starts_at, ends_at, status, now, now),
                )

    def import_codes(self, slug: str, codes: List[str]) -> int:
        now = utc_now()
        with self.connect() as conn:
            activity = self.get_activity(slug, conn)
            count = 0
            for code in codes:
                try:
                    conn.execute(
                        "INSERT INTO codes(activity_id, code, created_at) VALUES (?, ?, ?)",
                        (activity["id"], code, now),
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

    def list_activities(self) -> List[Dict[str, Any]]:
        with self.connect() as conn:
            rows = conn.execute(
                """
                SELECT * FROM activities
                ORDER BY updated_at DESC, id DESC
                """
            ).fetchall()
            return [self.activity_summary(row["slug"]) for row in rows]

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

    def activity_summary(self, slug: str) -> Dict[str, Any]:
        with self.connect() as conn:
            activity = self.get_activity(slug, conn)
            remaining = count_remaining(conn, activity["id"])
            claimed = conn.execute(
                "SELECT COUNT(*) FROM claims WHERE activity_id=?",
                (activity["id"],),
            ).fetchone()[0]
            claimable, status_text = activity_claim_state(activity, remaining)
            return {
                "id": activity["id"],
                "slug": activity["slug"],
                "name": activity["name"],
                "description": activity["description"],
                "use_url": activity["use_url"],
                "starts_at": activity["starts_at"],
                "ends_at": activity["ends_at"],
                "status": activity["status"],
                "status_text": status_text,
                "claimable": claimable,
                "remaining_count": remaining,
                "claimed_count": claimed,
            }

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

    def _claim_code_locked(
        self,
        conn: sqlite3.Connection,
        slug: str,
        email: str,
        email_hash: str,
        now: str,
    ) -> Tuple[Dict[str, Any], bool]:
        activity = self.get_activity(slug, conn)
        remaining = count_remaining(conn, activity["id"])
        claimable, status_text = activity_claim_state(activity, remaining)
        if not claimable:
            raise AppError(HTTPStatus.CONFLICT, status_text)

        existing = find_claim(conn, activity["id"], email_hash)
        if existing:
            return {"code": existing["code"], "claimed_at": existing["claimed_at"]}, True

        code = find_available_code(conn, activity["id"])
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
        return {"code": code["code"], "claimed_at": now}, False

    def list_claims(self, slug: str, email: str) -> List[Dict[str, Any]]:
        email_hash = hash_email(email)
        with self.connect() as conn:
            activity = self.get_activity(slug, conn)
            rows = conn.execute(
                """
                SELECT c.code, cl.claimed_at
                FROM claims cl
                JOIN codes c ON c.id = cl.code_id
                WHERE cl.activity_id=? AND cl.email_hash=?
                ORDER BY cl.claimed_at DESC
                """,
                (activity["id"], email_hash),
            ).fetchall()
            return [{"code": row["code"], "claimed_at": row["claimed_at"]} for row in rows]


def count_remaining(conn: sqlite3.Connection, activity_id: int) -> int:
    return conn.execute(
        "SELECT COUNT(*) FROM codes WHERE activity_id=? AND status='available'",
        (activity_id,),
    ).fetchone()[0]


def find_claim(conn: sqlite3.Connection, activity_id: int, email_hash: str) -> Optional[sqlite3.Row]:
    return conn.execute(
        """
        SELECT c.code, cl.claimed_at
        FROM claims cl
        JOIN codes c ON c.id = cl.code_id
        WHERE cl.activity_id=? AND cl.email_hash=?
        """,
        (activity_id, email_hash),
    ).fetchone()


def find_available_code(conn: sqlite3.Connection, activity_id: int) -> Optional[sqlite3.Row]:
    return conn.execute(
        """
        SELECT id, code FROM codes
        WHERE activity_id=? AND status='available'
        ORDER BY id ASC
        LIMIT 1
        """,
        (activity_id,),
    ).fetchone()


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
