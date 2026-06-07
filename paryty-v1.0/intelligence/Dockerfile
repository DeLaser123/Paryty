# Paryty Intelligence Service — Python ML Container
# Phase 6: Forecasting + Anomaly Detection

FROM python:3.11-slim

WORKDIR /app

# System dependencies for psycopg2 and compilation
RUN apt-get update && apt-get install -y --no-install-recommends \
    gcc \
    libpq-dev \
    && rm -rf /var/lib/apt/lists/*

# Copy requirements first for Docker layer caching
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# Copy application code into intelligence/ package directory
# This preserves the package structure so relative imports work
COPY . intelligence/

# Create model directories
RUN mkdir -p /data/models/forecasting /data/models/anomaly

# Expose gRPC port
EXPOSE 50051

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD python -c "import grpc; ch = grpc.insecure_channel('localhost:50051'); ch.close()" || exit 1

# Run the server
CMD ["python", "-m", "intelligence.server.main"]
