FROM golang:1.26-alpine AS build

ARG GOPROXY
ENV GOPROXY=${GOPROXY:-https://proxy.golang.org,direct}
ARG GO_BUILD_PARALLEL=2
ENV GOFLAGS=-p=${GO_BUILD_PARALLEL}

WORKDIR /src
# parker подтягивается по тегу (v0.1.0) из модульного прокси — см. go.mod / go.sum.
COPY go.mod go.sum ./
RUN --mount=type=cache,id=overmindv-gomod,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,id=overmindv-gomod,target=/go/pkg/mod \
    --mount=type=cache,id=overmindv-gobuild,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/users ./cmd/users

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
