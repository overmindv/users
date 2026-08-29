FROM golang:1.26-alpine AS build

ARG GOPROXY
ENV GOPROXY=${GOPROXY:-https://proxy.golang.org,direct}

WORKDIR /src
# parker подтягивается по тегу (v0.1.0) из модульного прокси — см. go.mod / go.sum.
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/users ./cmd/users

FROM alpine:3.22 AS api
# Группа "users" уже есть в базовом alpine (gid 100), поэтому создаём только пользователя.
RUN adduser -S -G users users
WORKDIR /app
COPY --from=build /src/migrations /app/migrations
USER users
COPY --from=build /out/users /usr/local/bin/users
EXPOSE 8080
ENTRYPOINT ["users"]

FROM api AS runtime
