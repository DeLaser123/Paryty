# Frontend CDN Setup

## Overview

The Paryty frontend is a single-page application (SPA) built with Vite. For production deployments, static assets should be served via a CDN with aggressive caching, while HTML and API requests bypass the CDN entirely.

---

## CloudFront Configuration

### Origin Setup

| Origin | Target | Protocol |
|--------|--------|----------|
| Frontend | nginx endpoint from frontend K8s service | HTTPS only |
| API | Query service endpoint | HTTPS only |

### Cache Policies

| Path Pattern | TTL | Cache Policy | Rationale |
|--------------|-----|--------------|-----------|
| `/assets/*` | 1 year | `CachingOptimized` | Vite-hashed filenames guarantee uniqueness |
| `/*.js` | 1 year | `CachingOptimized` | Bundled JS with content hash |
| `/*.css` | 1 year | `CachingOptimized` | Bundled CSS with content hash |
| `/*.woff2` | 1 year | `CachingOptimized` | Fonts rarely change |
| `/index.html` | 0 | `CachingDisabled` | Must always fetch fresh (contains asset references) |
| `/api/*` | 0 | `CachingDisabled` | Dynamic data, never cache |
| `/api/v1/ws` | 0 | `CachingDisabled` | WebSocket — bypass CDN entirely |
| `/api/v1/timeline/*` | 0 | `CachingDisabled` | SSE — bypass CDN entirely |

### SSL/TLS

1. Request ACM certificate for `*.paryty.io` and `paryty.io`
2. Attach to CloudFront distribution
3. Redirect HTTP → HTTPS
4. TLS 1.2 minimum

### WAF (Web Application Firewall)

Enable AWS WAF on the CloudFront distribution:

| Rule | Action |
|------|--------|
| AWS Managed Rules — Common | Block |
| AWS Managed Rules — Known Bad Inputs | Block |
| Rate limiting — 1000 req/min per IP | Block |
| Geo-restriction (if needed) | Allow/Block list |

### Custom Error Pages

| Error Code | Response | TTL |
|------------|----------|-----|
| 403 | `/index.html` (SPA routing) | 0 |
| 404 | `/index.html` (SPA routing) | 0 |

---

## nginx Configuration

The nginx server sits between CloudFront and the Vite build output:

```nginx
server {
    listen 443 ssl http2;
    server_name app.paryty.io;

    ssl_certificate     /etc/nginx/ssl/cert.pem;
    ssl_certificate_key /etc/nginx/ssl/key.pem;
    ssl_protocols       TLSv1.2 TLSv1.3;

    root /usr/share/nginx/html;
    index index.html;

    # Security headers
    add_header X-Frame-Options "SAMEORIGIN" always;
    add_header X-Content-Type-Options "nosniff" always;
    add_header X-XSS-Protection "1; mode=block" always;
    add_header Referrer-Policy "strict-origin-when-cross-origin" always;
    add_header Content-Security-Policy "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; connect-src 'self' wss://*.paryty.io; img-src 'self' data: blob:;" always;
    add_header Strict-Transport-Security "max-age=31536000; includeSubDomains" always;

    # Static assets — aggressive caching (Vite content-hashed filenames)
    location /assets/ {
        expires 1y;
        add_header Cache-Control "public, immutable";
        access_log off;
    }

    # JS/CSS bundles (Vite outputs to /assets/ but catch any stragglers)
    location ~* \.(js|css|woff2|woff|ttf|eot)$ {
        expires 1y;
        add_header Cache-Control "public, immutable";
        access_log off;
    }

    # SPA routing — all non-file requests serve index.html
    location / {
        try_files $uri $uri/ /index.html;
        add_header Cache-Control "no-cache, no-store, must-revalidate";
        add_header Pragma "no-cache";
        add_header Expires "0";
    }

    # API proxy (if nginx is also proxying API requests)
    location /api/ {
        proxy_pass http://paryty-query:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_no_cache 1;
        proxy_cache_bypass 1;
    }

    # WebSocket proxy
    location /api/v1/ws {
        proxy_pass http://paryty-query:8080;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_read_timeout 86400;
    }

    # Health check endpoint
    location /healthz {
        access_log off;
        return 200 "ok";
        add_header Content-Type text/plain;
    }
}
```

---

## Vite Build Configuration

Ensure Vite produces content-hashed filenames for optimal CDN caching:

```typescript
// vite.config.ts
export default defineConfig({
  build: {
    rollupOptions: {
      output: {
        // Content hash in filename for cache busting
        entryFileNames: 'assets/[name]-[hash].js',
        chunkFileNames: 'assets/[name]-[hash].js',
        assetFileNames: 'assets/[name]-[hash].[ext]',
      },
    },
    // Enable source maps for production debugging
    sourcemap: true,
  },
});
```

---

## Kubernetes Deployment

### Frontend Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: paryty-frontend
spec:
  replicas: 2
  selector:
    matchLabels:
      app: paryty-frontend
  template:
    spec:
      containers:
        - name: nginx
          image: nginx:alpine
          ports:
            - containerPort: 80
          volumeMounts:
            - name: frontend-build
              mountPath: /usr/share/nginx/html
              readOnly: true
            - name: nginx-config
              mountPath: /etc/nginx/conf.d/default.conf
              subPath: default.conf
          resources:
            requests:
              cpu: 100m
              memory: 128Mi
            limits:
              cpu: 500m
              memory: 256Mi
          livenessProbe:
            httpGet:
              path: /healthz
              port: 80
            initialDelaySeconds: 5
            periodSeconds: 10
          readinessProbe:
            httpGet:
              path: /healthz
              port: 80
            initialDelaySeconds: 5
            periodSeconds: 10
      volumes:
        - name: frontend-build
          configMap:
            name: paryty-frontend-build
        - name: nginx-config
          configMap:
            name: paryty-nginx-config
```

---

## DNS Configuration

| Record | Type | Value |
|--------|------|-------|
| `app.paryty.io` | CNAME | `d1234.cloudfront.net` |
| `api.paryty.io` | CNAME | `d5678.cloudfront.net` (or direct ALB) |
| `ws.paryty.io` | A/AAAA | Direct to ALB (WebSocket bypasses CDN) |

---

## Cache Invalidation

When deploying a new frontend version:

1. **No invalidation needed for assets/** — Vite content hashes ensure new filenames
2. **Invalidate index.html** — CloudFront invalidation: `/* ` or just `/index.html`
3. **Or use versioned paths** — Deploy to `/v2/`, update nginx root

```bash
# AWS CLI invalidation
aws cloudfront create-invalidation \
  --distribution-id E1234567890 \
  --paths "/index.html"
```

---

## Performance Targets

| Metric | Target |
|--------|--------|
| TTFB (CDN cache hit) | < 50ms |
| TTFB (CDN cache miss) | < 200ms |
| LCP | < 2.5s |
| FCP | < 1.0s |
| Asset load (cached) | < 100ms |
| Asset load (uncached) | < 1s |
