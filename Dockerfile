FROM golang:1.27.3-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go run ./cmd/frontend
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/wiki_x ./cmd/wiki_x

FROM alpine:3.22

RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -u 10001 wiki_x \
    && mkdir -p /data/media /data/audit \
    && chown -R wiki_x:wiki_x /data

COPY --from=build /out/wiki_x /usr/local/bin/wiki_x

USER wiki_x
WORKDIR /data
EXPOSE 8080
ENTRYPOINT ["wiki_x"]
CMD ["serve", "-config", "/data/wiki_x.yaml"]
