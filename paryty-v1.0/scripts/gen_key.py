import bcrypt
import secrets
import uuid
import psycopg2

# Generate API key
raw_bytes = secrets.token_bytes(32)
raw_key = 'pk_live_' + raw_bytes.hex()
key_prefix = raw_key[:16]

# Hash with bcrypt (Go-compatible $2a$ prefix)
hash_bytes = bcrypt.hashpw(raw_key.encode(), bcrypt.gensalt(rounds=10, prefix=b'2a'))
key_hash = hash_bytes.decode()
key_id = str(uuid.uuid4())
tenant_id = 'd1c05dd7-24cf-4890-9fdf-305493e768c3'

print(f'Raw key: {raw_key}')
print(f'Hash length: {len(key_hash)}')
print(f'Hash: {key_hash}')

# Verify hash matches
assert bcrypt.checkpw(raw_key.encode(), key_hash.encode()), 'Hash verification failed!'
print('Hash verification: OK')

# Insert into database
conn = psycopg2.connect('postgresql://paryty:paryty@postgres:5432/paryty')
cur = conn.cursor()
cur.execute(
    'INSERT INTO api_keys (key_id, tenant_id, name, key_hash, key_prefix) VALUES (%s, %s, %s, %s, %s)',
    (key_id, tenant_id, 'Agent Key (container gen)', key_hash, key_prefix)
)
conn.commit()

# Verify stored hash
cur.execute('SELECT key_hash FROM api_keys WHERE key_prefix = %s', (key_prefix,))
row = cur.fetchone()
stored_hash = row[0]
print(f'Stored hash length: {len(stored_hash)}')
print(f'Stored hash: {stored_hash}')
print(f'Match: {stored_hash == key_hash}')

# Re-verify from DB
assert bcrypt.checkpw(raw_key.encode(), stored_hash.encode()), 'DB hash verification failed!'
print('DB hash verification: OK')

cur.close()
conn.close()

# Save raw key to file for reference
with open('/app/agent_key.txt', 'w') as f:
    f.write(raw_key + '\n')
print(f'Key saved to /app/agent_key.txt')
print('DONE')
