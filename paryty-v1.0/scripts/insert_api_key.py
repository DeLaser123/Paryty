import psycopg2
import bcrypt

raw_key = "pk_live_6a89eaf0d573619d927dc19895039dc78897cfba8d1d5c39b079c828d552cbeb"

# Generate hash
hash_bytes = bcrypt.hashpw(raw_key.encode(), bcrypt.gensalt())
hash_str = hash_bytes.decode()
print(f"Generated hash: {hash_str}")

# Verify it
result = bcrypt.checkpw(raw_key.encode(), hash_bytes)
print(f"Verification: {result}")

# Connect to PostgreSQL
conn = psycopg2.connect(
    host="localhost",
    port=5432,
    database="paryty",
    user="paryty",
    password="paryty"
)
cur = conn.cursor()

# Delete existing key
cur.execute("DELETE FROM api_keys WHERE key_prefix = %s", ("pk_live_6a89eaf0",))

# Insert with proper hash
cur.execute(
    """INSERT INTO api_keys (key_id, tenant_id, name, key_hash, key_prefix) 
       VALUES (%s, %s, %s, %s, %s)""",
    (
        "550e8400-e29b-41d4-a716-446655440000",
        "11111111-1111-1111-1111-111111111111",
        "default-agent",
        hash_str,
        "pk_live_6a89eaf0"
    )
)
conn.commit()

# Verify
cur.execute("SELECT key_hash FROM api_keys WHERE key_prefix = %s", ("pk_live_6a89eaf0",))
stored_hash = cur.fetchone()[0]
print(f"Stored hash: {stored_hash}")

# Verify stored hash
result2 = bcrypt.checkpw(raw_key.encode(), stored_hash.encode())
print(f"Stored hash verification: {result2}")

cur.close()
conn.close()
print("Done!")
