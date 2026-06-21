import os
import sys
import bcrypt
import psycopg2

raw_key = os.environ.get("PARYTY_API_KEY")
if not raw_key:
    print("Error: PARYTY_API_KEY environment variable not set")
    sys.exit(1)

# Generate hash
hash_bytes = bcrypt.hashpw(raw_key.encode(), bcrypt.gensalt())
hash_str = hash_bytes.decode()
key_prefix = raw_key[:15]
print(f"Generated hash: {hash_str}")

# Verify it
result = bcrypt.checkpw(raw_key.encode(), hash_bytes)
print(f"Verification: {result}")

# Connect to PostgreSQL
conn = psycopg2.connect(
    host=os.environ.get("PARYTY_DB_HOST", "localhost"),
    port=int(os.environ.get("PARYTY_DB_PORT", "5432")),
    database=os.environ.get("PARYTY_DB_NAME", "paryty"),
    user=os.environ.get("PARYTY_DB_USER", "paryty"),
    password=os.environ.get("PARYTY_DB_PASSWORD", "paryty")
)
cur = conn.cursor()

# Delete existing key
cur.execute("DELETE FROM api_keys WHERE key_prefix = %s", (key_prefix,))

# Insert with proper hash
cur.execute(
    """INSERT INTO api_keys (key_id, tenant_id, name, key_hash, key_prefix) 
       VALUES (%s, %s, %s, %s, %s)""",
    (
        "550e8400-e29b-41d4-a716-446655440000",
        "11111111-1111-1111-1111-111111111111",
        "default-agent",
        hash_str,
        key_prefix
    )
)
conn.commit()

# Verify
cur.execute("SELECT key_hash FROM api_keys WHERE key_prefix = %s", (key_prefix,))
stored_hash = cur.fetchone()[0]
print(f"Stored hash: {stored_hash}")

# Verify stored hash
result2 = bcrypt.checkpw(raw_key.encode(), stored_hash.encode())
print(f"Stored hash verification: {result2}")

cur.close()
conn.close()
print("Done!")
