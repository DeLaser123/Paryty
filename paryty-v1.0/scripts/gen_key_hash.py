import bcrypt
import sys

raw_key = "pk_live_6a89eaf0d573619d927dc19895039dc78897cfba8d1d5c39b079c828d552cbeb"

# Generate hash
hash_bytes = bcrypt.hashpw(raw_key.encode(), bcrypt.gensalt())
hash_str = hash_bytes.decode()
print(f"Hash: {hash_str}")

# Verify it
result = bcrypt.checkpw(raw_key.encode(), hash_bytes)
print(f"Verification: {result}")

# Write SQL insert statement
print(f"\nSQL:")
print(f"INSERT INTO api_keys (key_id, tenant_id, name, key_hash, key_prefix) VALUES")
print(f"('550e8400-e29b-41d4-a716-446655440000', '11111111-1111-1111-1111-111111111111', 'default-agent', '{hash_str}', 'pk_live_6a89eaf0');")
