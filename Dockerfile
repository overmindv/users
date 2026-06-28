FROM golang:1.25-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/arcee ./cmd/arcee && \
    GOBIN=/out go install github.com/pressly/goose/v3/cmd/goose@v3.24.3

FROM alpine:3.22
RUN addgroup -S arcee && adduser -S -G arcee arcee
WORKDIR /app
COPY --from=build /src/migrations /app/migrations
USER arcee
COPY --from=build /out/arcee /usr/local/bin/arcee
COPY --from=build /out/goose /usr/local/bin/goose
EXPOSE 8080
ENTRYPOINT ["arcee"]
