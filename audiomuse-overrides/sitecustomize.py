"""Runtime compatibility shims for the bundled AudioMuse image."""

import os

from rq.job import Job


if not hasattr(Job, "get_id"):
    def get_id(self):
        return self.id


    Job.get_id = get_id


def _patch_search_column():
    try:
        import psycopg2
    except Exception:
        return

    host = os.getenv("POSTGRES_HOST")
    user = os.getenv("POSTGRES_USER")
    password = os.getenv("POSTGRES_PASSWORD")
    dbname = os.getenv("POSTGRES_DB")
    port = os.getenv("POSTGRES_PORT", "5432")
    if not host or not user or not password or not dbname:
        return

    conn = None
    try:
        conn = psycopg2.connect(
            host=host,
            user=user,
            password=password,
            dbname=dbname,
            port=port,
            connect_timeout=5,
        )
        conn.autocommit = True
        with conn.cursor() as cur:
            cur.execute(
                """
                SELECT pg_get_expr(ad.adbin, ad.adrelid)
                FROM pg_attribute a
                JOIN pg_attrdef ad
                  ON ad.adrelid = a.attrelid
                 AND ad.adnum = a.attnum
                WHERE a.attrelid = 'public.score'::regclass
                  AND a.attname = 'search_u'
                """
            )
            row = cur.fetchone()
            if not row:
                return
            expr = row[0] or ""
            if "COALESCE" in expr.upper():
                return
            cur.execute("ALTER TABLE score DROP COLUMN search_u CASCADE")
            cur.execute(
                """
                ALTER TABLE score
                ADD COLUMN search_u TEXT GENERATED ALWAYS AS (
                    lower(
                        immutable_unaccent(
                            COALESCE(title, '') || ' ' ||
                            COALESCE(author, '') || ' ' ||
                            COALESCE(album, '')
                        )
                    )
                ) STORED
                """
            )
            cur.execute(
                "CREATE INDEX IF NOT EXISTS score_search_u_trgm ON score USING gin (search_u gin_trgm_ops)"
            )
    except Exception:
        return
    finally:
        if conn is not None:
            conn.close()


_patch_search_column()
