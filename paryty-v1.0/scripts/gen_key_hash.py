import os
import bcrypt
import sys

raw_key = os.environ.get("PARYTY_API_KEY")
if not raw_key:
    print("Error: PARYTY_API_KEY environment variable not set")
    sys.exit(1)

# Generate hash
hash_bytes = bcrypt.hashpw(raw_key.encode(), bcrypt.gensalt())
hash_str = hash_bytes.decode()
key_prefix = raw_key[:15]  # pk_live_XXXXXXX
print(f"Hash: {hash_str}")
print(f"Prefix: {key_prefix}")

# Verify it
result = bcrypt.checkpw(raw_key.encode(), hash_bytes)
print(f"Verification: {result}")

# Write SQL insert statement
print(f"\nSQL:")
print(f"INSERT INTO api_keys (key_id, tenant_id, name, key_hash, key_prefix) VALUES")
print(f"('550e8400-e29b-41d4-a716-446655440000', '11111111-1111-1111-1111-111111111111', 'default-agent', '{hash_str}', '{key_prefix}');")
