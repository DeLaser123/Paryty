cd "d:\__Projects\Paryty\paryty-v1.0"
podman build --no-cache -t paryty-frontend:latest -f deploy\docker\Dockerfile.frontend frontend/
podman tag paryty-frontend:latest paryty/frontend:latest
podman stop -t 2 compose_paryty-frontend_1
podman rm -f compose_paryty-frontend_1
podman run -d --name compose_paryty-frontend_1 --network compose_default -p 3000:3000 localhost/paryty/frontend:latest
