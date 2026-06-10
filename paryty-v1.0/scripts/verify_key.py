import psycopg2
import bcrypt

conn = psycopg2.connect('postgresql://paryty:paryty@postgres:5432/paryty')
cur = conn.cursor()
cur.execute("SELECT key_hash FROM api_keys WHERE key_prefix = 'pk_live_9e379f2c'")
row = cur.fetchone()
h = row[0]
print('hash_len:', len(h))
print('hash_start:', h[:10])
print('full:', repr(h))

key = 'pk_live_9e379f2cec27272283b9076eb22eff5ee9b3fd15ce1919b1f5b7312a7680ba9f'
result = bcrypt.checkpw(key.encode(), h.encode())
print('match:', result)
cur.close()
conn.close()
