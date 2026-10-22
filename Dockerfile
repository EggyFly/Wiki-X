FROM golang:1.27.3-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go run ./cmd/frontend
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/wikix ./cmd/wikix

FROM alpine:3.22

RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -u 10001 wikix \
    && mkdir -p /data/media /data/audit \
    && chown -R wikix:wikix /data

COPY --from=build /out/wikix /usr/local/bin/wikix

USER wikix
WORKDIR /data
EXPOSE 8080
ENTRYPOINT ["wikix"]
CMD ["serve", "-config", "/data/wikix.yaml"]
