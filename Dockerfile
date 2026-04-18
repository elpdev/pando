FROM golang:1.26.2 AS build

WORKDIR /src

COPY go.mod go.sum ./
COPY vendor ./vendor
COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -mod=vendor -o /out/pando-relay ./cmd/pando-relay

FROM gcr.io/distroless/static-debian12

WORKDIR /

COPY --from=build /out/pando-relay /pando-relay

EXPOSE 80
VOLUME ["/storage"]

ENTRYPOINT ["/pando-relay"]
CMD ["--addr",":80","--store","/storage/relay.db"]
