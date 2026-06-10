$env:DATABASE_URL = "postgres://paryty:paryty@localhost:5432/paryty?sslmode=disable"
$env:PARYTY_JWT_SECRET = "paryty-dev-jwt-secret-change-in-production-min-32-bytes!!"
Set-Location "d:\__Projects\Paryty\paryty-v1.0\cluster"
.\bin\paryty-query.exe -port 8081
