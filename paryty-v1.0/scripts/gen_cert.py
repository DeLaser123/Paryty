"""Generate the self-signed cert for api.moonshot.cn proxy."""
import os, sys, datetime, ipaddress

CERT_DIR = os.path.dirname(os.path.abspath(__file__))
CERT_FILE = os.path.join(CERT_DIR, "moonshot_cert.pem")
KEY_FILE = os.path.join(CERT_DIR, "moonshot_key.pem")
DOMAIN = "api.moonshot.cn"

if os.path.exists(CERT_FILE) and os.path.exists(KEY_FILE):
    print("Certificate already exists.")
    sys.exit(0)

from cryptography import x509
from cryptography.x509.oid import NameOID
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import rsa

key = rsa.generate_private_key(public_exponent=65537, key_size=2048)
subject = issuer = x509.Name([
    x509.NameAttribute(NameOID.COMMON_NAME, DOMAIN),
    x509.NameAttribute(NameOID.ORGANIZATION_NAME, "Qoder Kimi Proxy"),
])
cert = (
    x509.CertificateBuilder()
    .subject_name(subject)
    .issuer_name(issuer)
    .public_key(key.public_key())
    .serial_number(x509.random_serial_number())
    .not_valid_before(datetime.datetime.utcnow())
    .not_valid_after(datetime.datetime.utcnow() + datetime.timedelta(days=3650))
    .add_extension(
        x509.SubjectAlternativeName([
            x509.DNSName(DOMAIN),
            x509.DNSName("*.moonshot.cn"),
            x509.IPAddress(ipaddress.IPv4Address("127.0.0.1")),
        ]),
        critical=False,
    )
    .sign(key, hashes.SHA256())
)

with open(KEY_FILE, "wb") as f:
    f.write(key.private_bytes(
        serialization.Encoding.PEM,
        serialization.PrivateFormat.TraditionalOpenSSL,
        serialization.NoEncryption(),
    ))
with open(CERT_FILE, "wb") as f:
    f.write(cert.public_bytes(serialization.Encoding.PEM))

print(f"Certificate: {CERT_FILE}")
print(f"Private key: {KEY_FILE}")
print("Done!")
