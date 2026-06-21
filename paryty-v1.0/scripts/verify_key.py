import os
import sys
import bcrypt
import psycopg2

key_prefix = os.environ.get("PARYTY_KEY_PREFIX")
raw_key = os.environ.get("PARYTY_API_KEY")
if not key_prefix or not raw_key:
    print("Error: PARYTY_KEY_PREFIX and PARYTY_API_KEY must be set")
    sys.exit(1)

conn = psycopg2.connect(
    host=os.environ.get("PARYTY_DB_HOST", "postgres"),
    port=int(os.environ.get("PARYTY_DB_PORT", "5432")),
    database=os.environ.get("PARYTY_DB_NAME", "paryty"),
    user=os.environ.get("PARYTY_DB_USER", "paryty"),
    password=os.environ.get("PARYTY_DB_PASSWORD", "paryty")
)
cur = conn.cursor()
cur.execute("SELECT key_hash FROM api_keys WHERE key_prefix = %s", (key_prefix,))
row = cur.fetchone()
if not row:
    print(f"No key found with prefix {key_prefix}")
    sys.exit(1)
h = row[0]
print('hash_len:', len(h))
print('hash_start:', h[:10])
print('full:', repr(h))

result = bcrypt.checkpw(raw_key.encode(), h.encode())
print('match:', result)
cur.close()
conn.close()
