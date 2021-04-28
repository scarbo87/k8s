FROM golang:alpine AS builder
WORKDIR $GOPATH/src/k8s/
RUN apk update && apk add --no-cache make
COPY . .
RUN make dep && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /go/bin/k8s

FROM scratch
ENTRYPOINT ["/app/k8s"]
EXPOSE 8000 8001
COPY --from=builder /go/bin/k8s /app/k8s