FROM golang:1.18-alpine3.17 AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . ./
ARG GOARCH
RUN apk add --update --no-cache --virtual .build-deps build-base && \
    GOARCH=${GOARCH} go build -o /server -ldflags "-X release.Version=v0.`cat VERSION`" backend/server/server.go && \
    apk del .build-deps

FROM alpine:3.17
COPY --from=builder /server /server
CMD ["/server"]  
