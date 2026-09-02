FROM golang:1.23-alpine AS builder
WORKDIR /src
RUN apk add --no-cache git
COPY go.work package.json pnpm-lock.yaml pnpm-workspace.yaml turbo.json ./
COPY server ./server
COPY packages ./packages
COPY web ./web
COPY scripts ./scripts
RUN corepack enable && corepack prepare pnpm@10.12.1 --activate
RUN pnpm install --frozen-lockfile --ignore-scripts
RUN pnpm turbo run build --filter=web --filter=@repo/docs \
    && chmod +x scripts/sync-static.sh && ./scripts/sync-static.sh
WORKDIR /src/server
RUN go mod download && CGO_ENABLED=0 go build -o /server ./cmd/server

FROM alpine:3.21
RUN apk add --no-cache ca-certificates ffmpeg
WORKDIR /app
COPY --from=builder /server /app/server
ENV SERVER_ADDR=127.0.0.1:8090
ENV DATABASE_PATH=/data/app.db
EXPOSE 8090
VOLUME ["/data", "/music"]
ENTRYPOINT ["/app/server"]
