FROM golang:1.26-alpine AS builder

RUN apk add --no-cache build-base ca-certificates

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 go build -trimpath -ldflags "-s -w" -o /out/api ./cmd/api \
    && CGO_ENABLED=1 go build -trimpath -ldflags "-s -w" -o /out/seed ./cmd/seed

FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app
COPY --from=builder /out/api /out/seed ./
COPY --from=builder /src/migrations /app/migrations
COPY --from=builder /src/cmd/seed/data/datos-prueba-HNL.json /app/cmd/seed/data/datos-prueba-HNL.json

EXPOSE 8080
CMD ["/app/api"]
