# Multi-stage build for the Soglia API. Build from the repository root:
#   docker build -f api/Dockerfile -t soglia-api ..
FROM golang:1.26-alpine AS go-builder
WORKDIR /src

COPY api/go.mod api/go.sum ./
RUN go mod download

# Commands and internal packages; migrations are embedded from
# internal/store/migrations at compile time.
COPY api/cmd ./cmd/
COPY api/internal ./internal/
RUN CGO_ENABLED=0 go build -o /out/profanity-api ./cmd/api

FROM python:3.14-slim
ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PYTHONPATH=/app
WORKDIR /app

RUN pip install --no-cache-dir \
    numpy==2.5.4 \
    onnxruntime==1.32.0 \
    tokenizers==0.23.3

COPY --from=go-builder /out/profanity-api /app/profanity-api
COPY api/worker.py /app/worker.py
COPY api/ui/dist /app/ui/dist
COPY v6 /app/v6

EXPOSE 8080
CMD ["/app/profanity-api", "-python", "python", "-worker", "/app/worker.py", "-model", "/app/v6"]
