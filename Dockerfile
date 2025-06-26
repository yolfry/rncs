# Build stage
FROM golang:1.24.4-alpine AS builder

WORKDIR /app

# Copia el código fuente
COPY src/ ./src/

WORKDIR /app/src

# Descarga dependencias
RUN go mod init rncs || true
RUN go mod tidy

# Compila el binario
RUN go build -o /app/rncs rncs.go

# Runtime stage
FROM alpine:latest

WORKDIR /app

# Copia el binario desde el build stage
COPY --from=builder /app/rncs .

# Puerto por defecto (puedes cambiarlo si usas otro)
EXPOSE 9922

# Comando por defecto
ENTRYPOINT ["./rncs"]