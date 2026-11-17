FROM golang:1.27 AS build

WORKDIR /src

COPY go.mod ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/redis-server ./cmd/server \
    && mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/redis-server /usr/local/bin/redis-server
COPY --from=build --chown=nonroot:nonroot /out/data /data

USER nonroot:nonroot
WORKDIR /data
VOLUME ["/data"]
EXPOSE 6379

ENTRYPOINT ["/usr/local/bin/redis-server"]
CMD ["-port", "6379", "-aof", "/data/appendonly.aof", "-appendfsync", "everysec"]
