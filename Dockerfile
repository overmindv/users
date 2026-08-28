FROM golang:1.25-alpine AS build

ARG GOPROXY
ENV GOPROXY=${GOPROXY:-https://proxy.golang.org,direct}

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/users ./cmd/users
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/users-worker ./cmd/users-worker
RUN GOBIN=/out go install -tags="no_clickhouse no_mssql no_mysql no_sqlite3 no_libsql no_ydb no_vertica" github.com/pressly/goose/v3/cmd/goose@v3.24.3

FROM alpine:3.22 AS api
# Группа "users" уже есть в базовом alpine (gid 100), поэтому создаём только пользователя.
RUN adduser -S -G users users
WORKDIR /app
COPY --from=build /src/migrations /app/migrations
USER users
COPY --from=build /out/users /usr/local/bin/users
COPY --from=build /out/goose /usr/local/bin/goose
EXPOSE 8080
ENTRYPOINT ["users"]

FROM alpine:3.22 AS worker
RUN apk add --no-cache ca-certificates wget && adduser -S -G users users
USER users
COPY --from=build /out/users-worker /usr/local/bin/users-worker
EXPOSE 8081
ENTRYPOINT ["users-worker"]

FROM api AS runtime
