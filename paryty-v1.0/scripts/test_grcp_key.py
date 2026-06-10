import grpc
from grpc_health.v1 import health_pb2, health_pb2_grpc

with open('/app/agent_key.txt') as f:
    api_key = f.read().strip()

channel = grpc.insecure_channel('paryty-ingestion:8080')
stub = health_pb2_grpc.HealthStub(channel)

metadata = [('x-api-key', api_key)]
try:
    resp = stub.Check(health_pb2.HealthCheckRequest(), timeout=3, metadata=metadata)
    print(f'SUCCESS! Health status: {resp.status}')
except Exception as e:
    print(f'FAILED: {e.code()}: {e.details()}')
