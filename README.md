# Banca en Línea (HNL)

Aplicación de banca en línea con backend en **Go**, base de datos **PostgreSQL**,
motor contable **TigerBeetle** y frontend en **Vite + React + TypeScript + Tailwind**.
Incluye un asistente por chat (function calling) con OpenRouter para consultar
saldos y ejecutar transferencias en dos fases.

## Arquitectura

- **TigerBeetle** es la fuente de verdad del dinero (saldos y movimientos).
- **PostgreSQL** guarda metadatos legibles: usuarios, cuentas, tokens de
  refresco, historial de movimientos y transferencias pendientes del chat.
- **API Go** expone auth (JWT + refresh rotativo), cuentas, movimientos,
  transferencias (con idempotency keys) y el chat.

### Decisiones técnicas del backend

| Área | Decisión |
|---|---|
| Acceso a datos | Interfaces (`UserRepository`, `AccountRepository`, `TransactionRepository`, `PendingStore`, `TokenStore`) con implementaciones pgx, para poder mockear. |
| Ledger | Interfaz `LedgerClient`; saldos en **batch** (`LookupAccounts`), timeouts por contexto. |
| IA | Interfaz `ChatProvider` desacoplada del wire format de OpenRouter. |
| Errores | Un único formato JSON: `{"error":{"code":"...","message":"..."}}`. |
| Idempotencia | Cabecera `Idempotency-Key` en `POST /transfers`, `POST /deposits` y `POST /withdrawals`; se cachea la respuesta y el id de transferencia TB es determinista (sha256 de user+key). |
| Seguridad | bcrypt, JWT HS256, refresh tokens con hash SHA-256, CORS con origen explícito, rate limiting en login/register. |
| Observabilidad | `slog` con `request_id`, `user_id` enmascarado y duración por petición. |
| Robustez | Timeouts por petición, graceful shutdown, migraciones idempotentes, seed idempotente. |

### Convenciones TigerBeetle

- `Ledger = 1`; códigos de cuenta: `1` checking, `2` savings, `3` investment, `9` EXTERNAL.
- Id de cuenta TB = último segmento del `account_number` (`4001-...-0001` → `1`).
- Cuenta EXTERNAL: id `9000001` (configurable con `TB_EXTERNAL_ACCOUNT_ID`).
- Transferencias del chat en dos fases: `pending` → `post` / `void`, timeout 300 s.

## Requisitos

- Docker + Docker Compose.
- (Opcional) `OPENROUTER_API_KEY` para activar el chat.
- Para compilar el backend localmente en Windows se necesita cgo:
  - Instalar MinGW-W64 y usar `CGO_ENABLED=1` con `CC` apuntando a `gcc.exe`.

## Puesta en marcha

```bash
cp .env.example .env   # ajustar credenciales si se desea
docker compose up -d --build
```

Los servicios:

| Servicio | URL |
|---|---|
| Frontend | http://localhost:5173 |
| API | http://localhost:8080 |
| PostgreSQL | localhost:5432 |
| TigerBeetle | localhost:3000 |

> Si el puerto 3000 está ocupado, usa `TB_HOST_PORT=3001 docker compose up -d`.

### Seed de datos de prueba

La API aplica las migraciones al arrancar y, si la base está vacía, carga
automáticamente el dataset de prueba (1000 usuarios, 1605 cuentas, 6429
transacciones) y verifica que los balances coincidan. Se desactiva con
`SEED_ON_START=false`. Para forzar una recarga manual:

```bash
# dentro del contenedor de la api (el JSON ya está copiado):
docker compose exec api /app/seed

# o compilar/ejecutar el seed fuera de docker (requiere cgo):
#   TB_ADDRESS=127.0.0.1:3001 go run ./cmd/seed -data cmd/seed/data/datos-prueba-HNL.json
```

El seed es idempotente (re-ejecutarlo no duplica usuarios, cuentas ni
movimientos: los ids de transferencia TB son deterministas y el `tb_transfer_id`
es único en Postgres) y termina verificando que el saldo de cada cuenta en
TigerBeetle coincida con `initial_balance + Σ movimientos`.

### Usuarios de prueba

Para probar el login, usa cualquier usuario del dataset sembrado — email y password están en backend/cmd/seed/data/datos-prueba-HNL.json

## Frontend

Aplicación **Vite + React + TypeScript + Tailwind** con componentes tipo shadcn.

- Rutas con carga diferida (`React.lazy` + `Suspense`) y guardas de sesión.
- `@tanstack/react-query` para cuentas, saldos y movimientos (con paginación).
- El dashboard muestra el saldo por cuenta y los últimos movimientos de todas
  las cuentas del usuario (`GET /transactions/recent`).
- Páginas: inicio/cuentas, detalle de cuenta, transferir y asistente por chat.
- El asistente muestra la transferencia pendiente creada por function calling con
  botones **Confirmar/Cancelar** (`/chat/confirm` y `/chat/cancel`).

```bash
cd frontend
npm install
npm run dev            # http://localhost:5173 (usa VITE_API_URL del .env)
```

## API

| Método | Ruta | Descripción |
|---|---|---|
| POST | `/auth/register` | Crear usuario |
| POST | `/auth/login` | Iniciar sesión (JWT) |
| POST | `/auth/refresh` | Rotar refresh token |
| POST | `/auth/logout` | Revocar refresh token |
| GET | `/accounts` | Cuentas del usuario con saldo (batch) |
| GET | `/accounts/{accountNumber}` | Detalle de cuenta |
| GET | `/accounts/{accountNumber}/transactions` | Movimientos paginados (`limit` 1-200, `offset`) |
| GET | `/transactions/recent` | Últimos 5 movimientos del usuario (dashboard) |
| POST | `/transfers` | Transferencia (`Idempotency-Key` opcional) |
| POST | `/deposits` | Depósito desde la cuenta externa (`Idempotency-Key` opcional) |
| POST | `/withdrawals` | Retiro hacia la cuenta externa (`Idempotency-Key` opcional) |
| POST | `/chat` | Mensaje al asistente |
| POST | `/chat/confirm` | Confirmar transferencia pendiente |
| POST | `/chat/cancel` | Cancelar transferencia pendiente |

Formato único de error:

```json
{ "error": { "code": "INSUFFICIENT_FUNDS", "message": "saldo insuficiente" } }
```

## Desarrollo

```bash
# backend (requiere cgo en Windows)
cd backend
go build ./... && go vet ./... && go test ./...
```
