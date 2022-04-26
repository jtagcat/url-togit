#FROM golang:$ver
FROM golang:1.18 AS builder
WORKDIR /wd

COPY go.mod go.sum ./
RUN go mod download

COPY *.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o app
# https://docs.docker.com/develop/develop-images/multistage-build/#use-multi-stage-builds

FROM alpine
LABEL org.opencontainers.image.source="https://github.com/jtagcat/url-togit"
WORKDIR /wd
RUN apk add --no-cache git
RUN git config --global --add safe.directory '*'

COPY --from=builder /wd/app ./
CMD ["./app"]
