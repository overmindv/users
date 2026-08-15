FROM golang:1.25-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/users ./cmd/users && \
    GOBIN=/out go install github.com/pressly/goose/v3/cmd/goose@v3.24.3

FROM alpine:3.22
# Группа "users" уже есть в базовом alpine (gid 100), поэтому создаём только пользователя.
RUN adduser -S -G users users
WORKDIR /app
COPY --from=build /src/migrations /app/migrations
USER users
COPY --from=build /out/users /usr/local/bin/users
COPY --from=build /out/goose /usr/local/bin/goose
EXPOSE 8080
ENTRYPOINT ["users"]
